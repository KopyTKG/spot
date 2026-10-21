package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/diamondburned/gotk4-layer-shell/pkg/gtk4layershell"
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// version is set at build time: -ldflags "-X main.version=0.2.0".
var version = "dev"

const usage = `usage: spot [flags] <config>

spot renders a TOML-described dialog as a Wayland layer-shell overlay.
<config> is a path to a .toml file, or a bare name looked up as
$XDG_CONFIG_HOME/spot/<name>.toml (default ~/.config/spot).

flags:
  -h, --help       show this help
  -v, --version    print the version

exit status:
  0  submitted
  1  cancelled (Esc, click outside)
  2  usage error
  3  config or runtime error
     (with on_error = "exit", the failed command's own status)

config reference: https://github.com/KopyTKG/spot#config-reference
`

// outcome is what the dialog reports back to main.
type outcome struct {
	submitted bool
	code      int // non-zero: exit with this status (on_error = "exit")
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "spot: "+format+"\n", args...)
	os.Exit(exitError)
}

func main() {
	var arg string
	for _, a := range os.Args[1:] {
		switch {
		case a == "-h" || a == "--help":
			fmt.Print(usage)
			return
		case a == "-v" || a == "--version":
			fmt.Println("spot", version)
			return
		case strings.HasPrefix(a, "-") && a != "-":
			fmt.Fprintf(os.Stderr, "spot: unknown flag %q\n\n%s", a, usage)
			os.Exit(exitUsage)
		case arg != "":
			fmt.Fprint(os.Stderr, "spot: only one config file is accepted\n\n"+usage)
			os.Exit(exitUsage)
		default:
			arg = a
		}
	}
	if arg == "" {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(exitUsage)
	}

	cfgPath, err := filepath.Abs(resolveConfigPath(arg))
	if err != nil {
		fatal("%v", err)
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		fatal("%v", err)
	}
	cfg, err := parseConfig(data, os.LookupEnv)
	if err != nil {
		fatal("%s: %v", cfgPath, err)
	}

	cssPath := ""
	if cfg.Style != "" {
		if filepath.IsAbs(cfg.Style) {
			cssPath = cfg.Style
		} else {
			cssPath = filepath.Join(filepath.Dir(cfgPath), cfg.Style)
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
	app := gtk.NewApplication(appID(base, os.Getpid()), gio.ApplicationFlagsNone)

	var out outcome
	app.ConnectActivate(func() { activate(app, cfg, cssPath, &out) })
	code := app.Run(nil)
	if out.code != 0 {
		os.Exit(out.code)
	}
	if code > 0 {
		os.Exit(exitError)
	}
	if !out.submitted {
		os.Exit(exitCancelled)
	}
}

func activate(app *gtk.Application, cfg *Config, cssPath string, out *outcome) {
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
	var listSelection *gtk.SingleSelection // for keyboard nav (single-select list)
	var listView *gtk.ListView             // for ScrollTo on key nav
	var multiSearch *gtk.SearchEntry       // multi-select list: Up/Down move focus into the list
	var multiView *gtk.ListView
	var textViews []*gtk.TextView // Enter inserts a newline in these; Ctrl+Enter submits
	multiValues := map[string]func() []string{}
	var checks []*check // items with required / pattern rules
	activeID := ""      // id of the last tiles group interacted with ({_active})
	var submitFn func() // late-bound so tile click handlers can call it

	for _, it := range cfg.Items {
		item := it
		row := gtk.NewBox(gtk.OrientationVertical, 4)
		row.AddCSSClass("spot-item")
		if item.Label != "" && item.Type != "toggle" {
			lbl := gtk.NewLabel(item.Label)
			lbl.AddCSSClass("spot-label")
			lbl.SetXAlign(0)
			row.Append(lbl)
		}
		var input gtk.Widgetter // the widget .spot-invalid is put on and focus goes to
		var onEdit func(func()) // registers a callback for edits (clears .spot-invalid)
		switch item.Type {
		case "field":
			entry := gtk.NewEntry()
			entry.AddCSSClass("spot-field")
			if item.Placeholder != "" {
				entry.SetPlaceholderText(item.Placeholder)
			}
			if item.Default != nil {
				entry.SetText(item.defaultString())
			}
			row.Append(entry)
			values[item.ID] = func() string { return entry.Text() }
			input, onEdit = entry, func(f func()) { entry.ConnectChanged(f) }
			if firstFocus == nil {
				firstFocus = entry
			}
		case "password":
			entry := gtk.NewPasswordEntry()
			entry.AddCSSClass("spot-password")
			entry.SetShowPeekIcon(true)
			row.Append(entry)
			values[item.ID] = func() string { return entry.Text() }
			input, onEdit = entry, func(f func()) { entry.ConnectChanged(f) }
			if firstFocus == nil {
				firstFocus = entry
			}
		case "select":
			sl := gtk.NewStringList(item.Options)
			dd := gtk.NewDropDown(sl, nil)
			dd.AddCSSClass("spot-select")
			if item.Default != nil {
				for i, opt := range item.Options {
					if opt == item.defaultString() {
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
			input = dd
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

			lv := gtk.NewListView(selModel, &factory.ListItemFactory)
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
				if selection != nil && filterModel.NItems() > 0 {
					selection.SetSelected(0)
				}
			})

			row.Append(search)
			row.Append(scrolled)

			selectedOf := func(obj *coreglib.Object) string {
				so, ok := obj.Cast().(*gtk.StringObject)
				if !ok {
					return ""
				}
				return so.String()
			}
			if item.Multi {
				multiValues[item.ID] = func() []string {
					var picked []string
					for i := uint(0); i < filterModel.NItems(); i++ {
						if multiSel.IsSelected(i) {
							picked = append(picked, selectedOf(filterModel.Item(i)))
						}
					}
					return picked
				}
				values[item.ID] = func() string {
					return strings.Join(multiValues[item.ID](), item.separator())
				}
				if item.Default != nil {
					want := map[string]bool{}
					for _, d := range strings.Split(item.defaultString(), item.separator()) {
						want[d] = true
					}
					for i, opt := range opts {
						if want[opt] {
							multiSel.SelectItem(uint(i), false)
						}
					}
				}
				multiSearch, multiView = search, lv
			} else {
				values[item.ID] = func() string {
					obj := selection.SelectedItem()
					if obj == nil {
						return ""
					}
					return selectedOf(obj)
				}
				if item.Default != nil {
					for i, opt := range opts {
						if opt == item.defaultString() {
							selection.SetSelected(uint(i))
							break
						}
					}
				}
				listSelection = selection
			}

			input = search
			if firstFocus == nil {
				firstFocus = search
			}
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
			if item.Default != nil {
				for i, tl := range tiles {
					if tl.Value == item.defaultString() {
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
		case "text":
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
				buf.ConnectChanged(func() { ph.SetVisible(buf.CharCount() == 0) })
				widget = ov
			}
			if item.Default != nil {
				buf.SetText(item.defaultString())
			}
			row.Append(widget)
			values[item.ID] = func() string {
				start, end := buf.Bounds()
				return buf.Text(start, end, false)
			}
			textViews = append(textViews, tv)
			input, onEdit = tv, func(f func()) { buf.ConnectChanged(f) }
			if firstFocus == nil {
				firstFocus = tv
			}
		case "toggle":
			cb := gtk.NewCheckButton()
			cb.AddCSSClass("spot-toggle")
			cb.SetLabel(item.Label)
			cb.SetActive(item.defaultBool())
			row.Append(cb)
			values[item.ID] = func() string {
				if cb.Active() {
					return item.onValue()
				}
				return item.offValue()
			}
			if firstFocus == nil {
				firstFocus = cb
			}
		case "scale":
			sc := gtk.NewScaleWithRange(gtk.OrientationHorizontal,
				item.minValue(), item.maxValue(), item.stepValue())
			sc.AddCSSClass("spot-scale")
			sc.SetHExpand(true)
			sc.SetDrawValue(true)
			sc.SetDigits(digitsFor(item.stepValue()))
			sc.SetValue(item.scaleDefault())
			row.Append(sc)
			values[item.ID] = func() string { return formatNumber(roundTo(sc.Value(), item.stepValue())) }
			if firstFocus == nil {
				firstFocus = sc
			}
		default:
			log.Printf("unknown item type: %q (skipping)", item.Type)
			continue
		}
		if item.Required || item.pattern != nil {
			if _, ok := values[item.ID]; ok {
				ck := &check{item: item, widget: gtk.BaseWidget(input)}
				if onEdit != nil {
					onEdit(ck.clear)
				}
				if item.Error != "" {
					ck.msg = gtk.NewLabel(item.Error)
					ck.msg.AddCSSClass("spot-item-error")
					ck.msg.SetXAlign(0)
					ck.msg.SetVisible(false)
					row.Append(ck.msg)
				}
				checks = append(checks, ck)
			}
		}
		root.Append(row)
	}

	if cfg.Footer != "" {
		f := gtk.NewLabel(cfg.Footer)
		f.AddCSSClass("spot-footer")
		f.SetXAlign(0)
		root.Append(f)
	}

	// Shown when an exec command fails and on_error = "show".
	execErr := gtk.NewLabel("")
	execErr.AddCSSClass("spot-error")
	execErr.SetXAlign(0)
	execErr.SetWrap(true)
	execErr.SetVisible(false)
	root.Append(execErr)

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

	submit := func() {
		vals := make(map[string]string, len(values))
		for id, get := range values {
			vals[id] = get()
		}

		// Invalid items block submission; the first one takes focus.
		var firstBad *check
		for _, ck := range checks {
			ok := ck.item.valid(vals[ck.item.ID])
			ck.mark(!ok)
			if !ok && firstBad == nil {
				firstBad = ck
			}
		}
		if firstBad != nil {
			firstBad.widget.GrabFocus()
			return
		}
		execErr.SetVisible(false)

		if cfg.Submit.Exec != "" {
			cmd := expand(cfg.Submit.Exec, activeID, vals)
			if cfg.Submit.Spawn {
				spawn(cmd)
			} else if stderr, status := runCommand(cmd); status != 0 {
				if cfg.Submit.OnError == "exit" {
					os.Stderr.WriteString(stderr)
					out.code = status
					win.Close()
					return
				}
				msg := strings.TrimSpace(stderr)
				if msg == "" {
					msg = fmt.Sprintf("command failed (exit status %d)", status)
				}
				execErr.SetText(msg)
				execErr.SetVisible(true)
				return
			}
		}
		if cfg.Submit.Stdout != "" {
			fmt.Println(expand(cfg.Submit.Stdout, activeID, vals))
		}
		if cfg.Submit.Format == "json" {
			multi := map[string][]string{}
			for id, get := range multiValues {
				multi[id] = get()
			}
			js, err := buildJSON(cfg.Items, vals, multi, activeID)
			if err != nil {
				log.Printf("json: %v", err)
			} else {
				fmt.Println(js)
			}
		}
		out.submitted = true
		win.Close()
	}
	submitFn = submit

	keys := gtk.NewEventControllerKey()
	// Capture phase so we see Esc/Enter before SearchEntry's built-in
	// "clear-on-Esc" / "activate-on-Enter" handlers eat them.
	keys.SetPropagationPhase(gtk.PhaseCapture)
	keys.ConnectKeyPressed(func(keyval, _ uint, state gdk.ModifierType) bool {
		// In a text area plain Enter is a newline; Ctrl+Enter submits.
		inText := false
		for _, tv := range textViews {
			if tv.HasFocus() {
				inText = true
			}
		}
		// Multi-select lists: Up/Down hand focus to the list, whose own
		// keys (arrows, Space, Shift/Ctrl+arrows) do the selecting.
		if multiView != nil && multiSearch.HasFocus() {
			switch keyval {
			case gdk.KEY_Up, gdk.KEY_KP_Up, gdk.KEY_Down, gdk.KEY_KP_Down:
				multiView.GrabFocus()
				return true
			}
		}
		switch keyval {
		case gdk.KEY_Escape:
			win.Close()
			return true
		case gdk.KEY_Return, gdk.KEY_KP_Enter:
			if inText && state&gdk.ControlMask == 0 {
				return false
			}
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
	h, v := parseAnchor(anchor)
	root.SetHAlign([]gtk.Align{gtk.AlignCenter, gtk.AlignStart, gtk.AlignEnd}[h])
	root.SetVAlign([]gtk.Align{gtk.AlignCenter, gtk.AlignStart, gtk.AlignEnd}[v])
}

func applyKeyboard(win *gtk.Window, mode string) {
	switch parseKeyboard(mode) {
	case "none":
		gtk4layershell.SetKeyboardMode(win, gtk4layershell.LayerShellKeyboardModeNone)
	case "on-demand":
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

// runCommand runs cmd to completion with its stdout passed through, and
// returns what it wrote to stderr and its exit status (0 on success).
func runCommand(cmd string) (stderr string, status int) {
	var buf bytes.Buffer
	c := exec.Command("sh", "-c", cmd)
	c.Stdout = os.Stdout
	c.Stderr = &buf
	err := c.Run()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return buf.String(), 0
	case errors.As(err, &ee):
		return buf.String(), ee.ExitCode()
	default:
		return err.Error() + "\n", 127
	}
}

// check ties an item's required / pattern rules to its widget.
type check struct {
	item   Item
	widget *gtk.Widget
	msg    *gtk.Label // optional error text under the item
}

func (c *check) mark(invalid bool) {
	if invalid {
		c.widget.AddCSSClass("spot-invalid")
	} else {
		c.widget.RemoveCSSClass("spot-invalid")
	}
	if c.msg != nil {
		c.msg.SetVisible(invalid)
	}
}

func (c *check) clear() { c.mark(false) }

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
