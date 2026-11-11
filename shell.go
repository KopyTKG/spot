package main

import (
	"fmt"
	"log"
	"os"

	"github.com/diamondburned/gotk4-layer-shell/pkg/gtk4layershell"
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// shell is the one layer-shell window every dialog is shown in. It is created
// once and reused: swapping dialogs replaces the window's child, so the
// compositor never sees a surface appear, vanish and appear again.
//
// All methods run on the GTK main thread.
type shell struct {
	app    *gtk.Application
	win    *gtk.ApplicationWindow
	fade   *fader
	sched  *scheduler
	mapped bool

	style *gtk.CSSProvider // the current dialog's stylesheet
}

func newShell(app *gtk.Application, oneshot bool) *shell {
	s := &shell{app: app}
	s.sched = &scheduler{p: s, t: glibTimers{}, oneshot: oneshot, onIdle: app.Quit}
	return s
}

// init creates the window. It is not shown until the first dialog arrives.
func (s *shell) init() {
	if s.win != nil { // activate can fire again; the window is made once
		return
	}
	// Default look of the keyboard cursor; any theme CSS overrides it.
	cursor := gtk.NewCSSProvider()
	cursor.LoadFromString(".spot-picked { background: alpha(currentColor, 0.18); }\n.spot-cursor { outline: 2px solid alpha(currentColor, 0.8); outline-offset: -2px; border-radius: 12px; }")
	gtk.StyleContextAddProviderForDisplay(
		gdk.DisplayGetDefault(), cursor, uint(gtk.STYLE_PROVIDER_PRIORITY_FALLBACK))

	win := gtk.NewApplicationWindow(s.app)
	s.win = win
	win.AddCSSClass("spot-window")
	gtk4layershell.InitForWindow(&win.Window)
	gtk4layershell.SetLayer(&win.Window, gtk4layershell.LayerShellLayerTop)
	gtk4layershell.SetNamespace(&win.Window, "spot")
	// The layer surface covers the whole output so clicks outside the dialog
	// land on it and can dismiss it; the dialog is positioned inside.
	for _, e := range []gtk4layershell.Edge{
		gtk4layershell.LayerShellEdgeTop, gtk4layershell.LayerShellEdgeBottom,
		gtk4layershell.LayerShellEdgeLeft, gtk4layershell.LayerShellEdgeRight,
	} {
		gtk4layershell.SetAnchor(&win.Window, e, true)
	}

	s.fade = newFader(win)

	// Click outside the dialog dismisses it (same as Esc).
	click := gtk.NewGestureClick()
	click.SetPropagationPhase(gtk.PhaseCapture)
	click.ConnectPressed(func(_ int, x, y float64) {
		d := s.live()
		if d == nil {
			return
		}
		if rx, ry, ok := win.TranslateCoordinates(d.root, x, y); ok && d.root.Contains(rx, ry) {
			return
		}
		d.cancel()
	})
	win.AddController(click)

	keys := gtk.NewEventControllerKey()
	// Capture phase so we see Esc/Enter before SearchEntry's built-in
	// "clear-on-Esc" / "activate-on-Enter" handlers eat them.
	keys.SetPropagationPhase(gtk.PhaseCapture)
	keys.ConnectKeyPressed(func(keyval, _ uint, state gdk.ModifierType) bool {
		d := s.live()
		if d == nil {
			return false
		}
		s.fade.keyPressed()
		return d.onKey(keyval, state, s.win.Focus())
	})
	win.AddController(keys)

	// The compositor can close a layer surface (output unplugged). Treat it as
	// Esc rather than letting GTK destroy the window the daemon reuses.
	win.ConnectCloseRequest(func() bool {
		if d := s.live(); d != nil {
			d.cancel()
		}
		s.unmap()
		return true
	})
}

// live is the dialog that currently takes input, or nil.
func (s *shell) live() *dialog {
	if cur := s.sched.cur; cur != nil && cur.dlg != nil && !cur.dlg.done {
		return cur.dlg
	}
	return nil
}

// ---- presenter ------------------------------------------------------------

func (s *shell) present(j *job) {
	d := newDialog(j.cfg, j.env, j.dir, j.back)
	d.finish = func(r result) { s.sched.complete(j, s.answer(d, r)) }
	j.dlg = d
	s.show(j)
	if !s.mapped {
		s.win.Present()
		s.mapped = true
	}
}

func (s *shell) suspend(j *job) {
	j.dlg.saved = s.win.Focus()
}

func (s *shell) resume(j *job) {
	s.show(j)
	if f, ok := j.dlg.saved.(interface{ GrabFocus() bool }); ok {
		coreglib.IdleAdd(func() { f.GrabFocus() })
	}
}

func (s *shell) retire(j *job) {
	d := j.dlg
	if d == nil {
		return
	}
	d.done = true
	// The window may keep showing it for a moment (--chain, Back); it must not
	// react, and must not keep the keyboard either, or the first keys typed into
	// whatever opens next would vanish. The next dialog asks for it again.
	d.root.SetSensitive(false)
	gtk4layershell.SetKeyboardMode(&s.win.Window, gtk4layershell.LayerShellKeyboardModeNone)
}

func (s *shell) conceal() {
	if c := s.win.Child(); c != nil {
		gtk.BaseWidget(c).SetVisible(false)
	}
}

func (s *shell) unmap() {
	if !s.mapped {
		return
	}
	s.win.SetVisible(false)
	s.win.SetChild(nil)
	s.mapped = false
}

// show puts j's dialog into the window and applies the look it asks for.
func (s *shell) show(j *job) {
	cfg, d := j.cfg, j.dlg
	if cfg.Title != "" {
		s.win.SetTitle(cfg.Title)
	} else {
		s.win.SetTitle("spot")
	}
	applyKeyboard(&s.win.Window, cfg.Keyboard)
	s.setStyle(j.css)
	op := cfg.Opacity
	if op == nil {
		op = &Opacity{}
	}
	s.fade.reset(op, d.root)
	d.root.SetVisible(true)
	s.win.SetChild(d.root)
}

// setStyle swaps the display-wide stylesheet for the one of the dialog being
// shown. The file is read every time, so theme edits show on the next open.
func (s *shell) setStyle(path string) {
	display := gdk.DisplayGetDefault()
	if s.style != nil {
		gtk.StyleContextRemoveProviderForDisplay(display, s.style)
		s.style = nil
	}
	if path == "" {
		return
	}
	css, err := os.ReadFile(path)
	if err != nil {
		log.Printf("css: %v (continuing without styles)", err)
		return
	}
	s.style = gtk.NewCSSProvider()
	s.style.LoadFromString(string(css))
	gtk.StyleContextAddProviderForDisplay(display, s.style, uint(gtk.STYLE_PROVIDER_PRIORITY_APPLICATION))
}

// answer turns how a dialog ended into what the caller of `spot` gets.
func (s *shell) answer(d *dialog, r result) response {
	code := exitCancelled
	if r.submitted {
		code = exitSubmitted
	}
	if r.code != 0 {
		code = r.code
	}
	return response{Code: code, Stdout: d.out.String(), Stderr: d.errOut.String(), Version: version}
}

// glibTimers runs grace periods on the GLib main loop.
type glibTimers struct{}

func (glibTimers) after(ms int, f func()) func() {
	h := coreglib.TimeoutAdd(uint(ms), func() bool {
		f()
		return false
	})
	return func() { coreglib.SourceRemove(h) }
}

func applyKeyboard(win *gtk.Window, mode string) {
	switch parseKeyboard(mode) {
	case "none":
		gtk4layershell.SetKeyboardMode(win, gtk4layershell.LayerShellKeyboardModeNone)
	case "on-demand":
		gtk4layershell.SetKeyboardMode(win, gtk4layershell.LayerShellKeyboardModeOnDemand)
	default:
		gtk4layershell.SetKeyboardMode(win, gtk4layershell.LayerShellKeyboardModeExclusive)
	}
}

// ---- opacity --------------------------------------------------------------

// fader fades the dialog between its "pointer over" and "pointer outside"
// opacities. One lives as long as the window; reset points it at the dialog
// being shown. keyPressed is called on every key press: the pointer is then
// hidden and ignored (the dialog counts as "inside") until it really moves
// again. Each dialog starts in that state.
type fader struct {
	win  *gtk.ApplicationWindow
	root *gtk.Box // the dialog being shown
	prov *gtk.CSSProvider

	in, out float64
	ms      int

	inside, typing bool
	lastX, lastY   float64
	haveLast       bool
}

func newFader(win *gtk.ApplicationWindow) *fader {
	f := &fader{win: win, in: 1, out: 0.85, ms: 150, inside: true, typing: true}
	f.prov = gtk.NewCSSProvider()
	gtk.StyleContextAddProviderForDisplay(
		gdk.DisplayGetDefault(), f.prov, uint(gtk.STYLE_PROVIDER_PRIORITY_USER))

	motion := gtk.NewEventControllerMotion()
	motion.SetPropagationPhase(gtk.PhaseCapture)
	// While the keyboard is in use the pointer is hidden and its position
	// ignored. GTK can re-send motion at the same spot (e.g. after a
	// relayout), so only a changed position counts as the pointer moving.
	//
	// A dialog starts in this state too: it was usually opened from the
	// keyboard (or from another dialog), so showing the cursor and fading to
	// "outside" because of wherever the pointer happens to rest would flash
	// the cursor and then blink the opacity on the first key press. The
	// position reported when the pointer enters is only a baseline.
	motion.ConnectEnter(func(x, y float64) {
		if f.typing {
			f.lastX, f.lastY, f.haveLast = x, y, true
		}
	})
	motion.ConnectMotion(func(x, y float64) {
		moved := !f.haveLast || x != f.lastX || y != f.lastY
		f.lastX, f.lastY, f.haveLast = x, y, true
		if f.typing {
			if !moved {
				return
			}
			f.typing = false
			win.SetCursor(nil)
		}
		if f.root == nil {
			return
		}
		rx, ry, ok := win.TranslateCoordinates(f.root, x, y)
		f.update(ok && f.root.Contains(rx, ry))
	})
	motion.ConnectLeave(func() {
		if !f.typing {
			f.update(false)
		}
	})
	win.AddController(motion)
	return f
}

// reset starts a dialog: its opacities, "inside", pointer hidden. The last
// pointer position is kept as the baseline, so a pointer that merely rests
// where it was does not count as movement.
func (f *fader) reset(o *Opacity, root *gtk.Box) {
	pct := func(p *float64, def float64) float64 {
		if p != nil {
			def = *p
		}
		return max(0, min(100, def)) / 100
	}
	f.in, f.out = pct(o.Inside, 100), pct(o.Outside, 85)
	f.ms = 150
	if o.Transition != nil {
		f.ms = *o.Transition
	}
	f.root = root
	f.inside, f.typing = true, true
	f.win.SetCursorFromName("none")
	f.set(f.in)
}

func (f *fader) set(a float64) {
	f.prov.LoadFromString(fmt.Sprintf(
		".spot-root { opacity: %.3f; transition: opacity %dms ease; }", a, f.ms))
}

func (f *fader) update(now bool) {
	if now == f.inside {
		return
	}
	f.inside = now
	if now {
		f.set(f.in)
	} else {
		f.set(f.out)
	}
}

func (f *fader) keyPressed() {
	if f.typing {
		return
	}
	f.typing = true
	f.win.SetCursorFromName("none")
	f.update(true)
}
