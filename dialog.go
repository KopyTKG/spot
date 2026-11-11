package main

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"math"
	"os/exec"
	"slices"
	"strings"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
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

	onKey  func(keyval uint, state gdk.ModifierType) bool // window key press, capture phase
	cancel func()                                         // Esc, click outside, client gone
	finish func(result)                                   // set by the shell

	done bool
	busy bool // submit.exec is running
}

// focusFirst puts the keyboard on the first input.
func (d *dialog) focusFirst() {
	if g, ok := d.firstFocus.(interface{ GrabFocus() bool }); ok {
		g.GrabFocus()
	}
}

// newDialog builds the widgets for cfg. env and dir are what submit.exec and
// submit.spawn commands run with: the caller's, not the daemon's.
func newDialog(cfg *Config, env []string, dir string) *dialog {
	d := &dialog{}

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
	placeRoot(root, cfg.Anchor)

	if cfg.Title != "" {
		t := gtk.NewLabel(cfg.Title)
		t.AddCSSClass("spot-title")
		t.SetXAlign(0.5)
		root.Append(t)
	}

	values := map[string]func() string{}
	var firstFocus gtk.Widgetter
	var listSelection *gtk.SingleSelection // for keyboard nav (single-select list)
	var listView *gtk.ListView             // for ScrollTo on key nav
	var multiSearch *gtk.SearchEntry       // multi-select list: Up/Down move focus into the list
	var multiView *gtk.ListView
	nav := &tileNav{}             // keyboard cursor across all tile sections
	var textViews []*gtk.TextView // Enter inserts a newline in these; Ctrl+Enter submits
	multiValues := map[string]func() []string{}
	var checks []*check // items with required / pattern rules
	activeID := ""      // id of the last tiles group interacted with ({_active})
	var submitFn func() // late-bound so tile click handlers can call it

	for _, it := range cfg.Items {
		item := it
		row := gtk.NewBox(gtk.OrientationVertical, 4)
		row.AddCSSClass("spot-item")
		if item.Label != "" && item.Type != "toggle" {
			lbl := gtk.NewLabel(item.Label)
			lbl.AddCSSClass("spot-label")
			lbl.SetXAlign(0)
			row.Append(lbl)
		}
		var input gtk.Widgetter // the widget .spot-invalid is put on and focus goes to
		var onEdit func(func()) // registers a callback for edits (clears .spot-invalid)
		switch item.Type {
		case "field":
			entry := gtk.NewEntry()
			entry.AddCSSClass("spot-field")
			if item.Placeholder != "" {
				entry.SetPlaceholderText(item.Placeholder)
			}
			if item.Default != nil {
				entry.SetText(item.defaultString())
			}
			row.Append(entry)
			values[item.ID] = func() string { return entry.Text() }
			input, onEdit = entry, func(f func()) { entry.ConnectChanged(f) }
			if firstFocus == nil {
				firstFocus = entry
			}
		case "password":
			entry := gtk.NewPasswordEntry()
			entry.AddCSSClass("spot-password")
			entry.SetShowPeekIcon(true)
			row.Append(entry)
			values[item.ID] = func() string { return entry.Text() }
			input, onEdit = entry, func(f func()) { entry.ConnectChanged(f) }
			if firstFocus == nil {
				firstFocus = entry
			}
		case "select":
			sl := gtk.NewStringList(item.Options)
			dd := gtk.NewDropDown(sl, nil)
			dd.AddCSSClass("spot-select")
			if item.Default != nil {
				for i, opt := range item.Options {
					if opt == item.defaultString() {
						dd.SetSelected(uint(i))
						break
					}
				}
			}
			row.Append(dd)
			values[item.ID] = func() string {
				idx := int(dd.Selected())
				if idx < 0 || idx >= len(item.Options) {
					return ""
				}
				return item.Options[idx]
			}
			input = dd
			if firstFocus == nil {
				firstFocus = dd
			}
		case "list":
			opts := item.Options
			model := gtk.NewStringList(opts)

			search := gtk.NewSearchEntry()
			search.AddCSSClass("spot-search")
			if item.Placeholder != "" {
				search.SetPlaceholderText(item.Placeholder)
			}

			var query string
			// match = "smart" on a single-select list reorders the list itself
			// (best match first), so the filter lets everything through.
			ranked := item.Match == "smart" && !item.Multi
			filt := gtk.NewCustomFilter(func(obj *coreglib.Object) bool {
				if query == "" || ranked {
					return true
				}
				so, ok := obj.Cast().(*gtk.StringObject)
				if !ok {
					return false
				}
				if item.Match == "smart" {
					return smartScore(so.String(), query) >= 0
				}
				return strings.Contains(
					strings.ToLower(so.String()),
					strings.ToLower(query),
				)
			})
			filterModel := gtk.NewFilterListModel(model, &filt.Filter)
			var selection *gtk.SingleSelection
			var multiSel *gtk.MultiSelection
			var selModel gtk.SelectionModeller
			if item.Multi {
				multiSel = gtk.NewMultiSelection(filterModel)
				selModel = multiSel
			} else {
				selection = gtk.NewSingleSelection(filterModel)
				selModel = selection
			}

			factory := gtk.NewSignalListItemFactory()
			factory.ConnectSetup(func(obj *coreglib.Object) {
				li, ok := obj.Cast().(*gtk.ListItem)
				if !ok {
					return
				}
				lbl := gtk.NewLabel("")
				lbl.AddCSSClass("spot-list-row")
				lbl.SetXAlign(0)
				li.SetChild(lbl)
			})
			factory.ConnectBind(func(obj *coreglib.Object) {
				li, ok := obj.Cast().(*gtk.ListItem)
				if !ok {
					return
				}
				lbl, ok := li.Child().(*gtk.Label)
				if !ok {
					return
				}
				so, ok := li.Item().Cast().(*gtk.StringObject)
				if !ok {
					return
				}
				lbl.SetText(so.String())
			})

			lv := gtk.NewListView(selModel, &factory.ListItemFactory)
			lv.AddCSSClass("spot-list")
			lv.SetSingleClickActivate(false)
			listView = lv

			scrolled := gtk.NewScrolledWindow()
			h := item.Height
			if h == 0 {
				h = 320
			}
			scrolled.SetMinContentHeight(h)
			scrolled.SetChild(lv)
			scrolled.SetHExpand(true)
			scrolled.SetVExpand(true)
			scrolled.AddCSSClass("spot-list-scroll")

			search.ConnectSearchChanged(func() {
				query = search.Text()
				if ranked {
					model.Splice(0, model.NItems(), rankSmart(opts, query))
				}
				filt.Changed(gtk.FilterChangeDifferent)
				if selection != nil && filterModel.NItems() > 0 {
					selection.SetSelected(0)
				}
			})

			row.Append(search)
			row.Append(scrolled)

			selectedOf := func(obj *coreglib.Object) string {
				so, ok := obj.Cast().(*gtk.StringObject)
				if !ok {
					return ""
				}
				return so.String()
			}
			if item.Multi {
				multiValues[item.ID] = func() []string {
					var picked []string
					for i := uint(0); i < filterModel.NItems(); i++ {
						if multiSel.IsSelected(i) {
							picked = append(picked, selectedOf(filterModel.Item(i)))
						}
					}
					return picked
				}
				values[item.ID] = func() string {
					return strings.Join(multiValues[item.ID](), item.separator())
				}
				if item.Default != nil {
					want := map[string]bool{}
					for _, d := range strings.Split(item.defaultString(), item.separator()) {
						want[d] = true
					}
					for i, opt := range opts {
						if want[opt] {
							multiSel.SelectItem(uint(i), false)
						}
					}
				}
				multiSearch, multiView = search, lv
			} else {
				values[item.ID] = func() string {
					obj := selection.SelectedItem()
					if obj == nil {
						return ""
					}
					return selectedOf(obj)
				}
				if item.Default != nil {
					for i, opt := range opts {
						if opt == item.defaultString() {
							selection.SetSelected(uint(i))
							break
						}
					}
				}
				listSelection = selection
			}

			input = search
			if firstFocus == nil {
				firstFocus = search
			}
		case "tiles":
			cols := item.Columns
			if cols <= 0 {
				cols = 3
			}
			flow := gtk.NewFlowBox()
			flow.AddCSSClass("spot-tiles")
			flow.SetSelectionMode(gtk.SelectionSingle)
			flow.SetMinChildrenPerLine(uint(cols))
			flow.SetMaxChildrenPerLine(uint(cols))
			flow.SetHomogeneous(true)

			tiles := item.Tiles
			children := make([]*gtk.FlowBoxChild, len(tiles))
			picks := make([]*gtk.Label, len(tiles)) // "●" marker, shown on the picked tile
			for i, tl := range tiles {
				tileBox := gtk.NewBox(gtk.OrientationVertical, 6)
				tileBox.AddCSSClass("spot-tile-body")
				tileBox.SetHAlign(gtk.AlignCenter)
				tileBox.SetVAlign(gtk.AlignCenter)

				iconLbl := gtk.NewLabel(tl.Icon)
				iconLbl.AddCSSClass("spot-tile-icon")
				iconLbl.SetHAlign(gtk.AlignCenter)
				tileBox.Append(iconLbl)

				if tl.Label != "" {
					nameLbl := gtk.NewLabel(tl.Label)
					nameLbl.AddCSSClass("spot-tile-label")
					nameLbl.SetHAlign(gtk.AlignCenter)
					tileBox.Append(nameLbl)
				}

				pick := gtk.NewLabel("●")
				pick.AddCSSClass("spot-tile-pick")
				pick.SetHAlign(gtk.AlignCenter)
				pick.SetVisible(false)
				tileBox.Append(pick)
				picks[i] = pick

				isCurrent := item.Current != "" && tl.Value == item.Current
				if isCurrent {
					badge := gtk.NewLabel("● active")
					badge.AddCSSClass("spot-tile-badge")
					badge.SetHAlign(gtk.AlignCenter)
					tileBox.Append(badge)
				}

				child := gtk.NewFlowBoxChild()
				child.AddCSSClass("spot-tile")
				if isCurrent {
					child.AddCSSClass("spot-active")
				}
				child.SetChild(tileBox)
				flow.Append(child)
				children[i] = child
			}

			defaultIdx := 0
			if item.Default != nil {
				for i, tl := range tiles {
					if tl.Value == item.defaultString() {
						defaultIdx = i
						break
					}
				}
			}
			if len(children) > 0 {
				flow.SelectChild(children[defaultIdx])
			}

			row.Append(flow)

			if activeID == "" {
				activeID = item.ID
			}
			itemID := item.ID
			group := &tileGroup{id: itemID, flow: flow, children: children, cols: cols}
			nav.groups = append(nav.groups, group)
			markPicked := func() {
				for i, c := range children {
					c.RemoveCSSClass("spot-picked")
					picks[i].SetVisible(false)
				}
				if i := group.selected(); i >= 0 {
					children[i].AddCSSClass("spot-picked")
					picks[i].SetVisible(true)
				}
			}
			markPicked()
			flow.ConnectSelectedChildrenChanged(func() {
				activeID = itemID
				markPicked()
			})
			focus := gtk.NewEventControllerFocus()
			focus.ConnectEnter(func() {
				nav.active, nav.cur = group, max(group.selected(), 0)
				nav.show(group)
			})
			focus.ConnectLeave(func() {
				if nav.active == group {
					nav.active = nil
					nav.hide()
				}
			})
			flow.AddController(focus)

			values[item.ID] = func() string {
				selected := flow.SelectedChildren()
				if len(selected) == 0 {
					return ""
				}
				idx := selected[0].Index()
				if idx < 0 || idx >= len(tiles) {
					return ""
				}
				return tiles[idx].Value
			}

			flow.ConnectChildActivated(func(child *gtk.FlowBoxChild) {
				flow.SelectChild(child)
				activeID = itemID
				if submitFn != nil {
					submitFn()
				}
			})

			if firstFocus == nil {
				firstFocus = flow
			}
		case "text":
			tv := gtk.NewTextView()
			tv.AddCSSClass("spot-text")
			tv.SetWrapMode(gtk.WrapWordChar)
			tv.SetAcceptsTab(false)
			buf := tv.Buffer()

			scrolled := gtk.NewScrolledWindow()
			h := item.Height
			if h == 0 {
				h = 120
			}
			scrolled.SetMinContentHeight(h)
			scrolled.SetChild(tv)
			scrolled.SetHExpand(true)
			scrolled.AddCSSClass("spot-text-scroll")

			var widget gtk.Widgetter = scrolled
			if item.Placeholder != "" {
				ph := gtk.NewLabel(item.Placeholder)
				ph.AddCSSClass("spot-text-placeholder")
				ph.SetXAlign(0)
				ph.SetYAlign(0)
				ph.SetCanTarget(false)
				ph.SetHAlign(gtk.AlignStart)
				ph.SetVAlign(gtk.AlignStart)
				ov := gtk.NewOverlay()
				ov.SetChild(scrolled)
				ov.AddOverlay(ph)
				buf.ConnectChanged(func() { ph.SetVisible(buf.CharCount() == 0) })
				widget = ov
			}
			if item.Default != nil {
				buf.SetText(item.defaultString())
			}
			row.Append(widget)
			values[item.ID] = func() string {
				start, end := buf.Bounds()
				return buf.Text(start, end, false)
			}
			textViews = append(textViews, tv)
			input, onEdit = tv, func(f func()) { buf.ConnectChanged(f) }
			if firstFocus == nil {
				firstFocus = tv
			}
		case "toggle":
			cb := gtk.NewCheckButton()
			cb.AddCSSClass("spot-toggle")
			cb.SetLabel(item.Label)
			cb.SetActive(item.defaultBool())
			row.Append(cb)
			values[item.ID] = func() string {
				if cb.Active() {
					return item.onValue()
				}
				return item.offValue()
			}
			if firstFocus == nil {
				firstFocus = cb
			}
		case "scale":
			sc := gtk.NewScaleWithRange(gtk.OrientationHorizontal,
				item.minValue(), item.maxValue(), item.stepValue())
			sc.AddCSSClass("spot-scale")
			sc.SetHExpand(true)
			sc.SetDrawValue(true)
			sc.SetDigits(digitsFor(item.stepValue()))
			sc.SetValue(item.scaleDefault())
			row.Append(sc)
			values[item.ID] = func() string { return formatNumber(roundTo(sc.Value(), item.stepValue())) }
			if firstFocus == nil {
				firstFocus = sc
			}
		default:
			log.Printf("unknown item type: %q (skipping)", item.Type)
			continue
		}
		if item.Required || item.pattern != nil {
			if _, ok := values[item.ID]; ok {
				ck := &check{item: item, widget: gtk.BaseWidget(input)}
				if onEdit != nil {
					onEdit(ck.clear)
				}
				if item.Error != "" {
					ck.msg = gtk.NewLabel(item.Error)
					ck.msg.AddCSSClass("spot-item-error")
					ck.msg.SetXAlign(0)
					ck.msg.SetVisible(false)
					row.Append(ck.msg)
				}
				checks = append(checks, ck)
			}
		}
		root.Append(row)
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
	d.firstFocus = firstFocus
	root.ConnectMap(func() {
		if d.saved == nil {
			d.focusFirst()
		}
	})

	end := func(r result) {
		if d.done {
			return
		}
		d.done = true
		d.finish(r)
	}
	d.cancel = func() { end(result{}) }

	setBusy := func(b bool) {
		d.busy = b
		root.SetSensitive(!b)
	}

	submit := func() {
		if d.done || d.busy {
			return
		}
		vals := make(map[string]string, len(values))
		for id, get := range values {
			vals[id] = get()
		}

		// Invalid items block submission; the first one takes focus.
		var firstBad *check
		for _, ck := range checks {
			ok := ck.item.valid(vals[ck.item.ID])
			ck.mark(!ok)
			if !ok && firstBad == nil {
				firstBad = ck
			}
		}
		if firstBad != nil {
			firstBad.widget.GrabFocus()
			return
		}
		execErr.SetVisible(false)

		// What happens once any exec command has succeeded.
		finish := func() {
			if cfg.Submit.Stdout != "" {
				fmt.Fprintln(&d.out, expand(cfg.Submit.Stdout, activeID, vals))
			}
			if cfg.Submit.Format == "json" {
				multi := map[string][]string{}
				for id, get := range multiValues {
					multi[id] = get()
				}
				js, err := buildJSON(cfg.Items, vals, multi, activeID)
				if err != nil {
					log.Printf("json: %v", err)
				} else {
					fmt.Fprintln(&d.out, js)
				}
			}
			end(result{submitted: true})
		}

		if cfg.Submit.Exec == "" {
			finish()
			return
		}
		cmd := expand(cfg.Submit.Exec, activeID, vals)
		if cfg.Submit.Spawn {
			spawn(cmd, env, dir)
			finish()
			return
		}
		// Run off the main loop. The daemon serves other dialogs meanwhile, and
		// a command that itself calls `spot` (a sub-dialog) would otherwise
		// wait on a loop that is waiting on it.
		setBusy(true)
		go func() {
			stdout, stderr, status := runCommand(cmd, env, dir)
			coreglib.IdleAdd(func() {
				setBusy(false)
				if d.done {
					return
				}
				d.out.WriteString(stdout)
				if status != 0 {
					if cfg.Submit.OnError == "exit" {
						d.errOut.WriteString(stderr)
						end(result{code: status})
						return
					}
					msg := strings.TrimSpace(stderr)
					if msg == "" {
						msg = fmt.Sprintf("command failed (exit status %d)", status)
					}
					execErr.SetText(msg)
					execErr.SetVisible(true)
					return
				}
				finish()
			})
		}()
	}
	submitFn = submit

	d.onKey = func(keyval uint, state gdk.ModifierType) bool {
		if d.busy {
			return false
		}
		// In a text area plain Enter is a newline; Ctrl+Enter submits.
		inText := false
		for _, tv := range textViews {
			if tv.HasFocus() {
				inText = true
			}
		}
		// Multi-select lists: Up/Down hand focus to the list, whose own
		// keys (arrows, Space, Shift/Ctrl+arrows) do the selecting.
		if multiView != nil && multiSearch.HasFocus() {
			switch keyval {
			case gdk.KEY_Up, gdk.KEY_KP_Up, gdk.KEY_Down, gdk.KEY_KP_Down:
				multiView.GrabFocus()
				return true
			}
		}
		// While a tile section has focus the arrow keys drive one cursor
		// across all sections (GTK's own navigation stops at a section's edge).
		if nav.active != nil {
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
			if (dx != 0 || dy != 0) && nav.move(dx, dy) {
				return true
			}
		}
		switch keyval {
		case gdk.KEY_Escape:
			end(result{})
			return true
		case gdk.KEY_Return, gdk.KEY_KP_Enter:
			if inText && state&gdk.ControlMask == 0 {
				return false
			}
			if nav.active != nil {
				activeID = nav.choose()
			}
			submit()
			return true
		case gdk.KEY_Up, gdk.KEY_KP_Up:
			if listSelection != nil {
				moveSelection(listSelection, listView, -1)
				return true
			}
		case gdk.KEY_Down, gdk.KEY_KP_Down:
			if listSelection != nil {
				moveSelection(listSelection, listView, 1)
				return true
			}
		case gdk.KEY_Page_Up:
			if listSelection != nil {
				moveSelection(listSelection, listView, -10)
				return true
			}
		case gdk.KEY_Page_Down:
			if listSelection != nil {
				moveSelection(listSelection, listView, 10)
				return true
			}
		}
		return false
	}
	return d
}

// spawn starts cmd and does not wait for it to finish.
func spawn(cmd string, env []string, dir string) {
	c := exec.Command("sh", "-c", cmd)
	c.Env, c.Dir = env, dir
	if err := c.Start(); err != nil {
		log.Printf("spawn: %v", err)
		return
	}
	go c.Wait() // reap it: the daemon outlives its children
}

// runCommand runs cmd to completion and returns what it wrote to stdout and
// stderr and its exit status (0 on success).
func runCommand(cmd string, env []string, dir string) (stdout, stderr string, status int) {
	var so, se bytes.Buffer
	c := exec.Command("sh", "-c", cmd)
	c.Env, c.Dir = env, dir
	c.Stdout, c.Stderr = &so, &se
	err := c.Run()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return so.String(), se.String(), 0
	case errors.As(err, &ee):
		return so.String(), se.String(), ee.ExitCode()
	default:
		return so.String(), err.Error() + "\n", 127
	}
}

// tileGroup is one `tiles` section.
type tileGroup struct {
	id       string
	flow     *gtk.FlowBox
	children []*gtk.FlowBoxChild
	cols     int
}

// selected is the index of the section's selected tile, or -1.
func (g *tileGroup) selected() int {
	sel := g.flow.SelectedChildren()
	if len(sel) == 0 {
		return -1
	}
	return sel[0].Index()
}

// tileNav is the keyboard cursor over every tile section, drawn with
// .spot-cursor. It moves independently of each section's selection (the
// picked tile), which only changes when the cursor tile is chosen with Enter.
type tileNav struct {
	groups []*tileGroup
	active *tileGroup // section holding keyboard focus, nil if none
	cur    int        // cursor tile within active
}

// choose selects the tile under the cursor and returns its section's id.
func (n *tileNav) choose() string {
	n.active.flow.SelectChild(n.active.children[n.cur])
	return n.active.id
}

// hide removes the cursor from every tile.
func (n *tileNav) hide() {
	for _, g := range n.groups {
		for _, c := range g.children {
			c.RemoveCSSClass("spot-cursor")
		}
	}
}

// show puts the cursor on tile n.cur of g and nowhere else.
func (n *tileNav) show(g *tileGroup) {
	n.hide()
	if n.cur >= 0 && n.cur < len(g.children) {
		g.children[n.cur].AddCSSClass("spot-cursor")
	}
}

// move steps the cursor by (dx, dy) tiles, crossing into the neighbouring
// section past the first or last row. It reports whether it moved; at the
// outer edge it returns false so GTK can move focus on.
func (n *tileNav) move(dx, dy int) bool {
	gi := slices.Index(n.groups, n.active)
	g := n.groups[gi]
	idx := n.cur
	row, col := idx/g.cols, idx%g.cols
	last := len(g.children) - 1

	target, next := g, idx
	switch {
	case dx != 0:
		next = idx + dx
		if next < 0 || next > last || next/g.cols != row {
			return true // end of the row: stay, but the key is ours
		}
	case dy > 0 && idx+g.cols <= last:
		next = idx + g.cols
	case dy > 0 && row < last/g.cols:
		next = last // short final row
	case dy > 0:
		if gi+1 >= len(n.groups) || len(n.groups[gi+1].children) == 0 {
			return false
		}
		target = n.groups[gi+1]
		next = min(col, len(target.children)-1)
	case dy < 0 && idx-g.cols >= 0:
		next = idx - g.cols
	default:
		if gi == 0 || len(n.groups[gi-1].children) == 0 {
			return false
		}
		target = n.groups[gi-1]
		next = min((len(target.children)-1)/target.cols*target.cols+col, len(target.children)-1)
	}
	target.children[next].GrabFocus()
	n.active, n.cur = target, next
	n.show(target)
	return true
}

// check ties an item's required / pattern rules to its widget.
type check struct {
	item   Item
	widget *gtk.Widget
	msg    *gtk.Label // optional error text under the item
}

func (c *check) mark(invalid bool) {
	if invalid {
		c.widget.AddCSSClass("spot-invalid")
	} else {
		c.widget.RemoveCSSClass("spot-invalid")
	}
	if c.msg != nil {
		c.msg.SetVisible(invalid)
	}
}

func (c *check) clear() { c.mark(false) }

// digitsFor is the number of decimals a slider with this step needs.
func digitsFor(step float64) int {
	d := 0
	for step != math.Trunc(step) && d < 6 {
		step *= 10
		d++
	}
	return d
}

// roundTo snaps v to a multiple of step so floating-point drift never shows.
func roundTo(v, step float64) float64 {
	return math.Round(v/step) * step
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

func placeRoot(root *gtk.Box, anchor string) {
	h, v := parseAnchor(anchor)
	root.SetHAlign([]gtk.Align{gtk.AlignCenter, gtk.AlignStart, gtk.AlignEnd}[h])
	root.SetVAlign([]gtk.Align{gtk.AlignCenter, gtk.AlignStart, gtk.AlignEnd}[v])
}
