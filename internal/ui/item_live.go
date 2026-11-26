package ui

import (
	"slices"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"gitlab.com/KopyTKG/spot/internal/config"
	"gitlab.com/KopyTKG/spot/internal/launcher"
)

// classOn adds or removes a CSS class.
func classOn(w gtk.Widgetter, class string, on bool) {
	if on {
		gtk.BaseWidget(w).AddCSSClass(class)
	} else {
		gtk.BaseWidget(w).RemoveCSSClass(class)
	}
}

// live registers a control: its value for submit, its state to be read, and the
// widget the keyboard can start on.
func (b *builder) live(c *control, w gtk.Widgetter) {
	gtk.BaseWidget(w).AddCSSClass("spot-control")
	b.values[c.item.ID] = func() string { return c.value }
	b.refreshers = append(b.refreshers, c.refresh)
	b.focus(w)
}

// switchItem is an on / off control: a switch, a button that stays pressed, or
// a check box.
func (b *builder) switchItem(item config.Item, row *gtk.Box) {
	look := item.Look
	if look == "" {
		look = "switch"
	}
	c := &control{b: b, item: item, value: item.OffValue()}
	if item.DefaultBool() {
		c.value = item.OnValue()
	}
	c.read = func(out string) (string, bool) {
		on, ok := item.ParseSwitch(out)
		if on {
			return item.OnValue(), ok
		}
		return item.OffValue(), ok
	}

	var (
		w, outer  gtk.Widgetter
		setActive func(bool)
		showing   bool // setActive is running: not the user
	)
	changed := func(active bool) {
		if showing {
			return
		}
		if active {
			c.set(item.OnValue())
		} else {
			c.set(item.OffValue())
		}
	}
	switch look {
	case "button":
		btn := gtk.NewToggleButtonWithLabel(item.Label)
		b.d.hook(btn, btn.ConnectToggled(func() { changed(btn.Active()) }))
		w, outer, setActive = btn, btn, btn.SetActive
	case "checkbox":
		cb := gtk.NewCheckButtonWithLabel(item.Label)
		b.d.hook(cb, cb.ConnectToggled(func() { changed(cb.Active()) }))
		w, outer, setActive = cb, cb, cb.SetActive
	default:
		sw := gtk.NewSwitch()
		sw.SetVAlign(gtk.AlignCenter)
		sl := &slider{sw: sw}
		b.d.hook(sw, sw.NotifyProperty("active", func() {
			if !sl.arrived() {
				changed(sw.Active())
			}
		}))
		lbl := gtk.NewLabel(item.Label)
		lbl.SetXAlign(0)
		lbl.SetHExpand(true)
		box := gtk.NewBox(gtk.OrientationHorizontal, 12)
		box.Append(lbl)
		box.Append(sw)
		w, outer, setActive = sw, box, sl.to
	}
	gtk.BaseWidget(outer).AddCSSClass("spot-switch")
	gtk.BaseWidget(outer).AddCSSClass("spot-switch-" + look)
	c.show = func(v string) {
		showing = true
		setActive(v == item.OnValue())
		showing = false
		classOn(outer, "spot-on", v == item.OnValue())
	}
	c.busy = func(busy bool) { classOn(outer, "spot-busy", busy) }
	c.show(c.value)
	row.Append(outer)
	b.live(c, w)
}

// segmented picks one of a few options, shown side by side.
func (b *builder) segmented(item config.Item, row *gtk.Box) {
	box := gtk.NewBox(gtk.OrientationHorizontal, 0)
	box.AddCSSClass("spot-segmented")
	box.AddCSSClass("linked")
	box.SetHomogeneous(true)

	c := &control{b: b, item: item, value: item.DefaultString()}
	if !slices.Contains(item.Options, c.value) {
		c.value = ""
	}
	c.read = func(out string) (string, bool) {
		v := config.LastLine(out)
		return v, slices.Contains(item.Options, v)
	}

	showing := false
	buttons := make([]*gtk.ToggleButton, len(item.Options))
	for i, opt := range item.Options {
		label := opt
		if len(item.Labels) > 0 {
			label = item.Labels[i]
		}
		btn := gtk.NewToggleButtonWithLabel(label)
		btn.AddCSSClass("spot-segment")
		btn.AddCSSClass("spot-control")
		if i > 0 {
			btn.SetGroup(buttons[0])
		}
		b.d.hook(btn, btn.ConnectToggled(func() {
			if !showing && btn.Active() {
				c.set(opt)
			}
		}))
		buttons[i] = btn
		box.Append(btn)
	}
	c.show = func(v string) {
		showing = true
		for i, opt := range item.Options {
			buttons[i].SetActive(opt == v)
			classOn(buttons[i], "spot-on", opt == v)
		}
		showing = false
	}
	c.busy = func(busy bool) { classOn(box, "spot-busy", busy) }
	c.show(c.value)
	row.Append(box)

	start := buttons[max(slices.Index(item.Options, c.value), 0)]
	b.values[item.ID] = func() string { return c.value }
	b.refreshers = append(b.refreshers, c.refresh)
	b.focus(start)
}

// button runs a command. With close = true it starts it and closes the dialog;
// otherwise the dialog stays and the states are read again afterwards.
func (b *builder) button(item config.Item, row *gtk.Box) {
	btn := gtk.NewButtonWithLabel(item.Label)
	btn.AddCSSClass("spot-button")
	btn.AddCSSClass("spot-control")
	running := false
	b.d.hook(btn, btn.ConnectClicked(func() {
		if running || b.d.done {
			return
		}
		cmd := fill(item.Exec, item.ID, item.ID, "")
		if item.Close {
			launcher.SpawnDetached(cmd, b.env, b.dir)
			b.end(result{submitted: true})
			return
		}
		running = true
		classOn(btn, "spot-busy", true)
		b.command(cmd, "", item.CommandTimeout(), func(_, stderr string, status int) {
			running = false
			classOn(btn, "spot-busy", false)
			b.report(stderr, status)
			b.refreshAll()
		})
	}))
	row.Append(btn)
	b.focus(btn)
}

// separator is a line between groups of items; its label sits above it.
func (b *builder) separator(row *gtk.Box) {
	line := gtk.NewSeparator(gtk.OrientationHorizontal)
	line.AddCSSClass("spot-separator")
	row.Append(line)
}

// liveScale makes a slider apply as it moves. Moves are gathered for a moment so
// that a drag does not start a command per pixel.
func (b *builder) liveScale(item config.Item, sc *gtk.Scale) {
	format := func() string { return config.FormatNumber(roundTo(sc.Value(), item.StepValue())) }
	c := &control{b: b, item: item, value: format()}
	c.read = func(out string) (string, bool) {
		v, ok := parseNumber(config.LastLine(out))
		return config.FormatNumber(v), ok
	}
	showing := false
	c.show = func(v string) {
		if n, ok := parseNumber(v); ok && v != format() {
			showing = true
			sc.SetValue(n)
			showing = false
		}
	}
	c.busy = func(busy bool) { classOn(sc, "spot-busy", busy) }

	var pending coreglib.SourceHandle
	cancel := func() {
		if pending != 0 {
			coreglib.SourceRemove(pending)
			pending = 0
		}
	}
	b.d.hooks = append(b.d.hooks, cancel)
	c.holding = func() bool { return pending != 0 }
	b.d.hook(sc, sc.ConnectValueChanged(func() {
		if showing {
			return
		}
		cancel()
		c.reads++ // or a read already under way would snap the slider back
		pending = coreglib.TimeoutAdd(120, func() bool {
			pending = 0
			c.set(format())
			return false
		})
	}))
	b.values[item.ID] = format
	b.refreshers = append(b.refreshers, c.refresh)
}
