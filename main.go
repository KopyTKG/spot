package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/diamondburned/gotk4-layer-shell/pkg/gtk4layershell"
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	Title    string
	Anchor   string // center (default) | top | bottom | left | right
	Width    int
	Height   int
	Style    string // path to CSS, relative to TOML file
	Footer   string
	Keyboard string // exclusive (default) | on-demand | none
	Items    []Item
	Submit   Submit
	Opacity  *Opacity
}

// Opacity fades the dialog itself depending on where the pointer is.
// Percentages are 0-100. Without an [opacity] table the defaults below apply;
// set outside = 100 for a constant, opaque dialog.
type Opacity struct {
	Inside     *float64 `toml:"inside"`     // % while the pointer is over the dialog (default 100)
	Outside    *float64 `toml:"outside"`    // % while the pointer is outside it (default 85)
	Transition *int     `toml:"transition"` // fade time in ms (default 150, 0 = instant)
}

type Item struct {
	Type        string // field | password | select | list | tiles
	ID          string
	Label       string
	Placeholder string
	Default     string
	Options     []string // select, list
	Stdin       bool     // list: read newline-delimited options from stdin
	Height      int      // list: min content height in px
	Columns     int      // tiles: max children per row (default 3)
	Tiles       []Tile   // tiles: per-tile icon/label/value
}

type Tile struct {
	Icon  string // text rendered large above the label (NF glyph, emoji, etc.)
	Label string
	Value string // returned as the item's value when selected
}

type Submit struct {
	Exec   string // command template; {id} substituted from field values
	Stdout string // print template to stdout on submit (exit 1 on cancel)
	Spawn  bool   // (exec only) fire-and-forget then close
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: spot <config.toml>")
		os.Exit(2)
	}
	cfgPath, err := filepath.Abs(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		log.Fatal(err)
	}
	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		log.Fatalf("parsing %s: %v", cfgPath, err)
	}

	cssPath := ""
	if cfg.Style != "" {
		if filepath.IsAbs(cfg.Style) {
			cssPath = cfg.Style
		} else {
			cssPath = filepath.Join(filepath.Dir(cfgPath), cfg.Style)
		}
	}

	// Env-var overrides so shared configs can be parameterised per invocation
	// (used by arch-menu / arch-askpass to set a per-call title or hint).
	if v, ok := os.LookupEnv("SPOT_TITLE"); ok {
		cfg.Title = v
	}
	if v, ok := os.LookupEnv("SPOT_FOOTER"); ok {
		cfg.Footer = v
	}
	if v, ok := os.LookupEnv("SPOT_PLACEHOLDER"); ok {
		for i := range cfg.Items {
			t := cfg.Items[i].Type
			if t == "field" || t == "password" || t == "list" {
				cfg.Items[i].Placeholder = v
				break
			}
		}
	}
	if v, ok := os.LookupEnv("SPOT_DEFAULT"); ok {
		for i := range cfg.Items {
			t := cfg.Items[i].Type
			if t == "field" || t == "select" || t == "list" || t == "tiles" {
				cfg.Items[i].Default = v
				break
			}
		}
	}

	// If any list item reads from stdin, slurp before GTK takes over.
	for i := range cfg.Items {
		if cfg.Items[i].Type == "list" && cfg.Items[i].Stdin {
			sc := bufio.NewScanner(os.Stdin)
			sc.Buffer(make([]byte, 1024*1024), 1024*1024)
			var lines []string
			for sc.Scan() {
				lines = append(lines, sc.Text())
			}
			cfg.Items[i].Options = lines
		}
	}

	// Per-invocation app ID so concurrent `spot` calls don't dedupe.
	base := strings.TrimSuffix(filepath.Base(cfgPath), filepath.Ext(cfgPath))
	appID := fmt.Sprintf("dev.kopy.spot.%s.p%d", sanitizeID(base), os.Getpid())
	app := gtk.NewApplication(appID, gio.ApplicationFlagsNone)

	submitted := false
	app.ConnectActivate(func() { activate(app, &cfg, cssPath, &submitted) })
	code := app.Run(nil)
	if code > 0 {
		os.Exit(code)
	}
	if !submitted {
		os.Exit(1)
	}
}

// sanitizeID strips characters that GApplication ID validation rejects.
func sanitizeID(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := b.String()
	if out == "" {
		out = "cfg"
	}
	return out
}

func activate(app *gtk.Application, cfg *Config, cssPath string, submitted *bool) {
	if cssPath != "" {
		if css, err := os.ReadFile(cssPath); err == nil {
			prov := gtk.NewCSSProvider()
			prov.LoadFromString(string(css))
			gtk.StyleContextAddProviderForDisplay(
				gdk.DisplayGetDefault(),
				prov,
				uint(gtk.STYLE_PROVIDER_PRIORITY_APPLICATION),
			)
		} else {
			log.Printf("css: %v (continuing without styles)", err)
		}
	}

	win := gtk.NewApplicationWindow(app)
	win.AddCSSClass("spot-window")
	if cfg.Title != "" {
		win.SetTitle(cfg.Title)
	}
	gtk4layershell.InitForWindow(&win.Window)
	gtk4layershell.SetLayer(&win.Window, gtk4layershell.LayerShellLayerTop)
	gtk4layershell.SetNamespace(&win.Window, "spot")
	// The layer surface covers the whole output so clicks outside the dialog
	// land on it and can dismiss it; the dialog is positioned inside.
	for _, e := range []gtk4layershell.Edge{
		gtk4layershell.LayerShellEdgeTop, gtk4layershell.LayerShellEdgeBottom,
		gtk4layershell.LayerShellEdgeLeft, gtk4layershell.LayerShellEdgeRight,
	} {
		gtk4layershell.SetAnchor(&win.Window, e, true)
	}
	applyKeyboard(&win.Window, cfg.Keyboard)

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
	var listSelection *gtk.SingleSelection // for keyboard nav
	var listView *gtk.ListView             // for ScrollTo on key nav
	activeID := ""                         // id of the last tiles group interacted with ({_active})
	var submitFn func()                    // late-bound so tile click handlers can call it

	for _, it := range cfg.Items {
		item := it
		row := gtk.NewBox(gtk.OrientationVertical, 4)
		row.AddCSSClass("spot-item")
		if item.Label != "" {
			lbl := gtk.NewLabel(item.Label)
			lbl.AddCSSClass("spot-label")
			lbl.SetXAlign(0)
			row.Append(lbl)
		}
		switch item.Type {
		case "field":
			entry := gtk.NewEntry()
			entry.AddCSSClass("spot-field")
			if item.Placeholder != "" {
				entry.SetPlaceholderText(item.Placeholder)
			}
			if item.Default != "" {
				entry.SetText(item.Default)
			}
			row.Append(entry)
			values[item.ID] = func() string { return entry.Text() }
			if firstFocus == nil {
				firstFocus = entry
			}
		case "password":
			entry := gtk.NewPasswordEntry()
			entry.AddCSSClass("spot-password")
			entry.SetShowPeekIcon(true)
			row.Append(entry)
			values[item.ID] = func() string { return entry.Text() }
			if firstFocus == nil {
				firstFocus = entry
			}
		case "select":
			sl := gtk.NewStringList(item.Options)
			dd := gtk.NewDropDown(sl, nil)
			dd.AddCSSClass("spot-select")
			if item.Default != "" {
				for i, opt := range item.Options {
					if opt == item.Default {
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
			filt := gtk.NewCustomFilter(func(obj *coreglib.Object) bool {
				if query == "" {
					return true
				}
				so, ok := obj.Cast().(*gtk.StringObject)
				if !ok {
					return false
				}
				return strings.Contains(
					strings.ToLower(so.String()),
					strings.ToLower(query),
				)
			})
			filterModel := gtk.NewFilterListModel(model, &filt.Filter)
			selection := gtk.NewSingleSelection(filterModel)

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

			lv := gtk.NewListView(selection, &factory.ListItemFactory)
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
				filt.Changed(gtk.FilterChangeDifferent)
				if filterModel.NItems() > 0 {
					selection.SetSelected(0)
				}
			})

			row.Append(search)
			row.Append(scrolled)

			values[item.ID] = func() string {
				obj := selection.SelectedItem()
				if obj == nil {
					return ""
				}
				so, ok := obj.Cast().(*gtk.StringObject)
				if !ok {
					return ""
				}
				return so.String()
			}

			if item.Default != "" {
				for i, opt := range opts {
					if opt == item.Default {
						selection.SetSelected(uint(i))
						break
					}
				}
			}

			if firstFocus == nil {
				firstFocus = search
			}
			listSelection = selection
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

				child := gtk.NewFlowBoxChild()
				child.AddCSSClass("spot-tile")
				child.SetChild(tileBox)
				flow.Append(child)
				children[i] = child
			}

			defaultIdx := 0
			if item.Default != "" {
				for i, tl := range tiles {
					if tl.Value == item.Default {
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
			flow.ConnectSelectedChildrenChanged(func() { activeID = itemID })

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
		default:
			log.Printf("unknown item type: %q (skipping)", item.Type)
			continue
		}
		root.Append(row)
	}

	if cfg.Footer != "" {
		f := gtk.NewLabel(cfg.Footer)
		f.AddCSSClass("spot-footer")
		f.SetXAlign(0)
		root.Append(f)
	}

	win.SetChild(root)

	// Click outside the dialog dismisses it (same as Esc).
	click := gtk.NewGestureClick()
	click.SetPropagationPhase(gtk.PhaseCapture)
	click.ConnectPressed(func(_ int, x, y float64) {
		if rx, ry, ok := win.TranslateCoordinates(root, x, y); ok && root.Contains(rx, ry) {
			return
		}
		win.Close()
	})
	win.AddController(click)

	op := cfg.Opacity
	if op == nil {
		op = &Opacity{}
	}
	setupOpacity(win, root, op)

	var submit func()
	submit = func() {
		exp := func(t string) string {
			t = strings.ReplaceAll(t, "{_active}", activeID)
			for id, get := range values {
				t = strings.ReplaceAll(t, "{"+id+"}", get())
			}
			return t
		}
		ranSomething := false
		if cfg.Submit.Exec != "" {
			spawn(exp(cfg.Submit.Exec))
			ranSomething = true
		}
		if cfg.Submit.Stdout != "" {
			fmt.Println(exp(cfg.Submit.Stdout))
			ranSomething = true
		}
		if !ranSomething {
			// No submit action configured — still count as submitted (Enter pressed).
		}
		*submitted = true
		win.Close()
	}
	submitFn = submit

	keys := gtk.NewEventControllerKey()
	// Capture phase so we see Esc/Enter before SearchEntry's built-in
	// "clear-on-Esc" / "activate-on-Enter" handlers eat them.
	keys.SetPropagationPhase(gtk.PhaseCapture)
	keys.ConnectKeyPressed(func(keyval, _ uint, _ gdk.ModifierType) bool {
		switch keyval {
		case gdk.KEY_Escape:
			win.Close()
			return true
		case gdk.KEY_Return, gdk.KEY_KP_Enter:
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
	})
	win.AddController(keys)

	// Defer GrabFocus until the surface is mapped — calling it before the
	// layer surface is realized is a no-op and leaves the layer without a
	// focused widget, which breaks key forwarding under some compositors.
	if firstFocus != nil {
		win.ConnectMap(func() {
			firstFocus.(interface{ GrabFocus() bool }).GrabFocus()
		})
	}
	win.Present()
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
	h, v := gtk.AlignCenter, gtk.AlignCenter
	for _, part := range strings.Fields(strings.ToLower(anchor)) {
		switch part {
		case "top":
			v = gtk.AlignStart
		case "bottom":
			v = gtk.AlignEnd
		case "left":
			h = gtk.AlignStart
		case "right":
			h = gtk.AlignEnd
		}
	}
	root.SetHAlign(h)
	root.SetVAlign(v)
}

func applyKeyboard(win *gtk.Window, mode string) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "none":
		gtk4layershell.SetKeyboardMode(win, gtk4layershell.LayerShellKeyboardModeNone)
	case "on-demand", "ondemand":
		gtk4layershell.SetKeyboardMode(win, gtk4layershell.LayerShellKeyboardModeOnDemand)
	default:
		gtk4layershell.SetKeyboardMode(win, gtk4layershell.LayerShellKeyboardModeExclusive)
	}
}

func spawn(cmd string) {
	c := exec.Command("sh", "-c", cmd)
	if err := c.Start(); err != nil {
		log.Printf("spawn: %v", err)
	}
}

// setupOpacity fades the dialog between its "pointer over" and "pointer
// outside" opacities.
func setupOpacity(win *gtk.ApplicationWindow, root *gtk.Box, o *Opacity) {
	pct := func(p *float64, def float64) float64 {
		if p != nil {
			def = *p
		}
		return max(0, min(100, def)) / 100
	}
	in, out := pct(o.Inside, 100), pct(o.Outside, 85)
	ms := 150
	if o.Transition != nil {
		ms = *o.Transition
	}

	prov := gtk.NewCSSProvider()
	gtk.StyleContextAddProviderForDisplay(
		gdk.DisplayGetDefault(), prov, uint(gtk.STYLE_PROVIDER_PRIORITY_USER))
	set := func(a float64) {
		prov.LoadFromString(fmt.Sprintf(
			".spot-root { opacity: %.3f; transition: opacity %dms ease; }", a, ms))
	}

	// Start at the "inside" opacity: until the pointer moves we cannot know
	// whether it is over the dialog.
	inside := true
	set(in)
	update := func(now bool) {
		if now == inside {
			return
		}
		inside = now
		if now {
			set(in)
		} else {
			set(out)
		}
	}

	motion := gtk.NewEventControllerMotion()
	motion.SetPropagationPhase(gtk.PhaseCapture)
	motion.ConnectMotion(func(x, y float64) {
		rx, ry, ok := win.TranslateCoordinates(root, x, y)
		update(ok && root.Contains(rx, ry))
	})
	motion.ConnectLeave(func() { update(false) })
	win.AddController(motion)
}
