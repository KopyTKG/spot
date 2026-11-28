package ui

import (
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"gitlab.com/KopyTKG/spot/internal/config"
)

// keys is the dialog's key handler: it gets every press before the widgets do
// and reports whether it used it.
func (b *builder) keys(back *config.BackSpec, goBack func(), end func(result)) func(uint, gdk.ModifierType, gtk.Widgetter) bool {
	d := b.d
	return func(keyval uint, state gdk.ModifierType, focus gtk.Widgetter) bool {
		if d.busy || inPopup(focus) && keyval != gdk.KEY_Escape {
			return false
		}
		if back != nil {
			switch {
			case keyval == gdk.KEY_Left && state&gdk.AltMask != 0:
				goBack()
				return true
			case keyval == gdk.KEY_BackSpace && state&(gdk.ShiftMask|gdk.ControlMask|gdk.AltMask) == 0 &&
				backspaceGoesBack(focus):
				goBack()
				return true
			}
		}
		if len(b.stops) > 0 {
			b.listKeys = b.listFor(focus)
		}
		// Up and Down leave a list or a text area at its first and last line.
		if state&(gdk.ControlMask|gdk.AltMask|gdk.ShiftMask) == 0 && focus != nil {
			by := 0
			switch keyval {
			case gdk.KEY_Up, gdk.KEY_KP_Up:
				by = -1
			case gdk.KEY_Down, gdk.KEY_KP_Down:
				by = 1
			}
			if tv, ok := focus.(*gtk.TextView); ok && by != 0 && b.leaveText(tv, by) {
				return true
			}
			if by != 0 && b.leaveList(focus, by) {
				return true
			}
		}
		// Collapsible lists: Left folds, Right unfolds (when the search is empty,
		// so the keys still move the cursor while typing).
		if b.groupToggle != nil && state&(gdk.ControlMask|gdk.AltMask|gdk.ShiftMask) == 0 {
			switch keyval {
			case gdk.KEY_Left, gdk.KEY_KP_Left:
				if b.groupToggle(-1) {
					return true
				}
			case gdk.KEY_Right, gdk.KEY_KP_Right:
				if b.groupToggle(1) {
					return true
				}
			}
		}
		// In a text area plain Enter is a newline; Ctrl+Enter submits.
		inText := false
		for _, tv := range b.textViews {
			if tv.HasFocus() {
				inText = true
			}
		}
		// Multi-select lists: Up/Down hand focus to the list, whose own
		// keys (arrows, Space, Shift/Ctrl+arrows) do the selecting.
		if b.multiView != nil && within(focus, b.multiSearch) {
			switch keyval {
			case gdk.KEY_Up, gdk.KEY_KP_Up, gdk.KEY_Down, gdk.KEY_KP_Down:
				b.multiView.GrabFocus()
				return true
			}
		}
		// While a tile section has focus the arrow keys drive one cursor
		// across all sections (GTK's own navigation stops at a section's edge).
		if b.nav.active != nil {
			var dx, dy int
			switch keyval {
			case gdk.KEY_Left, gdk.KEY_KP_Left:
				dx = -1
			case gdk.KEY_Right, gdk.KEY_KP_Right:
				dx = 1
			case gdk.KEY_Up, gdk.KEY_KP_Up:
				dy = -1
			case gdk.KEY_Down, gdk.KEY_KP_Down:
				dy = 1
			}
			if (dx != 0 || dy != 0) && b.nav.move(dx, dy) {
				return true
			}
			if dy != 0 { // past the first or last row: on to what is above or below
				b.walk(b.nav.active.stop, dy, b.nav.cur%b.nav.active.cols)
				return true
			}
		}
		if state&(gdk.ControlMask|gdk.AltMask|gdk.ShiftMask) == 0 {
			switch keyval {
			case gdk.KEY_Up, gdk.KEY_KP_Up, gdk.KEY_Down, gdk.KEY_KP_Down:
				if b.walks(focus) {
					by := 1
					if keyval == gdk.KEY_Up || keyval == gdk.KEY_KP_Up {
						by = -1
					}
					b.walk(b.stopOf(focus), by, 0)
					return true
				}
			case gdk.KEY_Left, gdk.KEY_KP_Left:
				if segment(focus, -1) {
					return true
				}
			case gdk.KEY_Right, gdk.KEY_KP_Right:
				if segment(focus, 1) {
					return true
				}
			}
		}
		// A focused switch, segment or button takes Enter itself.
		if w := focusedControl(focus); w != nil {
			switch keyval {
			case gdk.KEY_Return, gdk.KEY_KP_Enter:
				w.Activate()
				return true
			}
		}
		// Space flips a switch row too, as it does other switches. Only while the
		// search is empty: in "ssh estab" it is part of what is typed.
		if keyval == gdk.KEY_space && state&(gdk.ControlMask|gdk.AltMask|gdk.ShiftMask) == 0 &&
			b.rowActivate != nil && b.inList(focus) && backspaceGoesBack(focus) && b.rowActivate() {
			return true
		}
		switch keyval {
		case gdk.KEY_Escape:
			end(result{})
			return true
		case gdk.KEY_Return, gdk.KEY_KP_Enter:
			if inText && state&gdk.ControlMask == 0 {
				return false
			}
			if b.groupToggle != nil && b.groupToggle(0) { // Enter on a group header folds it
				return true
			}
			if b.rowActivate != nil && b.rowActivate() { // a switch row flips and stays
				return true
			}
			if b.nav.flip() { // a tile that switches something flips and stays
				return true
			}
			if b.nav.active != nil {
				b.activeID = b.nav.choose()
			}
			b.submit()
			return true
		case gdk.KEY_Up, gdk.KEY_KP_Up:
			if b.listSelection != nil {
				moveSelection(b.listSelection, b.listView, -1)
				return true
			}
		case gdk.KEY_Down, gdk.KEY_KP_Down:
			if b.listSelection != nil {
				moveSelection(b.listSelection, b.listView, 1)
				return true
			}
		case gdk.KEY_Page_Up:
			if b.listSelection != nil {
				moveSelection(b.listSelection, b.listView, -10)
				return true
			}
		case gdk.KEY_Page_Down:
			if b.listSelection != nil {
				moveSelection(b.listSelection, b.listView, 10)
				return true
			}
		}
		return false
	}
}

// focusedControl is the live control that has the keyboard, or nil.
func focusedControl(focus gtk.Widgetter) *gtk.Widget {
	if focus == nil {
		return nil
	}
	if w := gtk.BaseWidget(focus); w.HasCSSClass("spot-control") {
		return w
	}
	return nil
}

func moveSelection(sel *gtk.SingleSelection, lv *gtk.ListView, delta int) {
	model := sel.Model()
	if model == nil {
		return
	}
	n := int(model.NItems())
	if n == 0 {
		return
	}
	cur := int(sel.Selected())
	next := cur + delta
	if next < 0 {
		next = 0
	} else if next >= n {
		next = n - 1
	}
	sel.SetSelected(uint(next))
	if lv != nil {
		lv.ScrollTo(uint(next), gtk.ListScrollFocus|gtk.ListScrollSelect, nil)
	}
}

// backspaceGoesBack reports whether Backspace, with focus where it is, means
// Back: yes on tiles, on a list, or in an empty search box; no while editing
// text, where it deletes.
func backspaceGoesBack(focus gtk.Widgetter) bool {
	if focus == nil {
		return true
	}
	for w := focus; w != nil; w = gtk.BaseWidget(w).Parent() {
		switch v := w.(type) {
		case *gtk.SearchEntry:
			return v.Text() == ""
		case *gtk.Entry, *gtk.PasswordEntry, *gtk.TextView:
			return false
		}
		// A *gtk.Text is the editable inside an entry; keep climbing to find which.
	}
	return true
}
