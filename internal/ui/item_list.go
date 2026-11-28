package ui

import (
	"context"
	"strings"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"

	"gitlab.com/KopyTKG/spot/internal/config"
	"gitlab.com/KopyTKG/spot/internal/launcher"
	"gitlab.com/KopyTKG/spot/internal/match"
)

// list is a searchable list: plain, multi-select, collapsible, or the app launcher.
func (b *builder) list(item config.Item, row *gtk.Box) (input gtk.Widgetter) {
	d := b.d
	opts := item.Options
	var apps *launcher.Results
	if item.Type == "apps" {
		apps = launcher.NewResults(b.cfg.Apps, b.cfg.Sources, b.envOf, launcher.HomeOf(b.envOf))
		b.appsRes, b.appsID = apps, item.ID
		opts = apps.StartLines()
	}
	// Collapsible list: the rows shown are the flattened groups; `entries`
	// says what each shown line is (a header or a row).
	var gs *match.GroupSet
	var entries []match.GroupEntry
	var collapsed map[string]bool
	if item.Collapsible {
		gs = match.ParseGroups(opts, item.GroupSeparator())
		collapsed = map[string]bool{}
		for _, g := range gs.Order {
			collapsed[g] = item.CollapsedByDefault()
		}
		entries = gs.Flatten(collapsed, "", item.Match)
		opts = match.EntryTexts(entries)
	}
	var sw *switchRows
	switch {
	case item.From != "" || item.Parse != "":
		sw = modRows(item.Rows)
		opts = sw.labels
		if item.Note != "" && item.From == "" {
			b.errLabel.SetText(item.Note)
			b.errLabel.SetVisible(true)
		}
	case item.Switches:
		sw = parseSwitchRows(&item, opts)
		opts = sw.labels
	}
	// A click flips a switch row; where there is none a click only selects.
	clickActs := gsOrSwitches(item, sw)
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
		if query == "" || ranked || gs != nil || apps != nil {
			return true
		}
		so, ok := obj.Cast().(*gtk.StringObject)
		if !ok {
			return false
		}
		if item.Match == "smart" {
			return match.SmartScore(so.String(), query) >= 0
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

	// Apps with icons get a richer row: icon, name and comment.
	appRows := apps != nil && apps.Cfg.IconsShown()
	if appRows {
		ensureIconPaths(apps.Paths, apps.Home)
	}
	factory := gtk.NewSignalListItemFactory()
	d.hook(factory, factory.ConnectSetup(func(obj *coreglib.Object) {
		li, ok := obj.Cast().(*gtk.ListItem)
		if !ok {
			return
		}
		if appRows {
			box := gtk.NewBox(gtk.OrientationHorizontal, 12)
			box.AddCSSClass("spot-list-row")
			box.AddCSSClass("spot-app")
			img := gtk.NewImage()
			img.AddCSSClass("spot-app-icon")
			img.SetVAlign(gtk.AlignCenter)
			text := gtk.NewBox(gtk.OrientationVertical, 0)
			text.AddCSSClass("spot-app-text")
			text.SetVAlign(gtk.AlignCenter)
			name := gtk.NewLabel("")
			name.AddCSSClass("spot-app-name")
			name.SetXAlign(0)
			name.SetEllipsize(pango.EllipsizeEnd)
			comment := gtk.NewLabel("")
			comment.AddCSSClass("spot-app-comment")
			comment.SetXAlign(0)
			comment.SetEllipsize(pango.EllipsizeEnd)
			text.Append(name)
			text.Append(comment)
			box.Append(img)
			box.Append(text)
			li.SetChild(box)
			// The selected row can have a bigger icon (icon_size_selected).
			if apps.Cfg.SelectedIconPx() != apps.Cfg.IconPx() {
				d.hook(li, li.NotifyProperty("selected", func() {
					if li.Selected() {
						img.SetPixelSize(apps.Cfg.SelectedIconPx())
					} else {
						img.SetPixelSize(apps.Cfg.IconPx())
					}
				}))
			}
			return
		}
		lbl := gtk.NewLabel("")
		lbl.AddCSSClass("spot-list-row")
		lbl.SetXAlign(0)
		if sw != nil {
			li.SetChild(newSwitchRow(lbl))
			return
		}
		li.SetChild(lbl)
	}))
	d.hook(factory, factory.ConnectBind(func(obj *coreglib.Object) {
		li, ok := obj.Cast().(*gtk.ListItem)
		if !ok {
			return
		}
		if appRows {
			box, ok := li.Child().(*gtk.Box)
			so, ok2 := li.Item().Cast().(*gtk.StringObject)
			if !ok || !ok2 {
				return
			}
			img := box.FirstChild().(*gtk.Image)
			text := img.NextSibling().(*gtk.Box)
			name := text.FirstChild().(*gtk.Label)
			comment := name.NextSibling().(*gtk.Label)
			if apps.Src != nil { // a source's row: its label, comment and the source's icon
				if pos := int(li.Position()); pos < len(apps.SrcRows) {
					row := apps.SrcRows[pos]
					name.SetText(row.Label)
					comment.SetText(row.Comment)
					comment.SetVisible(row.Comment != "")
					if apps.Src.Icon != "" {
						img.SetVisible(true)
						setAppIcon(img, apps.Src.Icon, apps.Cfg.IconPx())
					} else {
						img.SetVisible(false)
					}
					return
				}
			}
			img.SetVisible(true)
			e := apps.EntryOf(so.String())
			if e == nil { // not an app line: show it as it is
				setAppIcon(img, "", apps.Cfg.IconPx())
				name.SetText(so.String())
				comment.SetVisible(false)
				return
			}
			size := apps.Cfg.IconPx()
			if li.Selected() {
				size = apps.Cfg.SelectedIconPx()
			}
			setAppIcon(img, e.Icon, size)
			name.SetText(e.Name)
			comment.SetText(e.Comment)
			comment.SetVisible(apps.Cfg.CommentShown() && e.Comment != "")
			return
		}
		if sw != nil {
			if so, ok := li.Item().Cast().(*gtk.StringObject); ok {
				sw.bind(li.Child(), so.String())
			}
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
		if gs != nil {
			lbl.RemoveCSSClass("spot-group-header")
			lbl.RemoveCSSClass("spot-group-row")
			if pos := int(li.Position()); pos < len(entries) {
				if entries[pos].Header {
					lbl.AddCSSClass("spot-group-header")
				} else {
					lbl.AddCSSClass("spot-group-row")
				}
			}
		}
	}))

	lv := gtk.NewListView(selModel, &factory.ListItemFactory)
	lv.AddCSSClass("spot-list")
	lv.SetSingleClickActivate(clickActs) // a click on a group header folds it, on a switch flips it
	b.listView = lv

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

	// rebuild redraws a collapsible list; keep, when set, is the group
	// whose header the selection should land on.
	var pendingSource coreglib.SourceHandle // the debounce timer of a source run
	rebuild := func(keep string) {
		entries = gs.Flatten(collapsed, query, item.Match)
		model.Splice(0, model.NItems(), match.EntryTexts(entries))
		if i := match.HeaderIndex(entries, keep); keep != "" && i >= 0 {
			selection.SetSelected(uint(i))
			lv.ScrollTo(uint(i), gtk.ListScrollFocus|gtk.ListScrollSelect, nil)
		} else if len(entries) > 0 {
			selection.SetSelected(0)
		}
	}
	if gs != nil {
		toggle := func(delta int) bool {
			pos := int(selection.Selected())
			if pos < 0 || pos >= len(entries) {
				return false
			}
			e := entries[pos]
			searching := strings.TrimSpace(query) != ""
			switch {
			case e.Header && searching:
				return delta == 0 // matching groups are open; Enter must not submit a header
			case e.Header:
				switch delta {
				case 0:
					collapsed[e.Group] = !collapsed[e.Group]
				case -1:
					collapsed[e.Group] = true
				case 1:
					collapsed[e.Group] = false
				}
				rebuild(e.Group)
				return true
			case delta == -1 && !searching:
				collapsed[e.Group] = true // Left on a row: fold its group, land on the header
				rebuild(e.Group)
				return true
			}
			return false
		}
		b.groupToggle = toggle
		d.hook(lv, lv.ConnectActivate(func(pos uint) {
			if int(pos) < len(entries) && entries[pos].Header {
				selection.SetSelected(pos)
				toggle(0)
			}
		}))
	}
	if sw != nil {
		b.switchList(item, sw, selection, lv)
	}
	if apps != nil {
		d.hook(lv, lv.ConnectActivate(func(uint) {
			if b.submit != nil {
				b.submit()
			}
		}))
	}

	d.hooks = append(d.hooks, func() {
		if pendingSource != 0 {
			coreglib.SourceRemove(pendingSource)
			pendingSource = 0
		}
	})
	d.hook(search, search.ConnectSearchChanged(func() {
		query = search.Text()
		switch {
		case gs != nil:
			rebuild("")
		case apps != nil:
			lines, req := apps.SetQuery(query)
			if pendingSource != 0 {
				coreglib.SourceRemove(pendingSource)
				pendingSource = 0
			}
			model.Splice(0, model.NItems(), lines)
			if req != nil {
				runIt := func() {
					ctx, cancel := context.WithCancel(context.Background())
					apps.SrcCancel = cancel
					go func() {
						rows, err := launcher.RunSource(ctx, req.Src, req.Query, b.env, b.dir)
						coreglib.IdleAdd(func() {
							if err != nil || ctx.Err() != nil {
								return
							}
							if got, ok := apps.FinishSource(req.Seq, rows); ok {
								model.Splice(0, model.NItems(), got)
								if selection != nil && filterModel.NItems() > 0 {
									selection.SetSelected(0)
								}
							}
						})
					}()
				}
				if wait := req.Src.Debounce(); wait > 0 {
					pendingSource = coreglib.TimeoutAdd(uint(wait.Milliseconds()), func() bool {
						pendingSource = 0
						runIt()
						return false
					})
				} else {
					runIt()
				}
			}
		case ranked:
			model.Splice(0, model.NItems(), match.RankSmart(opts, query))
		}
		filt.Changed(gtk.FilterChangeDifferent)
		if gs == nil && selection != nil && filterModel.NItems() > 0 {
			selection.SetSelected(0)
		}
	}))

	if item.HideEmpty { // a bare search bar until there is something to list
		scrolled.SetVisible(filterModel.NItems() > 0) // the model the list shows, after filtering
		d.hook(filterModel, filterModel.ConnectItemsChanged(func(_, _, _ uint) { scrolled.SetVisible(filterModel.NItems() > 0) }))
	}
	row.Append(search)
	row.Append(scrolled)
	if item.Query != "" { // the search box starts with this text (a prefix, say)
		search.SetText(item.Query)
		search.SetPosition(-1)
	}

	selectedOf := func(obj *coreglib.Object) string {
		so, ok := obj.Cast().(*gtk.StringObject)
		if !ok {
			return ""
		}
		return so.String()
	}
	if item.Multi {
		b.multiValues[item.ID] = func() []string {
			var picked []string
			for i := uint(0); i < filterModel.NItems(); i++ {
				if multiSel.IsSelected(i) {
					picked = append(picked, selectedOf(filterModel.Item(i)))
				}
			}
			return picked
		}
		b.values[item.ID] = func() string {
			return strings.Join(b.multiValues[item.ID](), item.JoinWith())
		}
		if item.Default != nil {
			want := map[string]bool{}
			for _, v := range strings.Split(item.DefaultString(), item.JoinWith()) {
				want[v] = true
			}
			for i, opt := range opts {
				if want[opt] {
					multiSel.SelectItem(uint(i), false)
				}
			}
		}
		b.multiSearch, b.multiView = search, lv
	} else {
		b.values[item.ID] = func() string {
			obj := selection.SelectedItem()
			if obj == nil {
				return ""
			}
			switch {
			case gs != nil: // a row's text without its group; a header has no value
				if pos := int(selection.Selected()); pos >= 0 && pos < len(entries) && !entries[pos].Header {
					return entries[pos].Value
				}
				return ""
			case apps != nil && apps.Src != nil: // a source row's value
				if row := apps.PickedRow(); row != nil {
					return row.Value
				}
				return ""
			case sw != nil: // the row's key
				return sw.key(selectedOf(obj))
			case apps != nil: // the desktop id
				if e := apps.EntryOf(selectedOf(obj)); e != nil {
					return e.ID
				}
				return ""
			}
			return selectedOf(obj)
		}
		if item.Default != nil && gs == nil && apps == nil {
			for i, opt := range opts {
				if opt == item.DefaultString() || sw != nil && (item.From != "" || item.Parse != "") && sw.key(opt) == item.DefaultString() {
					selection.SetSelected(uint(i))
					break
				}
			}
		}
		b.listSelection = selection
		if apps != nil {
			apps.Pick = func() int { return int(selection.Selected()) }
		}
	}

	input = search
	b.focus(search)
	return input
}

// gsOrSwitches reports whether one click on a row of the list does something.
func gsOrSwitches(item config.Item, sw *switchRows) bool {
	if item.Collapsible {
		return true
	}
	if sw == nil {
		return false
	}
	if item.From == "" && item.Parse == "" {
		return true // switches = true, as before
	}
	for _, r := range sw.rows {
		if r.has {
			return true
		}
	}
	return false
}
