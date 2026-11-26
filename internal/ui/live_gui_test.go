//go:build gui

package ui

// Live controls against real GTK widgets and real commands; nothing is shown.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"gitlab.com/KopyTKG/spot/internal/config"
)

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	ctx := glib.MainContextDefault()
	for end := time.Now().Add(3 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		for ctx.Iteration(false) {
		}
		if ok() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

// realize puts the dialog into a window that is never shown: GTK buttons ignore
// activation until they are realized.
func realize(t *testing.T, d *dialog) {
	t.Helper()
	win := gtk.NewWindow()
	win.SetChild(d.root)
	var walk func(gtk.Widgetter)
	walk = func(w gtk.Widgetter) {
		if gtk.BaseWidget(w).HasCSSClass("spot-control") {
			gtk.BaseWidget(w).Realize()
		}
		for c := gtk.BaseWidget(w).FirstChild(); c != nil; c = gtk.BaseWidget(c).NextSibling() {
			walk(c)
		}
	}
	walk(d.root)
	t.Cleanup(win.Destroy)
}

// settleLoop lets anything wrongly still in flight happen.
func settleLoop() {
	ctx := glib.MainContextDefault()
	for end := time.Now().Add(300 * time.Millisecond); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		for ctx.Iteration(false) {
		}
	}
}

func put(t *testing.T, path, s string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(path string) string {
	b, _ := os.ReadFile(path)
	return strings.TrimSpace(string(b))
}

func errorShown(d *dialog) string {
	l := widgetByClass(d.root, "spot-error").(*gtk.Label)
	if !l.Visible() {
		return ""
	}
	return l.Text()
}

func TestSwitchShowsItsStateAndAppliesAtOnce(t *testing.T) {
	for _, look := range []string{"switch", "button", "checkbox"} {
		state := filepath.Join(t.TempDir(), "state")
		put(t, state, "on")
		d, ended := guiDialog(t, `
[[items]]
type      = "switch"
id        = "night"
label     = "Nightlight"
look      = "`+look+`"
state     = "cat `+state+`"
on_change = "echo {value} > `+state+`"
`, nil, os.Environ())
		outer := widgetByClass(d.root, "spot-switch-"+look)
		w := widgetByClass(d.root, "spot-control")
		realize(t, d)
		on := func() bool { return gtk.BaseWidget(outer).HasCSSClass("spot-on") }
		waitFor(t, look+": the state to be read", on)

		w.(interface{ Activate() bool }).Activate()
		if sw, ok := w.(*gtk.Switch); ok && sw.Active() {
			sw.SetActive(false) // an unmapped switch does not toggle on activate
		}
		waitFor(t, look+": on_change", func() bool { return read(state) == "false" })
		settleLoop()
		if on() {
			t.Errorf("%s still shows on", look)
		}
		if len(*ended) != 0 {
			t.Errorf("%s: the dialog closed: %v", look, *ended)
		}
	}
}

func TestFailedChangeIsNotShownAsDone(t *testing.T) {
	d, _ := guiDialog(t, `
[[items]]
type      = "switch"
id        = "x"
label     = "X"
look      = "button"
on_change = "echo no such device >&2; exit 1"
`, nil, os.Environ())
	btn := widgetByClass(d.root, "spot-control").(*gtk.ToggleButton)
	btn.SetActive(true)
	waitFor(t, "the error", func() bool { return errorShown(d) == "no such device" })
	if btn.Active() || btn.HasCSSClass("spot-on") {
		t.Error("the switch stayed on although the command failed")
	}
}

func TestHangingCommandTimesOut(t *testing.T) {
	d, _ := guiDialog(t, `
[[items]]
type    = "button"
id      = "b"
label   = "Slow"
exec    = "sleep 30"
timeout = 200
`, nil, os.Environ())
	realize(t, d)
	d.onKey(gdk.KEY_Return, 0, widgetByClass(d.root, "spot-button"))
	waitFor(t, "the timeout", func() bool { return strings.Contains(errorShown(d), "timed out") })
}

func TestButtonStaysOpenAndRefreshesOrCloses(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	put(t, state, "off")
	d, ended := guiDialog(t, `
[[items]]
type  = "switch"
id    = "s"
label = "S"
look  = "button"
state = "cat `+state+`"
[[items]]
type  = "button"
id    = "go"
label = "Go"
exec  = "echo on > `+state+`"
[[items]]
type  = "button"
id    = "bye"
label = "Bye"
exec  = "true"
close = true
`, nil, os.Environ())
	sw := widgetByClass(d.root, "spot-switch-button").(*gtk.ToggleButton)
	var buttons []*gtk.Button
	for _, w := range allByClass(d.root, "spot-button") {
		buttons = append(buttons, w.(*gtk.Button))
	}
	realize(t, d)
	settleLoop()
	// Enter on a focused control activates it instead of submitting the dialog.
	d.onKey(gdk.KEY_Return, 0, buttons[0])
	waitFor(t, "the other control to follow", sw.Active)
	if len(*ended) != 0 {
		t.Fatalf("the dialog closed: %v", *ended)
	}
	d.onKey(gdk.KEY_Return, 0, buttons[1])
	waitFor(t, "close", func() bool { return len(*ended) == 1 })
}

func allByClass(w gtk.Widgetter, class string) (found []gtk.Widgetter) {
	if gtk.BaseWidget(w).HasCSSClass(class) {
		found = append(found, w)
	}
	for c := gtk.BaseWidget(w).FirstChild(); c != nil; c = gtk.BaseWidget(c).NextSibling() {
		found = append(found, allByClass(c, class)...)
	}
	return found
}

func TestSegmentedAndSlider(t *testing.T) {
	dir := t.TempDir()
	mode, vol := filepath.Join(dir, "mode"), filepath.Join(dir, "vol")
	put(t, mode, "balanced")
	put(t, vol, "30")
	d, _ := guiDialog(t, `
[[items]]
type      = "segmented"
id        = "mode"
options   = ["saver", "balanced", "performance"]
labels    = ["Saver", "Balanced", "Fast"]
state     = "cat `+mode+`"
on_change = "echo {value} > `+mode+`"
[[items]]
type      = "scale"
id        = "vol"
state     = "cat `+vol+`"
on_change = "echo {value} >> `+vol+`.log; echo {value} > `+vol+`"
[submit]
stdout = "{mode} {vol}"
`, nil, os.Environ())
	var segs []*gtk.ToggleButton
	for _, w := range allByClass(d.root, "spot-segment") {
		segs = append(segs, w.(*gtk.ToggleButton))
	}
	sc := widgetByClass(d.root, "spot-scale").(*gtk.Scale)
	waitFor(t, "both states", func() bool { return segs[1].Active() && sc.Value() == 30 })
	if _, err := os.Stat(vol + ".log"); err == nil {
		t.Error("reading the state must not run on_change")
	}
	segs[2].SetActive(true)
	waitFor(t, "the mode", func() bool { return read(mode) == "performance" })
	for v := 40.0; v <= 70; v += 10 {
		sc.SetValue(v) // one drag
	}
	waitFor(t, "the volume", func() bool { return read(vol) == "70" })
	if got := read(vol + ".log"); got != "70" {
		t.Errorf("a drag ran on_change for %q, want only the last value", got)
	}
	d.onKey(gdk.KEY_Return, 0, nil)
	if got := d.out.String(); got != "performance 70\n" {
		t.Errorf("submit printed %q", got)
	}
}

func TestListRowsWithSwitches(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "log")
	d, ended := guiDialog(t, `
[[items]]
type      = "list"
id        = "row"
switches  = true
options   = ["Nightlight\tnight\toff", "Broken\tbroken\toff", "Display\tdisplay"]
on_change = "echo {key} {value} >> `+log+`; test {key} != broken"
[submit]
stdout = "{row}"
`, nil, os.Environ())
	_, sel, _ := listLines(t, d)
	if lines, _, _ := listLines(t, d); strings.Join(lines, ",") != "Nightlight,Broken,Display" {
		t.Fatalf("rows %q", lines)
	}

	d.onKey(gdk.KEY_Return, 0, nil)
	waitFor(t, "on_change", func() bool { return read(log) == "night true" })
	settleLoop()
	if sel.Selected() != 0 || len(*ended) != 0 {
		t.Fatalf("selection %d, ended %v", sel.Selected(), *ended)
	}
	d.onKey(gdk.KEY_Return, 0, nil) // and back: the new state was kept
	waitFor(t, "the second flip", func() bool { return strings.HasSuffix(read(log), "night false") })
	settleLoop()

	sel.SetSelected(1)
	d.onKey(gdk.KEY_Return, 0, nil)
	waitFor(t, "the failure", func() bool { return errorShown(d) != "" })
	d.onKey(gdk.KEY_Return, 0, nil) // reverted, so it asks for "on" again
	waitFor(t, "the retry", func() bool { return strings.Count(read(log), "broken true") == 2 })
	settleLoop()

	sel.SetSelected(2) // an ordinary row
	d.onKey(gdk.KEY_Return, 0, nil)
	if len(*ended) != 1 || d.out.String() != "display\n" {
		t.Errorf("ended %v, printed %q", *ended, d.out.String())
	}
}

func TestStaleStateReadDoesNotUndoAPick(t *testing.T) {
	dir := t.TempDir()
	vol, night := filepath.Join(dir, "vol"), filepath.Join(dir, "night")
	put(t, vol, "30")
	put(t, night, "off")
	d, _ := guiDialog(t, `
[[items]]
type      = "scale"
id        = "vol"
state     = "v=$(cat `+vol+`); sleep 0.3; echo $v"
on_change = "echo {value} > `+vol+`"
[[items]]
type      = "switch"
id        = "night"
label     = "Night"
look      = "button"
state     = "v=$(cat `+night+`); sleep 0.3; echo $v"
on_change = "echo {value} > `+night+`"
`, nil, os.Environ())
	sc := widgetByClass(d.root, "spot-scale").(*gtk.Scale)
	btn := widgetByClass(d.root, "spot-switch-button").(*gtk.ToggleButton)
	// both first reads are under way, with the old values
	sc.SetValue(80)
	btn.SetActive(true)
	waitFor(t, "both changes", func() bool { return read(vol) == "80" && read(night) == "true" })
	for i := 0; i < 3; i++ {
		settleLoop()
	}
	if sc.Value() != 80 || !btn.Active() {
		t.Errorf("an old read undid the pick: slider %v, switch %v", sc.Value(), btn.Active())
	}
}

// A flip moves the switch that is on screen (so it slides); handing the widget to
// another row gives it a fresh one, already in position.
func TestSwitchRowIsUpdatedInPlace(t *testing.T) {
	gtk.Init()
	s := parseSwitchRows(&config.Item{}, []string{"A\ta\toff", "B\tb\ton"})
	box := newSwitchRow(gtk.NewLabel(""))
	s.bind(box, "A")
	_, first := rowParts(box)
	s.rows["A"].on = true
	s.update("A")
	if _, sw := rowParts(box); coreglib.BaseObject(sw).Native() != coreglib.BaseObject(first).Native() || !sw.Active() {
		t.Fatal("a flip must change the switch that is there")
	}
	s.bind(box, "B")
	if _, sw := rowParts(box); coreglib.BaseObject(sw).Native() == coreglib.BaseObject(first).Native() || !sw.Active() {
		t.Fatal("another row gets its own switch")
	}
	s.rows["A"].on = false
	s.update("A") // not on screen any more
	if _, sw := rowParts(box); !sw.Active() {
		t.Fatal("updating a row that is off screen changed another one")
	}
}
