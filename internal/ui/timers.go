package ui

import (
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
)

// glibTimers runs grace periods on the GLib main loop.
type glibTimers struct{}

func (glibTimers) After(ms int, f func()) func() {
	h := coreglib.TimeoutAdd(uint(ms), func() bool {
		f()
		return false
	})
	return func() { coreglib.SourceRemove(h) }
}
