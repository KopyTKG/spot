package ui

import (
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"gitlab.com/KopyTKG/spot/internal/config"
	"gitlab.com/KopyTKG/spot/internal/launcher"
	"gitlab.com/KopyTKG/spot/internal/match"
	"gitlab.com/KopyTKG/spot/internal/protocol"
	"gitlab.com/KopyTKG/spot/internal/scheduler"
)

// menuRow is a line of the list: an entry, and where it is.
type menuRow struct {
	text  string
	entry config.Entry
	page  string      // the page it is on
	below []string    // pages between the one shown and that one, for a search hit
	given *config.Row // a row a mod gave; the page says what picking it does
}

// menuStep is a page left behind, the row to come back to, and the key that
// had been picked to get to that page.
type menuStep struct {
	page   string
	row    int
	picked string
}

// source is where the rows of a page come from: pages with the same one share them.
type source struct{ from, run, parse string }

type fetched struct {
	rows []config.Row
	note string
	done bool
}

// menu is the state of a menu: where it is and what it shows.
type menu struct {
	cfg     *config.Config
	page    string
	history []menuStep
	rows    []menuRow
	query   string // what rows were made for
	picked  string // the key picked on the page before, for a page with group
	sources map[source]*fetched
}

// newMenu builds a menu: one list whose pages are swapped in place. page, when
// set, is where it starts; Back then goes up the way that leads there. It looks
// like a dialog with a list: the same classes, so the same stylesheet fits.
func newMenu(cfg *config.Config, path, page string, env []string, dir string, back *config.BackSpec, mods config.ModSource) *dialog {
	d := &dialog{}
	m := &menu{cfg: cfg, page: cfg.StartPage(), sources: map[source]*fetched{}}
	if page != "" {
		m.page = page
		if way := cfg.PagePath(page); way != nil {
			for i, p := range way[:len(way)-1] {
				m.history = append(m.history, menuStep{page: p, row: entryTo(cfg, p, way[i+1])})
			}
		}
	}

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
	placeRoot(root, cfg)

	// The first page has its title alone; further down a header with Back.
	plain := gtk.NewLabel("")
	plain.AddCSSClass("spot-title")
	plain.SetXAlign(0.5)
	header := gtk.NewCenterBox()
	header.AddCSSClass("spot-header")
	label := "‹ Back"
	if back != nil {
		label = back.Label
	} else if cfg.Back != nil && cfg.Back.Label != "" {
		label = cfg.Back.Label
	}
	btn := gtk.NewButtonWithLabel(label)
	btn.AddCSSClass("spot-back")
	btn.SetFocusable(false)
	btn.SetFocusOnClick(false)
	btn.SetVAlign(gtk.AlignCenter)
	header.SetStartWidget(btn)
	titled := gtk.NewLabel("")
	titled.AddCSSClass("spot-title")
	titled.SetXAlign(0.5)
	header.SetCenterWidget(titled)
	root.Append(plain)
	root.Append(header)

	search := gtk.NewSearchEntry()
	search.AddCSSClass("spot-search")
	height := 320
	if cfg.Menu != nil {
		search.SetPlaceholderText(cfg.Menu.Placeholder)
		if cfg.Menu.Height > 0 {
			height = cfg.Menu.Height
		}
	}
	model := gtk.NewStringList(nil)
	selection := gtk.NewSingleSelection(model)
	factory := gtk.NewSignalListItemFactory()
	d.hook(factory, factory.ConnectSetup(func(obj *coreglib.Object) {
		if li, ok := obj.Cast().(*gtk.ListItem); ok {
			lbl := gtk.NewLabel("")
			lbl.AddCSSClass("spot-list-row")
			lbl.SetXAlign(0)
			li.SetChild(lbl)
		}
	}))
	d.hook(factory, factory.ConnectBind(func(obj *coreglib.Object) {
		li, ok := obj.Cast().(*gtk.ListItem)
		if !ok {
			return
		}
		lbl, ok := li.Child().(*gtk.Label)
		so, ok2 := li.Item().Cast().(*gtk.StringObject)
		if ok && ok2 {
			lbl.SetText(so.String())
		}
	}))
	lv := gtk.NewListView(selection, &factory.ListItemFactory)
	lv.AddCSSClass("spot-list")
	scrolled := gtk.NewScrolledWindow()
	scrolled.SetMinContentHeight(height)
	scrolled.SetChild(lv)
	scrolled.SetHExpand(true)
	scrolled.SetVExpand(true)
	scrolled.AddCSSClass("spot-list-scroll")
	// A page can be a grid of tiles instead; search results are always the list.
	grid := newMenuGrid(height)
	row := gtk.NewBox(gtk.OrientationVertical, 4)
	row.AddCSSClass("spot-item")
	row.Append(search)
	row.Append(scrolled)
	row.Append(grid.scrolled)
	root.Append(row)
	// Why a mod's rows are not there, or are old; or why a request failed.
	problem := gtk.NewLabel("")
	problem.AddCSSClass("spot-error")
	problem.SetXAlign(0)
	problem.SetWrap(true)
	problem.SetVisible(false)
	if cfg.Footer != "" {
		f := gtk.NewLabel(cfg.Footer)
		f.AddCSSClass("spot-footer")
		f.SetXAlign(0)
		root.Append(f)
	}
	root.Append(problem)
	d.root = root
	d.firstFocus = search

	end := func(r result) {
		if d.done {
			return
		}
		d.done = true
		d.finish(r)
	}
	d.cancel = func() { end(result{}) }

	say := func(text string) {
		problem.SetText(text)
		problem.SetVisible(text != "")
	}
	// show puts the rows for what is typed on screen, with row sel selected.
	var show func(int)
	// want asks for the rows of the page shown, once for each source.
	want := func(sel int) {
		p := cfg.Pages[m.page]
		if !p.Dynamic() {
			say("")
			return
		}
		at := source{p.From, p.Run, p.Parse}
		if f := m.sources[at]; f != nil {
			say(f.note)
			return
		}
		f := &fetched{}
		m.sources[at] = f
		say("")
		go func() {
			rows, note := scheduler.Fetch(at.from, at.run, at.parse, env, dir, mods)
			coreglib.IdleAdd(func() {
				f.rows, f.note, f.done = rows, note, true
				now := cfg.Pages[m.page]
				if d.done || (source{now.From, now.Run, now.Parse}) != at {
					return
				}
				say(f.note)
				show(sel)
			})
		}()
	}
	show = func(sel int) {
		m.query = search.Text()
		m.rows = m.find(m.query)
		texts := make([]string, len(m.rows))
		for i, r := range m.rows {
			texts[i] = r.text
		}
		if sel < 0 || sel >= len(texts) {
			sel = 0
		}
		cols := cfg.PageColumns(m.page)
		grid.on = cols > 1 && strings.TrimSpace(m.query) == ""
		scrolled.SetVisible(!grid.on)
		grid.scrolled.SetVisible(grid.on)
		if grid.on {
			model.Splice(0, model.NItems(), nil)
			grid.fill(texts, cols)
			grid.choose(sel)
		} else {
			grid.fill(nil, cols)
			model.Splice(0, model.NItems(), texts)
			if len(texts) > 0 {
				selection.SetSelected(uint(sel))
				lv.ScrollTo(uint(sel), gtk.ListScrollFocus|gtk.ListScrollSelect, nil)
			}
		}
		title := cfg.PageTitle(m.page)
		deeper := len(m.history) > 0 || back != nil
		plain.SetText(title)
		titled.SetText(title)
		plain.SetVisible(!deeper && title != "")
		header.SetVisible(deeper)
	}
	enter := func(page, picked string, sel int) {
		m.page, m.picked = page, picked
		search.SetText("")
		want(sel)
		show(sel)
	}
	goBack := func() {
		if d.done {
			return
		}
		if n := len(m.history); n > 0 {
			step := m.history[n-1]
			m.history = m.history[:n-1]
			enter(step.page, step.picked, step.row)
			return
		}
		if back == nil {
			return
		}
		if back.Cmd != "" {
			spawn(back.Cmd, withoutEnv(env, "SPOT_BACK"), dir)
		}
		end(result{code: protocol.ExitBack})
	}
	activate := func() {
		if d.done {
			return
		}
		if search.Text() != m.query { // typed faster than the search follows
			show(0)
		}
		i := int(selection.Selected())
		if grid.on {
			i = grid.at
		}
		if i < 0 || i >= len(m.rows) {
			return
		}
		r := m.rows[i]
		var given []string // a mod's row, for the command, in its environment
		if r.given != nil {
			p := cfg.Pages[r.page]
			if p.Then != "" {
				m.history = append(m.history, menuStep{m.page, i, m.picked})
				enter(p.Then, r.given.Key, 0)
				return
			}
			// Nothing from outside may pass for an option of the command.
			if strings.HasPrefix(r.given.Key, "-") && !p.AllowLeadingDash {
				say(fmt.Sprintf("%q starts with a dash and would be read as an option (allow_leading_dash = true if that is meant)", r.given.Key))
				return
			}
			if p.Call != "" {
				if mods == nil {
					say("mods are not available here")
					return
				}
				d.busy = true
				root.SetSensitive(false)
				go func(key string) {
					err := mods.Call(p.Call, []string{key})
					coreglib.IdleAdd(func() {
						d.busy = false
						root.SetSensitive(true)
						if d.done {
							return
						}
						if err != nil {
							say(err.Error())
							search.GrabFocus()
							return
						}
						end(result{submitted: true, then: thenNothing})
					})
				}(r.given.Key)
				return
			}
			r.entry = config.Entry{Exec: p.Exec, Terminal: p.Terminal, Hold: p.Hold}
			given = []string{"SPOT_VALUE=" + r.given.Key, "SPOT_LABEL=" + r.given.Label}
		}
		if r.entry.Page != "" {
			// Back leads up the pages it is really on, each with its row.
			at := m.page
			for _, next := range append(slices.Clone(r.below), r.entry.Page) {
				sel := entryTo(cfg, at, next)
				m.history = append(m.history, menuStep{page: at, row: sel})
				at = next
			}
			enter(at, "", 0)
			return
		}
		cmd, runEnv := menuCommand(cfg, path, r, env, given)
		// Close first: what runs may want the keyboard or the screen. One that
		// opens a dialog with a Back here gets the window handed over.
		then := thenNothing
		if r.entry.Back {
			then = thenDialog
		}
		end(result{submitted: true, then: then})
		launcher.SpawnDetached(cmd, runEnv, dir)
	}

	d.hook(btn, btn.ConnectClicked(goBack))
	d.hook(search, search.ConnectSearchChanged(func() {
		if search.Text() != m.query {
			show(0)
		}
	}))
	d.hook(lv, lv.ConnectActivate(func(pos uint) {
		selection.SetSelected(pos)
		activate()
	}))
	d.hook(grid.flow, grid.flow.ConnectChildActivated(func(child *gtk.FlowBoxChild) {
		grid.choose(child.Index())
		activate()
	}))
	d.hook(root, root.ConnectMap(func() {
		if d.saved == nil {
			d.focusFirst()
		}
	}))
	d.onKey = func(keyval uint, state gdk.ModifierType, focus gtk.Widgetter) bool {
		if d.busy {
			return false
		}
		switch {
		case keyval == gdk.KEY_Escape:
			end(result{})
		case keyval == gdk.KEY_Left && state&gdk.AltMask != 0:
			goBack()
		case keyval == gdk.KEY_BackSpace && state&(gdk.ShiftMask|gdk.ControlMask|gdk.AltMask) == 0 && search.Text() == "":
			goBack()
		case keyval == gdk.KEY_Return || keyval == gdk.KEY_KP_Enter:
			activate()
		case grid.on && grid.key(keyval, state, search.Text() == ""):
		case keyval == gdk.KEY_Up || keyval == gdk.KEY_KP_Up:
			moveSelection(selection, lv, -1)
		case keyval == gdk.KEY_Down || keyval == gdk.KEY_KP_Down:
			moveSelection(selection, lv, 1)
		case keyval == gdk.KEY_Page_Up:
			moveSelection(selection, lv, -10)
		case keyval == gdk.KEY_Page_Down:
			moveSelection(selection, lv, 10)
		default:
			return false
		}
		return true
	}
	want(0)
	show(0)
	return d
}

// entryTo is the row of page that leads to next.
func entryTo(cfg *config.Config, page, next string) int {
	return max(0, slices.IndexFunc(cfg.Pages[page].Entry, func(e config.Entry) bool { return e.Page == next }))
}

// maxFound limits the hits from deeper pages.
const maxFound = 200

// find is the rows for query: the entries of this page that match, best first,
// then those of the pages below it, shown with the way there.
func (m *menu) find(query string) []menuRow {
	page := m.cfg.Pages[m.page]
	here := page.Entry
	all, ready := m.here()
	if !ready {
		// Until a mod's rows are here nothing is shown: a written row at the top
		// would be what an early Enter picks.
		return nil
	}
	if strings.TrimSpace(query) == "" {
		return all
	}
	type hit struct {
		row   menuRow
		score int
	}
	var near, far []hit
	for _, r := range all {
		if s := match.SmartScore(r.text, query); s >= 0 {
			near = append(near, hit{r, s})
		}
	}
	// Breadth first, so that of equal hits the nearest comes first. A page
	// reached twice, or that leads back, is looked at once.
	type place struct {
		page string
		way  []string // after the page shown, this one included
	}
	seen := map[string]bool{m.page: true}
	for _, h := range m.history {
		seen[h.page] = true // what is above is not below
	}
	var queue []place
	// A page reached by picking a mod's row (then) means nothing without that
	// row, so search follows written entries only.
	follow := func(entries []config.Entry, way []string) {
		for _, e := range entries {
			if e.Page != "" && !seen[e.Page] {
				seen[e.Page] = true
				queue = append(queue, place{e.Page, append(slices.Clone(way), e.Page)})
			}
		}
	}
	follow(here, nil)
	for i := 0; i < len(queue); i++ {
		p := queue[i]
		var crumbs []string
		for _, name := range p.way {
			crumbs = append(crumbs, config.WithoutIcon(m.cfg.PageTitle(name)))
		}
		for _, e := range m.cfg.Pages[p.page].Entry {
			name := config.WithoutIcon(e.Label)
			// A hit on the entry's own name beats one that needs the way too.
			score := match.SmartScore(name, query)
			if score >= 0 {
				score += 10000
			} else if score = match.SmartScore(strings.Join(crumbs, " ")+" "+name, query); score < 0 {
				continue
			}
			text := iconOf(e.Label) + strings.Join(append(slices.Clone(crumbs), name), " › ")
			far = append(far, hit{menuRow{text: text, entry: e, page: p.page, below: p.way}, score})
		}
		follow(m.cfg.Pages[p.page].Entry, p.way)
	}
	var rows []menuRow
	for _, hits := range [][]hit{near, far} {
		sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
		for _, h := range hits {
			rows = append(rows, h.row)
		}
		if len(rows) > maxFound {
			return rows[:maxFound]
		}
	}
	return rows
}

// here is every row of the page shown; ready is false while a mod's are fetched.
func (m *menu) here() (rows []menuRow, ready bool) {
	p := m.cfg.Pages[m.page]
	if p.Dynamic() {
		f := m.sources[source{p.From, p.Run, p.Parse}]
		if f == nil || !f.done {
			return nil, false
		}
		for i, r := range f.rows {
			if p.Group && m.picked != "" && r.Group != m.picked {
				continue
			}
			rows = append(rows, menuRow{text: r.Label, page: m.page, given: &f.rows[i]})
		}
	}
	for _, e := range p.Entry {
		rows = append(rows, menuRow{text: e.Label, entry: e, page: m.page})
	}
	return rows, true
}

// iconOf is what WithoutIcon takes off a label.
func iconOf(label string) string {
	return label[:max(0, len(label)-len(config.WithoutIcon(label)))]
}

// menuCommand is the shell command an entry runs, and its environment. Nothing
// but what the config says becomes part of the command: given, the row of a mod
// as NAME=value, is only ever in the environment.
func menuCommand(cfg *config.Config, path string, r menuRow, env, given []string) (string, []string) {
	env = withoutEnv(env, "SPOT_BACK")
	// What the caller inherited from another dialog is not this row's.
	env = slices.DeleteFunc(env, func(kv string) bool {
		return strings.HasPrefix(kv, "SPOT_VALUE") || strings.HasPrefix(kv, "SPOT_LABEL=")
	})
	env = append(env, given...)
	e := r.entry
	switch {
	case e.Exec != "":
		if e.Back {
			self, err := os.Executable()
			if err != nil || !utf8.ValidString(self) {
				self = "spot"
			}
			env = append(env, "SPOT_BACK="+shellQuote(self)+" --page "+shellQuote(r.page)+" "+shellQuote(path))
		}
		return e.Exec, env
	case e.Terminal != "":
		terminal := "xdg-terminal-exec"
		if cfg.Menu != nil && cfg.Menu.Terminal != "" {
			terminal = cfg.Menu.Terminal
		}
		inside := e.Terminal
		if e.Hold {
			inside += "\nprintf '\\nPress Enter to close.'; read -r _"
		}
		// A terminal may not pass its caller's environment on (one started by a
		// session service does not), so the row is set inside it too.
		run := "sh -c "
		if len(given) > 0 {
			run = "env "
			for _, kv := range given {
				run += shellQuote(kv) + " "
			}
			run += "sh -c "
		}
		return terminal + " " + run + shellQuote(inside), env
	default:
		return "xdg-open " + shellQuote(e.Open), env
	}
}

// shellQuote makes s one word of a shell command.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
