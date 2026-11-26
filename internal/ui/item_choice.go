package ui

import (
	"math"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"gitlab.com/KopyTKG/spot/internal/config"
)

// selectOne is a drop-down.
func (b *builder) selectOne(item config.Item, row *gtk.Box) (input gtk.Widgetter) {
	sl := gtk.NewStringList(item.Options)
	dd := gtk.NewDropDown(sl, nil)
	dd.AddCSSClass("spot-select")
	if item.Default != nil {
		for i, opt := range item.Options {
			if opt == item.DefaultString() {
				dd.SetSelected(uint(i))
				break
			}
		}
	}
	row.Append(dd)
	b.values[item.ID] = func() string {
		idx := int(dd.Selected())
		if idx < 0 || idx >= len(item.Options) {
			return ""
		}
		return item.Options[idx]
	}
	input = dd
	b.focus(dd)
	return input
}

// toggle is a check box.
func (b *builder) toggle(item config.Item, row *gtk.Box) {
	cb := gtk.NewCheckButton()
	cb.AddCSSClass("spot-toggle")
	cb.SetLabel(item.Label)
	cb.SetActive(item.DefaultBool())
	row.Append(cb)
	b.values[item.ID] = func() string {
		if cb.Active() {
			return item.OnValue()
		}
		return item.OffValue()
	}
	b.focus(cb)
}

// scale is a slider.
func (b *builder) scale(item config.Item, row *gtk.Box) {
	sc := gtk.NewScaleWithRange(gtk.OrientationHorizontal,
		item.MinValue(), item.MaxValue(), item.StepValue())
	sc.AddCSSClass("spot-scale")
	sc.SetHExpand(true)
	sc.SetDrawValue(true)
	sc.SetDigits(digitsFor(item.StepValue()))
	sc.SetValue(item.ScaleDefault())
	row.Append(sc)
	b.values[item.ID] = func() string { return config.FormatNumber(roundTo(sc.Value(), item.StepValue())) }
	b.focus(sc)
}

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
