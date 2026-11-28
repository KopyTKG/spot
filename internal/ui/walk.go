package ui

import (
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// The keyboard in a dialog: Up and Down go from item to item, Left and Right work
// within one (a slider, the segments of a control, a row of tiles). Lists keep Up
// and Down for their rows, and a text area for its lines.

// stop is an item the keyboard can be on.
type stop struct {
	row   *gtk.Box
	kind  string
	tiles *tileGroup // of a tiles item
	list  *listKeys  // of a list
}

// inList reports whether the keyboard is in a list, or nowhere in particular.
func (b *builder) inList(focus gtk.Widgetter) bool {
	i := b.stopOf(focus)
	return i < 0 || b.stops[i].list != nil
}

// listFor is the list the keys are for when focus is where it is: the one it is
// in, else the first below, else the last.
func (b *builder) listFor(focus gtk.Widgetter) listKeys {
	from := max(b.stopOf(focus), 0)
	if b.stopOf(focus) < 0 {
		from = len(b.stops)
	}
	for _, s := range b.stops[from:] {
		if s.list != nil {
			return *s.list
		}
	}
	for i := min(from, len(b.stops)) - 1; i >= 0; i-- {
		if b.stops[i].list != nil {
			return *b.stops[i].list
		}
	}
	return listKeys{}
}

// leaveList moves past the first or last row of the list focus is in, to the
// item above or below. It reports whether the key was used for that.
func (b *builder) leaveList(focus gtk.Widgetter, by int) bool {
	i := b.stopOf(focus)
	if i < 0 || b.stops[i].list == nil {
		return false
	}
	l := b.stops[i].list
	if l.multiView != nil {
		if within(focus, l.multiSearch) {
			return by < 0 && b.walk(i, by, 0) // down goes into the list
		}
		// The list moves its own cursor. If the key did not move it, it was at
		// an end: that is only known once GTK has had the key.
		coreglib.IdleAdd(func() {
			if !b.d.done && gtk.BaseWidget(focus).IsFocus() {
				b.walk(i, by, 0)
			}
		})
		return false
	}
	model := l.listSelection.Model()
	n, at := 0, int(l.listSelection.Selected())
	if model != nil {
		n = int(model.NItems())
	}
	if n > 0 && (by < 0 && at > 0 || by > 0 && at < n-1) {
		return false
	}
	return b.walk(i, by, 0)
}

// leaveText does the same for a text area: from its first or last line on screen.
func (b *builder) leaveText(tv *gtk.TextView, by int) bool {
	buf := tv.Buffer()
	edge := buf.StartIter()
	if by > 0 {
		edge = buf.EndIter()
	}
	at := buf.IterAtMark(buf.GetInsert())
	return tv.IterLocation(at).Y() == tv.IterLocation(edge).Y() && b.walk(b.stopOf(tv), by, 0)
}

// within reports whether focus is w or something inside it. A search box does
// not itself say it has the focus: the text field in it does.
func within(focus, w gtk.Widgetter) bool {
	if focus == nil {
		return false
	}
	f := gtk.BaseWidget(focus)
	return f.Native() == gtk.BaseWidget(w).Native() || f.IsAncestor(w)
}

// stopOf is the item holding focus, or -1.
func (b *builder) stopOf(focus gtk.Widgetter) int {
	if focus == nil {
		return -1
	}
	w := gtk.BaseWidget(focus)
	for i, s := range b.stops {
		if w.IsAncestor(s.row) {
			return i
		}
	}
	return -1
}

// walks reports whether Up and Down leave the item focus is in.
func (b *builder) walks(focus gtk.Widgetter) bool {
	i := b.stopOf(focus)
	if i < 0 {
		return false
	}
	switch b.stops[i].kind {
	case "switch", "segmented", "button", "scale", "toggle", "select":
		return true
	case "field", "password":
		return b.listFor(focus).listSelection == nil // beside a list they move its selection
	}
	return false
}

// walk moves the keyboard from item i one item down (by 1) or up (by -1), and
// reports whether there was one to go to. col is the column to land in among
// tiles.
func (b *builder) walk(i, by, col int) bool {
	if i < 0 {
		return false
	}
	for j := i + by; j >= 0 && j < len(b.stops); j += by {
		if b.enter(b.stops[j], by, col) {
			return true
		}
	}
	return false
}

// enter puts the keyboard on an item, coming from above (by 1) or below.
func (b *builder) enter(s stop, by, col int) bool {
	if g := s.tiles; g != nil {
		if len(g.children) == 0 {
			return false
		}
		n := min(col, len(g.children)-1, g.cols-1)
		if by < 0 { // the bottom row
			n = min((len(g.children)-1)/g.cols*g.cols+col, len(g.children)-1)
		}
		if !g.children[n].GrabFocus() {
			return false
		}
		b.nav.active, b.nav.cur = g, n
		b.nav.show(g)
		return true
	}
	// A segmented control is entered on what is chosen, not on its first segment.
	if s.kind == "segmented" {
		if box, ok := s.row.LastChild().(*gtk.Box); ok {
			for c := box.FirstChild(); c != nil; c = gtk.BaseWidget(c).NextSibling() {
				if gtk.BaseWidget(c).HasCSSClass("spot-on") {
					return gtk.BaseWidget(c).GrabFocus()
				}
			}
		}
	}
	return s.row.ChildFocus(gtk.DirTabForward)
}

// segment moves the keyboard to the neighbouring segment.
func segment(focus gtk.Widgetter, by int) bool {
	if focus == nil || !gtk.BaseWidget(focus).HasCSSClass("spot-segment") {
		return false
	}
	w := gtk.BaseWidget(focus)
	next := w.NextSibling()
	if by < 0 {
		next = w.PrevSibling()
	}
	if next != nil {
		gtk.BaseWidget(next).GrabFocus()
	}
	return true // at an end the key stays here
}

// inPopup reports whether focus is in an open drop-down: its keys are its own.
func inPopup(focus gtk.Widgetter) bool {
	for w := focus; w != nil; w = gtk.BaseWidget(w).Parent() {
		if _, ok := w.(*gtk.Popover); ok {
			return true
		}
	}
	return false
}
