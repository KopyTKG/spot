package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModKeys(t *testing.T) {
	item := func(s string) string { return "[[items]]\n" + s }
	for name, c := range map[string]struct{ toml, says string }{
		"a value pasted into exec":  {item("type='list'\nid='h'\nfrom='a.b'\n[submit]\nexec='ping {h}'"), "SPOT_VALUE_H"},
		"parsed, pasted":            {item("type='list'\nid='h'\nrun='x'\nparse='p'\n[submit]\nexec='ping {h}'"), "SPOT_VALUE_H"},
		"pasted and spawned":        {item("type='list'\nid='h'\nfrom='a.b'\n[submit]\nexec='ping {h}'\nspawn=true"), "SPOT_VALUE_H"},
		"from on a field":           {item("type='field'\nid='f'\nfrom='a.b'"), "from only applies"},
		"from without a dot":        {item("type='list'\nid='l'\nfrom='a'"), "<mod>.<value>"},
		"from a path":               {item("type='list'\nid='l'\nfrom='../a.b'"), "<mod>.<value>"},
		"from and options":          {item("type='list'\nid='l'\nfrom='a.b'\noptions=['x']"), "takes no options"},
		"from and state":            {item("type='switch'\nid='s'\nfrom='a.b'\nstate='echo'"), "take the place"},
		"call and on_change":        {item("type='switch'\nid='s'\ncall='a.b'\non_change='echo'"), "takes the place"},
		"call on a plain list":      {item("type='list'\nid='l'\noptions=['x']\ncall='a.b'"), "call only applies"},
		"button with neither":       {item("type='button'\nlabel='x'"), "exec or call"},
		"run without parse":         {item("type='list'\nid='l'\nrun='ss'"), "go together"},
		"parse a path":              {item("type='list'\nid='l'\nrun='ss'\nparse='../x'"), "name of a parser"},
		"an id with shell in it":    {item("type='list'\nid='a;b'\nfrom='a.b'"), "letters, digits"},
		"dash allowance for a form": {item("type='field'\nid='f'\nallow_leading_dash=true"), "only applies"},
	} {
		_, err := ParseConfig([]byte(c.toml), noEnv)
		if err == nil || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%s: %v, want %q", name, err, c.says)
		}
	}
	fine, err := ParseConfig([]byte(item("type='list'\nid='h'\nfrom='a.b'\n[submit]\nexec='ping -- \"$SPOT_VALUE_H\"'\nstdout='{h}'")), noEnv)
	if err != nil || !fine.UsesMods() {
		t.Errorf("the safe way: %v", err)
	}
	// An old config keeps working, with a warning.
	old, err := ParseConfig([]byte(item("type='list'\nid='f'\nstdin=true\n[submit]\nexec='rm {f}'")), noEnv)
	if err != nil || len(old.UnquotedStdin()) != 1 || old.UsesMods() {
		t.Errorf("stdin: %v %q", err, old.UnquotedStdin())
	}
	if err := fine.LeadingDash(map[string]string{"h": "-x"}); err == nil {
		t.Error("a leading dash")
	}
}

func TestRowsAndScalars(t *testing.T) {
	rows, err := Rows(json.RawMessage(`["a",{"label":"b\nc","key":"k","state":true,"extra":1},{"label":"d","state":"on"}]`))
	if err != nil || len(rows) != 3 || rows[1].Label != "b c" || rows[1].Key != "k" || rows[1].State == nil || rows[2].Key != "d" || rows[2].State != nil {
		t.Errorf("%+v %v", rows, err)
	}
	for _, bad := range []string{`{}`, `"x"`, `[1]`, `[{"key":"k"}]`, `[{"label":1}]`, `[{"label":"a","key":"\u0000"}]`, ``} {
		if _, err := Rows(json.RawMessage(bad)); err == nil {
			t.Errorf("%q: accepted", bad)
		}
	}
	for raw, want := range map[string]string{`true`: "true", `50`: "50", `0.5`: "0.5", `"x"`: "x"} {
		if got, err := Scalar(json.RawMessage(raw)); got != want || err != nil {
			t.Errorf("%s: %q %v", raw, got, err)
		}
	}
	for _, bad := range []string{`null`, `[1]`, `{}`, ``} {
		if _, err := Scalar(json.RawMessage(bad)); err == nil {
			t.Errorf("%q: accepted as a position", bad)
		}
	}
}

func TestMisspelledKeysAreWarnedAbout(t *testing.T) {
	got := UnknownKeys([]byte("titel='x'\n[[items]]\ntype='list'\nid='x'\nform='a.b'\n"))
	if len(got) != 2 || !strings.Contains(strings.Join(got, "|"), "form") {
		t.Errorf("%q", got)
	}
	// Nothing of what ships may be warned about.
	files, _ := filepath.Glob("../../examples/*.toml")
	more, _ := filepath.Glob("../../examples/*/*.toml")
	own, _ := filepath.Glob(os.ExpandEnv("$HOME/.config/spot/*.toml"))
	checked := 0
	for _, f := range append(append(files, more...), own...) {
		base := filepath.Base(f)
		if base == "daemon.toml" || base == "mods.toml" || base == "mod.toml" {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		checked++
		if w := UnknownKeys(data); len(w) > 0 {
			t.Logf("%s: %q", f, w)
			if !strings.HasPrefix(f, os.Getenv("HOME")+"/.config") {
				t.Errorf("a shipped config is warned about: %s", f)
			}
		}
	}
	t.Logf("%d configs checked", checked)
}

func TestSubmitCallAndCopy(t *testing.T) {
	list := "[[items]]\ntype='list'\nid='h'\nfrom='a.b'\n[[items]]\ntype='password'\nid='pw'\n[submit]\n"
	for name, c := range map[string]struct{ submit, says string }{
		"call without a dot":      {"call='a'", "<mod>.<request>"},
		"call and exec":           {"call='a.b'\nexec='x'", "takes the place"},
		"call and spawn":          {"call='a.b'\nspawn=true", "takes the place"},
		"args alone":              {"args=['{h}']", "goes with submit.call"},
		"a password to a mod":     {"call='a.b'\nargs=['{h}','{pw}']", "is a password"},
		"a password to clipboard": {"copy='x {pw}'", "is a password"},
	} {
		if _, err := ParseConfig([]byte(list+c.submit), noEnv); err == nil || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%s: %v", name, err)
		}
	}
	c, err := ParseConfig([]byte(list+"call='a.b'\nargs=['{h}']\ncopy='{h}'"), noEnv)
	if err != nil || !c.UsesMods() {
		t.Errorf("%v", err)
	}
}
