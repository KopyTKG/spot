// Package config is what a TOML file means: dialog and daemon configs, validation,
// SPOT_* overrides, placement and the submit templates. No GTK.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"gitlab.com/KopyTKG/spot/internal/match"
)

type Config struct {
	Title        string
	Anchor       string // center (default) | top | bottom | left | right
	MarginTop    int    `toml:"margin_top"`    // px kept free above the dialog
	MarginBottom int    `toml:"margin_bottom"` // ... below it
	MarginLeft   int    `toml:"margin_left"`   // ... to its left
	MarginRight  int    `toml:"margin_right"`  // ... to its right
	OffsetX      int    `toml:"offset_x"`      // px to shift the dialog from where the anchor puts it; + is right, - left
	OffsetY      int    `toml:"offset_y"`      // ... + is down, - up
	Width        int
	Height       int
	Style        string // path to CSS, relative to TOML file
	Footer       string
	Keyboard     string // exclusive (default) | on-demand | none
	Toggle       bool   // a keybind dialog: asking for it again closes it, and repeats do not queue up
	Items        []Item
	Submit       Submit
	Opacity      *Opacity
	Back         *Back
	Apps         *AppsConfig    // [apps]: the application index and how apps are launched (type = "apps")
	Sources      []SourceConfig // [[sources]]: prefixes that hand the launcher's search to a command (see package launcher)

	// From SPOT_BACK: the caller wants a Back control, and what to run on it.
	backEnv bool
	backCmd string
}

// Back configures the Back control. It is only shown when the caller asks for
// it (--back, or SPOT_BACK in the environment), because only the caller knows
// whether there is anything to go back to.
type Back struct {
	Label string // default "‹ Back"
	Show  string // auto (default: when asked for) | never
}

// Opacity fades the dialog itself depending on where the pointer is.
// Percentages are 0-100. Without an [opacity] table the defaults below apply;
// set outside = 100 for a constant, opaque dialog.
type Opacity struct {
	Inside     *float64 `toml:"inside"`     // % while the pointer is over the dialog (default 100)
	Outside    *float64 `toml:"outside"`    // % while the pointer is outside it (default 85)
	Transition *int     `toml:"transition"` // fade time in ms (default 150, 0 = instant)
}

type Item struct {
	Type        string // field | password | text | select | list | apps | tiles | toggle | scale
	ID          string
	Label       string
	Placeholder string
	Default     any      // string; bool for toggle, number for scale
	Options     []string // select, list
	Stdin       bool     // list: read newline-delimited options from stdin
	Height      int      // list, text: min content height in px
	Columns     int      // tiles: max children per row (default 3)
	Tiles       []Tile   // tiles: per-tile icon/label/value
	Current     string   // tiles: value of the tile marked as the live setting (.spot-active)
	Multi       bool     // list: allow several rows to be selected
	Match       string   // list: "contains" (default) | "smart" (see smartScore)
	Separator   *string  // list (multi): joins the selected options (default newline)
	Query       string   // list, apps: text the search box starts with (SPOT_QUERY overrides it)
	HideEmpty   bool     `toml:"hide_empty"` // list, apps: the results list is hidden while it has no rows (a bare search bar)
	Collapsible bool     // list: options are "GROUP<TAB>ROW"; groups fold and unfold (see package match)
	Collapsed   *bool    // list (collapsible): groups start folded (default true)
	GroupSep    *string  `toml:"group_separator"` // list (collapsible): what splits group from row (default tab)
	On          *string  // toggle: value when checked (default "true")
	Off         *string  // toggle: value when unchecked (default "false")
	Min         *float64 // scale (default 0)
	Max         *float64 // scale (default 100)
	Step        *float64 // scale (default 1)
	Required    bool     // field, password, text, select, list: must not be empty
	Pattern     string   // field, password, text: regexp the value must match
	Error       string   // message shown when the item is invalid

	pattern *regexp.Regexp
}

type Tile struct {
	Icon  string // text rendered large above the label (NF glyph, emoji, etc.)
	Label string
	Value string // returned as the item's value when selected
}

type Submit struct {
	Exec    string // command template; {id} substituted from field values
	Stdout  string // print template to stdout on submit (exit 1 on cancel)
	Format  string // "json": print every item's value as a JSON object
	Spawn   bool   // (exec only) fire-and-forget then close
	OnError string `toml:"on_error"` // (exec only) show (default) | exit
}

// ResolveConfigPath maps the command-line argument to a config file. An
// argument that is not an existing file and looks like a bare name
// ("ssh") is looked up as $XDG_CONFIG_HOME/spot/<name>.toml.
func ResolveConfigPath(arg string) string {
	if _, err := os.Stat(arg); err == nil {
		return arg
	}
	if arg == "" || strings.ContainsRune(arg, filepath.Separator) || strings.HasSuffix(arg, ".toml") {
		return arg
	}
	dir := configDir()
	if dir == "" {
		return arg
	}
	return filepath.Join(dir, arg+".toml")
}

// configDir is ${XDG_CONFIG_HOME:-~/.config}/spot, or "" without a home.
func configDir() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "spot")
}

// ParseConfig decodes and validates a config, and applies the SPOT_* overrides
// looked up through env.
func ParseConfig(data []byte, env func(string) (string, bool)) (*Config, error) {
	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	applyEnv(&cfg, env)
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// applyEnv lets one shared config be parameterised per invocation.
func applyEnv(cfg *Config, env func(string) (string, bool)) {
	if v, ok := env("SPOT_TITLE"); ok {
		cfg.Title = v
	}
	if v, ok := env("SPOT_FOOTER"); ok {
		cfg.Footer = v
	}
	if v, ok := env("SPOT_BACK"); ok && v != "" {
		cfg.backEnv, cfg.backCmd = true, v
	}
	if v, ok := env("SPOT_PLACEHOLDER"); ok {
		for i := range cfg.Items {
			t := cfg.Items[i].Type
			if t == "field" || t == "password" || t == "text" || t == "list" || t == "apps" {
				cfg.Items[i].Placeholder = v
				break
			}
		}
	}
	if v, ok := env("SPOT_QUERY"); ok {
		for i := range cfg.Items {
			if t := cfg.Items[i].Type; t == "list" || t == "apps" {
				cfg.Items[i].Query = v
				break
			}
		}
	}
	if v, ok := env("SPOT_DEFAULT"); ok {
		for i := range cfg.Items {
			if takesDefault(cfg.Items[i].Type) {
				cfg.Items[i].Default = v
				break
			}
		}
	}
	// SPOT_DEFAULT_<ID> targets one item by id (upper-cased, non-alphanumerics
	// as "_") and wins over SPOT_DEFAULT, so a config with several sections can
	// pre-select each one.
	for i := range cfg.Items {
		it := &cfg.Items[i]
		if it.ID == "" || !takesDefault(it.Type) {
			continue
		}
		if v, ok := env("SPOT_DEFAULT_" + strings.ToUpper(SanitizeID(it.ID))); ok {
			it.Default = v
		}
		// SPOT_CURRENT_<ID> marks the tile that is the live setting, e.g. the
		// power profile in use, independently of what is selected.
		if v, ok := env("SPOT_CURRENT_" + strings.ToUpper(SanitizeID(it.ID))); ok && it.Type == "tiles" {
			it.Current = v
		}
	}
}

// takesDefault reports whether an item type has a default value.
func takesDefault(t string) bool {
	switch t {
	case "field", "text", "select", "list", "tiles", "toggle", "scale":
		return true
	}
	return false
}

func (c *Config) validate() error {
	switch c.Submit.Format {
	case "", "json":
	default:
		return fmt.Errorf("submit.format: unknown format %q (want \"json\")", c.Submit.Format)
	}
	if c.Submit.Format != "" && c.Submit.Stdout != "" {
		return errors.New("submit.format and submit.stdout both write to stdout; set only one")
	}
	switch c.Submit.OnError {
	case "", "show", "exit":
	default:
		return fmt.Errorf("submit.on_error: unknown mode %q (want \"show\" or \"exit\")", c.Submit.OnError)
	}
	if c.Back != nil {
		switch c.Back.Show {
		case "", "auto", "never":
		default:
			return fmt.Errorf("back.show: unknown mode %q (want \"auto\" or \"never\")", c.Back.Show)
		}
	}
	if err := ValidateSources(c.Sources); err != nil {
		return err
	}
	if c.MarginTop < 0 || c.MarginBottom < 0 || c.MarginLeft < 0 || c.MarginRight < 0 {
		return errors.New("margin_top, margin_bottom, margin_left and margin_right must not be negative (use offset_x / offset_y to shift)")
	}
	if c.Apps != nil && (c.Apps.IconSize < 0 || c.Apps.IconSizeSel < 0) {
		return fmt.Errorf("apps.icon_size and apps.icon_size_selected must not be negative")
	}
	for i := range c.Items {
		it := &c.Items[i]
		switch it.Type {
		case "apps":
			if it.Multi || it.Stdin || len(it.Options) > 0 || it.Collapsible {
				return fmt.Errorf("item %q: an apps item takes no options, stdin, multi or collapsible", it.ID)
			}
			if c.Apps == nil {
				c.Apps = &AppsConfig{}
			}
			if it.Match == "" {
				it.Match = "smart"
			}
		case "list":
			if it.Collapsible && it.Multi {
				return fmt.Errorf("item %q: collapsible lists cannot be multi-select", it.ID)
			}
		default:
			if it.HideEmpty {
				return fmt.Errorf("item %q: hide_empty only applies to lists and apps", it.ID)
			}
			if it.Collapsible || it.Collapsed != nil || it.GroupSep != nil {
				return fmt.Errorf("item %q: collapsible, collapsed and group_separator only apply to lists", it.ID)
			}
		}
		if it.Pattern != "" {
			re, err := regexp.Compile(it.Pattern)
			if err != nil {
				return fmt.Errorf("item %q: pattern: %w", it.ID, err)
			}
			it.pattern = re
		}
		switch it.Match {
		case "", "contains", "smart":
		default:
			return fmt.Errorf("item %q: match: unknown mode %q (want \"contains\" or \"smart\")", it.ID, it.Match)
		}
		if it.Type == "scale" {
			if it.MinValue() > it.MaxValue() {
				return fmt.Errorf("item %q: min is greater than max", it.ID)
			}
		}
	}
	return nil
}

// Valid reports whether value satisfies the item's required / pattern rules.
func (it *Item) Valid(value string) bool {
	if value == "" {
		return !it.Required
	}
	return it.pattern == nil || it.pattern.MatchString(value)
}

// HasPattern reports whether values are checked against a pattern.
func (it *Item) HasPattern() bool { return it.pattern != nil }

// DefaultString renders Default as text whatever its TOML type.
func (it *Item) DefaultString() string {
	switch v := it.Default.(type) {
	case nil:
		return ""
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return FormatNumber(v)
	default:
		return fmt.Sprint(v)
	}
}

func (it *Item) DefaultBool() bool {
	switch v := it.Default.(type) {
	case bool:
		return v
	case string:
		on := it.OnValue()
		return v == on || strings.EqualFold(v, "true")
	}
	return false
}

func (it *Item) OnValue() string {
	if it.On != nil {
		return *it.On
	}
	return "true"
}

// CollapsedByDefault: collapsible lists start folded unless collapsed = false.
func (it *Item) CollapsedByDefault() bool { return it.Collapsed == nil || *it.Collapsed }

// GroupSeparator is what splits "GROUP<sep>ROW" in a collapsible list.
func (it *Item) GroupSeparator() string {
	if it.GroupSep == nil || *it.GroupSep == "" {
		return match.DefaultGroupSep
	}
	return *it.GroupSep
}

func (it *Item) OffValue() string {
	if it.Off != nil {
		return *it.Off
	}
	return "false"
}

func (it *Item) MinValue() float64 {
	if it.Min != nil {
		return *it.Min
	}
	return 0
}

func (it *Item) MaxValue() float64 {
	if it.Max != nil {
		return *it.Max
	}
	return 100
}

func (it *Item) StepValue() float64 {
	if it.Step != nil && *it.Step > 0 {
		return *it.Step
	}
	return 1
}

// ScaleDefault is the slider's starting position, clamped into [min, max].
func (it *Item) ScaleDefault() float64 {
	v := it.MinValue()
	if s := it.DefaultString(); s != "" {
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			v = f
		}
	}
	return max(it.MinValue(), min(it.MaxValue(), v))
}

func (it *Item) JoinWith() string {
	if it.Separator != nil {
		return *it.Separator
	}
	return "\n"
}

// SanitizeID strips characters that GApplication ID validation rejects.
func SanitizeID(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := b.String()
	if out == "" {
		out = "cfg"
	}
	return out
}

// BackSpec is the Back control of one dialog.
type BackSpec struct {
	Label string
	Cmd   string // run when Back is used; may be empty
}

// BackSpec reports the Back control this dialog gets, or nil for none. flag is
// --back on the command line; SPOT_BACK (already read into the config) asks
// for it too and also names the command to run. A config can opt out with
// back.show = "never" (a top-level menu, say, whatever its caller exports).
func (c *Config) BackSpec(flag bool) *BackSpec {
	if c.Back != nil && c.Back.Show == "never" {
		return nil
	}
	if !flag && !c.backEnv {
		return nil
	}
	b := &BackSpec{Label: "\u2039 Back", Cmd: c.backCmd}
	if c.Back != nil && c.Back.Label != "" {
		b.Label = c.Back.Label
	}
	return b
}
