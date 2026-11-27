package modd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTheMemorySetting(t *testing.T) {
	f := newFixture(t)
	f.write("mods/m/init.lua", `
spot.value("fine", function() return 1 end)
spot.on("use", function() local t = {} for i = 1, 150 do t[i] = ("x"):rep(1000000) .. i end return #t end)`)
	f.approve()
	f.get("usual", "fine", "1")
	limit := func() string {
		p, _ := f.provider("m")
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.h == nil || p.h.Cgroup() == "" {
			return ""
		}
		b, _ := os.ReadFile(filepath.Join(p.h.Cgroup(), "memory.max"))
		return strings.TrimSpace(string(b))
	}
	if got := limit(); got != "" && got != "268435456" {
		t.Errorf("usual limit: %s", got)
	}
	f.enable("[mod.m]\nmemory=512\n")
	f.get("raised", "fine", "1")
	if got := limit(); got != "" && got != "536870912" {
		t.Errorf("raised: %s", got)
	}
	if res, err := f.Call("m.use", nil); err != nil || string(res) != "150" {
		t.Errorf("150 MB with 512 allowed: %s %v", res, err)
	}
	f.enable("[mod.m]\nmemory=64\n")
	if _, err := f.Call("m.use", nil); err == nil {
		t.Error("150 MB with 64 allowed")
	}
	for _, bad := range []string{"memory='1G'", "memory=1", "memory=99999999", "memory=-5", "memory=1.5"} {
		f.enable("[mod.m]\n" + bad + "\n")
		if _, warning, err := f.Get("m.fine"); err == nil && !strings.Contains(warning, "megabytes") {
			t.Errorf("%s was accepted", bad)
		}
	}
}

func TestStatusSaysHowOftenAndWhose(t *testing.T) {
	f := newFixture(t)
	f.write("mods/up/mod.toml", "type='parser'\napi=1\nversion='1'\ndescription='d'\ninput='i'\n[returns]\nlabel='string'\n")
	f.write("mods/up/init.lua", `return function() return { {} } end`)
	f.write("mods/m/mod.toml", "uses=['up']\n")
	f.write("mods/m/init.lua", `
local up = spot.use("up")
spot.value("parsed", function() return up("x") end)
spot.value("own", function() error("mine") end)
spot.value("kept", function() local r = {} for i = 1, 2000 do r[i] = ("x"):rep(4000) end return r end)
spot.on("publish", function() local r = {} for i = 1, 2000 do r[i] = ("x"):rep(4000) end for i = 1, 6 do spot.publish("v" .. i, r) end end)`)
	f.approve()
	for range 3 {
		f.Get("m.parsed")
	}
	s := f.Statuses()
	if len(s) != 1 || s[0].Count != 3 || s[0].Fault != "parser up" {
		t.Errorf("a parser's fault: %+v", s)
	}
	f.Get("m.own")
	if s = f.Statuses(); s[0].Fault != "the mod" || s[0].Count != 4 {
		t.Errorf("the mod's: %+v", s)
	}
	// 32 MB kept in all: four values of about 8 MB each fit, not more.
	_, err := f.Call("m.publish", nil)
	if err == nil || !strings.Contains(err.Error(), "more than 32 MB") {
		t.Errorf("kept without end: %v", err)
	}
}
