package ui

import (
	"fmt"
	"log"
	"strings"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"gitlab.com/KopyTKG/spot/internal/config"
	"gitlab.com/KopyTKG/spot/internal/launcher"
)

// submitter returns what Enter does once the key is not wanted by an item:
// validate, run what the config asks for, and end the dialog.
func (b *builder) submitter(root *gtk.Box, execErr *gtk.Label, end func(result)) func() {
	d := b.d
	setBusy := func(b bool) {
		d.busy = b
		root.SetSensitive(!b)
	}

	submit := func() {
		if d.done || d.busy {
			return
		}
		vals := make(map[string]string, len(b.values))
		for id, get := range b.values {
			vals[id] = get()
		}

		// Invalid items block submission; the first one takes focus.
		var firstBad *check
		for _, ck := range b.checks {
			ok := ck.item.Valid(vals[ck.item.ID])
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

		// An apps list with no [submit] of its own launches the selected app.
		if b.appsRes != nil && b.cfg.Submit.Exec == "" && b.cfg.Submit.Stdout == "" && b.cfg.Submit.Format == "" {
			if b.appsRes.Src != nil { // a source row: run its activate command
				row := b.appsRes.PickedRow()
				if row == nil {
					return
				}
				src := b.appsRes.Src
				if src.Activate == "" {
					fmt.Fprintln(&d.out, row.Value)
				} else {
					senv := append(append([]string(nil), b.env...), "SPOT_VALUE="+row.Value, "SPOT_LABEL="+row.Label, "SPOT_QUERY="+b.appsRes.SrcQuery)
					launcher.SpawnDetached(launcher.ExpandSource(src.Activate, *row, b.appsRes.SrcQuery), senv, b.dir)
				}
				end(result{submitted: true})
				return
			}
			if e := b.appsRes.EntryByID(vals[b.appsID]); e != nil {
				b.appsRes.Launch(e, b.env, b.dir)
				end(result{submitted: true})
			}
			return
		}

		// What happens once any exec command has succeeded.
		finish := func() {
			if b.cfg.Submit.Stdout != "" {
				fmt.Fprintln(&d.out, config.Expand(b.cfg.Submit.Stdout, b.activeID, vals))
			}
			if b.cfg.Submit.Format == "json" {
				multi := map[string][]string{}
				for id, get := range b.multiValues {
					multi[id] = get()
				}
				js, err := config.BuildJSON(b.cfg.Items, vals, multi, b.activeID)
				if err != nil {
					log.Printf("json: %v", err)
				} else {
					fmt.Fprintln(&d.out, js)
				}
			}
			end(result{submitted: true})
		}

		if b.cfg.Submit.Exec == "" {
			finish()
			return
		}
		cmd := config.Expand(b.cfg.Submit.Exec, b.activeID, vals)
		if b.cfg.Submit.Spawn {
			spawn(cmd, b.env, b.dir)
			finish()
			return
		}
		// Run off the main loop. The daemon serves other dialogs meanwhile, and
		// a command that itself calls `spot` (a sub-dialog) would otherwise
		// wait on a loop that is waiting on it.
		setBusy(true)
		go func() {
			stdout, stderr, status := runCommand(cmd, b.env, b.dir)
			coreglib.IdleAdd(func() {
				setBusy(false)
				if d.done {
					return
				}
				d.out.WriteString(stdout)
				if status != 0 {
					if b.cfg.Submit.OnError == "exit" {
						d.errOut.WriteString(stderr)
						end(result{code: status})
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
				finish()
			})
		}()
	}
	return submit
}

// check ties an item's required / pattern rules to its widget.
type check struct {
	item   config.Item
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
