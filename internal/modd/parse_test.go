package modd

import (
	"strings"
	"testing"
)

func TestOnlyAnApprovedParserRuns(t *testing.T) {
	f := newFixture(t)
	f.write("mods/up/mod.toml", "type='parser'\napi=1\nversion='1'\ndescription='d'\ninput='i'\n[returns]\nlabel='string'\n")
	f.write("mods/up/init.lua", `return function(i) if i == "bad" then error("cannot read it") end return { { label = i:upper() } } end`)
	parse := func(what, name, input, want string) {
		t.Helper()
		got, err := f.Parse(name, []byte(input))
		if e, fails := strings.CutPrefix(want, "!"); fails {
			if err == nil || !strings.Contains(err.Error(), e) {
				t.Errorf("%s: %s %v; want %q", what, got, err, e)
			}
		} else if err != nil || string(got) != want {
			t.Errorf("%s: %s %v; want %s", what, got, err, want)
		}
	}
	parse("not approved", "up", "x", "!not approved")
	approve := func(name string) {
		m, err := f.store.Read(name)
		if err == nil {
			err = f.store.Approve(m)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	approve("up")
	parse("approved", "up", "x", `[{"label":"X"}]`)
	parse("its own error", "up", "bad", "!cannot read it")
	f.write("mods/up/extra.lua", "return 1")
	parse("changed", "up", "x", "!not approved")
	// As part of a provider that was approved with it.
	f.write("mods/m/mod.toml", "uses=['up']\n")
	f.write("mods/m/init.lua", "return 1")
	approve("m")
	parse("approved with a provider", "up", "x", `[{"label":"X"}]`)
	f.write("mods/up/init.lua", `return function() return {} end`)
	parse("changed since the provider was approved", "up", "x", "!not approved")
	parse("a provider", "m", "x", "!not a parser")
	parse("a path", "../up", "x", "!not a parser's name")
	parse("nothing there", "zz", "x", "!no init.lua")
	parse("too much", "up", strings.Repeat("x", 9<<20), "!8 MB")
}
