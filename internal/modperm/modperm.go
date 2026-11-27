// Package modperm turns what a mod.toml asks for into what a mod host is
// restricted to. It runs in the daemon, never in the host.
package modperm

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gitlab.com/KopyTKG/spot/internal/mod"
	"gitlab.com/KopyTKG/spot/internal/sandbox"
)

// Grants is a mod's permissions with paths and programs resolved. The host
// checks every call against it; the kernel checks again.
type Grants struct {
	Programs map[string]string `json:"programs,omitempty"` // as asked for -> the file
	Read     []string          `json:"read,omitempty"`
	Write    []string          `json:"write,omitempty"`
	Env      map[string]string `json:"env,omitempty"`       // permitted variables that are set
	EnvNames []string          `json:"env_names,omitempty"` // all permitted ones
	Setenv   []string          `json:"setenv,omitempty"`
	Ports    []uint16          `json:"ports,omitempty"`
	Netlink  []string          `json:"netlink,omitempty"`
	Sockets  []string          `json:"sockets,omitempty"`
	Vars     map[string]string `json:"vars,omitempty"` // what paths may name
}

var varName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// neverSet are variables no mod may set for a program, whatever it lists: they
// choose what code a program loads.
var neverSet = regexp.MustCompile(`^(PATH|HOME|LD_.*|GLIBC_.*|GCONV_PATH|LOCPATH|NLSPATH)$`)

// pathVars are the variables a path in a permission or a call may start with.
var pathVars = []string{"HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_RUNTIME_DIR"}

// Vars reads the variables paths may name, with their usual defaults.
func Vars(env func(string) string) map[string]string {
	v := map[string]string{}
	home := env("HOME")
	for name, def := range map[string]string{"HOME": home, "XDG_CONFIG_HOME": filepath.Join(home, ".config"),
		"XDG_CACHE_HOME": filepath.Join(home, ".cache"), "XDG_DATA_HOME": filepath.Join(home, ".local/share"),
		"XDG_STATE_HOME": filepath.Join(home, ".local/state"), "XDG_RUNTIME_DIR": ""} {
		if x := env(name); x != "" {
			v[name] = x
		} else if def != "" {
			v[name] = def
		}
	}
	return v
}

// Expand makes "~/x" and "$XDG_RUNTIME_DIR/x" absolute. Only the known
// variables, only at the start: nothing else in a path is interpreted.
func Expand(path string, vars map[string]string) (string, error) {
	p := path
	if p == "~" || strings.HasPrefix(p, "~/") {
		p = "$HOME" + p[1:]
	}
	if rest, ok := strings.CutPrefix(p, "$"); ok {
		name, tail, _ := strings.Cut(rest, "/")
		name = strings.TrimSuffix(strings.TrimPrefix(name, "{"), "}")
		value, known := vars[name]
		if !known || value == "" {
			return "", fmt.Errorf("%q: $%s is not one of %s", path, name, strings.Join(pathVars, ", "))
		}
		p = filepath.Join(value, tail)
	}
	if !filepath.IsAbs(p) || strings.ContainsRune(p, 0) {
		return "", fmt.Errorf("%q: not a full path", path)
	}
	return filepath.Clean(p), nil
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Resolve works out the grants of perms (nil: none). env is the daemon's.
func Resolve(perms *mod.Permissions, env func(string) string) (Grants, error) {
	g := Grants{Programs: map[string]string{}, Env: map[string]string{}, Vars: Vars(env)}
	if perms == nil {
		return g, nil
	}
	for _, name := range keys(perms.Run) {
		var path string
		if strings.ContainsRune(name, '/') { // a script of your own, by its path
			p, err := Expand(name, g.Vars)
			if err != nil {
				return g, fmt.Errorf("run %w", err)
			}
			path = p
		} else {
			path = filepath.Join(sandbox.SystemPath, name) // never the caller's PATH
		}
		st, err := os.Stat(path)
		if err != nil || !st.Mode().IsRegular() || st.Mode()&0o111 == 0 {
			return g, fmt.Errorf("run %q: there is no program %s", name, path)
		}
		g.Programs[name] = path
	}
	for kind, from := range map[string]map[string]string{"read": perms.Read, "write": perms.Write, "socket": perms.Socket} {
		for _, asked := range keys(from) {
			p, err := Expand(asked, g.Vars)
			if err != nil {
				return g, fmt.Errorf("%s %w", kind, err)
			}
			switch kind {
			case "read":
				g.Read = append(g.Read, p)
			case "write":
				// A file is written by renaming a new one over it, which takes
				// rights on its folder.
				if st, err := os.Stat(p); err != nil || !st.IsDir() {
					return g, fmt.Errorf("write %q: name a folder that exists", asked)
				}
				g.Write = append(g.Write, p)
			case "socket":
				g.Sockets = append(g.Sockets, p)
			}
		}
	}
	for _, name := range keys(perms.Env) {
		if !varName.MatchString(name) {
			return g, fmt.Errorf("env %q: not a variable name", name)
		}
		g.EnvNames = append(g.EnvNames, name)
		if v := env(name); v != "" {
			g.Env[name] = v
		}
	}
	for _, name := range keys(perms.Setenv) {
		if !varName.MatchString(name) || neverSet.MatchString(name) {
			return g, fmt.Errorf("setenv %q: a mod may never set it", name)
		}
		g.Setenv = append(g.Setenv, name)
	}
	for _, port := range keys(perms.Net) {
		n, err := strconv.ParseUint(port, 10, 16)
		if err != nil || n == 0 {
			return g, fmt.Errorf("net %q: not a port", port)
		}
		g.Ports = append(g.Ports, uint16(n))
	}
	g.Netlink = keys(perms.Netlink)
	return g, nil
}

// Policy is the sandbox for a host at self running with these grants.
func (g Grants) Policy(self string) sandbox.Policy {
	p := sandbox.Policy{Self: self, Read: g.Read, Write: g.Write, Ports: g.Ports, Netlink: g.Netlink, Sockets: g.Sockets}
	for _, name := range keys(g.Programs) {
		p.Programs = append(p.Programs, g.Programs[name])
	}
	return p
}
