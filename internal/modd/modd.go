// Package modd is the daemon's side of running providers: only those listed in
// daemon.toml and approved as they are now, each in its own host, started when
// first asked for. It touches no GTK.
package modd

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"gitlab.com/KopyTKG/spot/internal/approval"
	"gitlab.com/KopyTKG/spot/internal/config"
	"gitlab.com/KopyTKG/spot/internal/mod"
	"gitlab.com/KopyTKG/spot/internal/modhost"
	"gitlab.com/KopyTKG/spot/internal/modhost/channel"
	"gitlab.com/KopyTKG/spot/internal/modload"
	"gitlab.com/KopyTKG/spot/internal/modperm"
)

// A host that keeps dying is restarted with growing pauses. Never disabled for
// good: data from outside must not be able to switch a mod off.
var pauses = []time.Duration{2 * time.Second, 10 * time.Second, time.Minute, 5 * time.Minute}

type cached struct {
	result json.RawMessage
	at     time.Time
}

type fileID struct{ dev, ino uint64 }

type provider struct {
	mu       sync.Mutex // one thing at a time
	h        *modhost.Host
	hash     string
	settings string
	files    map[string]fileID // granted by name: replaced by an update, the grant is gone
	values   []string
	requests []string
	last     map[string]cached // the last good answer of each value

	failures  int
	notBefore time.Time
	lastErr   string
}

// Manager runs the providers.
type Manager struct {
	store approval.Store
	host  string // spot-mod-host
	empty string // where programs start

	mu     sync.Mutex
	mods   map[string]*provider
	closed bool
}

// New is a manager for the user's mods. host is the mod host's binary and
// runtime a folder only the user can read.
func New(store approval.Store, host, runtime string) (*Manager, error) {
	empty := filepath.Join(runtime, "spot-mods-empty")
	if err := os.MkdirAll(empty, 0o700); err != nil {
		return nil, err
	}
	return &Manager{store: store, host: host, empty: empty, mods: map[string]*provider{}}, nil
}

// Close stops every host.
func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	all := make([]*provider, 0, len(m.mods))
	for _, p := range m.mods {
		all = append(all, p)
	}
	m.mu.Unlock()
	for _, p := range all {
		p.mu.Lock()
		p.stop()
		p.mu.Unlock()
	}
}

func (p *provider) stop() {
	if p.h != nil {
		p.h.Stop()
		p.h = nil
	}
}

func (m *Manager) provider(name string) (*provider, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, errors.New("the daemon is leaving")
	}
	p := m.mods[name]
	if p == nil {
		p = &provider{last: map[string]cached{}}
		m.mods[name] = p
	}
	return p, nil
}

// Split takes "mod.item" apart.
func Split(full string) (name, item string, err error) {
	name, item, found := strings.Cut(full, ".")
	if !found || !mod.ValidName(name) || item == "" {
		return "", "", fmt.Errorf("%q: say <mod>.<name>", full)
	}
	return name, item, nil
}

func identify(path string) (fileID, bool) {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return fileID{}, false
	}
	s, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return fileID{}, false
	}
	return fileID{uint64(s.Dev), s.Ino}, true
}

func (p *provider) replaced() bool {
	for path, id := range p.files {
		if now, ok := identify(path); !ok || now != id {
			return true
		}
	}
	return false
}

// failed stops the host and sets when it may be started again.
func (p *provider) failed(err error) error {
	p.stop()
	pause := pauses[min(p.failures, len(pauses)-1)]
	p.failures++
	p.notBefore, p.lastErr = time.Now().Add(pause), err.Error()
	return err
}

// ready makes sure that name is running as what it is now: listed, approved,
// unchanged, with its settings and grants current. Called with p.mu held.
func (m *Manager) ready(name string, p *provider) error {
	conf, err := config.ModsConfig(m.store.Config)
	if err != nil {
		return err
	}
	if !slices.Contains(conf.Enabled, name) {
		p.stop()
		clear(p.last)
		return fmt.Errorf("%s is not enabled: list it in mods in %s/daemon.toml", name, m.store.Config)
	}
	store := m.store
	store.Mods = conf.Dir
	state, approved, _, err := store.State(name)
	if err != nil {
		p.stop()
		clear(p.last)
		return err
	}
	if state != approval.Approved {
		// Revoked or changed: it stops, and what it said is thrown away (S16).
		p.stop()
		clear(p.last)
		why := "is not approved"
		if state == approval.Changed {
			why = "has changed since you approved it"
		}
		return fmt.Errorf("%s %s and will not run: spot mods allow %s", name, why, name)
	}
	var settings []byte
	if s := conf.Settings[name]; s != nil {
		if settings, err = json.Marshal(s); err != nil {
			return fmt.Errorf("[mod.%s]: %w", name, err)
		}
	}
	if p.h != nil && (p.hash != approved.Hash || p.settings != string(settings) || p.replaced()) {
		if p.hash != approved.Hash {
			clear(p.last)
		}
		p.stop()
	}
	if p.h != nil {
		return nil
	}
	if p.hash != "" && p.hash != approved.Hash {
		clear(p.last)
		p.failures, p.notBefore = 0, time.Time{} // a new version gets a fresh start
	}
	p.hash, p.settings = approved.Hash, string(settings)
	if wait := time.Until(p.notBefore); wait > 0 {
		return fmt.Errorf("%s failed and is tried again in %s: %s", name, wait.Round(time.Second), p.lastErr)
	}
	c, load, err := modload.Approved(approved, modload.Options{
		Host: m.host, Env: m.store.Getenv, Config: settings, EmptyDir: m.empty,
		Protected: modload.Protected(modperm.Vars(m.store.Getenv)),
	})
	if err != nil {
		return p.failed(err)
	}
	c.Stderr = &complaints{mod: name, left: 3}
	h, err := modhost.Start(c)
	if err != nil {
		return p.failed(err)
	}
	p.h = h
	if !h.Limited {
		log.Printf("mod %s: no systemd user session: no memory or task limit", name)
	}
	// Which file each name granted means now.
	p.files = map[string]fileID{}
	rules, err := c.Policy.Rules()
	if err != nil {
		return p.failed(err)
	}
	for _, r := range rules {
		if id, ok := identify(r.Path); ok {
			p.files[r.Path] = id
		}
	}
	reply, err := h.Load(load)
	m.record(name, reply)
	if err != nil {
		return p.failed(err)
	}
	p.values, p.requests = reply.Values, reply.Requests
	return nil
}

// ask runs one thing on a mod that is ready.
func (m *Manager) ask(name string, p *provider, do func(h *modhost.Host) (channel.Reply, error)) (json.RawMessage, error) {
	reply, err := do(p.h)
	m.record(name, reply)
	var fault *modhost.ModError
	switch {
	case err == nil:
		p.failures, p.notBefore = 0, time.Time{}
		return reply.Result, nil
	case errors.As(err, &fault):
		return nil, err // the mod's mistake; its host is fine
	}
	return nil, p.failed(fmt.Errorf("%s: %w", name, err))
}

// Get is a value. When it cannot be had now but was before, the last good one
// comes back with warning saying why it is old.
func (m *Manager) Get(full string) (result json.RawMessage, warning string, err error) {
	name, item, err := Split(full)
	if err != nil {
		return nil, "", err
	}
	p, err := m.provider(name)
	if err != nil {
		return nil, "", err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	err = m.ready(name, p)
	if err == nil {
		result, err = m.ask(name, p, func(h *modhost.Host) (channel.Reply, error) { return h.Get(item) })
	}
	if err == nil {
		p.last[item] = cached{result, time.Now()}
		return result, "", nil
	}
	if old, ok := p.last[item]; ok { // emptied when revoked or changed
		return old.result, fmt.Sprintf("this is from %s ago: %v", time.Since(old.at).Round(time.Second), err), nil
	}
	return nil, "", err
}

// Call runs a request.
func (m *Manager) Call(full string, args []string) (json.RawMessage, error) {
	name, item, err := Split(full)
	if err != nil {
		return nil, err
	}
	p, err := m.provider(name)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := m.ready(name, p); err != nil {
		return nil, err
	}
	return m.ask(name, p, func(h *modhost.Host) (channel.Reply, error) { return h.Call(item, args) })
}

const maxAudit = 1 << 20

// AuditPath is where what a mod did is written.
func AuditPath(store approval.Store, name string) string {
	return filepath.Join(store.Dir, "audit", name+".log")
}

// record keeps what a mod said and did: its lines to the daemon's log, its
// events to the audit log, which no mod can write.
func (m *Manager) record(name string, r channel.Reply) {
	for _, l := range r.Log {
		log.Printf("mod %s says %s", name, strconv.Quote(l))
	}
	if len(r.Events) == 0 {
		return
	}
	path := AuditPath(m.store, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		log.Printf("mod %s: audit: %v", name, err)
		return
	}
	if st, err := os.Lstat(path); err == nil && st.Size() > maxAudit {
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		log.Printf("mod %s: audit: %v", name, err)
		return
	}
	defer f.Close()
	now := time.Now().Format("2006-01-02 15:04:05")
	var b strings.Builder
	for _, e := range r.Events {
		// Quoted: what a mod names cannot make a line of its own.
		fmt.Fprintf(&b, "%s %-8s %s\n", now, e.Kind, strconv.Quote(e.What))
	}
	if _, err := f.WriteString(b.String()); err != nil {
		log.Printf("mod %s: audit: %v", name, err)
	}
}

// complaints passes on the first lines a host writes to its error output. A
// host that runs out of memory prints a long report, and may be started again
// and again: the daemon's log keeps the lines that say what happened.
type complaints struct {
	mod  string
	left int
	line []byte
}

func (c *complaints) Write(p []byte) (int, error) {
	for _, b := range p {
		if c.left == 0 {
			break
		}
		if b != '\n' {
			if len(c.line) < 300 {
				c.line = append(c.line, b)
			}
			continue
		}
		if len(c.line) > 0 {
			log.Printf("mod %s: host: %s", c.mod, strconv.Quote(string(c.line)))
			c.left--
		}
		c.line = c.line[:0]
	}
	return len(p), nil
}
