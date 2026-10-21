package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Exit statuses, documented in the README.
const (
	exitSubmitted = 0
	exitCancelled = 1
	exitUsage     = 2
	exitError     = 3
)

type Config struct {
	Title    string
	Anchor   string // center (default) | top | bottom | left | right
	Width    int
	Height   int
	Style    string // path to CSS, relative to TOML file
	Footer   string
	Keyboard string // exclusive (default) | on-demand | none
	Items    []Item
	Submit   Submit
	Opacity  *Opacity
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
	Type        string // field | password | text | select | list | tiles | toggle | scale
	ID          string
	Label       string
	Placeholder string
	Default     any      // string; bool for toggle, number for scale
	Options     []string // select, list
	Stdin       bool     // list: read newline-delimited options from stdin
	Height      int      // list, text: min content height in px
	Columns     int      // tiles: max children per row (default 3)
	Tiles       []Tile   // tiles: per-tile icon/label/value
	Multi       bool     // list: allow several rows to be selected
	Separator   *string  // list (multi): joins the selected options (default newline)
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

// resolveConfigPath maps the command-line argument to a config file. An
// argument that is not an existing file and looks like a bare name
// ("ssh") is looked up as $XDG_CONFIG_HOME/spot/<name>.toml.
func resolveConfigPath(arg string) string {
	if _, err := os.Stat(arg); err == nil {
		return arg
	}
	if arg == "" || strings.ContainsRune(arg, filepath.Separator) || strings.HasSuffix(arg, ".toml") {
		return arg
	}
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return arg
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "spot", arg+".toml")
}

// parseConfig decodes and validates a config, and applies the SPOT_* overrides
// looked up through env.
func parseConfig(data []byte, env func(string) (string, bool)) (*Config, error) {
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
	if v, ok := env("SPOT_PLACEHOLDER"); ok {
		for i := range cfg.Items {
			t := cfg.Items[i].Type
			if t == "field" || t == "password" || t == "text" || t == "list" {
				cfg.Items[i].Placeholder = v
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
		if v, ok := env("SPOT_DEFAULT_" + strings.ToUpper(sanitizeID(it.ID))); ok {
			it.Default = v
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
	for i := range c.Items {
		it := &c.Items[i]
		if it.Pattern != "" {
			re, err := regexp.Compile(it.Pattern)
			if err != nil {
				return fmt.Errorf("item %q: pattern: %w", it.ID, err)
			}
			it.pattern = re
		}
		if it.Type == "scale" {
			if it.minValue() > it.maxValue() {
				return fmt.Errorf("item %q: min is greater than max", it.ID)
			}
		}
	}
	return nil
}

// valid reports whether value satisfies the item's required / pattern rules.
func (it *Item) valid(value string) bool {
	if value == "" {
		return !it.Required
	}
	return it.pattern == nil || it.pattern.MatchString(value)
}

// defaultString renders Default as text whatever its TOML type.
func (it *Item) defaultString() string {
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
		return formatNumber(v)
	default:
		return fmt.Sprint(v)
	}
}

func (it *Item) defaultBool() bool {
	switch v := it.Default.(type) {
	case bool:
		return v
	case string:
		on := it.onValue()
		return v == on || strings.EqualFold(v, "true")
	}
	return false
}

func (it *Item) onValue() string {
	if it.On != nil {
		return *it.On
	}
	return "true"
}

func (it *Item) offValue() string {
	if it.Off != nil {
		return *it.Off
	}
	return "false"
}

func (it *Item) minValue() float64 {
	if it.Min != nil {
		return *it.Min
	}
	return 0
}

func (it *Item) maxValue() float64 {
	if it.Max != nil {
		return *it.Max
	}
	return 100
}

func (it *Item) stepValue() float64 {
	if it.Step != nil && *it.Step > 0 {
		return *it.Step
	}
	return 1
}

// scaleDefault is the slider's starting position, clamped into [min, max].
func (it *Item) scaleDefault() float64 {
	v := it.minValue()
	if s := it.defaultString(); s != "" {
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			v = f
		}
	}
	return max(it.minValue(), min(it.maxValue(), v))
}

func (it *Item) separator() string {
	if it.Separator != nil {
		return *it.Separator
	}
	return "\n"
}

// formatNumber prints 50 as "50" and 0.5 as "0.5".
func formatNumber(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// expand substitutes {id} and {_active} in a single pass, so a value that
// itself contains "{other}" is never substituted again.
func expand(tmpl, active string, values map[string]string) string {
	pairs := make([]string, 0, 2*len(values)+2)
	pairs = append(pairs, "{_active}", active)
	for id, v := range values {
		pairs = append(pairs, "{"+id+"}", v)
	}
	return strings.NewReplacer(pairs...).Replace(tmpl)
}

// buildJSON renders the submitted values as a JSON object. multi holds the
// items that submit several values (they become arrays); _active is always
// included.
func buildJSON(items []Item, values map[string]string, multi map[string][]string, active string) (string, error) {
	obj := map[string]any{"_active": active}
	for _, it := range items {
		if it.ID == "" {
			continue
		}
		if m, ok := multi[it.ID]; ok {
			if m == nil {
				m = []string{}
			}
			obj[it.ID] = m
		} else if v, ok := values[it.ID]; ok {
			obj[it.ID] = v
		}
	}
	b, err := json.Marshal(obj)
	return string(b), err
}

type hAlign int
type vAlign int

const (
	hCenter hAlign = iota
	hStart
	hEnd
)

const (
	vCenter vAlign = iota
	vStart
	vEnd
)

// parseAnchor reads "top right" style anchors; unknown words are ignored.
func parseAnchor(anchor string) (hAlign, vAlign) {
	h, v := hCenter, vCenter
	for _, part := range strings.Fields(strings.ToLower(anchor)) {
		switch part {
		case "top":
			v = vStart
		case "bottom":
			v = vEnd
		case "left":
			h = hStart
		case "right":
			h = hEnd
		}
	}
	return h, v
}

// parseKeyboard normalises the layer-shell keyboard mode.
func parseKeyboard(mode string) string {
	switch m := strings.ToLower(strings.TrimSpace(mode)); m {
	case "none":
		return "none"
	case "on-demand", "ondemand":
		return "on-demand"
	default:
		return "exclusive"
	}
}

// sanitizeID strips characters that GApplication ID validation rejects.
func sanitizeID(s string) string {
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

// appID builds the per-invocation GApplication ID for a config name. Every
// dot-separated part of a valid ID must start with a letter, so a name such as
// "01-form" cannot be used as is.
func appID(name string, pid int) string {
	return fmt.Sprintf("dev.kopy.spot.c_%s.p%d", sanitizeID(name), pid)
}
