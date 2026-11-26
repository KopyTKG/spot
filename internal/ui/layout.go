package ui

import (
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"gitlab.com/KopyTKG/spot/internal/config"
)

func placeRoot(root *gtk.Box, cfg *config.Config) {
	h, v := config.ParseAnchor(cfg.Anchor)
	root.SetHAlign([]gtk.Align{gtk.AlignCenter, gtk.AlignStart, gtk.AlignEnd}[h])
	root.SetVAlign([]gtk.Align{gtk.AlignCenter, gtk.AlignStart, gtk.AlignEnd}[v])
	t, b, l, r := config.PlacementMargins(h, v, cfg.MarginTop, cfg.MarginBottom, cfg.MarginLeft, cfg.MarginRight, cfg.OffsetX, cfg.OffsetY)
	root.SetMarginTop(t)
	root.SetMarginBottom(b)
	root.SetMarginStart(l)
	root.SetMarginEnd(r)
}
