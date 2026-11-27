package modd

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

const live = `
local n, ticks = 0, 0
spot.value("every", { every = 300 }, function() n = n + 1 return n end)
spot.value("still", function() return "same" end)
spot.on("poke", function(a) spot.publish("said", a[1]) end)
spot.publish("said", "start")
spot.every(200, function() ticks = ticks + 1 spot.publish("ticks", ticks) end)
spot.after(100, function() spot.publish("once", "done") end)
spot.publish("once", "not yet")
spot.publish("ticks", 0)
spot.value("fails", function() if spot.exists("~/readable/break") then error("broken now") end return "good" end)
spot.on("disk", function() local s = ("x"):rep(8 * 1024 * 1024) for i = 1, 20 do local ok, e = spot.write("~/writable/f", s) if not ok then return { i, e } end end return "no limit" end)
`

func newLive(t *testing.T) fixture {
	f := newFixture(t)
	f.write("mods/m/mod.toml", "[permissions.read]\n\"~/readable\"='r'\n[permissions.write]\n\"~/writable\"='w'\n")
	f.write("mods/m/init.lua", live)
	os.MkdirAll(f.home+"/writable", 0o700)
	f.approve()
	return f
}

// next is the watcher's next update as text, "" when none comes in time.
func next(w *Watcher, wait time.Duration) string {
	select {
	case u, ok := <-w.C:
		switch {
		case !ok:
			return "closed"
		case u.Err != "":
			return "!" + u.Err
		case u.Warning != "":
			return "~" + string(u.Result)
		}
		return string(u.Result)
	case <-time.After(wait):
		return ""
	}
}

func until(w *Watcher, want func(string) bool) string {
	end := time.Now().Add(6 * time.Second)
	got := ""
	for time.Now().Before(end) {
		if got = next(w, time.Until(end)); want(got) {
			break
		}
	}
	return got
}

func is(s string) func(string) bool { return func(g string) bool { return strings.Contains(g, s) } }

func (f fixture) watch(item string) *Watcher {
	f.t.Helper()
	w, err := f.Watch("m." + item)
	if err != nil {
		f.t.Fatal(err)
	}
	return w
}

func quick(t *testing.T) {
	old := checkEvery
	checkEvery = 200 * time.Millisecond
	t.Cleanup(func() { checkEvery = old })
}

func TestWatchingEveryPublishAndTimers(t *testing.T) {
	quick(t)
	f := newLive(t)
	every := f.watch("every")
	for _, want := range []string{"1", "2", "3"} {
		if got := next(every, 3*time.Second); got != want {
			t.Errorf("every: %s, want %s", got, want)
		}
	}
	said := f.watch("said")
	if got := next(said, time.Second); got != `"start"` {
		t.Errorf("said: %s", got)
	}
	f.Call("m.poke", []string{"hello"})
	if got := next(said, time.Second); got != `"hello"` {
		t.Errorf("published: %s", got)
	}
	f.Call("m.poke", []string{"hello"})
	if got := next(said, 600*time.Millisecond); got != "" {
		t.Errorf("the same again was sent: %s", got)
	}
	if got := until(f.watch("once"), is("done")); got != `"done"` {
		t.Errorf("after: %s", got)
	}
	ticks := f.watch("ticks")
	if got := until(ticks, func(g string) bool { n, _ := strconv.Atoi(g); return n >= 3 }); got == "" {
		t.Error("a repeating timer does not repeat")
	}
}

// Nobody asks anything: a change and a revocation still stop it, and what it
// said is not served any more.
func TestChangesAreSeenWithoutAsking(t *testing.T) {
	quick(t)
	f := newLive(t)
	w := f.watch("still")
	if got := next(w, 3*time.Second); got != `"same"` {
		t.Fatal(got)
	}
	f.write("mods/m/extra.lua", "return 1")
	if got := next(w, 3*time.Second); !strings.HasPrefix(got, "!") || !strings.Contains(got, "has changed") {
		t.Errorf("changed: %s", got)
	}
	if f.running() {
		t.Error("it still runs")
	}
	f.approve()
	if got := next(w, 3*time.Second); got != `"same"` {
		t.Errorf("approved again: %s", got)
	}
	f.store.Revoke("m")
	if got := next(w, 3*time.Second); !strings.Contains(got, "not approved") {
		t.Errorf("revoked: %s", got)
	}
	if f.running() {
		t.Error("it still runs")
	}
	f.Unwatch(w)
}

func TestWithoutWatchersItStillStops(t *testing.T) {
	quick(t)
	f := newLive(t)
	f.get("running", "still", `"same"`)
	f.store.Revoke("m")
	time.Sleep(600 * time.Millisecond)
	if f.running() {
		t.Error("revoked, and still running")
	}
}

func TestToldOnceWhenItBreaksAndOnceWhenItWorks(t *testing.T) {
	f := newLive(t)
	f.get("good", "fails", `"good"`)
	f.write("readable/break", "")
	for range 5 {
		f.Get("m.fails")
	}
	f.Get("m.nothing") // the caller's mistake, not the mod's
	f.get("another value still works", "still", `"same"`)
	if n := f.heard.all(); len(n) != 1 || !strings.Contains(n[0], "is failing") || !strings.Contains(n[0], "broken now") {
		t.Errorf("after failing: %q", n)
	}
	os.Remove(f.home + "/readable/break")
	f.get("works", "fails", `"good"`)
	f.get("works", "fails", `"good"`)
	if n := f.heard.all(); len(n) != 2 || !strings.Contains(n[1], "works again") {
		t.Errorf("after recovering: %q", n)
	}
	if s := f.Statuses(); len(s) != 1 || s[0].Error != "" || !s[0].Running {
		t.Errorf("status: %+v", s)
	}
	for range 20 {
		f.write("readable/break", "")
		f.Get("m.fails")
		os.Remove(f.home + "/readable/break")
		f.Get("m.fails")
	}
	if n := len(f.heard.all()); n > maxNotesPerHour {
		t.Errorf("%d notifications from a mod that flaps", n)
	}
}

func TestOnlyWhatYouDidNotDoIsNotified(t *testing.T) {
	f := newLive(t)
	f.get("good", "still", `"same"`)
	f.store.Revoke("m")
	f.Get("m.still")
	os.Remove(f.home + "/daemon.toml")
	f.Get("m.still")
	if n := f.heard.all(); len(n) != 0 {
		t.Errorf("told about your own doing: %q", n)
	}
	f.enable("")
	f.approve()
	f.get("back", "still", `"same"`)
	f.write("mods/m/x.lua", "return 1")
	f.Get("m.still")
	f.Get("m.still")
	if n := f.heard.all(); len(n) != 1 || !strings.Contains(n[0], "spot mods allow m") {
		t.Errorf("a changed mod: %q", n)
	}
}

func TestLimits(t *testing.T) {
	f := newLive(t)
	res, err := f.Call("m.disk", nil)
	if err != nil || !strings.Contains(string(res), "within an hour") || !strings.HasPrefix(string(res), "[9,") {
		t.Errorf("writing: %.200s %v", res, err)
	}
	var all []*Watcher
	for range maxWatchers {
		all = append(all, f.watch("still"))
	}
	if _, err := f.Watch("m.still"); err == nil {
		t.Error("one watcher too many")
	}
	for _, w := range all {
		f.Unwatch(w)
	}
}

func TestWatchingWhatIsNotThereAndLeaving(t *testing.T) {
	quick(t)
	f := newLive(t)
	for _, bad := range []string{"m", "x.y.", "../m.still", "M.still"} {
		if _, err := f.Watch(bad); err == nil && bad != "x.y." {
			t.Errorf("%q was accepted", bad)
		}
	}
	nope := f.watch("nope")
	if got := next(nope, 3*time.Second); !strings.Contains(got, "no value") {
		t.Errorf("a value that is not there: %s", got)
	}
	unlisted, _ := f.Watch("other.thing")
	if got := next(unlisted, 3*time.Second); !strings.Contains(got, "not enabled") {
		t.Errorf("a mod that is not listed: %s", got)
	}
	w := f.watch("still")
	next(w, 3*time.Second)
	done := make(chan struct{})
	go func() { f.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("closing hangs")
	}
	if got := until(w, is("closed")); got != "closed" {
		t.Errorf("a watch survives the daemon: %s", got)
	}
	if _, err := f.Watch("m.still"); err == nil {
		t.Error("a watch on a closed manager")
	}
}
