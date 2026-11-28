package config

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/pelletier/go-toml/v2"
)

// A config with [page.<name>] tables is a menu: pages of entries in one window.

// MenuConfig is [menu].
type MenuConfig struct {
	Start       string // the first page; default "main"
	Placeholder string // of the search box
	Height      int    // of the list, px
	Terminal    string // the command that opens a terminal; default xdg-terminal-exec
}

// Page is one [page.<name>].
type Page struct {
	Title string
	Entry []Entry
}

// Entry is one row. It does exactly one of Page, Exec, Terminal and Open.
type Entry struct {
	Label    string
	Page     string // go to this page
	Exec     string // close the menu and run a command
	Terminal string // close the menu and run a command in a terminal
	Open     string // close the menu and open a file or URL
	Hold     bool   // terminal only: wait for Enter before the terminal closes
	Back     bool   // exec only: the command's dialog gets a Back to this page
}

const (
	maxPages    = 500
	maxEntries  = 10000
	maxIncludes = 100
)

// IsMenu reports whether the config is a menu.
func (c *Config) IsMenu() bool { return len(c.Pages) > 0 }

// StartPage is the menu's first page.
func (c *Config) StartPage() string {
	if c.Menu != nil && c.Menu.Start != "" {
		return c.Menu.Start
	}
	return "main"
}

// PageTitle is what a page is called on screen.
func (c *Config) PageTitle(name string) string {
	if p := c.Pages[name]; p != nil && p.Title != "" {
		return p.Title
	}
	if name == c.StartPage() {
		return c.Title
	}
	return name
}

// PagePath is the way from the start page to name, both included, over the
// fewest pages; nil when no entry leads there.
func (c *Config) PagePath(name string) []string {
	start := c.StartPage()
	from := map[string]string{start: ""}
	queue := []string{start}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		if p == name {
			var path []string
			for ; p != ""; p = from[p] {
				path = append(path, p)
			}
			slices.Reverse(path)
			return path
		}
		for _, e := range c.Pages[p].Entry {
			if _, seen := from[e.Page]; e.Page != "" && !seen {
				from[e.Page] = p
				queue = append(queue, e.Page)
			}
		}
	}
	return nil
}

// WithoutIcon is a label without the glyph and spaces in front of it.
func WithoutIcon(label string) string {
	return strings.TrimLeftFunc(label, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.Is(unicode.Co, r) || unicode.Is(unicode.So, r)
	})
}

// include reads the pages of the files c.Include names. They are looked for
// relative to the config and must lie below its folder, links followed.
func (c *Config) include(path string) error {
	if len(c.Include) == 0 {
		return nil
	}
	if path == "" {
		return errors.New("include: only a config file can include others")
	}
	root, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return err
	}
	var files []string
	for _, pattern := range c.Include {
		if !filepath.IsLocal(pattern) {
			return fmt.Errorf("include %q: only files below the config's folder", pattern)
		}
		found, err := filepath.Glob(filepath.Join(filepath.Dir(path), pattern))
		if err != nil {
			return fmt.Errorf("include %q: %w", pattern, err)
		}
		if len(found) == 0 && !strings.ContainsAny(pattern, "*?[") {
			return fmt.Errorf("include %q: no such file", pattern)
		}
		sort.Strings(found)
		files = append(files, found...)
	}
	if len(files) > maxIncludes {
		return fmt.Errorf("include: more than %d files", maxIncludes)
	}
	for _, f := range files {
		real, err := filepath.EvalSymlinks(f)
		if err != nil {
			return fmt.Errorf("include: %w", err)
		}
		if rel, err := filepath.Rel(root, real); err != nil || !filepath.IsLocal(rel) {
			return fmt.Errorf("include %s: it leads outside the config's folder", f)
		}
		data, err := readRegular(real)
		if err != nil {
			return fmt.Errorf("include: %w", err)
		}
		var keys map[string]any
		if err := toml.Unmarshal(data, &keys); err != nil {
			return fmt.Errorf("include %s: %w", f, err)
		}
		for k := range keys {
			if k != "page" {
				return fmt.Errorf("include %s: only [page.*] tables belong in it, not %q", f, k)
			}
		}
		var part Config
		if err := toml.Unmarshal(data, &part); err != nil {
			return fmt.Errorf("include %s: %w", f, err)
		}
		for name, p := range part.Pages {
			if _, ok := c.Pages[name]; ok {
				return fmt.Errorf("include %s: page %q is defined twice", f, name)
			}
			if c.Pages == nil {
				c.Pages = map[string]*Page{}
			}
			c.Pages[name] = p
		}
	}
	return nil
}

// meantAsMenu refuses a config that was meant as a menu and, misspelled, would
// open as an empty window.
func (c *Config) meantAsMenu(data []byte) error {
	if c.IsMenu() || len(c.Items) > 0 {
		return nil
	}
	var keys map[string]any
	if toml.Unmarshal(data, &keys) != nil {
		return nil
	}
	for _, k := range []string{"page", "pages", "include", "menu"} {
		if _, ok := keys[k]; ok {
			return errors.New("this looks like a menu, but it has no entries: write [[page.<name>.entry]]")
		}
	}
	return nil
}

// menuHint says how a page is written when that is likely what went wrong.
func menuHint(data []byte, err error) error {
	if bytes.Contains(data, []byte("[page.")) && strings.Contains(err.Error(), "table") {
		return fmt.Errorf("%w (an entry of a page is [[page.<name>.entry]], with two brackets)", err)
	}
	return err
}

func (c *Config) checkMenu() error {
	if !c.IsMenu() {
		if c.Menu != nil {
			return errors.New("[menu] without any [page.*]")
		}
		return nil
	}
	if len(c.Items) > 0 || c.Submit.Exec != "" || c.Submit.Stdout != "" || c.Submit.Format != "" ||
		c.Submit.Copy != "" || c.Submit.Call != "" || c.Apps != nil || len(c.Sources) > 0 {
		return errors.New("a menu has pages only: no [[items]], [submit], [apps] or [[sources]]")
	}
	if len(c.Pages) > maxPages {
		return fmt.Errorf("more than %d pages", maxPages)
	}
	if c.Pages[c.StartPage()] == nil {
		return fmt.Errorf("no [page.%s] to start on", c.StartPage())
	}
	entries := 0
	for _, name := range slices.Sorted(mapKeys(c.Pages)) {
		p := c.Pages[name]
		if len(p.Entry) == 0 {
			return fmt.Errorf("page %q has no entries", name)
		}
		entries += len(p.Entry)
		for i, e := range p.Entry {
			where := fmt.Sprintf("page %q, entry %d", name, i+1)
			if strings.TrimSpace(e.Label) == "" {
				return fmt.Errorf("%s: no label", where)
			}
			if strings.ContainsAny(e.Label, "\n\r") {
				return fmt.Errorf("%s: a label is one line", where)
			}
			actions := 0
			for _, a := range []string{e.Page, e.Exec, e.Terminal, e.Open} {
				if a != "" {
					actions++
				}
			}
			if actions != 1 {
				return fmt.Errorf("%s (%q): wants exactly one of page, exec, terminal, open", where, WithoutIcon(e.Label))
			}
			if e.Page != "" && c.Pages[e.Page] == nil {
				return fmt.Errorf("%s: there is no page %q", where, e.Page)
			}
			if e.Hold && e.Terminal == "" {
				return fmt.Errorf("%s: hold goes with terminal", where)
			}
			if e.Back && e.Exec == "" {
				return fmt.Errorf("%s: back goes with exec", where)
			}
			if strings.HasPrefix(e.Open, "-") {
				return fmt.Errorf("%s: open takes a file or URL, not an option", where)
			}
		}
	}
	if entries > maxEntries {
		return fmt.Errorf("more than %d entries", maxEntries)
	}
	return nil
}

func mapKeys[V any](m map[string]V) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}
