//go:build gui

package ui

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"gitlab.com/KopyTKG/spot/internal/protocol"
	"gitlab.com/KopyTKG/spot/internal/scheduler"
)

// fakeMods stands for the daemon's mods.
type fakeMods struct {
	mu      sync.Mutex
	values  map[string]string
	warning string
	down    bool
	calls   []string
	onCall  func(f *fakeMods, args []string)
}

func (f *fakeMods) Get(full string) (json.RawMessage, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		return nil, "", errors.New(full + " is not answering")
	}
	return json.RawMessage(f.values[full]), f.warning, nil
}

func (f *fakeMods) Call(full string, args []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		return errors.New("it is not answering")
	}
	f.calls = append(f.calls, full+"("+strings.Join(args, ",")+")")
	if f.onCall != nil {
		f.onCall(f, args)
	}
	return nil
}

func (f *fakeMods) Parse(string, []byte) (json.RawMessage, error) {
	return json.RawMessage(f.values["parsed"]), nil
}

func (f *fakeMods) made() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.calls, " ")
}

// modDialog builds a dialog the way the daemon does: the job first fetches what
// the items show.
func modDialog(t *testing.T, toml string, mods *fakeMods) (*dialog, *[]result) {
	t.Helper()
	gtk.Init()
	if gdk.DisplayGetDefault() == nil {
		t.Skip("no display")
	}
	path := filepath.Join(t.TempDir(), "d.toml")
	os.WriteFile(path, []byte(toml), 0o600)
	j, err := scheduler.BuildJob(protocol.Request{Op: protocol.OpShow, Config: path, Env: os.Environ(), Dir: t.TempDir()}, mods)
	if err != nil {
		t.Fatal(err)
	}
	d := newDialog(j.Cfg, j.Env, j.Dir, nil, mods)
	var ended []result
	d.finish = func(r result) { ended = append(ended, r) }
	return d, &ended
}

func TestAListFromAMod(t *testing.T) {
	m := &fakeMods{values: map[string]string{"net.table": `[{"label":"first row","key":"k1"},{"label":"second\trow","key":"k\t2"}]`}}
	d, ended := modDialog(t, "[[items]]\ntype='list'\nid='s'\nfrom='net.table'\n[submit]\nstdout='{s}'\n", m)
	d.onKey(gdk.KEY_Return, 0, nil)
	if got := strings.TrimSpace(d.out.String()); got != "k1" || len(*ended) != 1 {
		t.Errorf("submitted %q, ended %v: the value is the row's key", got, *ended)
	}
}

func TestAListThatCannotBeHadStillOpensAndSaysWhy(t *testing.T) {
	for name, m := range map[string]*fakeMods{
		"down":       {down: true},
		"not rows":   {values: map[string]string{"net.table": `{"a":1}`}},
		"no label":   {values: map[string]string{"net.table": `[{"key":"x"}]`}},
		"old values": {values: map[string]string{"net.table": `[]`}, warning: "this is from 5s ago"},
	} {
		d, _ := modDialog(t, "[[items]]\ntype='list'\nid='s'\nfrom='net.table'\n", m)
		realize(t, d)
		waitFor(t, name+": the reason to be shown", func() bool { return errorShown(d) != "" })
	}
}

func TestRowsWithAStateAreSwitches(t *testing.T) {
	m := &fakeMods{values: map[string]string{"svc.list": `[{"label":"ssh","key":"sshd","state":false}]`}}
	m.onCall = func(f *fakeMods, args []string) {
		f.values["svc.list"] = `[{"label":"ssh","key":"sshd","state":true}]`
	}
	d, ended := modDialog(t, "[[items]]\ntype='list'\nid='s'\nfrom='svc.list'\ncall='svc.set'\n", m)
	realize(t, d)
	d.onKey(gdk.KEY_Return, 0, nil)
	waitFor(t, "the request", func() bool { return m.made() == "svc.set(sshd,true)" })
	settleLoop()
	if len(*ended) != 0 {
		t.Errorf("flipping closed the dialog: %v", *ended)
	}
	// Without call the switch shows and cannot be flipped.
	m2 := &fakeMods{values: m.values}
	d2, _ := modDialog(t, "[[items]]\ntype='list'\nid='s'\nfrom='svc.list'\n", m2)
	realize(t, d2)
	d2.onKey(gdk.KEY_Return, 0, nil)
	settleLoop()
	if m2.made() != "" {
		t.Errorf("a list without call made %s", m2.made())
	}
}

func TestAControlFollowsItsModAndNeverShowsAnOldPosition(t *testing.T) {
	cfg := "[[items]]\ntype='switch'\nid='lock'\nfrom='idle.enabled'\ncall='idle.set'\n[submit]\nstdout='[{lock}]'\n"
	m := &fakeMods{values: map[string]string{"idle.enabled": `true`}}
	d, _ := modDialog(t, cfg, m)
	sw := widgetByClass(d.root, "spot-control")
	outer := widgetByClass(d.root, "spot-switch-switch")
	realize(t, d)
	on := func() bool { return gtk.BaseWidget(outer).HasCSSClass("spot-on") }
	unknown := func() bool { return gtk.BaseWidget(outer).HasCSSClass("spot-unknown") }
	waitFor(t, "the value", func() bool { return on() && !unknown() })
	if !gtk.BaseWidget(sw).IsSensitive() {
		t.Error("it cannot be moved")
	}
	for name, spoil := range map[string]func(*fakeMods){
		"the mod is down": func(m *fakeMods) { m.down = true },
		"an old value":    func(m *fakeMods) { m.warning = "this is from 9s ago" },
		"nonsense":        func(m *fakeMods) { m.values["idle.enabled"] = `"perhaps"` },
		"not one value":   func(m *fakeMods) { m.values["idle.enabled"] = `[1,2]` },
	} {
		m := &fakeMods{values: map[string]string{"idle.enabled": `true`}}
		spoil(m)
		d, _ := modDialog(t, cfg, m)
		outer := widgetByClass(d.root, "spot-switch-switch")
		realize(t, d)
		waitFor(t, name+": unknown", func() bool { return gtk.BaseWidget(outer).HasCSSClass("spot-unknown") && errorShown(d) != "" })
		settleLoop()
		if gtk.BaseWidget(outer).HasCSSClass("spot-on") || gtk.BaseWidget(widgetByClass(d.root, "spot-control")).IsSensitive() {
			t.Errorf("%s: it shows a position, or can be moved", name)
		}
	}
}

func TestAButtonWithCall(t *testing.T) {
	m := &fakeMods{}
	d, ended := modDialog(t, "[[items]]\ntype='button'\nlabel='Go'\ncall='x.go'\nclose=true\n", m)
	b := widgetByClass(d.root, "spot-button")
	realize(t, d)
	b.(*gtk.Button).Activate()
	waitFor(t, "the request, then the dialog closing", func() bool { return m.made() == "x.go()" && len(*ended) == 1 })
}

func TestWhatSomeoneElseChoseIsNotAnOption(t *testing.T) {
	m := &fakeMods{values: map[string]string{"net.table": `[{"label":"row","key":"-rf"}]`}}
	out := filepath.Join(t.TempDir(), "ran")
	d, ended := modDialog(t, "[[items]]\ntype='list'\nid='s'\nfrom='net.table'\n[submit]\nexec='echo ran > "+out+"'\n", m)
	realize(t, d)
	d.onKey(gdk.KEY_Return, 0, nil)
	settleLoop()
	if _, err := os.Stat(out); err == nil || len(*ended) != 0 || !strings.Contains(errorShown(d), "dash") {
		t.Errorf("it ran, or said nothing: %v %q", *ended, errorShown(d))
	}
}

// fakeCopy puts a wl-copy that records what it is given first in PATH.
func fakeCopy(t *testing.T) (copied string) {
	dir := t.TempDir()
	copied = filepath.Join(dir, "copied")
	os.WriteFile(filepath.Join(dir, "wl-copy"), []byte("#!/bin/sh\nprintf '%s|%d' \"$(cat)\" $# > "+copied+"\n"), 0o755)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	return copied
}

// Nothing of the value is read by a shell, and nothing becomes an option.
func TestSubmitCopy(t *testing.T) {
	copied := fakeCopy(t)
	nasty := "-n $(touch pwned); `x` 'q'"
	m := &fakeMods{values: map[string]string{"n.t": `[{"label":"row","key":` + strconv.Quote(nasty) + `}]`}}
	d, ended := modDialog(t, "[[items]]\ntype='list'\nid='s'\nfrom='n.t'\n[submit]\ncopy='{s}'\n", m)
	realize(t, d)
	d.onKey(gdk.KEY_Return, 0, nil)
	waitFor(t, "the dialog to end", func() bool { return len(*ended) == 1 })
	if got := read(copied); got != nasty+"|0" {
		t.Errorf("copied %q", got)
	}
}

func TestSubmitCall(t *testing.T) {
	cfg := "[[items]]\ntype='list'\nid='s'\nfrom='n.t'\n[submit]\ncall='net.connect'\nargs=['{s}','fixed']\nstdout='done {s}'\n"
	value := map[string]string{"n.t": `[{"label":"row","key":"home"}]`}
	m := &fakeMods{values: value}
	d, ended := modDialog(t, cfg, m)
	realize(t, d)
	d.onKey(gdk.KEY_Return, 0, nil)
	waitFor(t, "the request, then the end", func() bool { return len(*ended) == 1 })
	if m.made() != "net.connect(home,fixed)" || strings.TrimSpace(d.out.String()) != "done home" {
		t.Errorf("%s, %q", m.made(), d.out.String())
	}
	// A request that fails keeps the dialog open and says why.
	m = &fakeMods{values: value}
	d, ended = modDialog(t, cfg, m)
	realize(t, d)
	m.mu.Lock()
	m.down = true
	m.mu.Unlock()
	d.onKey(gdk.KEY_Return, 0, nil)
	waitFor(t, "the reason", func() bool { return errorShown(d) != "" })
	if len(*ended) != 0 || d.out.Len() != 0 {
		t.Errorf("it ended anyway: %v %q", *ended, d.out.String())
	}
	// A value that would be read as an option is not passed on.
	m = &fakeMods{values: map[string]string{"n.t": `[{"label":"row","key":"--all"}]`}}
	d, _ = modDialog(t, cfg, m)
	realize(t, d)
	d.onKey(gdk.KEY_Return, 0, nil)
	settleLoop()
	if m.made() != "" || !strings.Contains(errorShown(d), "dash") {
		t.Errorf("%q %q", m.made(), errorShown(d))
	}
}

// A list whose rows have icons still reads each row's label and switch.
func TestSwitchRowsWithIcons(t *testing.T) {
	m := &fakeMods{values: map[string]string{"n.t": `[{"label":"one","key":"1","icon":"network-wired","state":false},{"label":"two","key":"2"}]`}}
	d, _ := modDialog(t, "[[items]]\ntype='list'\nid='s'\nfrom='n.t'\ncall='n.set'\n", m)
	realize(t, d)
	d.onKey(gdk.KEY_Return, 0, nil)
	waitFor(t, "the switch of the row with an icon", func() bool { return m.made() == "n.set(1,true)" })
}
