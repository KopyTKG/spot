//go:build gui

package main

// Exercises the dialog's Back handling against a real GTK display. Nothing is
// shown on screen: widgets are built and their key handler is called directly.
//
//	go test -tags gui -run Back ./...

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

func guiDialog(t *testing.T, toml string, back *backSpec, env []string) (*dialog, *[]result) {
	t.Helper()
	gtk.Init()
	if gdk.DisplayGetDefault() == nil {
		t.Skip("no display")
	}
	cfg, err := parseConfig([]byte(toml), func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatal(err)
	}
	d := newDialog(cfg, env, t.TempDir(), back)
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
		d, ended := guiDialog(t, listCfg, &backSpec{label: "Back"}, nil)
		handled := d.onKey(tc.key, tc.state, tc.focus())
		got := len(*ended) == 1 && (*ended)[0].code == exitBack
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
	d, ended := guiDialog(t, listCfg, &backSpec{label: "Back", cmd: cmd}, env)
	d.onKey(gdk.KEY_Left, gdk.AltMask, nil)
	if len(*ended) != 1 || (*ended)[0].code != exitBack {
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
	d, ended := guiDialog(t, listCfg, &backSpec{label: "Back", cmd: "touch " + out}, os.Environ())
	d.onKey(gdk.KEY_Escape, 0, nil)
	time.Sleep(200 * time.Millisecond)
	if len(*ended) != 1 || (*ended)[0].code != 0 || (*ended)[0].submitted {
		t.Fatalf("Esc should cancel: %v", *ended)
	}
	if _, err := os.Stat(out); err == nil {
		t.Fatal("Esc must not run the Back command")
	}
}
