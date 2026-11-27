package config

import (
	"fmt"
	"strings"
	"time"
)

// validateLive checks the keys of the controls that apply at once.
func (it *Item) validateLive() error {
	fail := func(format string, args ...any) error {
		return fmt.Errorf("item %q: "+format, append([]any{it.ID}, args...)...)
	}
	switch it.Type {
	case "switch":
		if it.ID == "" {
			return fail("a switch needs an id")
		}
		switch it.Look {
		case "", "switch", "button", "checkbox":
		default:
			return fail("look: unknown %q (want \"switch\", \"button\" or \"checkbox\")", it.Look)
		}
	case "segmented":
		if it.ID == "" || len(it.Options) == 0 {
			return fail("a segmented control needs an id and options")
		}
		if len(it.Labels) != 0 && len(it.Labels) != len(it.Options) {
			return fail("labels must be as many as options")
		}
	case "button":
		if it.Exec == "" && it.Call == "" || it.Label == "" {
			return fail("a button needs a label and exec or call")
		}
	}
	if it.Switches && (it.Type != "list" || it.Multi || it.Collapsible) {
		return fail("switches only applies to a plain list (not multi or collapsible)")
	}
	live := it.Type == "switch" || it.Type == "segmented" || it.Type == "scale" || it.Switches
	if !live && (it.State != "" || it.OnChange != "") {
		return fail("state and on_change only apply to switch, segmented, scale and lists with switches")
	}
	if it.Look != "" && it.Type != "switch" {
		return fail("look only applies to a switch")
	}
	if it.Type != "button" && (it.Exec != "" || it.Close) {
		return fail("exec and close only apply to a button")
	}
	if it.Timeout < 0 {
		return fail("timeout must not be negative")
	}
	return nil
}

// CommandTimeout bounds a control's command, so that a hanging one cannot leave
// the control busy for ever.
func (it *Item) CommandTimeout() time.Duration {
	if it.Timeout == 0 {
		return 10 * time.Second
	}
	return time.Duration(it.Timeout) * time.Millisecond
}

// ParseSwitch reads what a state command printed: on, off, or neither (ok is
// false). Besides the item's own on / off values the usual words count.
func (it *Item) ParseSwitch(out string) (on, ok bool) {
	v := LastLine(out)
	switch {
	case v == "":
		return false, false
	case v == it.OnValue():
		return true, true
	case v == it.OffValue():
		return false, true
	}
	switch strings.ToLower(v) {
	case "on", "true", "1", "yes":
		return true, true
	case "off", "false", "0", "no":
		return false, true
	}
	return false, false
}

// LastLine is the last non-empty line of a command's output, trimmed.
func LastLine(out string) string {
	out = strings.TrimSpace(out)
	return strings.TrimSpace(out[strings.LastIndexByte(out, '\n')+1:])
}
