package modd

import (
	"os"
	"strings"
	"testing"
)

func (f fixture) text(path string) string {
	b, _ := os.ReadFile(path)
	return string(b)
}

func TestTheModLog(t *testing.T) {
	f := newFixture(t)
	f.write("mods/m/init.lua", `
spot.log("hello\n2026-01-01 00:00:00 works again")
spot.value("ok", function() return 1 end)
spot.value("bad", function() if spot.exists("~/readable/break") then error("broken\nworks again") end return 1 end)`)
	f.approve()
	f.get("fine", "bad", "1")
	f.write("readable/break", "")
	for range 3 {
		f.Get("m.bad")
	}
	os.Remove(f.home + "/readable/break")
	f.get("fine again", "bad", "1")
	path := LogPath(f.store, "m")
	st, err := os.Stat(path)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("%v %v", err, st)
	}
	lines := strings.Split(strings.TrimSpace(f.text(path)), "\n")
	var kinds []string
	for _, l := range lines {
		kinds = append(kinds, strings.Fields(l[20:])[0])
	}
	// Said once, failed (the same way: once), works again. Nothing a mod
	// wrote made a line of its own.
	if got := strings.Join(kinds, " "); got != "says error works" {
		t.Errorf("%s\n%s", got, f.text(path))
	}
}

func TestAChangedModSaysWhatItNowAsksFor(t *testing.T) {
	f := newFixture(t)
	f.approve()
	f.get("before", "count", "1")
	f.write("mods/m/mod.toml", "[permissions.read]\n\"~/readable\"='r'\n[permissions.net]\n\"443\"='n'\n[permissions.run]\nssh='s'\n")
	f.get("changed", "count", "!it now asks for net 443, run ssh")
	if n := f.heard.all(); len(n) != 1 || !strings.Contains(n[0], "run ssh") {
		t.Errorf("notified: %q", n)
	}
	f.approve()
	f.get("again", "count", "1")
	f.write("mods/m/other.lua", "return 1")
	f.get("only code", "count", "!it asks for nothing new")
}

func TestABrokenParserFailsItsUsersAtLoad(t *testing.T) {
	f := newFixture(t)
	f.write("mods/up/mod.toml", "type='parser'\napi=1\nversion='1'\ndescription='d'\ninput='i'\n[returns]\na='string?'\n")
	f.write("mods/up/init.lua", `return 1, 2`)
	f.write("mods/m/mod.toml", "uses=['up']\n")
	f.write("mods/m/init.lua", `spot.value("ok", function() return 1 end)`)
	f.approve()
	// The value does not even use the parser.
	f.get("at load", "ok", "!parser up")
	if s := f.Statuses(); len(s) != 1 || s[0].Fault != "parser up" {
		t.Errorf("%+v", s)
	}
}

func TestDaemonSettingsOpenToOthersRunNothing(t *testing.T) {
	f := newFixture(t)
	f.approve()
	f.write("daemon.toml", "grace = 1\n")
	f.get("fine", "count", "1")
	os.Chmod(f.home+"/daemon.toml", 0o666)
	if _, warning, err := f.Get("m.count"); err == nil && !strings.Contains(warning, "changed by others") {
		t.Errorf("%v %q", err, warning)
	}
	if f.running() {
		t.Error("it runs")
	}
}
