package ui

import (
	"strings"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"

	"gitlab.com/KopyTKG/spot/internal/config"
)

// menuGrid shows a menu page as tiles: the glyph of an entry's label large, the
// rest below. It uses the classes of a tiles item, so a theme dresses both. The
// keyboard stays in the search box; the grid keeps its own cursor.
type menuGrid struct {
	on       bool
	scrolled *gtk.ScrolledWindow
	flow     *gtk.FlowBox
	tiles    []*gtk.FlowBoxChild
	cols, at int
}

func newMenuGrid(height int) *menuGrid {
	g := &menuGrid{flow: gtk.NewFlowBox(), scrolled: gtk.NewScrolledWindow()}
	g.flow.AddCSSClass("spot-tiles")
	g.flow.AddCSSClass("spot-menu-grid")
	g.flow.SetSelectionMode(gtk.SelectionNone)
	g.flow.SetHomogeneous(true)
	g.flow.SetVAlign(gtk.AlignStart)
	g.scrolled.SetMinContentHeight(height)
	g.scrolled.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	g.scrolled.SetChild(g.flow)
	g.scrolled.SetHExpand(true)
	g.scrolled.SetVExpand(true)
	g.scrolled.SetVisible(false)
	return g
}

// fill puts one tile per label.
func (g *menuGrid) fill(labels []string, cols int) {
	for _, t := range g.tiles {
		g.flow.Remove(t)
	}
	g.tiles, g.cols, g.at = nil, max(cols, 1), 0
	if len(labels) == 0 {
		return
	}
	g.flow.SetMinChildrenPerLine(uint(g.cols))
	g.flow.SetMaxChildrenPerLine(uint(g.cols))
	for _, label := range labels {
		body := gtk.NewBox(gtk.OrientationVertical, 6)
		body.AddCSSClass("spot-tile-body")
		body.SetVAlign(gtk.AlignCenter)
		name := config.WithoutIcon(label)
		if glyph := iconOf(label); glyph != "" {
			icon := gtk.NewLabel(strings.TrimSpace(glyph))
			icon.AddCSSClass("spot-tile-icon")
			body.Append(icon)
		}
		text := gtk.NewLabel(name)
		text.AddCSSClass("spot-tile-label")
		text.SetJustify(gtk.JustifyCenter)
		text.SetWrap(true)
		text.SetLines(2)
		text.SetEllipsize(pango.EllipsizeEnd)
		text.SetMaxWidthChars(12)
		body.Append(text)
		child := gtk.NewFlowBoxChild()
		child.AddCSSClass("spot-tile")
		child.SetFocusable(false) // typing goes on into the search
		child.SetChild(body)
		g.flow.Append(child)
		g.tiles = append(g.tiles, child)
	}
}

// choose puts the cursor on tile i and brings it into view.
func (g *menuGrid) choose(i int) {
	if len(g.tiles) == 0 {
		return
	}
	g.at = max(0, min(i, len(g.tiles)-1))
	for n, t := range g.tiles {
		classOn(t, "spot-cursor", n == g.at)
	}
	if r, ok := g.tiles[g.at].ComputeBounds(g.flow); ok && r.Height() > 0 {
		g.scrolled.VAdjustment().ClampPage(float64(r.Y()), float64(r.Y()+r.Height()))
	}
}

// key moves the cursor. Left and Right belong to the search once something is
// typed there; empty says nothing is.
func (g *menuGrid) key(keyval uint, state gdk.ModifierType, empty bool) bool {
	if state&(gdk.ControlMask|gdk.AltMask|gdk.ShiftMask) != 0 {
		return false
	}
	n := len(g.tiles)
	switch keyval {
	case gdk.KEY_Left, gdk.KEY_KP_Left:
		if !empty {
			return false
		}
		g.choose(g.at - 1)
	case gdk.KEY_Right, gdk.KEY_KP_Right:
		if !empty {
			return false
		}
		g.choose(g.at + 1)
	case gdk.KEY_Up, gdk.KEY_KP_Up:
		if g.at >= g.cols {
			g.choose(g.at - g.cols)
		}
	case gdk.KEY_Down, gdk.KEY_KP_Down:
		switch {
		case g.at+g.cols < n:
			g.choose(g.at + g.cols)
		case n > 0 && g.at/g.cols < (n-1)/g.cols: // a short last row
			g.choose(n - 1)
		}
	case gdk.KEY_Page_Up:
		g.choose(g.at % g.cols)
	case gdk.KEY_Page_Down:
		g.choose(n - 1)
	default:
		return false
	}
	return true
}
