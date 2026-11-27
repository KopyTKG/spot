//go:build gui

package ui

// Exercises the dialog's Back handling against a real GTK display. Nothing is
// shown on screen: widgets are built and their key handler is called directly.
//
//	go test -tags gui -run Back ./internal/ui

import (
	"image"
	png_ "image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"gitlab.com/KopyTKG/spot/internal/config"
	"gitlab.com/KopyTKG/spot/internal/protocol"
)

func guiDialog(t *testing.T, toml string, back *config.BackSpec, env []string) (*dialog, *[]result) {
	t.Helper()
	gtk.Init()
	if gdk.DisplayGetDefault() == nil {
		t.Skip("no display")
	}
	cfg, err := config.ParseConfig([]byte(toml), func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatal(err)
	}
	d := newDialog(cfg, env, t.TempDir(), back, nil)
	var ended []result
	d.finish = func(r result) { ended = append(ended, r) }
	return d, &ended
}

const listCfg = "[[items]]\ntype = \"list\"\nid = \"c\"\noptions = [\"a\", \"b\"]\n[submit]\nstdout = \"{c}\"\n"

func TestBackKeysEndTheDialogWithStatus4(t *testing.T) {
	for _, tc := range []struct {
		name  string
		key   uint
		state gdk.ModifierType
		focus func() gtk.Widgetter
		want  bool
	}{
		{"Alt+Left anywhere", gdk.KEY_Left, gdk.AltMask, func() gtk.Widgetter { e := gtk.NewEntry(); e.SetText("typing"); return e }, true},
		{"Backspace with no editable focus (tiles/list)", gdk.KEY_BackSpace, 0, func() gtk.Widgetter { return nil }, true},
		{"Backspace in an empty search box", gdk.KEY_BackSpace, 0, func() gtk.Widgetter { return gtk.NewSearchEntry().Delegate() }, true},
		{"Backspace in a search box with text deletes instead", gdk.KEY_BackSpace, 0, func() gtk.Widgetter {
			se := gtk.NewSearchEntry()
			se.SetText("ab")
			return se.Delegate()
		}, false},
		{"Backspace in a plain entry deletes instead", gdk.KEY_BackSpace, 0, func() gtk.Widgetter { return gtk.NewEntry() }, false},
		{"Ctrl+Backspace is word-delete, not Back", gdk.KEY_BackSpace, gdk.ControlMask, func() gtk.Widgetter { return nil }, false},
	} {
		d, ended := guiDialog(t, listCfg, &config.BackSpec{Label: "Back"}, nil)
		handled := d.onKey(tc.key, tc.state, tc.focus())
		got := len(*ended) == 1 && (*ended)[0].code == protocol.ExitBack
		if got != tc.want {
			t.Errorf("%s: ended with Back = %v, want %v (handled=%v, ended=%v)", tc.name, got, tc.want, handled, *ended)
		}
	}
}

func TestNoBackControlMeansNoBackKeys(t *testing.T) {
	d, ended := guiDialog(t, listCfg, nil, nil)
	d.onKey(gdk.KEY_Left, gdk.AltMask, nil)
	d.onKey(gdk.KEY_BackSpace, 0, nil)
	if len(*ended) != 0 {
		t.Fatalf("a dialog opened without Back must ignore Back keys: %v", *ended)
	}
}

func TestBackRunsItsCommandWithoutSpotBack(t *testing.T) {
	out := filepath.Join(t.TempDir(), "ran")
	cmd := `printf '%s|%s' "$SPOT_BACK" "$KEEP" > ` + out
	env := append(os.Environ(), "SPOT_BACK="+cmd, "KEEP=yes")
	d, ended := guiDialog(t, listCfg, &config.BackSpec{Label: "Back", Cmd: cmd}, env)
	d.onKey(gdk.KEY_Left, gdk.AltMask, nil)
	if len(*ended) != 1 || (*ended)[0].code != protocol.ExitBack {
		t.Fatalf("ended: %v", *ended)
	}
	var got []byte
	for i := 0; i < 100 && len(got) == 0; i++ {
		got, _ = os.ReadFile(out)
		time.Sleep(20 * time.Millisecond)
	}
	if string(got) != "|yes" {
		t.Fatalf("command saw %q, want SPOT_BACK empty and the rest of the environment intact", got)
	}
}

func TestEscapeIsNotBackAndRunsNothing(t *testing.T) {
	out := filepath.Join(t.TempDir(), "ran")
	d, ended := guiDialog(t, listCfg, &config.BackSpec{Label: "Back", Cmd: "touch " + out}, os.Environ())
	d.onKey(gdk.KEY_Escape, 0, nil)
	time.Sleep(200 * time.Millisecond)
	if len(*ended) != 1 || (*ended)[0].code != 0 || (*ended)[0].submitted {
		t.Fatalf("Esc should cancel: %v", *ended)
	}
	if _, err := os.Stat(out); err == nil {
		t.Fatal("Esc must not run the Back command")
	}
}

// --- collapsible lists and the apps item ---------------------------------------

// widgetByClass finds the first widget below w that has the CSS class.
func widgetByClass(w gtk.Widgetter, class string) gtk.Widgetter {
	var found gtk.Widgetter
	var walk func(gtk.Widgetter) bool
	walk = func(x gtk.Widgetter) bool {
		if x == nil {
			return false
		}
		if gtk.BaseWidget(x).HasCSSClass(class) {
			found = x
			return true
		}
		for c := gtk.BaseWidget(x).FirstChild(); c != nil; c = gtk.BaseWidget(c).NextSibling() {
			if walk(c) {
				return true
			}
		}
		return false
	}
	walk(w)
	return found
}

// typeInto sets the search text and runs the main loop long enough for the
// entry's delayed "search-changed" to fire.
func typeInto(search *gtk.SearchEntry, text string) {
	search.SetText(text)
	ctx := glib.MainContextDefault()
	for end := time.Now().Add(400 * time.Millisecond); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		for ctx.Pending() {
			ctx.Iteration(false)
		}
	}
}

func listLines(t *testing.T, d *dialog) (lines []string, sel *gtk.SingleSelection, search *gtk.SearchEntry) {
	t.Helper()
	lvw := widgetByClass(d.root, "spot-list")
	sw := widgetByClass(d.root, "spot-search")
	if lvw == nil || sw == nil {
		t.Fatal("no list or search entry in the dialog")
	}
	lv := lvw.(*gtk.ListView)
	search = sw.(*gtk.SearchEntry)
	m := lv.Model()
	for i := uint(0); i < m.NItems(); i++ {
		lines = append(lines, m.Item(i).Cast().(*gtk.StringObject).String())
	}
	sel, _ = m.Cast().(*gtk.SingleSelection)
	return
}

const collapsibleCfg = `
[[items]]
type        = "list"
id          = "row"
collapsible = true
match       = "smart"
options     = ["Yazi\tseek forward", "Yazi\tquit", "mpv\tseek 5 seconds", "mpv\tpause"]
[submit]
stdout = "{row}"
`

func TestCollapsibleListFoldsAndSearches(t *testing.T) {
	d, ended := guiDialog(t, collapsibleCfg, nil, nil)
	lines, sel, search := listLines(t, d)
	if len(lines) != 2 || lines[0] != "▸ Yazi  (2)" || lines[1] != "▸ mpv  (2)" {
		t.Fatalf("starts folded: %q", lines)
	}
	if sel == nil || sel.Selected() != 0 {
		t.Fatalf("first header should be selected")
	}

	// Enter on a header unfolds it and does not submit.
	if !d.onKey(gdk.KEY_Return, 0, nil) {
		t.Error("Enter on a header should be handled")
	}
	lines, sel, _ = listLines(t, d)
	if len(lines) != 4 || lines[0] != "▾ Yazi  (2)" || lines[1] != "    seek forward" {
		t.Fatalf("after Enter: %q", lines)
	}
	if len(*ended) != 0 {
		t.Fatalf("a header must not submit: %v", *ended)
	}
	// Left on a row folds its group and lands on the header.
	sel.SetSelected(1)
	d.onKey(gdk.KEY_Left, 0, nil)
	lines, sel, _ = listLines(t, d)
	if len(lines) != 2 || sel.Selected() != 0 {
		t.Fatalf("Left on a row: %q selected %d", lines, sel.Selected())
	}
	// Right unfolds the header it is on; Left folds it again.
	d.onKey(gdk.KEY_Right, 0, nil)
	if lines, _, _ = listLines(t, d); len(lines) != 4 {
		t.Fatalf("Right: %q", lines)
	}
	d.onKey(gdk.KEY_Left, 0, nil)
	if lines, _, _ = listLines(t, d); len(lines) != 2 {
		t.Fatalf("Left on a header: %q", lines)
	}

	// Typing searches inside folded groups and opens the matching ones.
	typeInto(search, "seek")
	lines, sel, _ = listLines(t, d)
	want := []string{"▾ Yazi  (1)", "    seek forward", "▾ mpv  (1)", "    seek 5 seconds"}
	if len(lines) != len(want) {
		t.Fatalf("search: %q", lines)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("search line %d = %q, want %q", i, lines[i], want[i])
		}
	}
	// Enter on a row submits it, without its group.
	sel.SetSelected(3)
	d.onKey(gdk.KEY_Return, 0, nil)
	if len(*ended) != 1 || !(*ended)[0].submitted || d.out.String() != "seek 5 seconds\n" {
		t.Fatalf("submit: ended=%v out=%q", *ended, d.out.String())
	}
	// Clearing the search brings the folds back.
}

func TestCollapsibleHeaderEnterWhileSearchingDoesNotSubmit(t *testing.T) {
	d, ended := guiDialog(t, collapsibleCfg, nil, nil)
	_, sel, search := listLines(t, d)
	typeInto(search, "seek")
	sel.SetSelected(0) // a header
	d.onKey(gdk.KEY_Return, 0, nil)
	if len(*ended) != 0 {
		t.Fatalf("Enter on a header while searching must not submit: %v", *ended)
	}
}

func TestCollapsibleExpandedStart(t *testing.T) {
	cfg := "[[items]]\ntype = \"list\"\nid = \"row\"\ncollapsible = true\ncollapsed = false\noptions = [\"A\\tone\", \"A\\ttwo\"]\n"
	d, _ := guiDialog(t, cfg, nil, nil)
	if lines, _, _ := listLines(t, d); len(lines) != 3 || lines[0] != "▾ A  (2)" {
		t.Fatalf("collapsed = false: %q", lines)
	}
}

func TestCollapsibleConfigErrors(t *testing.T) {
	for name, src := range map[string]string{
		"multi":      "[[items]]\ntype=\"list\"\nid=\"x\"\ncollapsible=true\nmulti=true\noptions=[\"a\"]\n",
		"not a list": "[[items]]\ntype=\"field\"\nid=\"x\"\ncollapsible=true\n",
	} {
		if _, err := config.ParseConfig([]byte(src), func(string) (string, bool) { return "", false }); err == nil {
			t.Errorf("%s: should be rejected", name)
		}
	}
}

func appsTestDir(t *testing.T) (dir string, marker string) {
	t.Helper()
	dir = t.TempDir()
	marker = filepath.Join(t.TempDir(), "launched")
	writeDesktop(t, dir, "alpha.desktop", "[Desktop Entry]\nType=Application\nName=Alpha Editor\nComment=Edit things\nExec=alpha-editor --flag %U\n")
	writeDesktop(t, dir, "beta.desktop", "[Desktop Entry]\nType=Application\nName=Beta Browser\nExec=beta-browser\n")
	writeDesktop(t, dir, "hidden.desktop", "[Desktop Entry]\nType=Application\nName=Hidden One\nExec=h\nNoDisplay=true\n")
	return dir, marker
}

func TestAppsItemSearchesAndLaunches(t *testing.T) {
	dir, marker := appsTestDir(t)
	cfg := "[[items]]\ntype = \"apps\"\nid = \"app\"\n[apps]\npaths = [\"" + dir + "\"]\ndesktop = [\"Hyprland\"]\n[apps.launch]\ncommand = \"echo {exec} > " + marker + "\"\n"
	env := append(os.Environ(), "XDG_STATE_HOME="+t.TempDir())
	d, ended := guiDialog(t, cfg, nil, env)

	lines, _, search := listLines(t, d)
	if len(lines) != 2 { // the NoDisplay entry is not listed
		t.Fatalf("lines = %q", lines)
	}
	typeInto(search, "beta")
	if lines, _, _ = listLines(t, d); len(lines) != 1 || lines[0] != "Beta Browser" {
		t.Fatalf("search beta: %q", lines)
	}
	typeInto(search, "")
	typeInto(search, "alp")
	if lines, _, _ = listLines(t, d); len(lines) != 1 || lines[0] != "Alpha Editor  —  Edit things" {
		t.Fatalf("search alp: %q", lines)
	}
	d.onKey(gdk.KEY_Return, 0, nil)
	if len(*ended) != 1 || !(*ended)[0].submitted {
		t.Fatalf("Enter should launch and finish: %v", *ended)
	}
	var got []byte
	for i := 0; i < 50; i++ {
		if got, _ = os.ReadFile(marker); len(got) > 0 {
			break
		}
		time.Sleep(40 * time.Millisecond)
	}
	// %U is dropped from the Exec line; the command template wraps the rest.
	if string(got) != "alpha-editor --flag\n" {
		t.Fatalf("launched command wrote %q", got)
	}
}

func TestAppsItemWithSubmitStdoutDoesNotLaunch(t *testing.T) {
	dir, marker := appsTestDir(t)
	cfg := "[[items]]\ntype = \"apps\"\nid = \"app\"\n[apps]\npaths = [\"" + dir + "\"]\ndesktop = [\"Hyprland\"]\n[apps.launch]\ncommand = \"echo x > " + marker + "\"\n[submit]\nstdout = \"{app}\"\n"
	d, ended := guiDialog(t, cfg, nil, append(os.Environ(), "XDG_STATE_HOME="+t.TempDir()))
	_, _, search := listLines(t, d)
	typeInto(search, "beta")
	d.onKey(gdk.KEY_Return, 0, nil)
	if len(*ended) != 1 || d.out.String() != "beta\n" {
		t.Fatalf("stdout mode: ended=%v out=%q", *ended, d.out.String())
	}
	time.Sleep(150 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Error("an explicit [submit] must replace the default launch")
	}
}

func TestAppsItemRejectsListOptions(t *testing.T) {
	if _, err := config.ParseConfig([]byte("[[items]]\ntype=\"apps\"\nid=\"a\"\nmulti=true\n"), func(string) (string, bool) { return "", false }); err == nil {
		t.Error("apps + multi should be rejected")
	}
}

func TestSetAppIcon(t *testing.T) {
	gtk.Init()
	if gdk.DisplayGetDefault() == nil {
		t.Skip("no display")
	}
	// A theme icon name.
	img := gtk.NewImage()
	setAppIcon(img, "folder", 32)
	if img.PixelSize() != 32 || img.IconName() == "" {
		t.Errorf("theme icon: size=%d name=%q", img.PixelSize(), img.IconName())
	}
	// An absolute file path.
	dir := t.TempDir()
	png := filepath.Join(dir, "i.png")
	f, err := os.Create(png)
	if err != nil {
		t.Fatal(err)
	}
	im := image.NewRGBA(image.Rect(0, 0, 8, 8))
	if err := png_.Encode(f, im); err != nil {
		t.Fatal(err)
	}
	f.Close()
	img = gtk.NewImage()
	setAppIcon(img, png, 48)
	if img.PixelSize() != 48 || img.StorageType() == gtk.ImageEmpty {
		t.Errorf("file icon: size=%d storage=%v", img.PixelSize(), img.StorageType())
	}
	// Unknown names and missing files fall back to a generic icon.
	for _, bad := range []string{"", "definitely-not-an-icon-name", "/no/such/file.png"} {
		img = gtk.NewImage()
		setAppIcon(img, bad, 32)
		if img.IconName() != "application-x-executable" {
			t.Errorf("fallback for %q: %q", bad, img.IconName())
		}
	}
}

// --- launcher sources -----------------------------------------------------------

func sourcesCfg(dir, marker string, extra string) string {
	return "[[items]]\ntype = \"apps\"\nid = \"app\"\n" + extra +
		"[apps]\npaths = [\"" + dir + "\"]\ndesktop = [\"Hyprland\"]\n" +
		"[[sources]]\nname = \"calc\"\nprefix = \"=\"\ndelay = 0\n" +
		"exec = \"printf '%s\\\\t%s\\\\t%s\\\\n' \\\"answer: $SPOT_QUERY\\\" \\\"v:$SPOT_QUERY\\\" 'a comment'\"\n" +
		"activate = \"printf %s {value} > " + marker + "\"\n"
}

func TestSourcePrefixSwitchesTheListAndEnterActivates(t *testing.T) {
	dir, marker := appsTestDir(t)
	env := append(os.Environ(), "XDG_STATE_HOME="+t.TempDir())
	d, ended := guiDialog(t, sourcesCfg(dir, marker, ""), nil, env)
	lines, _, search := listLines(t, d)
	if len(lines) != 2 {
		t.Fatalf("apps first: %q", lines)
	}
	typeInto(search, "= 2+2")
	lines, _, _ = listLines(t, d)
	if len(lines) != 1 || lines[0] != "answer: 2+2" {
		t.Fatalf("the source's rows replace the apps: %q", lines)
	}
	// The prefix alone is below min_chars: an empty list, not the apps.
	typeInto(search, "=")
	if lines, _, _ = listLines(t, d); len(lines) != 0 {
		t.Fatalf("just the prefix: %q", lines)
	}
	typeInto(search, "= 7*6")
	d.onKey(gdk.KEY_Return, 0, nil)
	if len(*ended) != 1 || !(*ended)[0].submitted {
		t.Fatalf("Enter should activate: %v", *ended)
	}
	var got []byte
	for i := 0; i < 50; i++ {
		if got, _ = os.ReadFile(marker); len(got) > 0 {
			break
		}
		time.Sleep(40 * time.Millisecond)
	}
	if string(got) != "v:7*6" { // {value} is the row's second column, shell-quoted
		t.Fatalf("activate wrote %q", got)
	}
}

func TestSourceLeavingThePrefixBringsTheAppsBack(t *testing.T) {
	dir, marker := appsTestDir(t)
	d, _ := guiDialog(t, sourcesCfg(dir, marker, ""), nil, append(os.Environ(), "XDG_STATE_HOME="+t.TempDir()))
	_, _, search := listLines(t, d)
	typeInto(search, "= 1")
	typeInto(search, "beta")
	lines, _, _ := listLines(t, d)
	if len(lines) != 1 || lines[0] != "Beta Browser" {
		t.Fatalf("plain search again: %q", lines)
	}
}

func TestSourceWithoutActivatePrintsTheValue(t *testing.T) {
	dir, _ := appsTestDir(t)
	cfg := "[[items]]\ntype = \"apps\"\nid = \"app\"\n[apps]\npaths = [\"" + dir + "\"]\ndesktop = [\"Hyprland\"]\n" +
		"[[sources]]\nname = \"pick\"\nprefix = \">\"\ndelay = 0\nexec = \"printf 'one\\\\tfirst\\\\ntwo\\\\tsecond\\\\n'\"\n"
	d, ended := guiDialog(t, cfg, nil, append(os.Environ(), "XDG_STATE_HOME="+t.TempDir()))
	_, _, search := listLines(t, d)
	typeInto(search, "> x")
	d.onKey(gdk.KEY_Return, 0, nil)
	if len(*ended) != 1 || d.out.String() != "first\n" {
		t.Fatalf("no activate: ended=%v out=%q", *ended, d.out.String())
	}
}

func TestSpotQueryStartsTheSearchBox(t *testing.T) {
	dir, marker := appsTestDir(t)
	cfg := sourcesCfg(dir, marker, "query = \"= 5\"\n")
	d, _ := guiDialog(t, cfg, nil, append(os.Environ(), "XDG_STATE_HOME="+t.TempDir()))
	_, _, search := listLines(t, d)
	if search.Text() != "= 5" {
		t.Fatalf("search text = %q", search.Text())
	}
	// let its delayed change fire: the source answers for the preset query
	typeInto(search, "= 5")
	if lines, _, _ := listLines(t, d); len(lines) != 1 || lines[0] != "answer: 5" {
		t.Fatalf("preset query: %q", lines)
	}
}

func runBarCfg(dir string, hide bool) string {
	h := ""
	if hide {
		h = "hide_empty = true\n"
	}
	return "[[items]]\ntype = \"apps\"\nid = \"app\"\n" + h +
		"[apps]\npaths = [\"" + dir + "\"]\ndesktop = [\"Hyprland\"]\n" +
		"[[sources]]\nname = \"run\"\ndefault = true\ndelay = 0\n" +
		"exec = \"printf 'run: %s\\\\t%s\\\\n' \\\"$SPOT_QUERY\\\" \\\"$SPOT_QUERY\\\"\"\n" +
		"[[sources]]\nname = \"term\"\nprefix = \">>\"\ndelay = 0\n" +
		"exec = \"printf 'in a terminal: %s\\\\t%s\\\\n' \\\"$SPOT_QUERY\\\" \\\"$SPOT_QUERY\\\"\"\n"
}

func TestDefaultSourceOwnsTheSearchAndHidesTheApps(t *testing.T) {
	dir, _ := appsTestDir(t)
	d, _ := guiDialog(t, runBarCfg(dir, true), nil, append(os.Environ(), "XDG_STATE_HOME="+t.TempDir()))
	lines, _, search := listLines(t, d)
	if len(lines) != 0 {
		t.Fatalf("a default source means no app list: %q", lines)
	}
	scroll := widgetByClass(d.root, "spot-list-scroll")
	if gtk.BaseWidget(scroll).Visible() {
		t.Error("hide_empty: the list must be hidden while it is empty")
	}
	typeInto(search, "htop -x")
	lines, _, _ = listLines(t, d)
	if len(lines) != 1 || lines[0] != "run: htop -x" {
		t.Fatalf("no prefix typed: %q", lines)
	}
	if !gtk.BaseWidget(scroll).Visible() {
		t.Error("the list must appear once there are rows")
	}
	typeInto(search, ">> htop")
	if lines, _, _ = listLines(t, d); len(lines) != 1 || lines[0] != "in a terminal: htop" {
		t.Fatalf("a real prefix still wins: %q", lines)
	}
	typeInto(search, "")
	if lines, _, _ = listLines(t, d); len(lines) != 0 || gtk.BaseWidget(scroll).Visible() {
		t.Fatalf("empty again: %q visible=%v", lines, gtk.BaseWidget(scroll).Visible())
	}
}

func TestListStaysVisibleWithoutHideEmpty(t *testing.T) {
	dir, _ := appsTestDir(t)
	d, _ := guiDialog(t, runBarCfg(dir, false), nil, append(os.Environ(), "XDG_STATE_HOME="+t.TempDir()))
	if scroll := widgetByClass(d.root, "spot-list-scroll"); !gtk.BaseWidget(scroll).Visible() {
		t.Error("without hide_empty the list is always shown")
	}
}

func TestHideEmptyOnAPlainList(t *testing.T) {
	cfg := "[[items]]\ntype = \"list\"\nid = \"c\"\nhide_empty = true\nmatch = \"contains\"\noptions = [\"alpha\", \"beta\"]\n"
	d, _ := guiDialog(t, cfg, nil, nil)
	_, _, search := listLines(t, d)
	scroll := widgetByClass(d.root, "spot-list-scroll")
	if !gtk.BaseWidget(scroll).Visible() {
		t.Fatal("rows present: visible")
	}
	typeInto(search, "zzz")
	if gtk.BaseWidget(scroll).Visible() {
		t.Error("no match: hidden")
	}
	typeInto(search, "alp")
	if !gtk.BaseWidget(scroll).Visible() {
		t.Error("a match: visible again")
	}
	if _, err := config.ParseConfig([]byte("[[items]]\ntype=\"field\"\nid=\"x\"\nhide_empty=true\n"), func(string) (string, bool) { return "", false }); err == nil {
		t.Error("hide_empty on a field should be rejected")
	}
}

func TestDialogPlacementMarginsAreApplied(t *testing.T) {
	cfg := "anchor = \"top left\"\nmargin_left = 30\noffset_x = 20\noffset_y = 90\n[[items]]\ntype = \"list\"\nid = \"c\"\noptions = [\"a\"]\n"
	d, _ := guiDialog(t, cfg, nil, nil)
	root := gtk.BaseWidget(d.root)
	if root.HAlign() != gtk.AlignStart || root.VAlign() != gtk.AlignStart {
		t.Errorf("anchor: %v %v", root.HAlign(), root.VAlign())
	}
	if got := [4]int{root.MarginTop(), root.MarginBottom(), root.MarginStart(), root.MarginEnd()}; got != [4]int{90, 0, 50, 0} {
		t.Errorf("margins (top, bottom, start, end) = %v, want [90 0 50 0]", got)
	}
	// A centred dialog shifted up 50 px keeps a 100 px margin below it.
	d, _ = guiDialog(t, "offset_y = -50\n[[items]]\ntype = \"list\"\nid = \"c\"\noptions = [\"a\"]\n", nil, nil)
	root = gtk.BaseWidget(d.root)
	if got := [2]int{root.MarginTop(), root.MarginBottom()}; got != [2]int{0, 100} {
		t.Errorf("centred, up 50: top/bottom = %v, want [0 100]", got)
	}
}

func TestHiddenPointerDoesNotTargetTheDialog(t *testing.T) {
	d, _ := guiDialog(t, listCfg, nil, nil)
	f := newFader(gtk.NewWindow())
	check := func(when string, want bool) {
		t.Helper()
		if got := d.root.CanTarget(); got != want {
			t.Errorf("%s: CanTarget = %v, want %v", when, got, want)
		}
	}
	f.reset(&config.Opacity{}, d.root)
	check("on open", false)
	f.wake()
	check("pointer moved", true)
	f.keyPressed()
	check("key pressed", false)
}

// writeDesktop puts a .desktop file into an applications directory.
func writeDesktop(t *testing.T, dir, file, body string) {
	t.Helper()
	p := filepath.Join(dir, file)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
