package ui

import (
	"fmt"
	"slices"
	"strings"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"gitlab.com/KopyTKG/spot/internal/config"
)

// A list with switches = true takes rows "LABEL<TAB>KEY<TAB>STATE". A row with a
// state of on or off shows a switch: Enter or a click flips it and the list stays
// open. A row without one is an ordinary choice, submitted as its KEY.

type switchRow struct {
	icon    string // from the icon theme; "" for none
	key     string
	has, on bool
	busy    bool
}

type switchRows struct {
	icons  bool     // some row has one: all leave room for it
	labels []string // what the list shows, in input order, unique
	rows   map[string]*switchRow
	shown  map[string]shownRow // the rows on screen, by label
	undo   map[uintptr]func()  // disconnects the switch of each row widget
}

type shownRow struct {
	box *gtk.Box
	sw  *slider
}

// rowSet collects rows, telling apart those that show the same.
type rowSet struct {
	*switchRows
	next map[string]int
}

func newRowSet() rowSet {
	return rowSet{&switchRows{rows: map[string]*switchRow{}, shown: map[string]shownRow{}, undo: map[uintptr]func(){}}, map[string]int{}}
}

func (s rowSet) add(label string, r *switchRow) {
	if s.rows[label] != nil { // rows are told apart by what they show
		base := label + " (" + r.key + ")"
		label = base
		// Counting on from the last number used: repeated lines must not make
		// each one try every name before it.
		for s.rows[label] != nil {
			s.next[base]++
			label = fmt.Sprintf("%s #%d", base, s.next[base]+1)
		}
	}
	s.rows[label] = r
	s.labels = append(s.labels, label)
}

func parseSwitchRows(item *config.Item, lines []string) *switchRows {
	s := newRowSet()
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
		s.add(label, r)
	}
	return s.switchRows
}

// modRows are the rows a mod or a parser gave: kept apart as they came, so that
// nothing in a label or a key can be read as part of another field.
func modRows(rows []config.Row) *switchRows {
	s := newRowSet()
	for _, row := range rows {
		r := &switchRow{key: row.Key, icon: row.Icon}
		s.icons = s.icons || row.Icon != ""
		if row.State != nil {
			r.has, r.on = true, *row.State
		}
		s.add(row.Label, r)
	}
	return s.switchRows
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

// setRowIcon shows icon before the label of a row widget that has room for one.
// Widgets are reused, so the icon of the row shown before is replaced.
func setRowIcon(box *gtk.Box, icon string) {
	img, ok := box.FirstChild().(*gtk.Image)
	if !ok {
		img = gtk.NewImage()
		img.AddCSSClass("spot-row-icon")
		img.SetPixelSize(20)
		box.Prepend(img)
	}
	if icon == "" {
		img.Clear()
		img.SetSizeRequest(20, -1)
		return
	}
	img.SetFromIconName(icon)
}

// rowParts is the label of a row widget and its switch (nil before the first bind).
func rowParts(box *gtk.Box) (*gtk.Label, *gtk.Switch) {
	first := box.FirstChild()
	if _, icon := first.(*gtk.Image); icon {
		first = gtk.BaseWidget(first).NextSibling()
	}
	lbl, ok := first.(*gtk.Label)
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
	if r := s.rows[label]; s.icons && r != nil {
		setRowIcon(box, r.icon)
	}
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
		if r == nil || !r.has || item.From != "" && item.Call == "" {
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
		if item.OnChange == "" && item.Call == "" {
			s.update(label)
			return true
		}
		r.busy = true
		reads++ // a state read from before this flip is out of date
		s.update(label)
		done := func(stdout, stderr string, status int) {
			r.busy = false
			b.report(stderr, status)
			if status != 0 {
				r.on = !r.on // it did not happen: say so
			} else if on, ok := item.ParseSwitch(stdout); ok {
				r.on = on
			}
			s.update(label)
			b.refreshAll()
		}
		if item.Call != "" { // the row's key first, then the new value
			b.call(item.Call, []string{r.key, value}, done)
		} else {
			b.command(fill(item.OnChange, item.ID, r.key, value), value, item.CommandTimeout(), done)
		}
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
	if item.From != "" {
		b.refreshers = append(b.refreshers, b.followRows(item, s, &reads))
		return
	}
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

// followRows reads the switch positions of a list from its mod again. The rows
// themselves stay as they were when the dialog opened. A position the mod cannot
// give now is not shown at all.
func (b *builder) followRows(item config.Item, s *switchRows, reads *int) func() {
	first := true
	return func() {
		if first { // fetched before the dialog was drawn
			first = false
			if item.Note != "" {
				b.report(item.Note, 1)
			}
			return
		}
		if !slices.ContainsFunc(item.Rows, func(r config.Row) bool { return r.State != nil }) {
			return
		}
		*reads++
		n := *reads
		go func() {
			var rows []config.Row
			var warning string
			err := errNoMods
			if b.mods != nil {
				var raw []byte
				raw, warning, err = b.mods.Get(item.From)
				if err == nil {
					rows, err = config.Rows(raw)
				}
			}
			coreglib.IdleAdd(func() {
				if b.d.done || n != *reads {
					return
				}
				states := map[string]*bool{}
				if err == nil && warning == "" {
					for _, r := range rows {
						states[r.Key] = r.State
					}
				}
				for label, r := range s.rows {
					if r.busy {
						continue
					}
					on := states[r.key]
					r.has = on != nil
					r.on = on != nil && *on
					s.update(label)
				}
				switch {
				case err != nil:
					b.report("unknown: "+err.Error(), 1)
				case warning != "":
					b.report("unknown: "+warning, 1)
				}
			})
		}()
	}
}
