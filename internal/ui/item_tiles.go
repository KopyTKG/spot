package ui

import (
	"slices"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"gitlab.com/KopyTKG/spot/internal/config"
)

// tiles is one section of the tile picker.
func (b *builder) tiles(item config.Item, row *gtk.Box) {
	d := b.d
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
			if tl.Value == item.DefaultString() {
				defaultIdx = i
				break
			}
		}
	}
	if len(children) > 0 {
		flow.SelectChild(children[defaultIdx])
	}

	row.Append(flow)

	if b.activeID == "" {
		b.activeID = item.ID
	}
	itemID := item.ID
	group := &tileGroup{id: itemID, flow: flow, children: children, cols: cols}
	b.nav.groups = append(b.nav.groups, group)
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
	d.hook(flow, flow.ConnectSelectedChildrenChanged(func() {
		b.activeID = itemID
		markPicked()
	}))
	focus := gtk.NewEventControllerFocus()
	d.hook(focus, focus.ConnectEnter(func() {
		b.nav.active, b.nav.cur = group, max(group.selected(), 0)
		b.nav.show(group)
	}))
	d.hook(focus, focus.ConnectLeave(func() {
		if b.nav.active == group {
			b.nav.active = nil
			b.nav.hide()
		}
	}))
	flow.AddController(focus)

	b.values[item.ID] = func() string {
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

	d.hook(flow, flow.ConnectChildActivated(func(child *gtk.FlowBoxChild) {
		flow.SelectChild(child)
		b.activeID = itemID
		if b.submit != nil {
			b.submit()
		}
	}))

	b.focus(flow)
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
