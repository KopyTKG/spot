// Package mod reads mod folders: mod.toml, the manifest, and init.lua. It does
// not run anything.
package mod

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const (
	// API is the version of the mod contract this spot speaks.
	API = 1
	// MaxDepth is how long a chain of `uses` may be.
	MaxDepth = 8

	maxManifest = 64 << 10
	maxSource   = 1 << 20
)

// Manifest is a parser's mod.toml.
type Manifest struct {
	Type        string            `toml:"type" json:"type"`
	API         int               `toml:"api" json:"api"`
	Version     string            `toml:"version" json:"version"`
	Description string            `toml:"description" json:"description"`
	Input       string            `toml:"input" json:"input"`
	Returns     map[string]string `toml:"returns" json:"returns"`
	Uses        []string          `toml:"uses" json:"uses,omitempty"`
}

// Mod is a mod's name and files, as they travel to a mod host.
type Mod struct {
	Name     string   `json:"name"`
	Manifest Manifest `json:"manifest"`
	Source   []byte   `json:"source"` // init.lua
}

var name = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// ValidName reports whether s can name a mod. Checked before it becomes a path.
func ValidName(s string) bool { return len(s) <= 64 && name.MatchString(s) }

var fieldTypes = []string{"string", "number", "boolean", "list", "map"}

// FieldType splits a [returns] line into the type and whether it may be missing.
func FieldType(decl string) (typ string, optional bool) {
	typ, optional = strings.CutSuffix(decl, "?")
	return typ, optional
}

func parseManifest(data []byte) (Manifest, error) {
	var m Manifest
	d := toml.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&m); err != nil {
		return m, err
	}
	switch {
	case m.Type == "provider":
		return m, fmt.Errorf("provider mods are not built yet")
	case m.Type != "parser":
		return m, fmt.Errorf(`type: want "parser", got %q`, m.Type)
	case m.API != API:
		return m, fmt.Errorf("api: this spot speaks %d, the mod wants %d", API, m.API)
	case m.Version == "" || m.Description == "" || m.Input == "":
		return m, fmt.Errorf("version, description and input are required")
	case m.Returns == nil:
		return m, fmt.Errorf("[returns] is required: the fields of each record and their types")
	}
	for field, decl := range m.Returns {
		if typ, _ := FieldType(decl); !slices.Contains(fieldTypes, typ) {
			return m, fmt.Errorf("returns.%s: unknown type %q (want %s, with ? when it may be missing)", field, decl, strings.Join(fieldTypes, ", "))
		}
	}
	for _, u := range m.Uses {
		if !ValidName(u) {
			return m, fmt.Errorf("uses: %q is not a mod name", u)
		}
	}
	return m, nil
}

// readFile reads a plain file of a mod, never through a symlink.
func readFile(path string, limit int64) ([]byte, error) {
	st, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: not a plain file", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err == nil && int64(len(b)) > limit {
		err = fmt.Errorf("%s: larger than %d bytes", path, limit)
	}
	return b, err
}

// Load reads the mod in dir. Its name is the folder's.
func Load(dir string) (Mod, error) {
	m := Mod{Name: filepath.Base(filepath.Clean(dir))}
	if !ValidName(m.Name) {
		return m, fmt.Errorf("mod name %q: want [a-z][a-z0-9_-]*", m.Name)
	}
	data, err := readFile(filepath.Join(dir, "mod.toml"), maxManifest)
	if err != nil {
		return m, fmt.Errorf("mod %s: %w", m.Name, err)
	}
	if m.Manifest, err = parseManifest(data); err != nil {
		return m, fmt.Errorf("mod %s: mod.toml: %w", m.Name, err)
	}
	if m.Source, err = readFile(filepath.Join(dir, "init.lua"), maxSource); err != nil {
		return m, fmt.Errorf("mod %s: %w", m.Name, err)
	}
	return m, nil
}

// LoadSet reads the parser in dir and every parser it uses, from the folders
// beside it, refusing circles and chains that are too long.
func LoadSet(dir string) (string, map[string]Mod, error) {
	root, err := Load(dir)
	if err != nil {
		return "", nil, err
	}
	set := map[string]Mod{root.Name: root}
	var walk func(m Mod, chain []string) error
	walk = func(m Mod, chain []string) error {
		chain = append(chain, m.Name)
		for _, u := range m.Manifest.Uses {
			path := strings.Join(append(chain, u), " -> ")
			if slices.Contains(chain, u) {
				return fmt.Errorf("uses goes in a circle: %s", path)
			}
			if len(chain) >= MaxDepth {
				return fmt.Errorf("uses is more than %d deep: %s", MaxDepth, path)
			}
			next, ok := set[u]
			if !ok {
				if next, err = Load(filepath.Join(filepath.Dir(filepath.Clean(dir)), u)); err != nil {
					return fmt.Errorf("%s uses %s: %w", m.Name, u, err)
				}
				set[u] = next
			}
			if err := walk(next, chain); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(root, nil); err != nil {
		return "", nil, err
	}
	return root.Name, set, nil
}
