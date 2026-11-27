// Package mod reads mod folders: mod.toml, the manifest, and init.lua. It does
// not run anything.
package mod

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/pelletier/go-toml/v2"
)

const (
	// API is the version of the mod contract this spot speaks.
	API = 1
	// MaxDepth is how long a chain of `uses` may be.
	MaxDepth = 8

	maxManifest = 64 << 10
	maxSource   = 1 << 20
	maxFiles    = 100
)

// Permissions are what a provider asks for, each with its reason.
type Permissions struct {
	Run     map[string]string `toml:"run" json:"run,omitempty"`
	Read    map[string]string `toml:"read" json:"read,omitempty"`
	Write   map[string]string `toml:"write" json:"write,omitempty"`
	Env     map[string]string `toml:"env" json:"env,omitempty"`
	Setenv  map[string]string `toml:"setenv" json:"setenv,omitempty"`
	Net     map[string]string `toml:"net" json:"net,omitempty"`
	Netlink map[string]string `toml:"netlink" json:"netlink,omitempty"`
	Socket  map[string]string `toml:"socket" json:"socket,omitempty"`
}

// Kinds are the permissions by their table's name.
func (p *Permissions) Kinds() map[string]map[string]string {
	return map[string]map[string]string{"run": p.Run, "read": p.Read, "write": p.Write, "env": p.Env,
		"setenv": p.Setenv, "net": p.Net, "netlink": p.Netlink, "socket": p.Socket}
}

// Manifest is a mod.toml. A parser's needs all of it up to Uses; a provider's
// may be missing, and only a provider has permissions.
type Manifest struct {
	Permissions *Permissions `toml:"permissions" json:"permissions,omitempty"`

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
	Raw      []byte   `json:"-"`      // mod.toml as read; nil when there is none
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

// wrongKind is how the library words a value of the wrong type.
var wrongKind = regexp.MustCompile(`cannot decode TOML (\w+) into (?:struct field )?\S*?\.?(\w+) of type`)

// explain says which key is wrong, not which Go type it did not fit.
func explain(err error) error {
	var unknown *toml.StrictMissingError
	if errors.As(err, &unknown) {
		var keys []string
		for _, e := range unknown.Errors {
			keys = append(keys, strings.Join(e.Key(), "."))
		}
		return fmt.Errorf("unknown key %s (a misspelling? permissions go under [permissions.<kind>])", strings.Join(keys, ", "))
	}
	if m := wrongKind.FindStringSubmatch(err.Error()); m != nil {
		return fmt.Errorf("%s: a TOML %s will not do here (a number in quotes? a list where a table belongs?)", strings.ToLower(m[2]), m[1])
	}
	var bad *toml.DecodeError
	if errors.As(err, &bad) {
		line, _ := bad.Position()
		return fmt.Errorf("line %d: %s", line, strings.TrimPrefix(bad.Error(), "toml: "))
	}
	return err
}

func parseManifest(data []byte) (Manifest, error) {
	var m Manifest
	d := toml.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&m); err != nil {
		return m, explain(err)
	}
	for _, u := range m.Uses {
		if !ValidName(u) {
			return m, fmt.Errorf("uses: %q is not a mod name", u)
		}
	}
	switch m.Type {
	case "", "provider":
		m.Type = "provider"
		if m.API != 0 && m.API != API {
			return m, fmt.Errorf("api: this spot speaks %d, the mod wants %d", API, m.API)
		}
		if m.Returns != nil || m.Input != "" {
			return m, fmt.Errorf(`input and [returns] belong to a parser: if this is one, add type = "parser"`)
		}
		if m.Permissions != nil {
			for kind, entries := range m.Permissions.Kinds() {
				for what, why := range entries {
					if strings.TrimSpace(why) == "" {
						return m, fmt.Errorf("permissions.%s %q: needs the reason for it", kind, what)
					}
				}
			}
		}
		return m, nil
	case "parser":
	default:
		return m, fmt.Errorf(`type: want "parser" or "provider", got %q`, m.Type)
	}
	switch {
	case m.API == 0:
		return m, fmt.Errorf("api = %d is missing", API)
	case m.API != API:
		return m, fmt.Errorf("api: this spot speaks %d, the mod wants %d", API, m.API)
	case m.Version == "" || m.Description == "" || m.Input == "":
		return m, fmt.Errorf("version, description and input are required")
	case m.Returns == nil:
		return m, fmt.Errorf("[returns] is required: the fields of each record and their types")
	case m.Permissions != nil:
		return m, fmt.Errorf("a parser has no permissions")
	}
	for field, decl := range m.Returns {
		if typ, _ := FieldType(decl); !slices.Contains(fieldTypes, typ) {
			return m, fmt.Errorf("returns.%s: unknown type %q (want %s, with ? when it may be missing)", field, decl, strings.Join(fieldTypes, ", "))
		}
	}
	return m, nil
}

// ReadExample reads a file of a mod's example folder: plain and not huge.
func ReadExample(path string) ([]byte, error) { return readFile(path, 4<<20) }

// readExact reads a plain file of a mod, never through a symlink.
func readExact(path string, limit int64) ([]byte, error) {
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

// readFile is readExact for text. Some editors begin a file with a byte order
// mark, which is not part of it.
func readFile(path string, limit int64) ([]byte, error) {
	b, err := readExact(path, limit)
	return bytes.TrimPrefix(b, []byte("\ufeff")), err
}

// Load reads the mod in dir. Its name is the folder's.
func Load(dir string) (Mod, error) {
	m := Mod{Name: filepath.Base(filepath.Clean(dir))}
	if !ValidName(m.Name) {
		return m, fmt.Errorf("the folder is the mod's name, and %q will not do: small letters, digits, - and _, starting with a letter", m.Name)
	}
	data, err := readFile(filepath.Join(dir, "mod.toml"), maxManifest)
	if errors.Is(err, fs.ErrNotExist) { // a provider may do without
		data, err = nil, nil
	}
	if err != nil {
		return m, fmt.Errorf("mod %s: %w", m.Name, err)
	}
	m.Raw = data
	if m.Manifest, err = parseManifest(data); err != nil {
		return m, fmt.Errorf("mod %s: mod.toml: %w", m.Name, err)
	}
	if m.Source, err = readFile(filepath.Join(dir, "init.lua"), maxSource); errors.Is(err, fs.ErrNotExist) {
		return m, fmt.Errorf("mod %s: there is no init.lua in %s: that file is the mod's code", m.Name, dir)
	} else if err != nil {
		return m, fmt.Errorf("mod %s: %w", m.Name, err)
	}
	return m, nil
}

// Files are every Lua file of the mod in dir, init.lua and what it may load,
// by their path inside it. Symlinks are refused: what is read must be what is
// in the folder.
func Files(dir string) (map[string][]byte, error) {
	// The folder itself may be a link, as when a mod is developed elsewhere;
	// walking a link would find nothing and report nothing.
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{}
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		// A name is shown in reviews: one with a newline or invisible
		// characters could write lines that look like spot's.
		if strings.ContainsFunc(rel, func(r rune) bool { return !unicode.IsPrint(r) }) {
			return fmt.Errorf("a file name with control or invisible characters: %q", rel)
		}
		if d.Type()&fs.ModeSymlink != 0 && rel != "." {
			return fmt.Errorf("%s: a symlink in a mod", rel)
		}
		if d.IsDir() || filepath.Ext(path) != ".lua" {
			return nil
		}
		if len(files) >= maxFiles {
			return fmt.Errorf("more than %d Lua files", maxFiles)
		}
		files[rel], err = readFile(path, maxSource)
		return err
	})
	return files, err
}

// LoadSet reads the parser in dir and every parser it uses, from the folders
// beside it, refusing circles and chains that are too long.
func LoadSet(dir string) (string, map[string]Mod, error) {
	root, err := Load(dir)
	if err != nil {
		return "", nil, err
	}
	if root.Manifest.Type != "parser" {
		return "", nil, fmt.Errorf("mod %s is not a parser", root.Name)
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
				if next.Manifest.Type != "parser" {
					return fmt.Errorf("%s uses %s, which is not a parser", m.Name, u)
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
