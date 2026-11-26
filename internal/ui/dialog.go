// Package ui draws dialogs with GTK: the layer-shell window and one widget tree
// per dialog.
package ui

import (
	"bytes"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"gitlab.com/KopyTKG/spot/internal/config"
	"gitlab.com/KopyTKG/spot/internal/launcher"
	"gitlab.com/KopyTKG/spot/internal/protocol"
)

// result is how a dialog ended.
type result struct {
	submitted bool
	code      int // non-zero: end with this status (on_error = "exit")
}

// dialog is one config rendered as a widget tree. It does not own a window:
// the shell puts d.root into the one layer-shell window and routes keys to
// d.onKey, so dialogs can be swapped without the surface ever going away.
type dialog struct {
	root       *gtk.Box
	firstFocus gtk.Widgetter
	saved      gtk.Widgetter // focus to restore when a covering dialog closes

	out, errOut bytes.Buffer // what `spot` prints to stdout / stderr when this ends

	onKey  func(keyval uint, state gdk.ModifierType, focus gtk.Widgetter) bool // window key press, capture phase
	cancel func()                                                              // Esc, click outside, client gone
	finish func(result)                                                        // set by the shell

	done bool
	busy bool // submit.exec is running

	hooks []func() // undo every callback registered on the widgets; see release
}

// hook records a signal connection so release can undo it.
//
// Every callback must go through here. The bindings keep a callback alive as long
// as the object it is connected to, and one that refers back to that object, or to
// a parent of it, then keeps the whole widget tree alive for ever: in the daemon
// each closed dialog stayed in memory.
func (d *dialog) hook(obj coreglib.Objector, h coreglib.SignalHandle) {
	o := coreglib.BaseObject(obj)
	d.hooks = append(d.hooks, func() { o.HandlerDisconnect(h) })
}

// release lets a finished dialog's widgets be freed. Call it once they are no
// longer on screen: a list without its callbacks cannot draw its rows.
func (d *dialog) release() {
	for _, undo := range d.hooks {
		undo()
	}
	d.hooks, d.onKey, d.cancel = nil, nil, nil
	d.firstFocus, d.saved = nil, nil
}

// focusFirst puts the keyboard on the first input.
func (d *dialog) focusFirst() {
	if g, ok := d.firstFocus.(interface{ GrabFocus() bool }); ok {
		g.GrabFocus()
	}
}

// newDialog builds the widgets for cfg. env and dir are what submit.exec and
// submit.spawn commands run with: the caller's, not the daemon's.
func newDialog(cfg *config.Config, env []string, dir string, back *config.BackSpec) *dialog {
	d := &dialog{}
	var goBack func() // late-bound: needs `end`, defined below

	root := gtk.NewBox(gtk.OrientationVertical, 12)
	root.AddCSSClass("spot-root")
	if cfg.Width > 0 || cfg.Height > 0 {
		w, h := cfg.Width, cfg.Height
		if w == 0 {
			w = -1
		}
		if h == 0 {
			h = -1
		}
		root.SetSizeRequest(w, h)
	}
	placeRoot(root, cfg)

	if back != nil {
		// Header row: the Back button on the left, the title truly centred (a
		// CenterBox, so the button's width does not push it off-centre).
		header := gtk.NewCenterBox()
		header.AddCSSClass("spot-header")
		btn := gtk.NewButtonWithLabel(back.Label)
		btn.AddCSSClass("spot-back")
		btn.SetFocusable(false) // Enter and Tab belong to the dialog's input
		btn.SetFocusOnClick(false)
		btn.SetVAlign(gtk.AlignCenter)
		d.hook(btn, btn.ConnectClicked(func() { goBack() }))
		header.SetStartWidget(btn)
		if cfg.Title != "" {
			t := gtk.NewLabel(cfg.Title)
			t.AddCSSClass("spot-title")
			t.SetXAlign(0.5)
			header.SetCenterWidget(t)
		}
		root.Append(header)
	} else if cfg.Title != "" {
		t := gtk.NewLabel(cfg.Title)
		t.AddCSSClass("spot-title")
		t.SetXAlign(0.5)
		root.Append(t)
	}

	b := &builder{
		d: d, cfg: cfg, env: env, dir: dir,
		envOf:       launcher.EnvGetter(env),
		values:      map[string]func() string{},
		multiValues: map[string]func() []string{},
		nav:         &tileNav{},
	}

	for _, item := range cfg.Items {
		if row := b.item(item); row != nil {
			root.Append(row)
		}
	}

	if cfg.Footer != "" {
		f := gtk.NewLabel(cfg.Footer)
		f.AddCSSClass("spot-footer")
		f.SetXAlign(0)
		root.Append(f)
	}

	// Shown when an exec command fails and on_error = "show".
	execErr := gtk.NewLabel("")
	execErr.AddCSSClass("spot-error")
	execErr.SetXAlign(0)
	execErr.SetWrap(true)
	execErr.SetVisible(false)
	root.Append(execErr)
	d.root = root
	d.firstFocus = b.firstFocus
	d.hook(root, root.ConnectMap(func() {
		if d.saved == nil {
			d.focusFirst()
		}
	}))

	end := func(r result) {
		if d.done {
			return
		}
		d.done = true
		d.finish(r)
	}
	d.cancel = func() { end(result{}) }
	if back != nil {
		goBack = func() {
			if d.done || d.busy {
				return
			}
			if back.Cmd != "" {
				// The command must not see SPOT_BACK itself, or the dialog it
				// opens would offer a Back to where it is.
				spawn(back.Cmd, withoutEnv(env, "SPOT_BACK"), dir)
			}
			end(result{code: protocol.ExitBack})
		}
	}

	b.submit = b.submitter(root, execErr, end)
	d.onKey = b.keys(back, goBack, end)
	return d
}
