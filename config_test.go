package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/diamondburned/gotk4/pkg/gio/v2"
)

func noEnv(string) (string, bool) { return "", false }

func envOf(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func TestExpand(t *testing.T) {
	vals := map[string]string{"user": "me", "host": "box", "tricky": "{user}"}
	cases := []struct{ tmpl, want string }{
		{"ssh {user}@{host}", "ssh me@box"},
		{"{_active}:{user}", "power:me"},
		{"{tricky}", "{user}"}, // values are not substituted twice
		{"{unknown} stays", "{unknown} stays"},
		{"", ""},
	}
	for _, c := range cases {
		if got := expand(c.tmpl, "power", vals); got != c.want {
			t.Errorf("expand(%q) = %q, want %q", c.tmpl, got, c.want)
		}
	}
}

func TestParseAnchor(t *testing.T) {
	cases := []struct {
		in string
		h  hAlign
		v  vAlign
	}{
		{"", hCenter, vCenter},
		{"center", hCenter, vCenter},
		{"top", hCenter, vStart},
		{"bottom", hCenter, vEnd},
		{"left", hStart, vCenter},
		{"top right", hEnd, vStart},
		{"  BOTTOM   Left ", hStart, vEnd},
		{"nonsense", hCenter, vCenter},
	}
	for _, c := range cases {
		h, v := parseAnchor(c.in)
		if h != c.h || v != c.v {
			t.Errorf("parseAnchor(%q) = %v,%v want %v,%v", c.in, h, v, c.h, c.v)
		}
	}
}

func TestParseKeyboard(t *testing.T) {
	cases := map[string]string{
		"": "exclusive", "exclusive": "exclusive", "none": "none", " NONE ": "none",
		"on-demand": "on-demand", "ondemand": "on-demand", "bogus": "exclusive",
	}
	for in, want := range cases {
		if got := parseKeyboard(in); got != want {
			t.Errorf("parseKeyboard(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitizeID(t *testing.T) {
	cases := map[string]string{"ssh": "ssh", "my-conf.v2": "my_conf_v2", "": "cfg"}
	for in, want := range cases {
		if got := sanitizeID(in); got != want {
			t.Errorf("sanitizeID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseConfig(t *testing.T) {
	cfg, err := parseConfig([]byte(`
title  = "Hello"
anchor = "top right"
width  = 420

[opacity]
outside = 50

[[items]]
type    = "field"
id      = "port"
default = "22"
pattern = "^[0-9]+$"
required = true

[[items]]
type    = "scale"
id      = "vol"
min     = 0
max     = 100
step    = 5
default = 50

[[items]]
type    = "toggle"
id      = "sudo"
default = true
on      = "yes"

[submit]
exec  = "echo {port}"
spawn = true
`), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Title != "Hello" || cfg.Anchor != "top right" || cfg.Width != 420 {
		t.Errorf("top level: %+v", cfg)
	}
	if cfg.Opacity == nil || *cfg.Opacity.Outside != 50 {
		t.Errorf("opacity: %+v", cfg.Opacity)
	}
	if len(cfg.Items) != 3 {
		t.Fatalf("items: %d", len(cfg.Items))
	}
	if got := cfg.Items[0].defaultString(); got != "22" {
		t.Errorf("field default = %q", got)
	}
	scale := cfg.Items[1]
	if scale.minValue() != 0 || scale.maxValue() != 100 || scale.stepValue() != 5 || scale.scaleDefault() != 50 {
		t.Errorf("scale: min %v max %v step %v default %v", scale.minValue(), scale.maxValue(), scale.stepValue(), scale.scaleDefault())
	}
	toggle := cfg.Items[2]
	if !toggle.defaultBool() || toggle.onValue() != "yes" || toggle.offValue() != "false" {
		t.Errorf("toggle: %+v", toggle)
	}
	if !cfg.Submit.Spawn || cfg.Submit.Exec != "echo {port}" {
		t.Errorf("submit: %+v", cfg.Submit)
	}
}

func TestParseConfigErrors(t *testing.T) {
	bad := map[string]string{
		"syntax":       `title = `,
		"bad pattern":  "[[items]]\ntype=\"field\"\nid=\"a\"\npattern=\"(\"",
		"bad format":   "[submit]\nformat = \"xml\"",
		"both stdout":  "[submit]\nformat = \"json\"\nstdout = \"{a}\"",
		"bad on_error": "[submit]\non_error = \"maybe\"",
		"min > max":    "[[items]]\ntype=\"scale\"\nid=\"a\"\nmin=10\nmax=1",
	}
	for name, src := range bad {
		if _, err := parseConfig([]byte(src), noEnv); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestEnvOverrides(t *testing.T) {
	src := `
title  = "Orig"
footer = "orig footer"

[[items]]
type = "tiles"
id   = "t"

[[items]]
type = "password"
id   = "pw"

[[items]]
type = "field"
id   = "f"
`
	cfg, err := parseConfig([]byte(src), envOf(map[string]string{
		"SPOT_TITLE": "New", "SPOT_FOOTER": "new footer",
		"SPOT_PLACEHOLDER": "hint", "SPOT_DEFAULT": "dflt",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Title != "New" || cfg.Footer != "new footer" {
		t.Errorf("title/footer: %q %q", cfg.Title, cfg.Footer)
	}
	// Placeholder goes to the first field/password/list/text item, default to
	// the first item that can take one.
	if cfg.Items[1].Placeholder != "hint" || cfg.Items[2].Placeholder != "" {
		t.Errorf("placeholder: %q %q", cfg.Items[1].Placeholder, cfg.Items[2].Placeholder)
	}
	if cfg.Items[0].defaultString() != "dflt" || cfg.Items[2].defaultString() != "" {
		t.Errorf("default: %q %q", cfg.Items[0].defaultString(), cfg.Items[2].defaultString())
	}
}

func TestEnvDefaultReachesScaleAndToggle(t *testing.T) {
	cfg, err := parseConfig([]byte("[[items]]\ntype=\"scale\"\nid=\"v\"\nmax=10\n"),
		envOf(map[string]string{"SPOT_DEFAULT": "7"}))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Items[0].scaleDefault(); got != 7 {
		t.Errorf("scale default = %v", got)
	}
}

func TestScaleDefaultClamped(t *testing.T) {
	it := Item{Type: "scale", Default: int64(500)}
	if got := it.scaleDefault(); got != 100 {
		t.Errorf("clamped default = %v", got)
	}
}

func TestValid(t *testing.T) {
	cfg, err := parseConfig([]byte(`
[[items]]
type = "field"
id = "port"
required = true
pattern = "^[0-9]+$"

[[items]]
type = "field"
id = "opt"
pattern = "^[a-z]+$"
`), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	port, opt := cfg.Items[0], cfg.Items[1]
	for v, want := range map[string]bool{"": false, "22": true, "x": false} {
		if got := port.valid(v); got != want {
			t.Errorf("required port.valid(%q) = %v", v, got)
		}
	}
	for v, want := range map[string]bool{"": true, "abc": true, "ABC": false} {
		if got := opt.valid(v); got != want {
			t.Errorf("optional opt.valid(%q) = %v", v, got)
		}
	}
}

func TestBuildJSON(t *testing.T) {
	items := []Item{{ID: "user"}, {ID: "tags"}, {ID: "note"}, {}}
	got, err := buildJSON(items,
		map[string]string{"user": `a "quoted" name`, "note": "line1\nline2"},
		map[string][]string{"tags": {"x", "y"}}, "grp")
	if err != nil {
		t.Fatal(err)
	}
	want := `{"_active":"grp","note":"line1\nline2","tags":["x","y"],"user":"a \"quoted\" name"}`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	empty, _ := buildJSON([]Item{{ID: "tags"}}, nil, map[string][]string{"tags": nil}, "")
	if empty != `{"_active":"","tags":[]}` {
		t.Errorf("empty multi: %s", empty)
	}
}

func TestResolveConfigPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	existing := filepath.Join(dir, "plain")
	if err := os.WriteFile(existing, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ in, want string }{
		{existing, existing},                            // an existing file wins, whatever its name
		{"ssh", filepath.Join(dir, "spot", "ssh.toml")}, // bare name -> XDG lookup
		{"sub/ssh", "sub/ssh"},                          // has a separator: a path
		{"ssh.toml", "ssh.toml"},                        // .toml suffix: a path
		{"", ""},
	}
	for _, c := range cases {
		if got := resolveConfigPath(c.in); got != c.want {
			t.Errorf("resolveConfigPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestResolveConfigPathHomeFallback(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	want := filepath.Join(home, ".config", "spot", "ssh.toml")
	if got := resolveConfigPath("ssh"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatNumber(t *testing.T) {
	for in, want := range map[float64]string{50: "50", 0.5: "0.5", -3: "-3", 2.25: "2.25"} {
		if got := formatNumber(in); got != want {
			t.Errorf("formatNumber(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestSeparator(t *testing.T) {
	if (&Item{}).separator() != "\n" {
		t.Error("default separator should be a newline")
	}
	s := ","
	if got := (&Item{Separator: &s}).separator(); !reflect.DeepEqual(got, ",") {
		t.Errorf("separator = %q", got)
	}
}

// Every shipped example must stay valid, so the docs never rot.
func TestExamplesParse(t *testing.T) {
	files, err := filepath.Glob("examples/*.toml")
	if err != nil {
		t.Fatal(err)
	}
	demos, _ := filepath.Glob("examples/demo/*.toml")
	files = append(files, demos...)
	if len(files) == 0 {
		t.Fatal("no examples found")
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := parseConfig(data, noEnv); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
}

// GApplication rejects an ID with a part that starts with a digit; config files
// named like "01-form.toml" must still produce a valid one.
func TestAppIDValid(t *testing.T) {
	for _, name := range []string{"ssh", "01-form", "9", "my conf.v2", "", "ünï"} {
		id := appID(name, 123)
		if !gio.ApplicationIDIsValid(id) {
			t.Errorf("appID(%q) = %q is not a valid application ID", name, id)
		}
	}
}

func TestEnvDefaultByID(t *testing.T) {
	src := `
[[items]]
type = "tiles"
id   = "action"

[[items]]
type    = "tiles"
id      = "battery-mode"
default = "balanced"

[[items]]
type = "password"
id   = "pw"
`
	cfg, err := parseConfig([]byte(src), envOf(map[string]string{
		"SPOT_DEFAULT":              "first",
		"SPOT_DEFAULT_BATTERY_MODE": "performance",
		"SPOT_DEFAULT_PW":           "ignored", // password takes no default
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Items[0].defaultString(); got != "first" {
		t.Errorf("SPOT_DEFAULT should still reach the first item, got %q", got)
	}
	if got := cfg.Items[1].defaultString(); got != "performance" {
		t.Errorf("SPOT_DEFAULT_BATTERY_MODE: got %q", got)
	}
	if got := cfg.Items[2].defaultString(); got != "" {
		t.Errorf("password should not take a default, got %q", got)
	}

	// Without the per-item variable the config's own default stays.
	cfg, _ = parseConfig([]byte(src), noEnv)
	if got := cfg.Items[1].defaultString(); got != "balanced" {
		t.Errorf("config default lost: %q", got)
	}
}
