// Package approval is what stands between a mod and its first run: the record
// of what you approved, tied to a hash of every byte of it. No mod is exempt,
// your own included: your files can be changed by someone else.
package approval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"gitlab.com/KopyTKG/spot/internal/config"
	"gitlab.com/KopyTKG/spot/internal/mod"
	"gitlab.com/KopyTKG/spot/internal/modperm"
	"gitlab.com/KopyTKG/spot/internal/scan"
)

const (
	maxProgram = 16 << 20
	maxRecord  = 64 << 20
)

// Store is where mods are and where approvals are kept.
type Store struct {
	Mods    string // folder of mods
	Records string // folder of approval records
	Home    string // for the paths in permissions
}

// Default is the store of this user, found through the system's user database
// and not through environment variables: a daemon started on demand inherits
// the environment of whoever called, which could point it somewhere else.
func Default() (Store, error) {
	u, err := user.LookupId(strconv.Itoa(os.Getuid()))
	if err != nil {
		return Store{}, fmt.Errorf("who is running: %w", err)
	}
	if !filepath.IsAbs(u.HomeDir) {
		return Store{}, errors.New("this user has no home folder")
	}
	mods, err := config.ModsDir(filepath.Join(u.HomeDir, ".config", "spot"))
	if err != nil {
		return Store{}, err
	}
	return Store{Mods: mods, Records: filepath.Join(u.HomeDir, ".local", "state", "spot", "approvals"), Home: u.HomeDir}, nil
}

// Mod is a mod as read from disk, once: what is shown, hashed and later loaded
// are these bytes.
type Mod struct {
	Name     string
	Provider mod.Mod            // the mod itself, a parser too
	Files    map[string][]byte  // every .lua file
	Parsers  map[string]mod.Mod // that it uses, directly or through another
	Programs map[string]string  // programs of your own, by path: their digest
	Hash     string
}

// trusted refuses a file or folder others than you or root could change,
// checking every folder above it too (S17). A folder anyone may write is fine
// when only owners can remove things from it, as in /tmp.
func trusted(path string) error {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	for _, start := range []string{filepath.Clean(path), real} {
		for p := start; ; p = filepath.Dir(p) {
			st, err := os.Lstat(p)
			if err != nil {
				return err
			}
			sys, ok := st.Sys().(*syscall.Stat_t)
			if !ok {
				return fmt.Errorf("%s: cannot tell who owns it", p)
			}
			if sys.Uid != 0 && int(sys.Uid) != os.Getuid() {
				return fmt.Errorf("%s belongs to someone else", p)
			}
			if st.Mode()&fs.ModeSymlink == 0 && st.Mode()&0o022 != 0 && st.Mode()&fs.ModeSticky == 0 {
				return fmt.Errorf("%s can be changed by others (mode %o)", p, st.Mode().Perm())
			}
			if p == filepath.Dir(p) {
				break
			}
		}
	}
	return nil
}

func digest(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// Read reads the installed mod name with everything it uses.
func (s Store) Read(name string) (*Mod, error) {
	if !mod.ValidName(name) {
		return nil, fmt.Errorf("%q is not a mod name", name)
	}
	dir := filepath.Join(s.Mods, name)
	read := func(dir string) (mod.Mod, map[string][]byte, error) {
		m, err := mod.Load(dir)
		if err != nil {
			return m, nil, err
		}
		files, err := mod.Files(dir)
		if err != nil {
			return m, nil, fmt.Errorf("mod %s: %w", m.Name, err)
		}
		files["init.lua"] = m.Source
		for _, rel := range append([]string{".", "mod.toml"}, keys(files)...) {
			p := filepath.Join(dir, rel)
			if _, err := os.Lstat(p); errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err := trusted(p); err != nil {
				return m, nil, fmt.Errorf("mod %s: %w", m.Name, err)
			}
		}
		return m, files, nil
	}
	m, files, err := read(dir)
	if err != nil {
		return nil, err
	}
	out := &Mod{Name: name, Provider: m, Files: files, Parsers: map[string]mod.Mod{}, Programs: map[string]string{}}
	var use func(m mod.Mod, chain []string) error
	use = func(m mod.Mod, chain []string) error {
		for _, u := range m.Manifest.Uses {
			if slices.Contains(chain, u) {
				return fmt.Errorf("uses goes in a circle: %s", strings.Join(append(chain, u), " -> "))
			}
			if len(chain) >= mod.MaxDepth {
				return fmt.Errorf("uses is more than %d deep", mod.MaxDepth)
			}
			p, ok := out.Parsers[u]
			if !ok {
				var err error
				if p, _, err = read(filepath.Join(s.Mods, u)); err != nil {
					return fmt.Errorf("%s uses %s: %w", m.Name, u, err)
				}
				if p.Manifest.Type != "parser" {
					return fmt.Errorf("%s uses %s, which is not a parser", m.Name, u)
				}
				out.Parsers[u] = p
			}
			if err := use(p, append(chain, u)); err != nil {
				return err
			}
		}
		return nil
	}
	if err := use(m, []string{name}); err != nil {
		return nil, err
	}
	// Programs of your own are part of what is approved (S10).
	if perms := m.Manifest.Permissions; perms != nil {
		for run := range perms.Run {
			if !strings.ContainsRune(run, '/') {
				continue
			}
			path, err := modperm.Expand(run, modperm.Vars(s.env))
			if err != nil {
				return nil, fmt.Errorf("mod %s: run %w", name, err)
			}
			b, err := program(path)
			if err != nil {
				return nil, fmt.Errorf("mod %s: run %q: %w", name, run, err)
			}
			out.Programs[path] = digest(b)
		}
	}
	out.Hash = out.hash()
	return out, nil
}

func (s Store) env(name string) string {
	if name == "HOME" {
		return s.Home
	}
	return ""
}

func program(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return nil, errors.New("not a plain file")
	}
	b, err := io.ReadAll(io.LimitReader(f, maxProgram+1))
	if err == nil && len(b) > maxProgram {
		err = fmt.Errorf("larger than %d bytes", maxProgram)
	}
	return b, err
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// hash covers every file of the mod and of the parsers it uses, whether each
// has a mod.toml, and its own programs. Every part is written with its length,
// so that no content can pass for the next part's name.
func (m *Mod) hash() string {
	h := sha256.New()
	part := func(fields ...string) {
		for _, f := range fields {
			fmt.Fprintf(h, "%d:%s", len(f), f)
		}
		h.Write([]byte{'\n'})
	}
	one := func(name string, x mod.Mod, files map[string][]byte) {
		if x.Raw == nil {
			part("no-manifest", name)
		} else {
			part("manifest", name, strconv.Itoa(len(x.Raw)), digest(x.Raw))
		}
		for _, f := range keys(files) {
			part("file", name, f, strconv.Itoa(len(files[f])), digest(files[f]))
		}
	}
	one(m.Name, m.Provider, m.Files)
	for _, n := range keys(m.Parsers) {
		one(n, m.Parsers[n], map[string][]byte{"init.lua": m.Parsers[n].Source})
	}
	for _, p := range keys(m.Programs) {
		part("program", p, m.Programs[p])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Texts is every file as approved, for showing and comparing: "name/file".
func (m *Mod) Texts() map[string]string {
	out := map[string]string{}
	add := func(name string, x mod.Mod, files map[string][]byte) {
		if x.Raw != nil {
			out[name+"/mod.toml"] = string(x.Raw)
		}
		for f, b := range files {
			out[name+"/"+f] = string(b)
		}
	}
	add(m.Name, m.Provider, m.Files)
	for n, p := range m.Parsers {
		add(n, p, map[string][]byte{"init.lua": p.Source})
	}
	return out
}

// Refused is the code that breaks the strict form: such a mod cannot be
// approved.
func (m *Mod) Refused() []string {
	var out []string
	check := func(name string, r scan.Report) {
		for _, n := range r.Refused {
			out = append(out, name+"/"+n.Where()+"  "+n.What)
		}
	}
	check(m.Name, scan.Mod(m.Files, m.Provider.Manifest.Permissions))
	for _, n := range keys(m.Parsers) {
		check(n, scan.Mod(map[string][]byte{"init.lua": m.Parsers[n].Source}, nil))
	}
	return out
}

// Record is one approval.
type Record struct {
	Name        string            `json:"name"`
	Hash        string            `json:"hash"`
	Approved    time.Time         `json:"approved"`
	Permissions *mod.Permissions  `json:"permissions,omitempty"`
	Files       map[string]string `json:"files"`
}

func (s Store) recordPath(name string) string { return filepath.Join(s.Records, name+".json") }

// Record is the approval of name, nil when there is none.
func (s Store) Record(name string) (*Record, error) {
	if !mod.ValidName(name) {
		return nil, fmt.Errorf("%q is not a mod name", name)
	}
	path := s.recordPath(name)
	if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err := trusted(path); err != nil {
		return nil, fmt.Errorf("the approval of %s is not to be trusted: %w", name, err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r Record
	if len(b) > maxRecord || json.Unmarshal(b, &r) != nil || r.Name != name {
		return nil, fmt.Errorf("the approval of %s is damaged: approve it again", name)
	}
	return &r, nil
}

// Approve records m as approved: exactly these bytes.
func (s Store) Approve(m *Mod) error {
	r := Record{Name: m.Name, Hash: m.Hash, Approved: time.Now(), Permissions: m.Provider.Manifest.Permissions, Files: m.Texts()}
	b, err := json.MarshalIndent(r, "", " ")
	if err != nil {
		return err
	}
	if len(b) > maxRecord {
		return errors.New("the mod is too large to keep a record of")
	}
	if err := os.MkdirAll(s.Records, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(s.Records, "."+m.Name+"-*")
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), s.recordPath(m.Name))
	}
	if err != nil {
		os.Remove(f.Name())
	}
	return err
}

// Revoke removes the approval of name; it reports whether there was one.
func (s Store) Revoke(name string) (bool, error) {
	if !mod.ValidName(name) {
		return false, fmt.Errorf("%q is not a mod name", name)
	}
	err := os.Remove(s.recordPath(name))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// State of a mod against its approval.
type State int

const (
	NotApproved State = iota
	Changed           // approved once, different now
	Approved
)

// State reads name and compares it with its approval. This is what anything that
// starts a mod must ask, and then load the returned bytes and no others.
func (s Store) State(name string) (State, *Mod, *Record, error) {
	m, err := s.Read(name)
	if err != nil {
		return NotApproved, nil, nil, err
	}
	r, err := s.Record(name)
	switch {
	case err != nil:
		return NotApproved, m, nil, err
	case r == nil:
		return NotApproved, m, nil, nil
	case r.Hash != m.Hash:
		return Changed, m, r, nil
	}
	return Approved, m, r, nil
}
