// Package modd is the daemon's side of running providers: only those listed in
// daemon.toml and approved as they are now, each in its own host, started when
// first asked for. It keeps watched values current, runs timers, and tells the
// user when a mod breaks. It touches no GTK.
package modd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
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
	"gitlab.com/KopyTKG/spot/internal/notify"
	"gitlab.com/KopyTKG/spot/internal/sandbox"
)

// A host that keeps dying is restarted with growing pauses. Never disabled for
// good: data from outside must not be able to switch a mod off.
var pauses = []time.Duration{2 * time.Second, 10 * time.Second, time.Minute, 5 * time.Minute}

// checkEvery is how soon a change or a revocation stops a running mod, and how
// often a watcher of a mod that is not running is looked after.
var checkEvery = 2 * time.Second

const (
	maxWatchers     = 64
	maxNotesPerHour = 6
	maxAudit        = 1 << 20
)

type cached struct {
	result json.RawMessage
	at     time.Time
}

type fileID struct{ dev, ino uint64 }

// Update is what a watcher is told: the value, with Warning when it is an old
// one, or Err when there is none.
type Update struct {
	Result  json.RawMessage
	Warning string
	Err     string
}

func (u Update) same(v Update) bool {
	// An old value's warning carries its age, which changes all the time.
	return bytes.Equal(u.Result, v.Result) && u.Err == v.Err && (u.Warning == "") == (v.Warning == "")
}

// Watcher receives a value each time it changes. A reader that is slow gets the
// newest, not every one. C is closed when the daemon leaves.
type Watcher struct {
	C    chan Update
	p    *provider
	item string
	last Update
	sent bool
}

type provider struct {
	name string
	mu   sync.Mutex // one thing at a time

	h        *modhost.Host
	hash     string
	settings string
	files    map[string]fileID // granted by name: replaced by an update, the grant is gone
	values   []string
	requests []string
	last     map[string]cached // the last good answer of each value

	every    map[string]time.Duration
	tick     time.Time            // when the mod's next timer is due; zero: none
	due      map[string]time.Time // watched values to compute
	watchers map[*Watcher]bool
	looping  bool
	wake     chan struct{}

	failures  int
	notBefore time.Time
	lastErr   string

	broken map[string]string // what is failing now: "" is the mod itself
	since  time.Time
	times  int    // failures since it last worked
	fault  string // whose: the parser, the mod, its host, or a change
	free   bool   // it runs without memory and task limits
	told   bool
	notes  []time.Time
}

// Manager runs the providers.
type Manager struct {
	store approval.Store
	host  string // spot-mod-host
	empty string // where programs start

	// Notify tells the user. A field so that tests can listen.
	Notify func(title, body string)

	mu     sync.Mutex
	mods   map[string]*provider
	closed bool
	done   chan struct{}
	loops  sync.WaitGroup
}

// New is a manager for the user's mods. host is the mod host's binary and
// runtime a folder only the user can read.
func New(store approval.Store, host, runtime string) (*Manager, error) {
	empty := filepath.Join(runtime, "spot-mods-empty")
	if err := os.MkdirAll(empty, 0o700); err != nil {
		return nil, err
	}
	m := &Manager{store: store, host: host, empty: empty, mods: map[string]*provider{}, done: make(chan struct{})}
	m.Notify = func(title, body string) {
		conf, err := config.ModsConfig(store.Config)
		if err != nil {
			conf.Notify = nil
		}
		go notify.Send(conf.Notify, title, body)
	}
	return m, nil
}

// Close stops every host and ends every watch.
func (m *Manager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	close(m.done)
	all := m.all()
	m.mu.Unlock()
	m.loops.Wait()
	for _, p := range all {
		p.mu.Lock()
		p.stop()
		for w := range p.watchers {
			close(w.C)
		}
		clear(p.watchers)
		p.mu.Unlock()
	}
}

// all is the providers; called with m.mu held.
func (m *Manager) all() []*provider {
	all := make([]*provider, 0, len(m.mods))
	for _, p := range m.mods {
		all = append(all, p)
	}
	return all
}

func (p *provider) stop() {
	if p.h != nil {
		p.h.Stop()
		p.h = nil
	}
	p.tick = time.Time{}
}

func (m *Manager) provider(name string) (*provider, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, errors.New("the daemon is leaving")
	}
	p := m.mods[name]
	if p == nil {
		p = &provider{name: name, last: map[string]cached{}, every: map[string]time.Duration{}, due: map[string]time.Time{},
			watchers: map[*Watcher]bool{}, broken: map[string]string{}, wake: make(chan struct{}, 1)}
		m.mods[name] = p
	}
	return p, nil
}

// Split takes "mod.item" apart.
func Split(full string) (name, item string, err error) {
	name, item, found := strings.Cut(full, ".")
	if !found || !mod.ValidName(name) || item == "" || len(item) > 64 {
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

// --- health: told once when a mod breaks, and once when it works again.

func (m *Manager) note(p *provider, title, body string) {
	now := time.Now()
	p.notes = slices.DeleteFunc(p.notes, func(t time.Time) bool { return now.Sub(t) > time.Hour })
	if len(p.notes) >= maxNotesPerHour {
		return // failing and recovering in turn must not flood
	}
	p.notes = append(p.notes, now)
	m.Notify(title, approval.Oneline(body))
}

// health records that what (a value, a request, "" the mod itself) works or not.
func (m *Manager) health(p *provider, what string, err error) {
	if err == nil {
		if len(p.broken) > 0 && (p.broken[what] != "" || p.broken[""] != "") && len(p.broken) <= 2 {
			keep(LogPath(m.store, p.name), []string{"works again"})
		}
		delete(p.broken, what)
		delete(p.broken, "")
		if p.told && len(p.broken) == 0 {
			p.told = false
			m.note(p, "spot: "+p.name+" works again", p.name+" is answering")
		}
		return
	}
	if len(p.broken) == 0 {
		p.since, p.times = time.Now(), 0
	}
	p.times++
	p.fault = blame(err.Error())
	if p.broken[what] != err.Error() { // each new way of failing, once
		keep(LogPath(m.store, p.name), []string{"error " + strconv.Quote(err.Error()) + " (fault of " + p.fault + ")"})
	}
	p.broken[what] = err.Error()
	if !p.told {
		p.told = true
		m.note(p, "spot: mod "+p.name+" is failing", err.Error())
	}
}

var parserBlamed = regexp.MustCompile(`parser ([a-z][a-z0-9_-]*):`)

// blame says whose fault an error is, so that the right thing gets fixed.
func blame(msg string) string {
	switch {
	case strings.Contains(msg, "has changed since"):
		return "changed"
	case strings.Contains(msg, "host is gone") || strings.Contains(msg, "starting the host"):
		return "its host"
	}
	if m := parserBlamed.FindStringSubmatch(msg); m != nil {
		return "parser " + m[1]
	}
	return "the mod"
}

// forget drops what was known of a mod that you switched off yourself.
func (p *provider) forget() {
	p.stop()
	clear(p.last)
	clear(p.broken)
	p.told = false
}

// failed stops the host and sets when it may be started again.
func (m *Manager) failed(p *provider, err error) error {
	p.stop()
	pause := pauses[min(p.failures, len(pauses)-1)]
	p.failures++
	p.notBefore, p.lastErr = time.Now().Add(pause), err.Error()
	m.health(p, "", err)
	return err
}

// memorySetting is `memory` of a [mod.<name>]: megabytes for the mod's host
// and everything it starts. 0 is the usual 256.
func memorySetting(settings map[string]any) (int64, error) {
	v, ok := settings["memory"]
	if !ok {
		return 0, nil
	}
	mb, ok := v.(int64)
	if !ok || mb < 64 || mb > 16384 {
		return 0, errors.New("memory is a whole number of megabytes, from 64 to 16384")
	}
	return mb << 20, nil
}

// ready makes sure that name is running as what it is now: listed, approved,
// unchanged, with its settings and grants current. Called with p.mu held.
func (m *Manager) ready(p *provider) error {
	name := p.name
	// What says which mods run, what runs them and where their programs are
	// found must be yours or root's (S17).
	for _, path := range []string{filepath.Join(m.store.Config, "mods.toml"), filepath.Join(m.store.Config, "daemon.toml"), m.host, sandbox.SystemPath} {
		if err := approval.Trusted(path); err != nil {
			p.stop()
			return fmt.Errorf("mods do not run: %w", err)
		}
	}
	conf, err := config.ModsConfig(m.store.Config)
	if err != nil {
		return err
	}
	if !slices.Contains(conf.Enabled, name) {
		p.forget()
		return fmt.Errorf("%s is not enabled: list it in mods in %s/mods.toml", name, m.store.Config)
	}
	store := m.store
	store.Mods = conf.Dir
	state, approved, record, err := store.State(name)
	if err != nil {
		p.stop()
		clear(p.last)
		m.health(p, "", err)
		return err
	}
	switch state {
	case approval.NotApproved:
		p.forget() // never approved, or revoked: your doing, nothing to tell
		return fmt.Errorf("%s is not approved and will not run: spot mods allow %s", name, name)
	case approval.Changed:
		// It stops, and what it said is thrown away (S16).
		p.stop()
		clear(p.last)
		err := fmt.Errorf("%s has changed since you approved it and will not run (%s): spot mods allow %s", name, approval.Asks(approved, record), name)
		m.health(p, "", err)
		return err
	}
	var settings []byte
	memory, err := memorySetting(conf.Settings[name])
	if err != nil {
		return fmt.Errorf("[mod.%s]: %w", name, err)
	}
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
		return m.failed(p, err)
	}
	c.Stderr = &complaints{mod: name, left: 3}
	c.Memory = memory
	h, err := modhost.Start(c)
	if err != nil {
		return m.failed(p, err)
	}
	p.h, p.free = h, !h.Limited
	if !h.Limited {
		log.Printf("mod %s: no systemd user session: no memory or task limit", name)
	}
	// Which file each name granted means now.
	p.files = map[string]fileID{}
	rules, err := c.Policy.Rules()
	if err != nil {
		return m.failed(p, err)
	}
	for _, r := range rules {
		if id, ok := identify(r.Path); ok {
			p.files[r.Path] = id
		}
	}
	reply, err := h.Load(load)
	m.record(name, reply)
	if err != nil {
		return m.failed(p, err)
	}
	p.values, p.requests = reply.Values, reply.Requests
	p.every = map[string]time.Duration{}
	for v, ms := range reply.Every {
		p.every[v] = time.Duration(ms) * time.Millisecond
	}
	m.follow(p, reply)
	for w := range p.watchers { // a new host: ask anew
		p.due[w.item] = time.Now()
	}
	// While it runs, a change or a revocation must be seen.
	m.loop(p)
	return nil
}

// follow takes from a reply what to come back for.
func (m *Manager) follow(p *provider, r channel.Reply) {
	if !r.Ready {
		return
	}
	p.tick = time.Time{}
	if r.Next > 0 {
		p.tick = time.Now().Add(time.Duration(r.Next) * time.Millisecond)
	}
	for _, item := range r.Changed {
		for w := range p.watchers {
			if w.item == item {
				p.due[item] = time.Now()
			}
		}
	}
	if len(r.Changed) > 0 || r.Next > 0 {
		poke(p)
	}
}

func poke(p *provider) {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

// ask runs one thing on a mod that is ready. what names it for health.
func (m *Manager) ask(p *provider, what string, do func(h *modhost.Host) (channel.Reply, error)) (json.RawMessage, error) {
	reply, err := do(p.h)
	m.record(p.name, reply)
	var fault *modhost.ModError
	switch {
	case err == nil:
		p.failures, p.notBefore = 0, time.Time{}
		m.follow(p, reply)
		m.health(p, what, nil)
		return reply.Result, nil
	case errors.As(err, &fault): // the mod's mistake; its host is fine
		m.follow(p, reply)
		// Asking for something it does not have is the caller's mistake.
		if !strings.Contains(fault.Message, ": there is no ") {
			m.health(p, what, err)
		}
		return nil, err
	}
	return nil, m.failed(p, fmt.Errorf("%s: %w", p.name, err))
}

// value computes one value, falling back to the last good one. p.mu is held.
func (m *Manager) value(p *provider, item string) (json.RawMessage, string, error) {
	err := m.ready(p)
	var result json.RawMessage
	if err == nil {
		result, err = m.ask(p, item, func(h *modhost.Host) (channel.Reply, error) { return h.Get(item) })
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
	return m.value(p, item)
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
	if err := m.ready(p); err != nil {
		return nil, err
	}
	return m.ask(p, "request "+item, func(h *modhost.Host) (channel.Reply, error) { return h.Call(item, args) })
}

// Watch is a value now and on every change, until Unwatch.
func (m *Manager) Watch(full string) (*Watcher, error) {
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
	if len(p.watchers) >= maxWatchers {
		return nil, fmt.Errorf("more than %d are watching %s", maxWatchers, name)
	}
	w := &Watcher{C: make(chan Update, 1), p: p, item: item}
	p.watchers[w] = true
	p.due[item] = time.Now()
	m.loop(p)
	poke(p)
	return w, nil
}

// Unwatch ends a watch.
func (m *Manager) Unwatch(w *Watcher) {
	w.p.mu.Lock()
	defer w.p.mu.Unlock()
	delete(w.p.watchers, w)
	poke(w.p)
}

func (w *Watcher) offer(u Update) {
	if w.sent && w.last.same(u) {
		return
	}
	w.last, w.sent = u, true
	select {
	case <-w.C: // not read yet: replaced
	default:
	}
	w.C <- u
}

// loop looks after a mod that runs or is watched. p.mu is held.
func (m *Manager) loop(p *provider) {
	if p.looping {
		return
	}
	m.mu.Lock() // so that Close does not wait before we are counted
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.loops.Add(1)
	m.mu.Unlock()
	p.looping = true
	go func() {
		defer m.loops.Done()
		for {
			p.mu.Lock()
			next := time.Now().Add(checkEvery)
			sooner := func(t time.Time) {
				if !t.IsZero() && t.Before(next) {
					next = t
				}
			}
			sooner(p.tick)
			for _, t := range p.due {
				sooner(t)
			}
			p.mu.Unlock()

			select {
			case <-m.done:
				return
			case <-p.wake:
			case <-time.After(max(time.Until(next), 0)):
			}

			p.mu.Lock()
			if !m.step(p) {
				p.looping = false
				p.mu.Unlock()
				return
			}
			p.mu.Unlock()
		}
	}()
}

// step does what is due. It reports whether there is still a reason to loop.
func (m *Manager) step(p *provider) bool {
	watched := map[string]bool{}
	for w := range p.watchers {
		watched[w.item] = true
	}
	for item := range p.due {
		if !watched[item] {
			delete(p.due, item)
		}
	}
	if len(watched) == 0 && p.h == nil {
		return false // nothing runs and nobody waits for it
	}
	// A change, a revocation or a replaced file takes effect without anyone
	// asking. This also starts a mod that is watched and not running.
	if err := m.ready(p); err != nil {
		for w := range p.watchers {
			u := Update{Err: err.Error()}
			if old, ok := p.last[w.item]; ok {
				u = Update{Result: old.result, Warning: err.Error()}
			}
			w.offer(u)
		}
		return len(watched) > 0
	}
	now := time.Now()
	if !p.tick.IsZero() && !p.tick.After(now) {
		p.tick = time.Time{}
		m.ask(p, "timer", func(h *modhost.Host) (channel.Reply, error) { return h.Tick() })
	}
	for item, when := range p.due {
		if when.After(time.Now()) || p.h == nil {
			continue
		}
		delete(p.due, item)
		result, warning, err := m.value(p, item)
		u := Update{Result: result, Warning: warning}
		if err != nil {
			u = Update{Err: err.Error()}
		}
		for w := range p.watchers {
			if w.item == item {
				w.offer(u)
			}
		}
		if every := p.every[item]; every > 0 {
			p.due[item] = time.Now().Add(every)
		}
	}
	return len(watched) > 0 || p.h != nil
}

// Reload stops one mod, or every one, so that it starts anew; what was
// switched off is seen at once.
func (m *Manager) Reload(name string) {
	m.mu.Lock()
	all := m.all()
	m.mu.Unlock()
	for _, p := range all {
		if name != "" && p.name != name {
			continue
		}
		p.mu.Lock()
		p.stop()
		p.failures, p.notBefore = 0, time.Time{}
		for w := range p.watchers {
			p.due[w.item] = time.Now()
		}
		poke(p)
		p.mu.Unlock()
	}
}

// Status is how one mod is doing.
type Status struct {
	Name      string `json:"name"`
	Running   bool   `json:"running"`
	Unlimited bool   `json:"unlimited,omitempty"` // no memory or task limit
	Watchers  int    `json:"watchers,omitempty"`
	Error     string `json:"error,omitempty"`
	Since     string `json:"since,omitempty"`
	Count     int    `json:"count,omitempty"`
	Fault     string `json:"fault,omitempty"`
}

// Statuses is every mod asked for since the daemon started.
func (m *Manager) Statuses() []Status {
	m.mu.Lock()
	all := m.all()
	m.mu.Unlock()
	var out []Status
	for _, p := range all {
		p.mu.Lock()
		s := Status{Name: p.name, Running: p.h != nil, Unlimited: p.h != nil && p.free, Watchers: len(p.watchers)}
		what := make([]string, 0, len(p.broken))
		for k := range p.broken {
			what = append(what, k)
		}
		sort.Strings(what)
		if len(what) > 0 {
			s.Error, s.Since, s.Count, s.Fault = p.broken[what[0]], p.since.Format("15:04:05"), p.times, p.fault
		}
		p.mu.Unlock()
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// AuditPath is where what a mod did is written.
func AuditPath(store approval.Store, name string) string {
	return filepath.Join(store.Dir, "audit", name+".log")
}

// LogPath is where what a mod said, and how it failed, is written.
func LogPath(store approval.Store, name string) string {
	return filepath.Join(store.Dir, "logs", name+".log")
}

// keep appends lines to a file of the user's own, which no mod can write. One
// older file is kept when it grows large.
func keep(path string, lines []string) {
	if len(lines) == 0 {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		log.Printf("%s: %v", path, err)
		return
	}
	if st, err := os.Lstat(path); err == nil && st.Size() > maxAudit {
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		log.Printf("%s: %v", path, err)
		return
	}
	defer f.Close()
	now := time.Now().Format("2006-01-02 15:04:05")
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(now + " " + l + "\n")
	}
	if _, err := f.WriteString(b.String()); err != nil {
		log.Printf("%s: %v", path, err)
	}
}

// record keeps what a mod said, in its log, and what it did, in the audit log.
// Everything of the mod's is quoted: it cannot make a line of its own.
func (m *Manager) record(name string, r channel.Reply) {
	var said, did []string
	for _, l := range r.Log {
		said = append(said, "says  "+strconv.Quote(l))
	}
	for _, e := range r.Events {
		did = append(did, fmt.Sprintf("%-8s %s", e.Kind, strconv.Quote(e.What)))
	}
	keep(LogPath(m.store, name), said)
	keep(AuditPath(m.store, name), did)
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

// parserTime is how long one parse may take inside Lua.
const parserTime = 5 * time.Second

// Parse runs an approved parser on input, in a host of its own that lives for
// this one call and is granted nothing. A parser is approved by itself, or as
// part of a provider that is.
func (m *Manager) Parse(name string, input []byte) (json.RawMessage, error) {
	if !mod.ValidName(name) {
		return nil, fmt.Errorf("%q is not a parser's name", name)
	}
	if len(input) > 8<<20 {
		return nil, errors.New("more than 8 MB to parse")
	}
	conf, err := config.ModsConfig(m.store.Config)
	if err != nil {
		return nil, err
	}
	store := m.store
	store.Mods = conf.Dir
	for _, path := range []string{m.host, sandbox.SystemPath} {
		if err := approval.Trusted(path); err != nil {
			return nil, fmt.Errorf("mods do not run: %w", err)
		}
	}
	set, err := approvedParser(store, name)
	if err != nil {
		return nil, err
	}
	h, err := modhost.Start(modhost.Config{Name: name, Policy: sandbox.Policy{Self: m.host}, Stderr: &complaints{mod: name, left: 3}})
	if err != nil {
		return nil, err
	}
	defer h.Stop()
	result, lines, err := h.Parse(channel.Parse{Name: name, Mods: set, Input: input}, parserTime)
	m.record(name, channel.Reply{Log: lines})
	return result, err
}

// approvedParser is the parser name with those it uses, from the bytes that
// were approved.
func approvedParser(store approval.Store, name string) (map[string]mod.Mod, error) {
	state, a, _, err := store.State(name)
	if err != nil {
		return nil, err
	}
	if a.Provider.Manifest.Type != "parser" {
		return nil, fmt.Errorf("%s is not a parser", name)
	}
	with := func(root mod.Mod, used map[string]mod.Mod) map[string]mod.Mod {
		set := map[string]mod.Mod{name: root}
		for n, p := range used {
			set[n] = p
		}
		return set
	}
	if state == approval.Approved {
		return with(a.Provider, a.Parsers), nil
	}
	entries, err := os.ReadDir(store.Mods)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		s, m, _, err := store.State(e.Name())
		if err != nil || s != approval.Approved {
			continue
		}
		if p, uses := m.Parsers[name]; uses {
			return with(p, m.Parsers), nil // the bytes that provider was approved with
		}
	}
	return nil, fmt.Errorf("parser %s is not approved: spot mods allow %s", name, name)
}
