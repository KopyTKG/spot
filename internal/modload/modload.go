// Package modload prepares a provider for its host, in the daemon: it reads the
// mod once, refuses code that breaks the strict form, resolves the permissions
// and pairs the bytes that were checked with the sandbox they will run in.
//
// Approval, which will stand between this and starting anything, is not built.
package modload

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gitlab.com/KopyTKG/spot/internal/approval"
	"gitlab.com/KopyTKG/spot/internal/config"
	"gitlab.com/KopyTKG/spot/internal/mod"
	"gitlab.com/KopyTKG/spot/internal/modhost"
	"gitlab.com/KopyTKG/spot/internal/modhost/channel"
	"gitlab.com/KopyTKG/spot/internal/modperm"
	"gitlab.com/KopyTKG/spot/internal/sandbox"
	"gitlab.com/KopyTKG/spot/internal/scan"
)

// Options of one load.
type Options struct {
	Host      string              // the host binary
	Env       func(string) string // the daemon's
	Protected sandbox.Protected
	Config    json.RawMessage // [mod.<name>]
	EmptyDir  string          // where programs start
}

// Protected are spot's own places for a daemon with this environment.
func Protected(vars map[string]string) sandbox.Protected {
	p := sandbox.Protected{Socket: config.SocketPath()}
	for _, base := range []string{vars["XDG_CONFIG_HOME"], vars["XDG_STATE_HOME"], vars["XDG_DATA_HOME"]} {
		if base != "" {
			p.NoWrite = append(p.NoWrite, filepath.Join(base, "spot"))
		}
	}
	return p
}

func strict(name string, files map[string][]byte, m mod.Mod) error {
	files["init.lua"] = m.Source // what is checked is what runs
	r := scan.Mod(files, m.Manifest.Permissions)
	if len(r.Refused) == 0 {
		return nil
	}
	var why []string
	for _, n := range r.Refused {
		why = append(why, n.Where()+"  "+n.What)
	}
	return errors.New(name + " would not be run:\n  " + strings.Join(why, "\n  "))
}

// Provider reads the provider in dir.
func Provider(dir string, o Options) (modhost.Config, channel.Load, error) {
	var c modhost.Config
	var l channel.Load
	m, err := mod.Load(dir)
	if err != nil {
		return c, l, err
	}
	if m.Manifest.Type != "provider" {
		return c, l, fmt.Errorf("mod %s is not a provider", m.Name)
	}
	files, err := mod.Files(dir)
	if err != nil {
		return c, l, fmt.Errorf("mod %s: %w", m.Name, err)
	}
	if err := strict(m.Name, files, m); err != nil {
		return c, l, err
	}
	parsers := map[string]mod.Mod{}
	for _, use := range m.Manifest.Uses {
		_, set, err := mod.LoadSet(filepath.Join(filepath.Dir(filepath.Clean(dir)), use))
		if err != nil {
			return c, l, fmt.Errorf("mod %s uses %s: %w", m.Name, use, err)
		}
		for name, p := range set {
			if err := strict(name, map[string][]byte{}, p); err != nil {
				return c, l, err
			}
			parsers[name] = p
		}
	}
	env := o.Env
	if env == nil {
		env = os.Getenv
	}
	grants, err := modperm.Resolve(m.Manifest.Permissions, env)
	if err != nil {
		return c, l, fmt.Errorf("mod %s: %w", m.Name, err)
	}
	c = modhost.Config{Name: m.Name, Policy: grants.Policy(o.Host), Protected: o.Protected}
	l = channel.Load{Mod: m, Files: files, Parsers: parsers, Grants: grants, Config: o.Config, EmptyDir: o.EmptyDir}
	return c, l, nil
}

// Approved prepares a provider from the bytes an approval was checked against.
// Nothing is read from disk again, so what runs is what was approved (S15).
func Approved(m *approval.Mod, o Options) (modhost.Config, channel.Load, error) {
	var c modhost.Config
	var l channel.Load
	if m.Provider.Manifest.Type != "provider" {
		return c, l, fmt.Errorf("mod %s is not a provider", m.Name)
	}
	if why := m.Refused(); len(why) > 0 {
		return c, l, errors.New(m.Name + " would not be run:\n  " + strings.Join(why, "\n  "))
	}
	grants, err := modperm.Resolve(m.Provider.Manifest.Permissions, o.Env)
	if err != nil {
		return c, l, fmt.Errorf("mod %s: %w", m.Name, err)
	}
	c = modhost.Config{Name: m.Name, Policy: grants.Policy(o.Host), Protected: o.Protected}
	l = channel.Load{Mod: m.Provider, Files: m.Files, Parsers: m.Parsers, Grants: grants, Config: o.Config, EmptyDir: o.EmptyDir}
	return c, l, nil
}
