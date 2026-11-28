//go:build gui

package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"gitlab.com/KopyTKG/spot/internal/config"
)

type tileView struct {
	*openMenu
	tiles []*gtk.FlowBoxChild
}

// openTiles shows a dialog whose switch states are files in a folder: DIR/<name>.
func openTiles(t *testing.T, toml string) (*tileView, string) {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(dir+"/a", []byte("on"), 0o600)
	os.WriteFile(dir+"/b", []byte("off"), 0o600)
	os.WriteFile(dir+"/level", []byte("87%\n"), 0o600)
	d, _ := guiDialog(t, strings.ReplaceAll(toml, "DIR", dir), nil, os.Environ())
	v := &tileView{openMenu: &openMenu{t: t, d: d}}
	d.finish = func(r result) { v.ended = append(v.ended, r) }
	find[*gtk.FlowBoxChild](d.root, &v.tiles)
	return v, dir
}

func (v *tileView) classes(i int) string {
	var out []string
	for _, c := range []string{"spot-on", "spot-unknown", "spot-busy"} {
		if v.tiles[i].HasCSSClass(c) {
			out = append(out, strings.TrimPrefix(c, "spot-"))
		}
	}
	var labels []*gtk.Label
	find[*gtk.Label](v.tiles[i], &labels)
	for _, l := range labels {
		if l.HasCSSClass("spot-tile-state") {
			out = append(out, l.Text())
		}
	}
	return strings.Join(out, " ")
}

func (v *tileView) wait(i int, want string) {
	v.t.Helper()
	if !pump(func() bool { return v.classes(i) == want }) {
		v.t.Fatalf("tile %d: %q, want %q (%s)", i, v.classes(i), want, v.problem())
	}
}

// click activates the tile as a click does.
func (v *tileView) click(i int) { v.tiles[i].Activate() }

const quick = `
[[items]]
type = "tiles"
id = "q"
columns = 4
[[items.tiles]]
label = "A"
value = "a"
class = "warm"
state = "cat DIR/a"
on_change = "echo {value} > DIR/a; echo {id}:$SPOT_VALUE >> DIR/log"
[[items.tiles]]
label = "B"
value = "b"
state = "cat DIR/b"
on_change = "echo cannot >&2; exit 1"
[[items.tiles]]
label = "C"
value = "c"
state = "cat DIR/c"
on_change = "echo ran >> DIR/c-changed"
[[items.tiles]]
label = "Open"
value = "open settings"
[[items]]
type = "meter"
id = "m"
label = "Battery"
state = "cat DIR/level"
format = "{value} %"
[submit]
stdout = "{q}"
`

func (v *tileView) meterText() (float64, string) {
	var bars []*gtk.ProgressBar
	find[*gtk.ProgressBar](v.d.root, &bars)
	var labels []*gtk.Label
	find[*gtk.Label](v.d.root, &labels)
	for _, l := range labels {
		if l.HasCSSClass("spot-meter-text") {
			return bars[0].Fraction(), l.Text()
		}
	}
	return 0, ""
}

func TestLiveTiles(t *testing.T) {
	v, dir := openTiles(t, quick)
	v.wait(0, "on On")
	v.wait(1, "Off")
	v.wait(2, "unknown …") // its state command fails: not off, unknown
	if !pump(func() bool { _, s := v.meterText(); return s == "87 %" }) {
		t.Fatal(v.meterText())
	}
	if f, _ := v.meterText(); f < 0.869 || f > 0.871 {
		t.Errorf("fraction %v", f)
	}
	for _, class := range []string{"spot-value-a", "warm"} {
		if !v.tiles[0].HasCSSClass(class) {
			t.Error("no class", class)
		}
	}
	if !v.tiles[3].HasCSSClass("spot-value-open-settings") {
		t.Error("no class from the value")
	}

	// Flipping applies it, reads again and stays open.
	os.WriteFile(dir+"/level", []byte("250"), 0o600)
	v.click(0)
	v.wait(0, "Off")
	if !pump(func() bool { b, _ := os.ReadFile(dir + "/log"); return string(b) == "a:off\n" }) {
		b, _ := os.ReadFile(dir + "/log")
		t.Errorf("on_change: %q", b)
	}
	if !pump(func() bool { f, s := v.meterText(); return f == 1 && s == "250 %" }) {
		t.Error("the meter after a change did not move")
	}
	// A change that fails leaves it as it was, and says so.
	v.click(1)
	if !pump(func() bool { return v.problem() != "" && v.classes(1) == "Off" }) {
		t.Errorf("%q %q", v.classes(1), v.problem())
	}
	// A tile whose state nobody knows is not flipped on a guess.
	v.click(2)
	pump(func() bool { return false })
	if _, err := os.Stat(dir + "/c-changed"); err == nil {
		t.Error("an unknown tile was changed")
	}
	// A state that stops being readable does not keep the old one on show.
	os.Remove(dir + "/a")
	os.WriteFile(dir+"/level", []byte("lots"), 0o600)
	v.click(1)
	v.wait(0, "unknown …")
	if !pump(func() bool { _, s := v.meterText(); return s == "…" }) {
		t.Error("junk on a meter was kept")
	}
	if len(v.ended) != 0 {
		t.Fatalf("switching closed the dialog: %v", v.ended)
	}
	// A plain tile in the same grid still answers the dialog.
	v.click(3)
	if len(v.ended) != 1 || !v.ended[0].submitted || v.d.out.String() != "open settings\n" {
		t.Errorf("%v %q", v.ended, v.d.out.String())
	}
}

// Enter flips the tile under the keyboard cursor; on any other tile it does not.
func TestEnterFlipsTheTileUnderTheCursor(t *testing.T) {
	gtk.Init()
	if gdk.DisplayGetDefault() == nil {
		t.Skip("no display")
	}
	flow := gtk.NewFlowBox()
	kids := []*gtk.FlowBoxChild{gtk.NewFlowBoxChild(), gtk.NewFlowBoxChild()}
	for _, k := range kids {
		flow.Append(k)
	}
	flipped := 0
	g := &tileGroup{flow: flow, children: kids, cols: 2, flips: []func(){func() { flipped++ }, nil}}
	n := &tileNav{groups: []*tileGroup{g}}
	if n.flip() {
		t.Error("with no cursor")
	}
	n.active = g
	if !n.flip() || flipped != 1 || g.selected() != 0 {
		t.Errorf("flipped %d, selected %d", flipped, g.selected())
	}
	n.cur = 1
	if n.flip() || flipped != 1 {
		t.Error("a plain tile flipped")
	}
}

func TestLiveTileFromAMod(t *testing.T) {
	gtk.Init()
	if gdk.DisplayGetDefault() == nil {
		t.Skip("no display")
	}
	mods := &menuMods{values: map[string]string{"m.idle": `true`}}
	toml := "[[items]]\ntype='tiles'\nid='q'\n[[items.tiles]]\nlabel='Idle'\nvalue='idle'\nfrom='m.idle'\ncall='m.set'\n[[items.tiles]]\nlabel='Gone'\nvalue='g'\nfrom='m.gone'\ncall='m.set'\n"
	cfg, err := config.ParseConfig([]byte(toml), func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatal(err)
	}
	d := newDialog(cfg, os.Environ(), t.TempDir(), nil, mods)
	v := &tileView{openMenu: &openMenu{t: t, d: d}}
	d.finish = func(r result) { v.ended = append(v.ended, r) }
	find[*gtk.FlowBoxChild](d.root, &v.tiles)
	v.wait(0, "on On")
	v.wait(1, "unknown …")
	v.click(0)
	if !pump(func() bool { return strings.Contains(mods.log(), "m.set(off)") }) {
		t.Error(mods.log())
	}
	v.click(1)
	pump(func() bool { return false })
	if strings.Count(mods.log(), "m.set(") != 1 {
		t.Error("an unknown tile made a request: " + mods.log())
	}
}

// The component demos must keep working: each is built, as shipped, and reads
// its states without complaint, but for the one that is broken on purpose.
func TestComponentDemos(t *testing.T) {
	gtk.Init()
	if gdk.DisplayGetDefault() == nil {
		t.Skip("no display")
	}
	dir, err := filepath.Abs("../../examples/components")
	if err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "XDG_RUNTIME_DIR="+t.TempDir())
	files, _ := filepath.Glob(dir + "/*.toml")
	if len(files) != 5 {
		t.Fatalf("%d demos", len(files))
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := config.ParseConfigAt(f, data, func(string) (string, bool) { return "", false })
		if err != nil {
			t.Errorf("%s: %v", f, err)
			continue
		}
		if w := config.UnknownKeys(data); len(w) != 0 {
			t.Errorf("%s: %v", f, w)
		}
		if _, err := os.Stat(filepath.Join(dir, cfg.Style)); err != nil {
			t.Errorf("%s: style: %v", f, err)
		}
		var d *dialog
		if cfg.IsMenu() {
			d = newMenu(cfg, f, "", env, dir, nil, nil)
		} else {
			d = newDialog(cfg, env, dir, nil, nil)
		}
		d.finish = func(result) {}
		m := &openMenu{t: t, d: d}
		pump(func() bool { return false })
		want := ""
		if filepath.Base(f) == "toggles.toml" {
			want = "demo.sh: this one cannot be read, on purpose"
		}
		if got := m.problem(); got != want {
			t.Errorf("%s: %q, want %q", filepath.Base(f), got, want)
		}
	}
}
