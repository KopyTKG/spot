package ui

import (
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"gitlab.com/KopyTKG/spot/internal/config"
)

// field is a one-line text entry.
func (b *builder) field(item config.Item, row *gtk.Box) (input gtk.Widgetter, onEdit func(func())) {
	d := b.d
	entry := gtk.NewEntry()
	entry.AddCSSClass("spot-field")
	if item.Placeholder != "" {
		entry.SetPlaceholderText(item.Placeholder)
	}
	if item.Default != nil {
		entry.SetText(item.DefaultString())
	}
	row.Append(entry)
	b.values[item.ID] = func() string { return entry.Text() }
	input, onEdit = entry, func(f func()) { d.hook(entry, entry.ConnectChanged(f)) }
	b.focus(entry)
	return input, onEdit
}

// password is an entry that hides what is typed.
func (b *builder) password(item config.Item, row *gtk.Box) (input gtk.Widgetter, onEdit func(func())) {
	d := b.d
	entry := gtk.NewPasswordEntry()
	entry.AddCSSClass("spot-password")
	entry.SetShowPeekIcon(true)
	row.Append(entry)
	b.values[item.ID] = func() string { return entry.Text() }
	input, onEdit = entry, func(f func()) { d.hook(entry, entry.ConnectChanged(f)) }
	b.focus(entry)
	return input, onEdit
}

// text is a multi-line editor.
func (b *builder) text(item config.Item, row *gtk.Box) (input gtk.Widgetter, onEdit func(func())) {
	d := b.d
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
		d.hook(buf, buf.ConnectChanged(func() { ph.SetVisible(buf.CharCount() == 0) }))
		widget = ov
	}
	if item.Default != nil {
		buf.SetText(item.DefaultString())
	}
	row.Append(widget)
	b.values[item.ID] = func() string {
		start, end := buf.Bounds()
		return buf.Text(start, end, false)
	}
	b.textViews = append(b.textViews, tv)
	input, onEdit = tv, func(f func()) { d.hook(buf, buf.ConnectChanged(f)) }
	b.focus(tv)
	return input, onEdit
}
