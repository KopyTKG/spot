package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/diamondburned/gotk4-layer-shell/pkg/gtk4layershell"
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
}

type Item struct {
	Type        string   // field | password | select
	ID          string
	Label       string
	Placeholder string
	Default     string
	Options     []string // select only
}

type Submit struct {
	Exec  string // command template; {id} substituted from field values
	Spawn bool   // true = fire-and-forget + close window
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

	// Per-config app ID so multiple `spot <foo.toml>` invocations don't dedupe.
	appID := fmt.Sprintf("dev.kopy.spot.%s",
		strings.TrimSuffix(filepath.Base(cfgPath), filepath.Ext(cfgPath)))
	app := gtk.NewApplication(appID, gio.ApplicationFlagsNone)
	app.ConnectActivate(func() { activate(app, &cfg, cssPath) })
	if code := app.Run(nil); code > 0 {
		os.Exit(code)
	}
}

func activate(app *gtk.Application, cfg *Config, cssPath string) {
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
	if cfg.Width > 0 || cfg.Height > 0 {
		w := cfg.Width
		if w == 0 {
			w = -1
		}
		h := cfg.Height
		if h == 0 {
			h = -1
		}
		win.SetDefaultSize(w, h)
	}

	gtk4layershell.InitForWindow(&win.Window)
	gtk4layershell.SetLayer(&win.Window, gtk4layershell.LayerShellLayerTop)
	applyAnchor(&win.Window, cfg.Anchor)
	applyKeyboard(&win.Window, cfg.Keyboard)

	root := gtk.NewBox(gtk.OrientationVertical, 12)
	root.AddCSSClass("spot-root")

	if cfg.Title != "" {
		t := gtk.NewLabel(cfg.Title)
		t.AddCSSClass("spot-title")
		t.SetXAlign(0)
		root.Append(t)
	}

	// id -> getter
	values := map[string]func() string{}
	var firstFocus gtk.Widgetter

	for _, it := range cfg.Items {
		item := it // capture per-iter
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
			// Note: GtkPasswordEntry has no direct SetPlaceholderText API; setting
			// it via "placeholder-text" property if needed comes later.
			row.Append(entry)
			values[item.ID] = func() string { return entry.Text() }
			if firstFocus == nil {
				firstFocus = entry
			}
		case "select":
			sl := gtk.NewStringList(item.Options)
			dd := gtk.NewDropDown(sl, nil)
			dd.AddCSSClass("spot-select")
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

	submit := func() {
		if cfg.Submit.Exec == "" {
			win.Close()
			return
		}
		cmd := cfg.Submit.Exec
		for id, get := range values {
			cmd = strings.ReplaceAll(cmd, "{"+id+"}", get())
		}
		spawn(cmd)
		win.Close()
	}

	keys := gtk.NewEventControllerKey()
	keys.ConnectKeyPressed(func(keyval, _ uint, _ gdk.ModifierType) bool {
		switch keyval {
		case gdk.KEY_Escape:
			win.Close()
			return true
		case gdk.KEY_Return, gdk.KEY_KP_Enter:
			submit()
			return true
		}
		return false
	})
	win.AddController(keys)

	win.Show()
	if firstFocus != nil {
		firstFocus.(interface{ GrabFocus() bool }).GrabFocus()
	}
}

func applyAnchor(win *gtk.Window, anchor string) {
	a := strings.ToLower(strings.TrimSpace(anchor))
	if a == "" || a == "center" {
		return // no anchor = centered by layer-shell
	}
	for _, part := range strings.Fields(a) {
		switch part {
		case "top":
			gtk4layershell.SetAnchor(win, gtk4layershell.LayerShellEdgeTop, true)
		case "bottom":
			gtk4layershell.SetAnchor(win, gtk4layershell.LayerShellEdgeBottom, true)
		case "left":
			gtk4layershell.SetAnchor(win, gtk4layershell.LayerShellEdgeLeft, true)
		case "right":
			gtk4layershell.SetAnchor(win, gtk4layershell.LayerShellEdgeRight, true)
		}
	}
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
