package ui

import (
	"log"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"gitlab.com/KopyTKG/spot/internal/config"
	"gitlab.com/KopyTKG/spot/internal/launcher"
)

// builder holds what the items of one dialog share while it is built and used.
type builder struct {
	d     *dialog
	cfg   *config.Config
	env   []string // the caller's environment and directory, for commands
	dir   string
	envOf func(string) string
	mods  config.ModSource // for from and call

	values      map[string]func() string   // each item's value, by id
	multiValues map[string]func() []string // multi-select lists: the picked rows
	checks      []*check                   // items with required / pattern rules
	firstFocus  gtk.Widgetter

	// for the key handler
	listSelection *gtk.SingleSelection // single-select list
	listView      *gtk.ListView        // for ScrollTo on key nav
	multiSearch   *gtk.SearchEntry     // multi-select list: Up/Down move focus into the list
	multiView     *gtk.ListView
	nav           *tileNav             // keyboard cursor across all tile sections
	textViews     []*gtk.TextView      // Enter inserts a newline in these; Ctrl+Enter submits
	groupToggle   func(delta int) bool // collapsible list: fold/unfold the selected group; false = not handled

	activeID string            // id of the last tiles group interacted with ({_active})
	appsRes  *launcher.Results // type = "apps": the index the list shows
	appsID   string            // id of that item
	submit   func()            // set once every item is built; tile and list clicks call it
	end      func(result)      // set with it; a closing button calls it

	// live controls (see live.go)
	refreshers  []func()    // re-read each state
	errLabel    *gtk.Label  // where a failed command is reported
	rowActivate func() bool // list with switches: flip the selected row; false = not a switch
}

// focus makes w the widget the dialog starts on, unless an earlier one is.
func (b *builder) focus(w gtk.Widgetter) {
	if b.firstFocus == nil {
		b.firstFocus = w
	}
}

// item builds one [[items]] entry as a row: its label, widget and error text.
// An unknown type gives nil.
func (b *builder) item(item config.Item) *gtk.Box {
	row := gtk.NewBox(gtk.OrientationVertical, 4)
	row.AddCSSClass("spot-item")
	if item.Label != "" && item.Type != "toggle" && item.Type != "switch" && item.Type != "button" {
		lbl := gtk.NewLabel(item.Label)
		lbl.AddCSSClass("spot-label")
		lbl.SetXAlign(0)
		row.Append(lbl)
	}
	var input gtk.Widgetter // the widget .spot-invalid is put on and focus goes to
	var onEdit func(func()) // registers a callback for edits (clears .spot-invalid)
	switch item.Type {
	case "field":
		input, onEdit = b.field(item, row)
	case "password":
		input, onEdit = b.password(item, row)
	case "text":
		input, onEdit = b.text(item, row)
	case "select":
		input = b.selectOne(item, row)
	case "list", "apps":
		input = b.list(item, row)
	case "tiles":
		b.tiles(item, row)
	case "toggle":
		b.toggle(item, row)
	case "scale":
		b.scale(item, row)
	case "switch":
		b.switchItem(item, row)
	case "segmented":
		b.segmented(item, row)
	case "button":
		b.button(item, row)
	case "separator":
		b.separator(row)
	default:
		log.Printf("unknown item type: %q (skipping)", item.Type)
		return nil
	}
	if item.Required || item.HasPattern() {
		if _, ok := b.values[item.ID]; ok {
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
			b.checks = append(b.checks, ck)
		}
	}
	return row
}
