package approval

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type home struct {
	t *testing.T
	Store
}

func newHome(t *testing.T) home {
	d := t.TempDir()
	os.Chmod(d, 0o700)
	h := home{t, Store{Mods: d + "/mods", Records: d + "/approvals", Home: d}}
	h.write("mods/up/mod.toml", "type='parser'\napi=1\nversion='1'\ndescription='d'\ninput='i'\n[returns]\nx='string?'\n")
	h.write("mods/up/init.lua", "return function() return {} end\n")
	h.write("mods/m/init.lua", "local up = spot.use('up')\nspot.run({ 'tool' })\n")
	h.write("mods/m/lib.lua", "return 1\n")
	h.write("bin/tool", "#!/usr/bin/sh\necho 1\n")
	os.Chmod(d+"/bin/tool", 0o700)
	h.write("mods/m/mod.toml", "uses=['up']\n[permissions.run]\n\""+d+"/bin/tool\"='my own'\n")
	return h
}

func (h home) write(rel, text string) {
	p := filepath.Join(h.Home, rel)
	os.MkdirAll(filepath.Dir(p), 0o700)
	if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
		h.t.Fatal(err)
	}
}

func (h home) state(name string) State {
	h.t.Helper()
	s, _, _, err := h.State(name)
	if err != nil {
		h.t.Fatal(err)
	}
	return s
}

func (h home) approve(name string) *Mod {
	h.t.Helper()
	m, err := h.Read(name)
	if err != nil {
		h.t.Fatal(err)
	}
	if err := h.Approve(m); err != nil {
		h.t.Fatal(err)
	}
	return m
}

// Whatever changes, the approval is for what was there before.
func TestEveryChangeVoidsTheApproval(t *testing.T) {
	for name, change := range map[string]func(h home){
		"init.lua": func(h home) { h.write("mods/m/init.lua", "-- other\n") },
		"one byte": func(h home) { h.write("mods/m/lib.lua", "return 2\n") },
		"mod.toml": func(h home) { h.write("mods/m/mod.toml", "uses=['up']\n") },
		"only a reason": func(h home) {
			h.write("mods/m/mod.toml", "uses=['up']\n[permissions.run]\n\""+h.Home+"/bin/tool\"='other words'\n")
		},
		"a new file":         func(h home) { h.write("mods/m/more.lua", "return 1\n") },
		"a file in a folder": func(h home) { h.write("mods/m/a/b.lua", "return 1\n") },
		"a file removed":     func(h home) { os.Remove(h.Home + "/mods/m/lib.lua") },
		"a file renamed":     func(h home) { os.Rename(h.Home+"/mods/m/lib.lua", h.Home+"/mods/m/lib2.lua") },
		"the parser":         func(h home) { h.write("mods/up/init.lua", "return function() return {{}} end\n") },
		"the parser's manifest": func(h home) {
			h.write("mods/up/mod.toml", "type='parser'\napi=1\nversion='2'\ndescription='d'\ninput='i'\n[returns]\nx='string?'\n")
		},
		"your own program": func(h home) { h.write("bin/tool", "#!/usr/bin/sh\necho 2\n") },
	} {
		h := newHome(t)
		h.approve("m")
		if h.state("m") != Approved {
			t.Fatalf("%s: not approved to begin with", name)
		}
		change(h)
		if h.state("m") != Changed {
			t.Errorf("%s: still counts as approved", name)
		}
	}
}

func TestMovingBytesBetweenFilesChangesTheHash(t *testing.T) {
	h := newHome(t)
	h.write("mods/m/a.lua", "xy")
	h.write("mods/m/b.lua", "z")
	h.approve("m")
	h.write("mods/m/a.lua", "x")
	h.write("mods/m/b.lua", "yz")
	if h.state("m") != Changed {
		t.Error("the same bytes split differently pass")
	}
}

func TestWhatOthersCanChangeIsNotTrusted(t *testing.T) {
	for name, spoil := range map[string]func(h home) error{
		"a mod file open to others": func(h home) error { return os.Chmod(h.Home+"/mods/m/init.lua", 0o666) },
		"the mod folder":            func(h home) error { return os.Chmod(h.Home+"/mods/m", 0o777) },
		"the mods folder":           func(h home) error { return os.Chmod(h.Home+"/mods", 0o770) },
		"the parser":                func(h home) error { return os.Chmod(h.Home+"/mods/up/init.lua", 0o602) },
		"the approval record":       func(h home) error { return os.Chmod(h.Home+"/approvals/m.json", 0o666) },
		"the folder of the records": func(h home) error { return os.Chmod(h.Home+"/approvals", 0o777) },
		"a damaged record":          func(h home) error { return os.WriteFile(h.Home+"/approvals/m.json", []byte("{"), 0o600) },
		"another mod's record": func(h home) error {
			b, _ := os.ReadFile(h.Home + "/approvals/m.json")
			return os.WriteFile(h.Home+"/approvals/up.json", b, 0o600)
		},
	} {
		h := newHome(t)
		h.approve("m")
		if err := spoil(h); err != nil {
			t.Fatal(err)
		}
		target := "m"
		if name == "another mod's record" {
			target = "up"
		}
		if s, _, _, err := h.State(target); err == nil || s == Approved {
			t.Errorf("%s: state %v, %v", name, s, err)
		}
	}
}

func TestWhatHasToBeTyped(t *testing.T) {
	h := newHome(t)
	h.write("mods/net/init.lua", "return 1\n")
	h.write("mods/net/mod.toml", `
[permissions.run]
ls = "list"
curl = "fetch"
[permissions.read]
"~/.ssh" = "hosts"
"~/notes" = "notes"
[permissions.write]
"~/.config/autostart" = "start"
[permissions.net]
"443" = "weather"
[permissions.env]
GITHUB_TOKEN = "api"
LANG = "language"
[permissions.setenv]
LC_ALL = "c"
GIT_SSH_COMMAND = "x"
`)
	m, err := h.Read("net")
	if err != nil {
		t.Fatal(err)
	}
	items := Items(m, nil)
	words := Words(m, items)
	slices.Sort(words)
	if want := []string{"curl", "env", "ls", "net", "read", "setenv", "write"}; !slices.Equal(words, want) {
		t.Errorf("words: %v, want %v", words, want)
	}
	if len(Combinations(items)) != 2 {
		t.Errorf("combinations: %q", Combinations(items))
	}
	if items[0].Danger == "" {
		t.Error("what is dangerous does not come first")
	}
	for line, ok := range map[string]bool{
		"curl env ls net read setenv write":       true,
		"write, setenv, read, net, ls, env, curl": true,
		"curl env ls net read setenv":             false,
		"curl env ls net read setenv write yes":   false,
		"yes":                                     false, "y": false, "": false, "all": false, "*": false,
	} {
		if got := Accepted(line+"\n", Words(m, items)); got != ok {
			t.Errorf("%q: accepted = %v", line, got)
		}
	}
	// Only what is new or dangerous is asked for again, and every program.
	h.approve("net")
	s, _ := os.ReadFile(h.Home + "/mods/net/mod.toml")
	h.write("mods/net/mod.toml", string(s)+"\n[permissions.socket]\n\"/run/dbus/x\"='bus'\n")
	_, m2, old, err := h.State("net")
	if err != nil {
		t.Fatal(err)
	}
	it := Items(m2, old)
	if !it[0].New || it[0].Kind != "socket" || !slices.Contains(Words(m2, it), "socket") {
		t.Errorf("the new one: %+v", it[0])
	}
}

func TestAModCannotDressUpTheScreen(t *testing.T) {
	h := newHome(t)
	h.write("mods/m/mod.toml", "version = \"1\\u001b[2J\"\ndescription = \"ok\\u202e\\nApproved already\"\n[permissions.run]\nls = \"list\\r\\u001b[32mSAFE\\nsecond line\\u200b "+strings.Repeat("x", 500)+"\"\n")
	h.write("mods/m/init.lua", "-- \x1b[8mhidden\u202e\nreturn 1\n")
	m, err := h.Read("m")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	Review(&out, m, nil)
	for _, bad := range []string{"\x1b", "\u202e", "\u200b", "\r", "\nApproved already", "\nsecond line", strings.Repeat("x", 201)} {
		if strings.Contains(out.String(), bad) {
			t.Errorf("the review holds %q:\n%s", bad, &out)
		}
	}
	if got := Visible(m.Texts()["m/init.lua"]); !strings.Contains(got, `\x1b[8mhidden\u{202e}`) {
		t.Errorf("code must be shown as it is: %q", got)
	}
}
