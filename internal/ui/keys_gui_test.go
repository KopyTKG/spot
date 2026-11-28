//go:build gui

package ui

// Where the keyboard goes can only be seen in a real window. These run on a
// display without a screen (gtk4-broadwayd), so that none appears on yours:
// `just test-keys`. Elsewhere they are skipped.

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"gitlab.com/KopyTKG/spot/internal/config"
)

type keyed struct {
	t     *testing.T
	d     *dialog
	win   *gtk.Window
	ended int
}

func inWindow(t *testing.T, toml, dir string) *keyed {
	t.Helper()
	if os.Getenv("GDK_BACKEND") != "broadway" {
		t.Skip("needs a window: just test-keys")
	}
	gtk.Init()
	cfg, err := config.ParseConfig([]byte(toml), func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatal(err)
	}
	k := &keyed{t: t, d: newDialog(cfg, append(os.Environ(), "XDG_RUNTIME_DIR="+t.TempDir()), dir, nil, nil)}
	k.d.finish = func(result) { k.ended++ }
	k.win = gtk.NewWindow()
	k.win.SetChild(k.d.root)
	k.win.Present()
	t.Cleanup(k.win.Destroy)
	pump(func() bool { return false })
	k.d.focusFirst()
	return k
}

func demo(t *testing.T, name string) *keyed {
	t.Helper()
	dir, err := filepath.Abs("../../examples/components")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return inWindow(t, string(data), dir)
}

// at names where the keyboard is.
func (k *keyed) at() string {
	f := k.win.Focus()
	if f == nil {
		return "nowhere"
	}
	for w := f; w != nil; w = gtk.BaseWidget(w).Parent() {
		if _, ok := w.(*gtk.DropDown); ok {
			return "drop-down"
		}
	}
	switch w := f.(type) {
	case *gtk.ToggleButton:
		if w.HasCSSClass("spot-segment") {
			return "segment " + w.Label()
		}
		return "toggle button"
	case *gtk.CheckButton:
		return "check " + w.Label()
	case *gtk.Button:
		return "button"
	case *gtk.Switch:
		return "switch"
	case *gtk.Scale:
		return "scale"
	case *gtk.FlowBoxChild:
		for _, c := range w.CSSClasses() {
			if v, ok := strings.CutPrefix(c, "spot-value-"); ok {
				return "tile " + v
			}
		}
	}
	for w := f; w != nil; w = gtk.BaseWidget(w).Parent() {
		switch w.(type) {
		case *gtk.SearchEntry:
			return "search"
		case *gtk.Entry:
			return "field"
		case *gtk.TextView:
			return "text"
		case *gtk.ListView:
			return "list"
		}
	}
	return gtk.BaseWidget(f).Type().Name()
}

// walk presses key n times and says where the keyboard was after each.
func (k *keyed) walk(key uint, n int) []string {
	var out []string
	for range n {
		k.d.onKey(key, 0, k.win.Focus())
		out = append(out, k.at())
	}
	return out
}

func (k *keyed) wants(what string, got []string, want ...string) {
	k.t.Helper()
	if !slices.Equal(got, want) {
		k.t.Errorf("%s:\n got %q\nwant %q", what, got, want)
	}
}

func TestKeysThroughControls(t *testing.T) {
	k := demo(t, "controls.toml")
	pump(func() bool { return false }) // the states are read
	k.wants("start", []string{k.at()}, "switch")
	k.wants("down", k.walk(gdk.KEY_Down, 6),
		"toggle button", "check A check box", "segment Balanced", "scale", "scale", "scale")
	var scales []*gtk.Scale
	find[*gtk.Scale](k.d.root, &scales)
	if scales[0].Value() != 40 {
		t.Errorf("walking moved the slider to %v", scales[0].Value())
	}
	k.wants("up, out of the slider", k.walk(gdk.KEY_Up, 5),
		"segment Balanced", "check A check box", "toggle button", "switch", "switch")
	k.walk(gdk.KEY_Down, 3)
	k.wants("within the segments", append(k.walk(gdk.KEY_Right, 2), k.walk(gdk.KEY_Left, 3)...),
		"segment Fast", "segment Fast", "segment Balanced", "segment Saver", "segment Saver")
	k.wants("off a segment", k.walk(gdk.KEY_Down, 1), "scale")
	if k.ended != 0 {
		t.Error("walking ended the dialog")
	}
}

func TestKeysThroughTilesAndButton(t *testing.T) {
	k := demo(t, "toggles.toml")
	k.wants("start", []string{k.at()}, "tile wifi")
	k.wants("down, out of the grid", k.walk(gdk.KEY_Down, 3), "tile flight", "button", "button")
	k.wants("up, into the bottom row", k.walk(gdk.KEY_Up, 3), "tile flight", "tile wifi", "tile wifi")
	k.walk(gdk.KEY_Right, 1)
	k.wants("the same column", k.walk(gdk.KEY_Down, 2), "tile broken", "button")
}

// reached is the item the keyboard is on, by its place in the dialog.
func (k *keyed) reached(b *builderProbe) int {
	f := k.win.Focus()
	if f == nil {
		return -1
	}
	for i, row := range b.rows {
		if gtk.BaseWidget(f).IsAncestor(row) {
			return i
		}
	}
	return -1
}

// builderProbe is the items of a dialog the keyboard can be on.
type builderProbe struct{ rows []*gtk.Box }

func probe(d *dialog) *builderProbe {
	var all []*gtk.Box
	find[*gtk.Box](d.root, &all)
	p := &builderProbe{}
	for _, box := range all {
		if !box.HasCSSClass("spot-item") {
			continue
		}
		var meters []*gtk.ProgressBar
		var lines []*gtk.Separator
		find[*gtk.ProgressBar](box, &meters)
		find[*gtk.Separator](box, &lines)
		if len(meters) == 0 && len(lines) == 0 {
			p.rows = append(p.rows, box)
		}
	}
	return p
}

// Whatever a demo holds, Down alone reaches every item and Up alone gets back to
// the first. This is the test for "I cannot get from here to there".
func TestKeysReachEverythingAndBack(t *testing.T) {
	for _, name := range []string{"controls.toml", "toggles.toml", "lists.toml", "form.toml"} {
		k := demo(t, name)
		pump(func() bool { return false })
		p := probe(k.d)
		seen := map[int]bool{k.reached(p): true}
		press := func(key uint) {
			k.d.onKey(key, 0, k.win.Focus())
			ctxIdle()
			seen[k.reached(p)] = true
		}
		for range 40 {
			press(gdk.KEY_Down)
		}
		if len(seen) != len(p.rows) || seen[-1] {
			t.Errorf("%s: Down reached %v of %d items", name, seen, len(p.rows))
		}
		for range 60 {
			press(gdk.KEY_Up)
		}
		if at := k.reached(p); at != 0 {
			t.Errorf("%s: Up from the bottom stops at item %d (%s)", name, at, k.at())
		}
		if k.ended != 0 {
			t.Errorf("%s: walking ended the dialog", name)
		}
	}
}

func ctxIdle() {
	ctx := glib.MainContextDefault()
	for ctx.Iteration(false) {
	}
}

// Two lists: the keys are for the one they are pressed in.
func TestKeysGoToTheListTheyArePressedIn(t *testing.T) {
	k := demo(t, "lists.toml")
	pump(func() bool { return false })
	var views []*gtk.ListView
	find[*gtk.ListView](k.d.root, &views)
	sel := func(i int) int { return int(views[i].Model().Cast().(*gtk.SingleSelection).Selected()) }
	rows := func(i int) int { return int(views[i].Model().Cast().(*gtk.SingleSelection).NItems()) }
	press := func(key uint) { k.d.onKey(key, 0, k.win.Focus()) }

	press(gdk.KEY_Down)
	if sel(0) != 1 || sel(1) != 0 {
		t.Fatalf("Down in the first list moved %d, %d", sel(0), sel(1))
	}
	folded := rows(1)
	press(gdk.KEY_Right) // folding belongs to the second list
	if rows(1) != folded {
		t.Error("a key in the first list unfolded the second")
	}
	press(gdk.KEY_Down)
	press(gdk.KEY_Down) // past the last row
	press(gdk.KEY_Down)
	if sel(0) != 2 || sel(1) != 1 {
		t.Errorf("after leaving: %d, %d", sel(0), sel(1))
	}
	press(gdk.KEY_Right)
	if rows(1) == folded {
		t.Error("Right in the second list did not unfold its group")
	}
	if sel(0) != 2 {
		t.Error("the first list moved")
	}
}

// Switch rows flip with Enter and with Space; typed text keeps its spaces.
func TestKeysFlipSwitchRows(t *testing.T) {
	k := demo(t, "lists.toml")
	pump(func() bool { return false })
	var switches []*gtk.Switch
	find[*gtk.Switch](k.d.root, &switches)
	var searches []*gtk.SearchEntry
	find[*gtk.SearchEntry](k.d.root, &searches)
	press := func(key uint) bool {
		used := k.d.onKey(key, 0, k.win.Focus())
		pump(func() bool { return false })
		return used
	}
	on := func(i int) bool { return switches[i].Active() }
	was := on(0)
	if !press(gdk.KEY_Return) || on(0) == was {
		t.Errorf("Enter: %v -> %v", was, on(0))
	}
	if !press(gdk.KEY_space) || on(0) != was {
		t.Errorf("Space: still %v", on(0))
	}
	press(gdk.KEY_Down)
	press(gdk.KEY_Down) // "Display…" is no switch: Space is for the search
	if press(gdk.KEY_space) {
		t.Error("Space on a row without a switch was taken")
	}
	searches[0].SetText("auto")
	pump(func() bool { return false })
	if press(gdk.KEY_space) {
		t.Error("Space was taken from what is being typed")
	}
	was = on(0)
	if !press(gdk.KEY_Return) || k.ended != 0 {
		t.Errorf("Enter on a searched switch row: ended %d", k.ended)
	}
}

// A grid below something else is not jumped to over it.
func TestKeysDoNotSkipBetweenGrids(t *testing.T) {
	tile := "[[items.tiles]]\nlabel='x'\nvalue='%'\n"
	k := inWindow(t, "[[items]]\ntype='tiles'\nid='a'\n"+strings.ReplaceAll(tile, "%", "one")+
		"[[items]]\ntype='button'\nid='b'\nlabel='mid'\nexec='true'\n"+
		"[[items]]\ntype='tiles'\nid='c'\n"+strings.ReplaceAll(tile, "%", "two"), t.TempDir())
	k.wants("down", k.walk(gdk.KEY_Down, 2), "button", "tile two")
	k.wants("up", k.walk(gdk.KEY_Up, 2), "button", "tile one")
}

// The keyboard's place is marked once the window says so.
func TestKeyboardFocusIsMarked(t *testing.T) {
	k := demo(t, "controls.toml")
	k.win.SetFocusVisible(true) // as the shell does
	k.walk(gdk.KEY_Down, 1)
	if gtk.BaseWidget(k.win.Focus()).StateFlags()&gtk.StateFlagFocusVisible == 0 {
		t.Error("the focused control has no focus-visible state to style")
	}
}
