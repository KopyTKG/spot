package ui

import "github.com/diamondburned/gotk4/pkg/gtk/v4"

// slider moves a switch from code the way a click does. GTK slides a switch only
// when it is activated (SetActive jumps), reports the new position when it
// arrives, and finishes a slide once begun: so the wanted position is kept, and a
// switch that arrives somewhere else is sent on.
type slider struct {
	sw     *gtk.Switch
	want   bool
	moving bool // activated by us, not arrived yet
}

// to sends the switch to on.
func (s *slider) to(on bool) {
	s.want = on
	switch {
	case s.moving || s.sw.Active() == on:
	case s.sw.Mapped():
		s.moving = true
		s.sw.Activate()
	default:
		s.sw.SetActive(on) // not on screen: nothing to animate
	}
}

// arrived is for the switch's notify::active. It reports whether the change was
// ours, so that it is not taken for the user's.
func (s *slider) arrived() bool {
	if !s.moving {
		return false
	}
	s.moving = false
	s.to(s.want)
	return true
}
