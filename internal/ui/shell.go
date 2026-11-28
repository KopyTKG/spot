package ui

import (
	"fmt"
	"log"

	"github.com/diamondburned/gotk4-layer-shell/pkg/gtk4layershell"
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"gitlab.com/KopyTKG/spot/internal/config"
	"gitlab.com/KopyTKG/spot/internal/protocol"
	"gitlab.com/KopyTKG/spot/internal/scheduler"
)

// Shell is the one layer-shell window every dialog is shown in. It is created
// once and reused: swapping dialogs replaces the window's child, so the
// compositor never sees a surface appear, vanish and appear again.
//
// All methods run on the GTK main thread.
type Shell struct {
	app    *gtk.Application
	win    *gtk.ApplicationWindow
	fade   *fader
	Sched  *scheduler.Scheduler
	mapped bool

	style *gtk.CSSProvider // the current dialog's stylesheet
	shown *dialog          // whose widgets are in the window
}

func NewShell(app *gtk.Application, oneshot bool, dc *config.DaemonConfig) *Shell {
	s := &Shell{app: app}
	s.Sched = &scheduler.Scheduler{
		P: s, T: glibTimers{}, Oneshot: oneshot, OnIdle: app.Quit,
		Grace: dc.GraceMillis(), Chain: dc.ChainMillis(),
	}
	return s
}

// Init creates the window. It is not shown until the first dialog arrives.
func (s *Shell) Init() {
	if s.win != nil { // activate can fire again; the window is made once
		return
	}
	// Default look of the keyboard cursor; any theme CSS overrides it.
	cursor := gtk.NewCSSProvider()
	cursor.LoadFromString(".spot-picked { background: alpha(currentColor, 0.18); }\n.spot-tile.spot-on { box-shadow: inset 0 -4px currentColor; }\n.spot-tile.spot-unknown { opacity: 0.5; }\n.spot-control:focus-visible, .spot-scale:focus-visible, .spot-toggle:focus-visible { outline: 2px solid alpha(currentColor, 0.8); outline-offset: 2px; }\n.spot-cursor { outline: 2px solid alpha(currentColor, 0.8); outline-offset: -2px; border-radius: 12px; }")
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

	s.fade = newFader(&win.Window)

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
		s.win.SetFocusVisible(true) // whoever uses keys must see where they are
		return d.onKey(keyval, state, s.win.Focus())
	})
	win.AddController(keys)

	// The compositor can close a layer surface (output unplugged). Treat it as
	// Esc rather than letting GTK destroy the window the daemon reuses.
	win.ConnectCloseRequest(func() bool {
		if d := s.live(); d != nil {
			d.cancel()
		}
		s.Unmap()
		return true
	})
}

// view is the dialog built for j, nil before it is presented.
func view(j *scheduler.Job) *dialog {
	d, _ := j.View.(*dialog)
	return d
}

// Raise puts the window above ordinary dialogs. For a dialog shown by its own
// process, which cannot be stacked inside the daemon's window.
func (s *Shell) Raise() {
	gtk4layershell.SetLayer(&s.win.Window, gtk4layershell.LayerShellLayerOverlay)
}

// live is the dialog that currently takes input, or nil.
func (s *Shell) live() *dialog {
	if cur := s.Sched.Cur; cur != nil {
		if d := view(cur); d != nil && !d.done {
			return d
		}
	}
	return nil
}

// ---- presenter ------------------------------------------------------------

func (s *Shell) Present(j *scheduler.Job) {
	var d *dialog
	if j.Cfg.IsMenu() {
		d = newMenu(j.Cfg, j.Config, j.Page, j.Env, j.Dir, j.Back, j.Mods)
	} else {
		d = newDialog(j.Cfg, j.Env, j.Dir, j.Back, j.Mods)
	}
	d.finish = func(r result) {
		if r.then != asAsked {
			j.Follows(r.then == thenDialog)
		}
		s.Sched.Complete(j, s.answer(d, r))
	}
	j.View = d
	s.show(j)
	if !s.mapped {
		s.win.Present()
		s.mapped = true
	}
}

func (s *Shell) Suspend(j *scheduler.Job) {
	view(j).saved = s.win.Focus()
}

func (s *Shell) Resume(j *scheduler.Job) {
	s.show(j)
	if f, ok := view(j).saved.(interface{ GrabFocus() bool }); ok {
		coreglib.IdleAdd(func() { f.GrabFocus() })
	}
}

func (s *Shell) Retire(j *scheduler.Job) {
	d := view(j)
	if d == nil {
		return
	}
	d.done = true
	if d != s.shown {
		d.release() // covered by a --sub dialog: nothing will replace it on screen
		return
	}
	// The window may keep showing it for a moment (--chain, Back); it must not
	// react, and must not keep the keyboard either, or the first keys typed into
	// whatever opens next would vanish. The next dialog asks for it again.
	d.root.SetSensitive(false)
	gtk4layershell.SetKeyboardMode(&s.win.Window, gtk4layershell.LayerShellKeyboardModeNone)
}

func (s *Shell) Conceal() {
	if c := s.win.Child(); c != nil {
		gtk.BaseWidget(c).SetVisible(false)
	}
}

func (s *Shell) Unmap() {
	if !s.mapped {
		return
	}
	s.win.SetVisible(false)
	s.win.SetChild(nil)
	s.mapped = false
	s.replaceShown(nil)
}

// replaceShown notes whose widgets the window holds now, and releases the dialog
// they replaced if it is finished (one under a --sub dialog comes back).
func (s *Shell) replaceShown(d *dialog) {
	old := s.shown
	s.shown = d
	if old != nil && old != d && old.done {
		old.release()
	}
}

// show puts j's dialog into the window and applies the look it asks for.
func (s *Shell) show(j *scheduler.Job) {
	cfg, d := j.Cfg, view(j)
	if cfg.Title != "" {
		s.win.SetTitle(cfg.Title)
	} else {
		s.win.SetTitle("spot")
	}
	applyKeyboard(&s.win.Window, cfg.Keyboard)
	s.setStyle(j.CSS)
	op := cfg.Opacity
	if op == nil {
		op = &config.Opacity{}
	}
	s.fade.reset(op, d.root)
	d.root.SetVisible(true)
	s.win.SetChild(d.root)
	s.win.SetFocusVisible(true) // a dialog is usually opened from the keyboard
	s.replaceShown(d)
}

// setStyle swaps the display-wide stylesheet for the one of the dialog being
// shown. The file is read every time, so theme edits show on the next open.
func (s *Shell) setStyle(path string) {
	display := gdk.DisplayGetDefault()
	if s.style != nil {
		gtk.StyleContextRemoveProviderForDisplay(display, s.style)
		s.style = nil
	}
	if path == "" {
		return
	}
	css, err := readSmall(path, 4<<20)
	if err != nil {
		log.Printf("css: %v (continuing without styles)", err)
		return
	}
	s.style = gtk.NewCSSProvider()
	s.style.LoadFromString(string(css))
	gtk.StyleContextAddProviderForDisplay(display, s.style, uint(gtk.STYLE_PROVIDER_PRIORITY_APPLICATION))
}

// answer turns how a dialog ended into what the caller of `spot` gets.
func (s *Shell) answer(d *dialog, r result) protocol.Response {
	code := protocol.ExitCancelled
	if r.submitted {
		code = protocol.ExitSubmitted
	}
	if r.code != 0 {
		code = r.code
	}
	return protocol.Response{Code: code, Stdout: d.out.String(), Stderr: d.errOut.String(), Version: protocol.Version}
}

func applyKeyboard(win *gtk.Window, mode string) {
	switch config.ParseKeyboard(mode) {
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
//
// Ignored means the dialog is not a pointer target: hiding the cursor alone
// would leave GTK highlighting (:hover) whatever lies under it, and collapsible
// lists would even move their selection there.
type fader struct {
	win  *gtk.Window
	root *gtk.Box // the dialog being shown
	prov *gtk.CSSProvider

	in, out float64
	ms      int

	inside, typing bool
	lastX, lastY   float64
	haveLast       bool
}

func newFader(win *gtk.Window) *fader {
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
			f.wake()
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

	// A wheel turn or a button press is the pointer in use too, even if it has
	// not moved.
	scroll := gtk.NewEventControllerScroll(gtk.EventControllerScrollBothAxes)
	scroll.SetPropagationPhase(gtk.PhaseCapture)
	scroll.ConnectScroll(func(_, _ float64) bool {
		f.wake()
		return false
	})
	win.AddController(scroll)
	press := gtk.NewGestureClick()
	press.SetButton(0)
	press.SetPropagationPhase(gtk.PhaseCapture)
	press.ConnectPressed(func(int, float64, float64) { f.wake() })
	win.AddController(press)
	return f
}

// reset starts a dialog: its opacities, "inside", pointer hidden. The last
// pointer position is kept as the baseline, so a pointer that merely rests
// where it was does not count as movement.
func (f *fader) reset(o *config.Opacity, root *gtk.Box) {
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
	f.inside = true
	f.sleep()
	f.set(f.in)
}

// sleep hides the pointer and stops the dialog reacting to it.
func (f *fader) sleep() {
	f.typing = true
	f.win.SetCursorFromName("none")
	if f.root != nil {
		f.root.SetCanTarget(false)
	}
}

// wake gives the pointer back.
func (f *fader) wake() {
	if !f.typing {
		return
	}
	f.typing = false
	f.win.SetCursor(nil)
	if f.root != nil {
		f.root.SetCanTarget(true)
	}
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
	f.sleep()
	f.update(true)
}
