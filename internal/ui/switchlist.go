package ui

import (
	"fmt"
	"strings"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"gitlab.com/KopyTKG/spot/internal/config"
)

// A list with switches = true takes rows "LABEL<TAB>KEY<TAB>STATE". A row with a
// state of on or off shows a switch: Enter or a click flips it and the list stays
// open. A row without one is an ordinary choice, submitted as its KEY.

type switchRow struct {
	key     string
	has, on bool
	busy    bool
}

type switchRows struct {
	labels []string // what the list shows, in input order, unique
	rows   map[string]*switchRow
	shown  map[string]shownRow // the rows on screen, by label
	undo   map[uintptr]func()  // disconnects the switch of each row widget
}

type shownRow struct {
	box *gtk.Box
	sw  *slider
}

func parseSwitchRows(item *config.Item, lines []string) *switchRows {
	s := &switchRows{rows: map[string]*switchRow{}, shown: map[string]shownRow{}, undo: map[uintptr]func(){}}
	next := map[string]int{}
	for _, line := range lines {
		parts := strings.SplitN(line, "\t", 3)
		label := parts[0]
		r := &switchRow{key: label}
		if len(parts) > 1 && parts[1] != "" {
			r.key = parts[1]
		}
		if len(parts) > 2 {
			r.on, r.has = item.ParseSwitch(parts[2])
		}
		if s.rows[label] != nil { // rows are told apart by what they show
			base := label + " (" + r.key + ")"
			label = base
			// Counting on from the last number used: repeated lines must not make
			// each one try every name before it.
			for s.rows[label] != nil {
				next[base]++
				label = fmt.Sprintf("%s #%d", base, next[base]+1)
			}
		}
		s.rows[label] = r
		s.labels = append(s.labels, label)
	}
	return s
}

func (s *switchRows) key(label string) string {
	if r := s.rows[label]; r != nil {
		return r.key
	}
	return ""
}

func newSwitchRow(lbl *gtk.Label) *gtk.Box {
	lbl.SetHExpand(true)
	box := gtk.NewBox(gtk.OrientationHorizontal, 12)
	box.AddCSSClass("spot-switch-row")
	box.Append(lbl)
	return box
}

// rowParts is the label of a row widget and its switch (nil before the first bind).
func rowParts(box *gtk.Box) (*gtk.Label, *gtk.Switch) {
	lbl, ok := box.FirstChild().(*gtk.Label)
	if !ok {
		return nil, nil
	}
	if next := lbl.NextSibling(); next != nil {
		sw, _ := next.(*gtk.Switch)
		return lbl, sw
	}
	return lbl, nil
}

// bind puts a row into a widget the list view hands out, which may have shown
// another row a moment ago. The switch is made anew each time, so that it appears
// in position: only a flip should slide it.
func (s *switchRows) bind(child gtk.Widgetter, label string) {
	box, ok := child.(*gtk.Box)
	if !ok {
		return
	}
	lbl, old := rowParts(box)
	if lbl == nil {
		return
	}
	lbl.SetText(label)
	id := coreglib.BaseObject(box).Native()
	if undo := s.undo[id]; undo != nil {
		undo()
	}
	if old != nil {
		box.Remove(old)
	}
	r := s.rows[label]
	sw := gtk.NewSwitch()
	sw.AddCSSClass("spot-row-switch")
	sw.SetVAlign(gtk.AlignCenter)
	sw.SetCanTarget(false) // the row takes the click
	sw.SetFocusable(false) // the search box keeps the keyboard
	sw.SetActive(r != nil && r.on)
	box.Append(sw)
	sl := &slider{sw: sw, want: sw.Active()}
	handle := sw.NotifyProperty("active", func() { sl.arrived() })
	s.undo[id] = func() { sw.HandlerDisconnect(handle) }
	s.shown[label] = shownRow{box, sl}
	s.update(label)
}

// update brings a row that is on screen up to date; its switch slides there.
func (s *switchRows) update(label string) {
	row, r := s.shown[label], s.rows[label]
	if row.box == nil || r == nil {
		return
	}
	lbl, sw := rowParts(row.box)
	if lbl == nil || sw == nil || lbl.Text() != label { // the widget shows another row now
		delete(s.shown, label)
		return
	}
	sw.SetVisible(r.has)
	if r.has {
		row.sw.to(r.on)
	}
	classOn(row.box, "spot-on", r.has && r.on)
	classOn(row.box, "spot-busy", r.has && r.busy)
}

// release lets go of the row widgets, which would otherwise keep the dialog alive.
func (s *switchRows) release() {
	for _, undo := range s.undo {
		undo()
	}
	clear(s.undo)
	clear(s.shown)
}

// switchList wires flipping and state reading for a list with switches.
func (b *builder) switchList(item config.Item, s *switchRows, selection *gtk.SingleSelection, lv *gtk.ListView) {
	b.d.hooks = append(b.d.hooks, s.release)
	reads := 0 // only the newest state read counts
	flip := func(label string) bool {
		r := s.rows[label]
		if r == nil || !r.has {
			return false
		}
		if r.busy {
			return true
		}
		r.on = !r.on
		value := item.OffValue()
		if r.on {
			value = item.OnValue()
		}
		if item.OnChange == "" {
			s.update(label)
			return true
		}
		r.busy = true
		reads++ // a state read from before this flip is out of date
		s.update(label)
		cmd := fill(item.OnChange, item.ID, r.key, value)
		b.command(cmd, value, item.CommandTimeout(), func(stdout, stderr string, status int) {
			r.busy = false
			b.report(stderr, status)
			if status != 0 {
				r.on = !r.on // it did not happen: say so
			} else if on, ok := item.ParseSwitch(stdout); ok {
				r.on = on
			}
			s.update(label)
			b.refreshAll()
		})
		return true
	}
	selected := func() (string, bool) {
		obj := selection.SelectedItem()
		if obj == nil {
			return "", false
		}
		so, ok := obj.Cast().(*gtk.StringObject)
		if !ok {
			return "", false
		}
		return so.String(), true
	}
	b.rowActivate = func() bool {
		label, ok := selected()
		return ok && flip(label)
	}
	b.d.hook(lv, lv.ConnectActivate(func(pos uint) {
		selection.SetSelected(pos)
		if label, ok := selected(); ok && !flip(label) && b.submit != nil {
			b.submit()
		}
	}))
	if item.State == "" {
		return
	}
	b.refreshers = append(b.refreshers, func() {
		reads++
		n := reads
		b.command(fill(item.State, item.ID, "", ""), "", item.CommandTimeout(), func(stdout, stderr string, status int) {
			if n != reads {
				return
			}
			if status != 0 {
				b.report(stderr, status)
				return
			}
			// "KEY<TAB>STATE" lines.
			states := map[string]string{}
			for _, line := range strings.Split(stdout, "\n") {
				if key, v, ok := strings.Cut(line, "\t"); ok {
					states[key] = v
				}
			}
			for label, r := range s.rows {
				if v, ok := states[r.key]; ok && !r.busy {
					if on, ok := item.ParseSwitch(v); ok {
						r.has, r.on = true, on
						s.update(label)
					}
				}
			}
		})
	})
}
