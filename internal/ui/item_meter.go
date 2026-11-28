package ui

import (
	"strings"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"gitlab.com/KopyTKG/spot/internal/config"
)

// meter shows a number as a bar, without changing it: a battery, a full disk.
// It is read when the dialog opens and after every change made in it.
func (b *builder) meter(item config.Item, row *gtk.Box) {
	low, high := 0.0, 100.0
	if item.Min != nil {
		low = *item.Min
	}
	if item.Max != nil {
		high = *item.Max
	}
	if high <= low {
		high = low + 100
	}
	format := item.Format
	if format == "" {
		format = "{value}"
	}

	bar := gtk.NewProgressBar()
	bar.AddCSSClass("spot-meter")
	bar.SetHExpand(true)
	bar.SetVAlign(gtk.AlignCenter)
	text := gtk.NewLabel("")
	text.AddCSSClass("spot-meter-text")
	box := gtk.NewBox(gtk.OrientationHorizontal, 12)
	box.AddCSSClass("spot-meter-row")
	box.Append(bar)
	box.Append(text)
	row.Append(box)

	c := &control{b: b, item: item, strict: true}
	c.read = func(out string) (string, bool) {
		// "87%" is as good as "87".
		v, ok := parseNumber(strings.TrimSpace(strings.TrimSuffix(config.LastLine(out), "%")))
		return config.FormatNumber(v), ok
	}
	c.show = func(v string) {
		n, ok := parseNumber(v)
		if !ok {
			return
		}
		bar.SetFraction(max(0, min(1, (n-low)/(high-low))))
		text.SetText(strings.ReplaceAll(format, "{value}", v))
	}
	c.busy = func(bool) {}
	c.unsure = func(unsure bool) {
		classOn(box, "spot-unknown", unsure)
		if unsure {
			bar.SetFraction(0)
			text.SetText("…")
		}
	}
	c.unsure(true)
	b.refreshers = append(b.refreshers, c.refresh)
}
