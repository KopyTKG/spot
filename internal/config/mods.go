package config

// Dialogs that use mods: `from` shows a value of a mod, `call` makes its
// request, `run` with `parse` shows a command's output through a parser.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/pelletier/go-toml/v2"
)

// ModSource is how a dialog reaches mods: in the daemon directly, elsewhere
// through it.
type ModSource interface {
	// Get is a value; warning says why it is an old one.
	Get(full string) (result json.RawMessage, warning string, err error)
	Call(full string, args []string) error
	Parse(parser string, input []byte) (json.RawMessage, error)
}

// Row is one row of a mod's value or a parser's output.
type Row struct {
	Label, Key string
	Group      string // a menu: which pick on the page before it belongs to
	Icon       string // a name from the icon theme, never a file
	State      *bool  // a switch row
}

// themeIcon is a name an icon theme can hold. A path is not one: a mod's rows
// cannot name an image file (S31).
var themeIcon = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,127}$`)

var (
	modItem   = regexp.MustCompile(`^[a-z][a-z0-9_-]*\.[a-z][a-z0-9_]{0,63}$`)
	modName   = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
	modItemID = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*$`)
)

// foreign reports whether the item's value can be chosen by someone else: a mod
// or a parser's input.
func (it *Item) foreign() bool { return it.From != "" || it.Parse != "" }

// UsesMods reports whether the dialog reaches any mod.
func (c *Config) UsesMods() bool {
	if c.Submit.Call != "" {
		return true
	}
	for _, p := range c.Pages {
		if p.Dynamic() || p.Call != "" {
			return true
		}
	}
	for _, it := range c.Items {
		if it.foreign() || it.Call != "" {
			return true
		}
	}
	return false
}

func (it *Item) validateMods() error {
	fail := func(format string, args ...any) error {
		return fmt.Errorf("item %q: "+format, append([]any{it.ID}, args...)...)
	}
	if it.From != "" {
		switch it.Type {
		case "list":
			if len(it.Options) > 0 || it.Stdin || it.Multi || it.Collapsible || it.Run != "" || it.Parse != "" {
				return fail("a list with from takes no options, stdin, multi, collapsible, run or parse")
			}
		case "switch", "segmented", "scale":
		default:
			return fail("from only applies to lists, switch, segmented and scale")
		}
		if !modItem.MatchString(it.From) {
			return fail("from: say <mod>.<value>")
		}
		if it.State != "" || it.OnChange != "" {
			return fail("from and call take the place of state and on_change")
		}
	}
	if it.Call != "" {
		switch {
		case it.Type == "list" && it.From != "", it.Type == "switch", it.Type == "segmented", it.Type == "scale", it.Type == "button":
		default:
			return fail("call only applies to lists with from, switch, segmented, scale and button")
		}
		if !modItem.MatchString(it.Call) {
			return fail("call: say <mod>.<request>")
		}
		if it.OnChange != "" || it.Exec != "" || it.State != "" {
			return fail("call takes the place of on_change and exec")
		}
	}
	if it.Run != "" || it.Parse != "" {
		switch {
		case it.Type != "list":
			return fail("run and parse only apply to lists")
		case it.Run == "" || it.Parse == "":
			return fail("run and parse go together")
		case len(it.Options) > 0 || it.Stdin || it.Multi || it.Collapsible:
			return fail("a list with parse takes no options, stdin, multi or collapsible")
		case !modName.MatchString(it.Parse):
			return fail("parse: the name of a parser")
		}
	}
	if it.AllowLeadingDash && !it.foreign() {
		return fail("allow_leading_dash only applies to items with from or parse")
	}
	if it.foreign() && it.ID != "" && !modItemID.MatchString(it.ID) {
		return fail("the id of an item with from or parse is letters, digits, - and _")
	}
	return nil
}

// checkModValues refuses to paste what someone else chose into a shell command.
func (c *Config) checkModValues() error {
	for _, it := range c.Items {
		if it.foreign() && it.ID != "" && strings.Contains(c.Submit.Exec, "{"+it.ID+"}") {
			return fmt.Errorf("submit.exec: {%s} is pasted into the command, and a mod chooses that value: use \"$%s\" instead", it.ID, valueVar(it.ID))
		}
	}
	return nil
}

// checkSubmit checks submit.call and submit.copy.
func (c *Config) checkSubmit() error {
	if err := c.checkStdinInExec(); err != nil {
		return err
	}
	s := c.Submit
	switch {
	case s.Call != "" && !modItem.MatchString(s.Call):
		return errors.New("submit.call: say <mod>.<request>")
	case s.Call != "" && (s.Exec != "" || s.Spawn):
		return errors.New("submit.call takes the place of exec and spawn")
	case s.Call == "" && len(s.Args) > 0:
		return errors.New("submit.args only goes with submit.call")
	}
	// A typed password must not travel into a mod, whose actions are logged,
	// nor into the clipboard, whose history is kept.
	for _, it := range c.Items {
		if it.Type != "password" || it.ID == "" {
			continue
		}
		for _, t := range append([]string{s.Copy}, s.Args...) {
			if strings.Contains(t, "{"+it.ID+"}") {
				return fmt.Errorf("submit: {%s} is a password: it is not given to a mod or the clipboard", it.ID)
			}
		}
	}
	return nil
}

// checkStdinInExec refuses a stdin list whose value submit.exec pastes into the
// command: the rows come from outside, so a row could run anything.
func (c *Config) checkStdinInExec() error {
	for _, it := range c.Items {
		if it.Stdin && it.ID != "" && strings.Contains(c.Submit.Exec, "{"+it.ID+"}") {
			return fmt.Errorf("submit.exec: {%s} is from stdin and would be pasted into the command; use \"$%s\"", it.ID, valueVar(it.ID))
		}
	}
	return nil
}

// LeadingDash is the item whose foreign value would be read as an option.
func (c *Config) LeadingDash(values map[string]string) error {
	for _, it := range c.Items {
		if it.foreign() && !it.AllowLeadingDash && strings.HasPrefix(values[it.ID], "-") {
			return fmt.Errorf("%s: %q starts with a dash and would be read as an option (allow_leading_dash = true if that is meant)", it.ID, values[it.ID])
		}
	}
	return nil
}

func shown(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}

// Rows reads a value as the rows of a list: texts, or records with a label and
// optionally a key and a state.
func Rows(raw json.RawMessage) ([]Row, error) {
	var list []any
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, errors.New("this is not a list of rows")
	}
	rows := make([]Row, 0, len(list))
	for i, x := range list {
		var r Row
		switch x := x.(type) {
		case string:
			r = Row{Label: x, Key: x}
		case map[string]any:
			label, ok := x["label"].(string)
			if !ok {
				return nil, fmt.Errorf("row %d has no label", i+1)
			}
			r = Row{Label: label, Key: label}
			if k, ok := x["key"].(string); ok && k != "" {
				r.Key = k
			}
			if icon, ok := x["icon"].(string); ok && themeIcon.MatchString(icon) {
				r.Icon = icon
			}
			if on, ok := x["state"].(bool); ok {
				r.State = &on
			}
			if g, ok := x["group"].(string); ok {
				r.Group = g
			}
		default:
			return nil, fmt.Errorf("row %d is not a text or a record", i+1)
		}
		if strings.ContainsRune(r.Key, 0) {
			return nil, fmt.Errorf("row %d: a key with a NUL byte", i+1)
		}
		r.Label = shown(r.Label)
		rows = append(rows, r)
	}
	return rows, nil
}

// Scalar reads a value as the state of a control.
func Scalar(raw json.RawMessage) (string, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", err
	}
	switch v := v.(type) {
	case string:
		return v, nil
	case bool:
		return strconv.FormatBool(v), nil
	case float64:
		return FormatNumber(v), nil
	}
	return "", errors.New("this is not a single value")
}

// UnknownKeys are keys of a config that nothing reads: most often a misspelling,
// which would otherwise only show as something not happening. They have always
// been ignored, so they are warned about and not refused.
func UnknownKeys(data []byte) []string {
	var c Config
	d := toml.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	var unknown *toml.StrictMissingError
	if !errors.As(d.Decode(&c), &unknown) {
		return nil
	}
	var out []string
	for _, e := range unknown.Errors {
		line, _ := e.Position()
		out = append(out, fmt.Sprintf("line %d: unknown key %s, ignored", line, shown(strings.Join(e.Key(), "."))))
	}
	return out
}
