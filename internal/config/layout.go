package config

import (
	"strings"
)

type hAlign int

type vAlign int

const (
	hCenter hAlign = iota
	hStart
	hEnd
)

const (
	vCenter vAlign = iota
	vStart
	vEnd
)

// ParseAnchor reads "top right" style anchors; unknown words are ignored.
func ParseAnchor(anchor string) (hAlign, vAlign) {
	h, v := hCenter, vCenter
	for _, part := range strings.Fields(strings.ToLower(anchor)) {
		switch part {
		case "top":
			v = vStart
		case "bottom":
			v = vEnd
		case "left":
			h = hStart
		case "right":
			h = hEnd
		}
	}
	return h, v
}

// PlacementMargins turns margins and offsets into the four GTK margins of the
// dialog, for its alignment. An offset moves the dialog from its anchor: right /
// down when positive. Margins cannot be negative in GTK, so a shift toward an edge
// is the margin on that side growing, a shift away from it the margin shrinking
// (never below zero). On a centred axis the dialog sits in the middle of what the
// margins leave free, so shifting by n px takes a margin difference of 2n.
func PlacementMargins(h hAlign, v vAlign, top, bottom, left, right, ox, oy int) (t, b, l, r int) {
	t, b, l, r = top, bottom, left, right
	switch h {
	case hStart:
		l += ox
	case hEnd:
		r -= ox
	default:
		if ox > 0 {
			l += 2 * ox
		} else {
			r += -2 * ox
		}
	}
	switch v {
	case vStart:
		t += oy
	case vEnd:
		b -= oy
	default:
		if oy > 0 {
			t += 2 * oy
		} else {
			b += -2 * oy
		}
	}
	return max(t, 0), max(b, 0), max(l, 0), max(r, 0)
}

// ParseKeyboard normalises the layer-shell keyboard mode.
func ParseKeyboard(mode string) string {
	switch m := strings.ToLower(strings.TrimSpace(mode)); m {
	case "none":
		return "none"
	case "on-demand", "ondemand":
		return "on-demand"
	default:
		return "exclusive"
	}
}
