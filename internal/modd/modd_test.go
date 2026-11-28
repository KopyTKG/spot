package modd

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"gitlab.com/KopyTKG/spot/internal/approval"
	"gitlab.com/KopyTKG/spot/internal/sandbox"
)

type heard struct {
	mu    sync.Mutex
	notes []string
}

func (h *heard) add(title, body string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.notes = append(h.notes, title+" | "+body)
}

func (h *heard) all() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.notes)
}

type fixture struct {
	t *testing.T
	*Manager
	home  string
	heard *heard
}

func newFixture(t *testing.T) fixture {
	if sandbox.ABI() < sandbox.MinABI {
		t.Skipf("Landlock ABI %d here, %d needed", sandbox.ABI(), sandbox.MinABI)
	}
	d := t.TempDir()
	os.Chmod(d, 0o700)
	// A prebuilt host, to run these where there is no Go (in a container).
	bin := os.Getenv("SPOT_TEST_HOST")
	if bin == "" {
		bin = d + "/spot-mod-host"
		c := exec.Command("go", "build", "-o", bin, "gitlab.com/KopyTKG/spot/cmd/spot-mod-host")
		c.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("building the host: %v\n%s", err, out)
		}
	}
	store := approval.Store{Mods: d + "/mods", Records: d + "/approvals", Home: d, Config: d, Dir: d + "/state"}
	m, err := New(store, bin, d+"/run")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	// A test must never reach the desktop of whoever runs it.
	f := fixture{t, m, d, &heard{}}
	m.Notify = f.heard.add
	f.write("readable/a", "x")
	f.write("mods/m/mod.toml", "[permissions.read]\n\"~/readable\"='r'\n")
	f.write("mods/m/init.lua", `
local n = 0
spot.value("count", function() n = n + 1 return n end)
spot.value("flaky", function() if spot.exists("~/readable/break") then error("broken now") end return "good" end)
spot.value("word", function() return spot.config.word or "none" end)
spot.on("memory", function() local t = {} while true do t[#t+1] = ("x"):rep(1000000) .. #t end end)
spot.on("forge", function() spot.read("~/nowhere/x\n2026-01-01 00:00:00 run      \"fine\"") end)
`)
	f.enable("")
	return f
}

func (f fixture) write(rel, text string) {
	p := filepath.Join(f.home, rel)
	os.MkdirAll(filepath.Dir(p), 0o700)
	if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
		f.t.Fatal(err)
	}
}

func (f fixture) enable(settings string) { f.write("mods.toml", "mods=['m']\n"+settings) }

func (f fixture) approve() {
	m, err := f.store.Read("m")
	if err == nil {
		err = f.store.Approve(m)
	}
	if err != nil {
		f.t.Fatal(err)
	}
}

// want: the value, or a part of the error when it starts "!"; "~" before a
// value says it is an old one.
func (f fixture) get(what, item, want string) {
	f.t.Helper()
	res, warning, err := f.Get("m." + item)
	got := string(res)
	if warning != "" {
		got = "~" + got
	}
	if e, fails := strings.CutPrefix(want, "!"); fails {
		if err == nil || !strings.Contains(err.Error(), e) {
			f.t.Errorf("%s: %s, %v; want an error with %q", what, got, err, e)
		}
	} else if err != nil || got != want {
		f.t.Errorf("%s: %s, %v; want %s", what, got, err, want)
	}
}

func (f fixture) running() bool {
	p, _ := f.provider("m")
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.h != nil
}

func TestOnlyListedAndApprovedRun(t *testing.T) {
	f := newFixture(t)
	f.get("listed, not approved", "count", "!not approved")
	f.approve()
	os.Remove(f.home + "/mods.toml")
	f.get("approved, not listed", "count", "!not enabled")
	if f.running() {
		t.Error("a host runs")
	}
	f.enable("")
	f.get("both", "count", "1")
	f.get("it keeps its state", "count", "2")
}

func TestChangeAndRevokeStopAndForget(t *testing.T) {
	f := newFixture(t)
	f.approve()
	f.get("before", "count", "1")
	f.write("mods/m/other.lua", "return 1")
	f.get("changed", "count", "!has changed")
	if f.running() {
		t.Error("it still runs")
	}
	f.approve()
	f.get("approved again, from new", "count", "1")
	f.store.Revoke("m")
	f.get("revoked: nothing old either", "count", "!not approved")
	f.approve()
	f.get("back", "count", "1")
	os.Remove(f.home + "/mods.toml")
	f.get("unlisted", "count", "!not enabled")
	if f.running() {
		t.Error("it still runs")
	}
}

func TestFailuresKeepTheLastGoodValueAndPause(t *testing.T) {
	f := newFixture(t)
	f.approve()
	f.get("good", "flaky", `"good"`)
	f.write("readable/break", "")
	f.get("an error of the mod: the old one", "flaky", `~"good"`)
	if !f.running() {
		t.Error("a mod's own error must not restart it")
	}
	old := pauses
	pauses = []time.Duration{300 * time.Millisecond}
	defer func() { pauses = old }()
	if _, err := f.Call("m.memory", nil); err == nil {
		t.Fatal("it survived")
	}
	f.get("waiting, with an old value", "flaky", `~"good"`)
	f.get("waiting, without one", "count", "!tried again")
	time.Sleep(400 * time.Millisecond)
	f.get("started again", "count", "1")
}

func TestSettingsAndReplacedFiles(t *testing.T) {
	f := newFixture(t)
	f.approve()
	f.enable("[mod.m]\nword='first'\n")
	f.get("settings", "word", `"first"`)
	f.get("count", "count", "1")
	f.enable("[mod.m]\nword='second'\n")
	f.get("changed settings", "word", `"second"`)
	f.get("restarted", "count", "1")
	// S43: a granted file replaced, as updates do.
	p, _ := f.provider("m")
	f.write("granted", "old")
	id, _ := identify(f.home + "/granted")
	p.files[f.home+"/granted"] = id
	f.get("unchanged", "count", "2")
	f.write("granted.new", "new")
	os.Rename(f.home+"/granted.new", f.home+"/granted")
	f.get("replaced: a new host", "count", "1")
}

func TestAudit(t *testing.T) {
	f := newFixture(t)
	f.approve()
	log.SetOutput(&strings.Builder{})
	defer log.SetOutput(os.Stderr)
	f.Call("m.forge", nil)
	path := AuditPath(f.store, "m")
	b, err := os.ReadFile(path)
	st, _ := os.Stat(path)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("%v, mode %v", err, st)
	}
	if lines := strings.Split(strings.TrimSpace(string(b)), "\n"); len(lines) != 1 {
		t.Errorf("a name made a line of its own:\n%s", b)
	}
}

func TestStaleScopes(t *testing.T) {
	units := []string{"spot-mod-a-100-1.scope", "spot-mod-b-c-200-3.scope", "spot-mod-x.scope", "app.scope", "spot-mod-a-300-1.service"}
	got := StaleScopes(units, func(pid int) bool { return pid == 100 })
	if !slices.Equal(got, []string{"spot-mod-b-c-200-3.scope"}) {
		t.Error(fmt.Sprint(got))
	}
}

func TestWhatOthersCanChangeRunsNothing(t *testing.T) {
	f := newFixture(t)
	f.approve()
	f.get("as it should be", "count", "1")
	for _, spoil := range []string{f.home + "/mods.toml", f.host} {
		st, err := os.Stat(spoil)
		if err != nil {
			t.Fatal(err)
		}
		os.Chmod(spoil, st.Mode().Perm()|0o022)
		if _, warning, err := f.Get("m.count"); err == nil && !strings.Contains(warning, "can be changed by others") {
			t.Errorf("%s open to others: %v %q", spoil, err, warning)
		}
		if f.running() {
			t.Errorf("%s open to others, and it runs", spoil)
		}
		os.Chmod(spoil, st.Mode().Perm())
	}
}
