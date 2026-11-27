// Package sandbox restricts a mod host, and everything it starts, to what its
// mod was granted: Landlock for files, ports and Unix sockets, and a seccomp
// filter for what Landlock does not cover. See docs/modding.md, "The sandbox".
package sandbox

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// SystemPath is where a program given by bare name is found (S10).
const SystemPath = "/usr/bin"

// MinABI is the oldest Landlock the sandbox is tested on. Older: no mod runs.
const MinABI = 11

const loader = "/lib64/ld-linux-x86-64.so.2"

// Netlink are the kernel tables a mod may ask for.
var Netlink = map[string]uint32{"route": 0, "diag": 4}

// Policy is what one mod host may do. Paths are absolute, with "~" and
// variables already expanded.
type Policy struct {
	Self     string   `json:"self"`               // the host's own program file
	Programs []string `json:"programs,omitempty"` // [permissions.run], as full paths
	Read     []string `json:"read,omitempty"`
	Write    []string `json:"write,omitempty"`
	Ports    []uint16 `json:"ports,omitempty"` // [permissions.net]
	Netlink  []string `json:"netlink,omitempty"`
	Sockets  []string `json:"sockets,omitempty"`
}

// Access is what a rule allows on a file or folder.
type Access int

const (
	Exec    Access = 1 << iota // execute, and read the file
	Read                       // read files, list folders
	Write                      // create, change and remove
	Connect                    // connect to a Unix socket
)

// Rule allows Access on Path. An Optional path that does not exist is skipped.
type Rule struct {
	Path     string
	Access   Access
	Optional bool
}

// Protected are spot's own places, which no permission may reach (S36).
type Protected struct {
	Sockets []string // spot's sockets: never granted, nor a folder with one
	NoWrite []string // configuration, mods, approvals, logs
}

var procOfProcess = regexp.MustCompile(`^/proc/(self|thread-self|[0-9]+)(/|$)`)

// within reports whether path is dir or lies under it.
func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
}

// resolve follows every symlink; of a path that does not exist yet, the part
// that does.
func resolve(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("%q is not an absolute path", path)
	}
	path = filepath.Clean(path)
	r, err := filepath.EvalSymlinks(path)
	if errors.Is(err, fs.ErrNotExist) && path != "/" {
		parent, perr := resolve(filepath.Dir(path))
		if perr != nil {
			return "", perr
		}
		return filepath.Join(parent, filepath.Base(path)), nil
	}
	return r, err
}

// places are what decides what a path means: the links on the way to it, and
// the names it goes by - as written, and with every link followed.
func places(path string) (links, names []string) {
	names = []string{filepath.Clean(path)}
	var walk func(p string, depth int)
	walk = func(p string, depth int) {
		at := "/"
		rest := strings.Split(strings.Trim(filepath.Clean(p), "/"), "/")
		for i, part := range rest {
			next := filepath.Join(at, part)
			st, err := os.Lstat(next)
			if err == nil && st.Mode()&fs.ModeSymlink != 0 {
				target, err := os.Readlink(next)
				if err != nil || depth > 40 {
					return
				}
				if !filepath.IsAbs(target) {
					target = filepath.Join(at, target)
				}
				links = append(links, next)
				walk(filepath.Join(append([]string{target}, rest[i+1:]...)...), depth+1)
				return
			}
			at = next
		}
		names = append(names, at)
	}
	walk(path, 0)
	return links, names
}

// reaches reports whether a grant on dir gives access to own: it holds one of
// the links that say where own is, or it and own lie one in the other.
func reaches(dir, own string, insideToo bool) bool {
	links, names := places(own)
	for _, l := range links {
		if within(l, dir) {
			return true
		}
	}
	for _, n := range names {
		// A name that is itself a link is judged by where it leads.
		if st, err := os.Lstat(n); err == nil && st.Mode()&fs.ModeSymlink != 0 {
			continue
		}
		if within(n, dir) || insideToo && within(dir, n) {
			return true
		}
	}
	return false
}

// linksInside are the links within one of spot's folders, and within the
// folders they lead to: a linked daemon.toml or mod is changed by writing where
// it leads.
func linksInside(dir string) []string {
	var out []string
	seen := map[string]bool{}
	count := 0
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		real, err := filepath.EvalSymlinks(dir)
		if err != nil || seen[real] || depth > 4 {
			return
		}
		seen[real] = true
		filepath.WalkDir(real, func(p string, d fs.DirEntry, err error) error {
			if count++; err != nil || count > 5000 {
				return filepath.SkipAll
			}
			if d.Type()&fs.ModeSymlink != 0 {
				out = append(out, p)
				if st, err := os.Stat(p); err == nil && st.IsDir() {
					walk(p, depth+1)
				}
			}
			return nil
		})
	}
	walk(dir, 0)
	return out
}

// Check refuses permissions that may never be granted.
func (p Policy) Check(pr Protected) error {
	for _, n := range p.Netlink {
		if _, ok := Netlink[n]; !ok {
			return fmt.Errorf("netlink %q: not one of diag, route", n)
		}
	}
	for _, port := range p.Ports {
		if port == 0 {
			return errors.New("net: port 0")
		}
	}
	noWrite := append([]string{"/sys/fs/cgroup"}, pr.NoWrite...)
	for _, g := range []struct {
		kind  string
		paths []string
	}{{"read", p.Read}, {"write", p.Write}, {"socket", p.Sockets}} {
		for _, asked := range g.paths {
			path, err := resolve(asked)
			if err != nil {
				return fmt.Errorf("%s %q: %w", g.kind, asked, err)
			}
			refuse := func(why string) error { return fmt.Errorf("%s %q: %s", g.kind, asked, why) }
			switch {
			case path == "/" || path == "/proc" || path == "/sys":
				return refuse("too broad, name what is needed")
			case within(path, "/dev"):
				return refuse("/dev is not granted")
			// /proc/net and /proc/mounts are the named exceptions: they lead into
			// the host's own folder, so they are judged by the name asked for.
			case procOfProcess.MatchString(path) && !within(asked, "/proc/net") && asked != "/proc/mounts":
				return refuse("a process's own files are not granted")
			case g.kind != "read" && (within(path, "/proc") || within(path, "/sys")):
				return refuse("nothing under /proc or /sys is written")
			}
			for _, dir := range []string{path, filepath.Clean(asked)} {
				// Holding the socket's folder is enough to replace the socket.
				for _, sock := range pr.Sockets {
					if reaches(dir, sock, false) {
						return refuse("it holds spot's own socket")
					}
				}
				if g.kind != "write" {
					continue
				}
				for _, own := range noWrite {
					// A linked daemon.toml or mods folder is spot's wherever it is.
					all := append([]string{own}, linksInside(own)...)
					for _, o := range all {
						if reaches(dir, o, true) {
							return refuse("it reaches " + own + ", which no mod may write")
						}
					}
				}
			}
		}
	}
	return nil
}

// Rules is the base grant and what the permissions add.
func (p Policy) Rules() ([]Rule, error) {
	rules := []Rule{{Path: p.Self, Access: Exec}}
	add := func(a Access, optional bool, paths ...string) {
		for _, path := range paths {
			rules = append(rules, Rule{path, a, optional})
		}
	}
	if len(p.Programs) > 0 {
		add(Read|Write, false, "/dev/null")
		add(Exec, false, loader)
		add(Read, false, "/usr/lib")
		add(Read, true, "/usr/lib64") // where Fedora and others keep libraries; a link to lib elsewhere
		add(Read, true, "/etc/localtime", "/usr/share/zoneinfo", "/etc/passwd", "/etc/group")
	}
	for _, prog := range p.Programs {
		add(Exec, false, prog)
		interp, err := interpreters(prog)
		if err != nil {
			return nil, err
		}
		add(Exec, false, interp...)
	}
	if len(p.Ports) > 0 {
		// Certificates only: the folders beside them hold private keys.
		add(Read, true, "/etc/resolv.conf", "/etc/hosts", "/etc/ssl/certs", "/etc/ca-certificates",
			"/etc/pki/tls/certs", "/etc/pki/ca-trust") // as Fedora has them
	}
	for _, path := range p.Read {
		add(Read, true, path)
		add(Read, true, sysTargets(path)...)
	}
	add(Write, true, p.Write...)
	// Connecting needs no more than that; the folder of a socket is listed too,
	// which programs do to find it.
	add(Connect|Read, true, p.Sockets...)
	return rules, nil
}

// sysTargets are the real folders the direct entries of a folder under /sys
// point to, no further: following links inside those reaches all of /sys.
func sysTargets(dir string) []string {
	if !within(filepath.Clean(dir), "/sys") {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.Type()&fs.ModeSymlink == 0 {
			continue
		}
		t, err := filepath.EvalSymlinks(filepath.Join(dir, e.Name()))
		if err == nil && within(t, "/sys") && t != "/sys" && !within(filepath.Clean(dir), t) {
			out = append(out, t)
		}
	}
	return out
}

// interpreters are what runs a script, none for anything else.
func interpreters(program string) ([]string, error) {
	f, err := os.Open(program)
	if err != nil {
		return nil, fmt.Errorf("program %s: %w", program, err)
	}
	defer f.Close()
	line, _ := bufio.NewReader(io.LimitReader(f, 512)).ReadString('\n')
	rest, ok := strings.CutPrefix(line, "#!")
	if !ok {
		return nil, nil
	}
	words := strings.Fields(rest)
	if len(words) == 0 {
		return nil, fmt.Errorf("program %s: empty #! line", program)
	}
	if filepath.Base(words[0]) != "env" {
		return words[:1], nil
	}
	// "#!/usr/bin/env python3" starts env, which then starts python3.
	for _, w := range words[1:] {
		if strings.HasPrefix(w, "-") || strings.Contains(w, "=") {
			continue
		}
		if !filepath.IsAbs(w) {
			w = filepath.Join(SystemPath, w)
		}
		return []string{words[0], w}, nil
	}
	return nil, fmt.Errorf("program %s: its #! line names env and nothing to run", program)
}
