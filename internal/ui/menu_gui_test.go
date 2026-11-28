//go:build gui

package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"gitlab.com/KopyTKG/spot/internal/config"
	"gitlab.com/KopyTKG/spot/internal/protocol"
)

// A menu three pages deep, two of whose pages hold a "Kitty".
const treeMenu = `
title = "Menu"
[[page.main.entry]]
label = "  Install"
page = "install"
[[page.main.entry]]
label = "  Remove"
page = "remove"
[[page.main.entry]]
label = "Kit list"
exec = "printf here > OUT"
[page.install]
title = "Install"
[[page.install.entry]]
label = "Other"
exec = "true"
[[page.install.entry]]
label = "  Terminal"
page = "install-term"
[page.install-term]
title = "Terminal"
[[page.install-term.entry]]
label = "Kitty"
exec = "printf install > OUT"
[page.remove]
title = "Remove"
[[page.remove.entry]]
label = "Kitty"
exec = "printf remove > OUT"
[[page.remove.entry]]
label = "Home"
page = "main"
`

type openMenu struct {
	t     *testing.T
	d     *dialog
	ended []result
	out   string
}

func newOpenMenu(t *testing.T, toml, page string, back *config.BackSpec) *openMenu {
	t.Helper()
	gtk.Init()
	if gdk.DisplayGetDefault() == nil {
		t.Skip("no display")
	}
	m := &openMenu{t: t, out: filepath.Join(t.TempDir(), "ran")}
	cfg, err := config.ParseConfig([]byte(strings.ReplaceAll(toml, "OUT", m.out)), func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatal(err)
	}
	m.d = newMenu(cfg, "/tmp/menu.toml", page, os.Environ(), t.TempDir(), back, nil)
	m.d.finish = func(r result) { m.ended = append(m.ended, r) }
	return m
}

func (m *openMenu) press(keys ...uint) {
	for _, k := range keys {
		m.d.onKey(k, 0, nil)
	}
}

// find walks the widgets of the menu.
func find[T gtk.Widgetter](w gtk.Widgetter, out *[]T) {
	if x, ok := w.(T); ok {
		*out = append(*out, x)
	}
	for c := gtk.BaseWidget(w).FirstChild(); c != nil; c = gtk.BaseWidget(c).NextSibling() {
		find(c, out)
	}
}

func (m *openMenu) state() (title string, rows []string, selected int) {
	var views []*gtk.ListView
	find[*gtk.ListView](m.d.root, &views)
	sel := views[0].Model().Cast().(*gtk.SingleSelection)
	for i := uint(0); i < sel.NItems(); i++ {
		rows = append(rows, config.WithoutIcon(sel.Item(i).Cast().(*gtk.StringObject).String()))
	}
	var labels []*gtk.Label
	find[*gtk.Label](m.d.root, &labels)
	for _, l := range labels {
		if l.HasCSSClass("spot-title") && l.Visible() {
			title = l.Text()
		}
	}
	var headers []*gtk.CenterBox
	find[*gtk.CenterBox](m.d.root, &headers)
	if headers[0].Visible() {
		title = "‹ " + headers[0].CenterWidget().(*gtk.Label).Text()
	}
	return title, rows, int(sel.Selected())
}

func (m *openMenu) is(what, title, rows string, selected int) {
	m.t.Helper()
	gotTitle, gotRows, gotSel := m.state()
	if gotTitle != title || strings.Join(gotRows, " | ") != rows || gotSel != selected {
		m.t.Errorf("%s: %q [%s] row %d, want %q [%s] row %d", what, gotTitle, strings.Join(gotRows, " | "), gotSel, title, rows, selected)
	}
}

func (m *openMenu) search(text string) {
	var s []*gtk.SearchEntry
	find[*gtk.SearchEntry](m.d.root, &s)
	s[0].SetText(text) // the menu catches up when Enter is pressed, or after GTK's delay
	ctx := glib.MainContextDefault()
	for end := time.Now().Add(400 * time.Millisecond); time.Now().Before(end); {
		ctx.Iteration(false)
		time.Sleep(5 * time.Millisecond)
	}
}

func (m *openMenu) ran(want string) {
	m.t.Helper()
	for range 200 {
		if b, _ := os.ReadFile(m.out); string(b) == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	b, _ := os.ReadFile(m.out)
	m.t.Errorf("ran %q, want %q", b, want)
}

const top = "Install | Remove | Kit list"

func TestMenuPagesAndBack(t *testing.T) {
	m := newOpenMenu(t, treeMenu, "", nil)
	m.is("start", "Menu", top, 0)
	m.press(gdk.KEY_Down, gdk.KEY_Return)
	m.is("into Remove", "‹ Remove", "Kitty | Home", 0)
	m.press(gdk.KEY_BackSpace)
	m.is("Back lands on what was entered", "Menu", top, 1)
	m.press(gdk.KEY_BackSpace)
	m.is("nothing above the first page", "Menu", top, 1)
	m.press(gdk.KEY_Up, gdk.KEY_Return, gdk.KEY_Down, gdk.KEY_Return)
	m.is("two down", "‹ Terminal", "Kitty", 0)
	m.d.onKey(gdk.KEY_Left, gdk.AltMask, nil)
	m.is("one up", "‹ Install", "Other | Terminal", 1)
	m.press(gdk.KEY_Escape)
	if len(m.ended) != 1 || m.ended[0].submitted || m.ended[0].code != 0 {
		t.Errorf("Esc: %+v", m.ended)
	}
}

func TestMenuSearchGoesDeeper(t *testing.T) {
	m := newOpenMenu(t, treeMenu, "", nil)
	m.search("kit")
	m.is("here first, then deeper with the way", "Menu", "Kit list | Remove › Kitty | Install › Terminal › Kitty", 0)
	m.search("inst kit")
	m.is("the way is searched too", "Menu", "Install › Terminal › Kitty", 0)
	m.search("zzzz")
	m.is("nothing", "Menu", "", int(gtk.InvalidListPosition))
	m.press(gdk.KEY_Return)
	if len(m.ended) != 0 {
		t.Errorf("Enter on nothing: %+v", m.ended)
	}
	m.search("inst kit")
	m.press(gdk.KEY_Return)
	if len(m.ended) != 1 || !m.ended[0].submitted || m.ended[0].then != thenNothing {
		t.Errorf("%+v", m.ended)
	}
	m.ran("install")
}

func TestMenuSearchedPageKeepsTheWayBack(t *testing.T) {
	m := newOpenMenu(t, treeMenu, "", nil)
	m.search("terminal")
	m.is("a page deeper down", "Menu", "Install › Terminal | Install › Terminal › Kitty", 0)
	m.press(gdk.KEY_Return)
	m.is("there", "‹ Terminal", "Kitty", 0)
	m.press(gdk.KEY_BackSpace)
	m.is("Back goes up the real way", "‹ Install", "Other | Terminal", 1)
	m.press(gdk.KEY_BackSpace)
	m.is("and up again", "Menu", top, 0)
}

// Pages that lead back to one another: searching ends, and shows no page above.
func TestMenuSearchWithCircles(t *testing.T) {
	m := newOpenMenu(t, treeMenu, "", nil)
	m.press(gdk.KEY_Down, gdk.KEY_Return)
	m.search("kit")
	m.is("only what is below", "‹ Remove", "Kitty", 0)
}

// Enter right after typing takes what is typed, not what was on screen.
func TestMenuEnterBeforeTheSearchCatchesUp(t *testing.T) {
	m := newOpenMenu(t, treeMenu, "", nil)
	var s []*gtk.SearchEntry
	find[*gtk.SearchEntry](m.d.root, &s)
	s[0].SetText("kit list")
	m.press(gdk.KEY_Return)
	m.ran("here")
}

func TestMenuOpenedOnAPage(t *testing.T) {
	m := newOpenMenu(t, treeMenu, "install-term", nil)
	m.is("there", "‹ Terminal", "Kitty", 0)
	m.press(gdk.KEY_BackSpace)
	m.is("up", "‹ Install", "Other | Terminal", 1)
	m.press(gdk.KEY_BackSpace)
	m.is("top", "Menu", top, 0)
}

func TestMenuBackBeyondTheMenu(t *testing.T) {
	out := filepath.Join(t.TempDir(), "back")
	m := newOpenMenu(t, treeMenu, "", &config.BackSpec{Label: "‹ Power", Cmd: "printf %s \"x$SPOT_BACK\" > " + out})
	m.is("the first page offers it", "‹ Menu", top, 0)
	m.press(gdk.KEY_BackSpace)
	if len(m.ended) != 1 || m.ended[0].code != protocol.ExitBack {
		t.Fatalf("%+v", m.ended)
	}
	m.out = out
	m.ran("x")
}

func TestMenuCommands(t *testing.T) {
	out := t.TempDir() + "/o"
	env := []string{"PATH=/usr/bin", "SPOT_BACK=somebody else's"}
	cfg := &config.Config{Menu: &config.MenuConfig{Terminal: "env WRAPPED=1"}}
	row := func(e config.Entry) menuRow { return menuRow{entry: e, page: "p's"} }

	cmd, got := menuCommand(cfg, "/c/menu.toml", row(config.Entry{Exec: "x y"}), env, nil)
	if cmd != "x y" || strings.Join(got, " ") != "PATH=/usr/bin" {
		t.Errorf("exec: %q %q", cmd, got)
	}
	_, got = menuCommand(cfg, "/c d/it's.toml", row(config.Entry{Exec: "x", Back: true}), env, nil)
	// The Back command, run as a shell would, gets its words unharmed.
	back := strings.TrimPrefix(got[len(got)-1], "SPOT_BACK=")
	words, _, _ := runCommand(`set -- `+back+`; shift; printf '[%s]' "$@"`, env, "/")
	if words != "[--page][p's][/c d/it's.toml]" {
		t.Errorf("back: %q from %q", words, back)
	}
	for _, text := range []string{"it's $HOME; `id` && echo \"q\"", "-x\nsecond line"} {
		cmd, _ = menuCommand(cfg, "/c", row(config.Entry{Terminal: "printf %s " + shellQuote(text) + " > " + out}), env, nil)
		if _, se, status := runCommand(cmd, env, "/"); status != 0 {
			t.Fatalf("terminal: %s", se)
		}
		if b, _ := os.ReadFile(out); string(b) != text {
			t.Errorf("terminal: %q", b)
		}
	}
	cmd, _ = menuCommand(cfg, "/c", row(config.Entry{Open: "https://x/?a=1&b='; id"}), env, nil)
	if words, _, _ := runCommand(strings.Replace(cmd, "xdg-open", "printf '[%s]'", 1), env, "/"); words != "[https://x/?a=1&b='; id]" {
		t.Errorf("open: %q from %q", words, cmd)
	}
	if !strings.HasPrefix(cmd, "xdg-open ") {
		t.Errorf("open runs %q", cmd)
	}
}
