//go:build gui

package ui

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"gitlab.com/KopyTKG/spot/internal/config"
)

type menuMods struct {
	sync.Mutex
	values map[string]string
	slow   chan struct{} // Get waits for it
	asked  []string
	fail   error
}

func (m *menuMods) Get(full string) (json.RawMessage, string, error) {
	if m.slow != nil {
		<-m.slow
	}
	m.Lock()
	defer m.Unlock()
	m.asked = append(m.asked, "get "+full)
	v, ok := m.values[full]
	if !ok {
		return nil, "", errors.New("mod m is not approved")
	}
	return json.RawMessage(v), "", nil
}

func (m *menuMods) Call(full string, args []string) error {
	m.Lock()
	defer m.Unlock()
	m.asked = append(m.asked, full+"("+strings.Join(args, ",")+")")
	return m.fail
}

func (m *menuMods) Parse(string, []byte) (json.RawMessage, error) { return nil, errors.New("no") }

func (m *menuMods) log() string {
	m.Lock()
	defer m.Unlock()
	return strings.Join(m.asked, " ")
}

func pump(until func() bool) bool {
	ctx := glib.MainContextDefault()
	for end := time.Now().Add(3 * time.Second); time.Now().Before(end); {
		ctx.Iteration(false)
		if until() {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return false
}

const dynMenu = `
[[page.main.entry]]
label = "Remote"
page = "groups"
[[page.main.entry]]
label = "Do"
page = "do"
[page.groups]
from = "m.groups"
then = "hosts"
[[page.groups.entry]]
label = "Custom"
exec = "printf custom > OUT"
[page.hosts]
from = "m.hosts"
group = true
exec = 'printf "%s|%s" "$SPOT_VALUE" "$SPOT_LABEL" > OUT'
[page.do]
from = "m.hosts"
call = "m.go"
`

func hostile() *menuMods {
	return &menuMods{values: map[string]string{
		"m.groups": `[{"label":"Home","key":"home"},{"label":"Cloud","key":"cloud"}]`,
		"m.hosts":  `[{"label":"nas","key":"nas","group":"home"},{"label":"evil","key":"$(touch OUT.evil); x","group":"cloud"},{"label":"opt","key":"-oProxyCommand=touch OUT.evil","group":"cloud"}]`,
	}}
}

func openDyn(t *testing.T, mods *menuMods, page string) *openMenu {
	t.Helper()
	gtk.Init()
	if gdk.DisplayGetDefault() == nil {
		t.Skip("no display")
	}
	m := &openMenu{t: t, out: t.TempDir() + "/ran"}
	for k, v := range mods.values {
		mods.values[k] = strings.ReplaceAll(v, "OUT", m.out)
	}
	cfg, err := config.ParseConfig([]byte(strings.ReplaceAll(dynMenu, "OUT", m.out)), func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "SPOT_VALUE=inherited", "SPOT_VALUE_X=inherited", "SPOT_LABEL=inherited")
	m.d = newMenu(cfg, "/tmp/menu.toml", page, env, t.TempDir(), nil, mods)
	m.d.finish = func(r result) { m.ended = append(m.ended, r) }
	return m
}

func (m *openMenu) waitRows(rows string) {
	m.t.Helper()
	if !pump(func() bool { _, r, _ := m.state(); return strings.Join(r, " | ") == rows }) {
		_, r, _ := m.state()
		m.t.Fatalf("rows [%s], want [%s]", strings.Join(r, " | "), rows)
	}
}

func (m *openMenu) problem() string {
	var labels []*gtk.Label
	find[*gtk.Label](m.d.root, &labels)
	for _, l := range labels {
		if l.HasCSSClass("spot-error") && l.Visible() {
			return l.Text()
		}
	}
	return ""
}

func TestMenuRowsFromAMod(t *testing.T) {
	mods := hostile()
	mods.slow = make(chan struct{})
	m := openDyn(t, mods, "")
	m.press(gdk.KEY_Return) // into Remote, whose rows are not here yet
	// Nothing to pick while they are fetched: not even the written entry.
	if _, rows, _ := m.state(); len(rows) != 0 {
		t.Fatalf("early rows %v", rows)
	}
	m.press(gdk.KEY_Return)
	if len(m.ended) != 0 {
		t.Fatal("an early Enter picked something")
	}
	close(mods.slow)
	m.waitRows("Home | Cloud | Custom")
	m.press(gdk.KEY_Down, gdk.KEY_Return)
	m.waitRows("evil | opt") // only that group's
	m.press(gdk.KEY_BackSpace)
	m.waitRows("Home | Cloud | Custom")
	if _, _, sel := m.state(); sel != 1 {
		t.Errorf("Back lands on row %d", sel)
	}
	m.press(gdk.KEY_Up, gdk.KEY_Return)
	m.waitRows("nas")
	m.press(gdk.KEY_Return)
	m.ran("nas|nas")
	if strings.Count(mods.log(), "get m.hosts") != 1 {
		t.Errorf("fetched again: %s", mods.log())
	}
}

// What a row holds is never part of the command.
func TestMenuHostileRows(t *testing.T) {
	m := openDyn(t, hostile(), "")
	m.press(gdk.KEY_Return)
	m.waitRows("Home | Cloud | Custom")
	m.press(gdk.KEY_Down, gdk.KEY_Return)
	m.waitRows("evil | opt")
	m.press(gdk.KEY_Down, gdk.KEY_Return) // starts with a dash
	if len(m.ended) != 0 || !strings.Contains(m.problem(), "dash") {
		t.Fatalf("a leading dash: %v %q", m.ended, m.problem())
	}
	m.press(gdk.KEY_Up, gdk.KEY_Return)
	want := "$(touch " + m.out + ".evil); x|evil"
	m.ran(want)
	if _, err := os.Stat(m.out + ".evil"); err == nil {
		t.Error("a row ran something")
	}
}

func TestMenuCallAndFailures(t *testing.T) {
	mods := hostile()
	mods.fail = errors.New("the mod said no")
	m := openDyn(t, mods, "do")
	m.waitRows("nas | evil | opt")
	m.press(gdk.KEY_Return)
	if !pump(func() bool { return m.problem() == "the mod said no" }) || len(m.ended) != 0 {
		t.Fatalf("%q %v", m.problem(), m.ended)
	}
	mods.Lock()
	mods.fail = nil
	mods.Unlock()
	m.press(gdk.KEY_Return)
	if !pump(func() bool { return len(m.ended) == 1 }) || !strings.HasSuffix(mods.log(), "m.go(nas)") {
		t.Errorf("%v %s", m.ended, mods.log())
	}

	// A mod that cannot answer: said so, and the written entries still work.
	m = openDyn(t, &menuMods{values: map[string]string{}}, "groups")
	m.waitRows("Custom")
	if !strings.Contains(m.problem(), "not approved") {
		t.Errorf("%q", m.problem())
	}
	m.press(gdk.KEY_BackSpace)
	if m.problem() != "" {
		t.Errorf("the message stays on the next page: %q", m.problem())
	}
}

// A menu closed, or left, before a slow mod answers is not touched by it.
func TestMenuLeftBeforeTheModAnswers(t *testing.T) {
	mods := hostile()
	mods.slow = make(chan struct{})
	m := openDyn(t, mods, "")
	m.press(gdk.KEY_Return, gdk.KEY_BackSpace)
	close(mods.slow)
	pump(func() bool { return strings.Contains(mods.log(), "get") })
	pump(func() bool { return false })
	m.waitRows("Remote | Do")
}

func TestMenuTerminalGetsTheRowInside(t *testing.T) {
	out := t.TempDir() + "/o"
	cfg := &config.Config{Menu: &config.MenuConfig{Terminal: "env -i PATH=/usr/bin"}} // passes nothing on
	cmd, env := menuCommand(cfg, "/c", menuRow{entry: config.Entry{Terminal: `printf %s "$SPOT_VALUE" > ` + out}}, []string{"PATH=/usr/bin"},
		[]string{"SPOT_VALUE=a'b $(id) `x`"})
	if _, se, status := runCommand(cmd, env, "/"); status != 0 {
		t.Fatal(se)
	}
	if b, _ := os.ReadFile(out); string(b) != "a'b $(id) `x`" {
		t.Errorf("%q from %q", b, cmd)
	}
}
