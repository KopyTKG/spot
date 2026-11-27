// Package scheduler decides which dialog is on screen, covered or waiting. It
// knows nothing about GTK: a Presenter does the drawing.
package scheduler

import (
	"slices"

	"gitlab.com/KopyTKG/spot/internal/config"
	"gitlab.com/KopyTKG/spot/internal/protocol"
)

// Job is one request on its way through the daemon.
type Job struct {
	req   protocol.Request
	Cfg   *config.Config
	CSS   string // absolute path of the stylesheet, or ""
	Env   []string
	Dir   string
	Back  *config.BackSpec
	Mods  config.ModSource       // for from, call and parse
	Reply chan protocol.Response // buffered: the scheduler never blocks on a slow client
	View  any                    // the presenter\'s widgets, set while the dialog exists

	finished bool
}

// presenter is the window as the scheduler sees it.
type presenter interface {
	Present(j *Job) // show j's dialog (building it) and map the window if needed
	Suspend(j *Job) // j goes under a --sub dialog: keep its state, stop showing it
	Resume(j *Job)  // j is back on top
	Retire(j *Job)  // j is done: stop it accepting input; the window keeps showing it until replaced
	Conceal()       // hide the content, keep the surface
	Unmap()         // release the surface
}

// timers schedules the grace periods.
type timers interface {
	After(ms int, f func()) (cancel func())
}

type Scheduler struct {
	P presenter
	T timers

	Cur   *Job
	stack []*Job
	queue []*Job

	Grace, Chain int // ms: graceMs / chainMs, or what daemon.toml says

	cancelGrace func()
	quitting    bool   // opQuit received: leave once idle
	Oneshot     bool   // a single-dialog run: leave as soon as it is over
	OnIdle      func() // called when a quitting/oneshot scheduler runs out of work
}

// maxWaiting is how many dialogs may wait their turn or lie covered. Each holds
// its whole request.
const maxWaiting = 32

// Enqueue takes a new request.
func (s *Scheduler) Enqueue(j *Job) {
	if s.toggled(j) {
		return
	}
	if len(s.queue)+len(s.stack) >= maxWaiting {
		j.finished = true
		j.Reply <- protocol.Response{Code: protocol.ExitError, Stderr: "spot: too many dialogs are already waiting\n", Fault: true, Version: protocol.Version}
		return
	}
	s.stopGrace()
	switch {
	case s.Cur == nil:
		s.show(j)
	case j.req.Sub:
		s.P.Suspend(s.Cur)
		s.stack = append(s.stack, s.Cur)
		s.show(j)
	default:
		s.queue = append(s.queue, j)
	}
}

// toggled handles a request for a config with toggle = true that is already
// showing or waiting (a keybind pressed again, or held down). Asking for the
// dialog that is on screen closes it, like a toggle key; asking for one that is
// already queued does nothing. Either way the repeat is answered as cancelled
// (status 1) and never queues, so mashing the key cannot pile up windows to
// close one by one. A --sub request is a deliberate nesting and is left alone.
func (s *Scheduler) toggled(j *Job) bool {
	if j.Cfg == nil || !j.Cfg.Toggle || j.req.Sub {
		return false
	}
	same := func(x *Job) bool {
		return x != nil && x != j && x.Cfg != nil && x.Cfg.Toggle && x.req.Config == j.req.Config
	}
	cancelled := protocol.Response{Code: protocol.ExitCancelled, Version: protocol.Version}
	switch {
	case same(s.Cur):
		j.finished = true
		j.Reply <- cancelled
		s.Complete(s.Cur, cancelled) // the key pressed again: close it
		return true
	case slices.ContainsFunc(s.queue, same):
		j.finished = true
		j.Reply <- cancelled
		return true
	}
	return false
}

func (s *Scheduler) show(j *Job) {
	s.Cur = j
	s.P.Present(j)
}

// Complete ends j with r. It is safe to call for any job in any state and
// more than once.
func (s *Scheduler) Complete(j *Job, r protocol.Response) {
	if j.finished {
		return
	}
	j.finished = true
	j.Reply <- r
	s.P.Retire(j)

	switch {
	case j != s.Cur: // a waiting or covered dialog ended early (client gone)
		s.stack = slices.DeleteFunc(s.stack, func(x *Job) bool { return x == j })
		s.queue = slices.DeleteFunc(s.queue, func(x *Job) bool { return x == j })
	case len(s.stack) > 0:
		parent := s.stack[len(s.stack)-1]
		s.stack = s.stack[:len(s.stack)-1]
		s.Cur = parent
		s.P.Resume(parent)
	case len(s.queue) > 0:
		next := s.queue[0]
		s.queue = s.queue[1:]
		s.show(next)
	default:
		s.Cur = nil
		s.idle(j, r)
	}
}

// idle runs when the last dialog has closed and nothing is waiting. A chain is
// only expected after an answer: if the dialog was cancelled the caller is
// done, so the window gets the ordinary short grace instead. Back always
// expects a follow-up (the dialog it returns to is being started), so it holds
// the window like --chain does.
func (s *Scheduler) idle(last *Job, r protocol.Response) {
	switch {
	case s.Oneshot || s.quitting:
		s.OnIdle()
	case last.req.Last && r.Code != protocol.ExitBack:
		s.P.Unmap()
	case last.req.Chain && r.Code == protocol.ExitSubmitted, r.Code == protocol.ExitBack:
		s.hold(s.Chain)
	default:
		s.P.Conceal()
		s.hold(s.Grace)
	}
}

// hold keeps the surface for ms, then releases it. Once the timer has fired
// its cancel function must not be used again: GLib may have handed the source
// id to something else by then.
func (s *Scheduler) hold(ms int) {
	s.cancelGrace = s.T.After(ms, func() {
		s.cancelGrace = nil
		s.P.Unmap()
	})
}

func (s *Scheduler) stopGrace() {
	if s.cancelGrace != nil {
		s.cancelGrace()
		s.cancelGrace = nil
	}
}

// DismissAll cancels the dialog on screen, everything under it and
// everything waiting, and releases the window at once, also when it was only
// being held open for a --chain that never came.
func (s *Scheduler) DismissAll() {
	cancelled := protocol.Response{Code: protocol.ExitCancelled, Version: protocol.Version}
	waiting := slices.Concat(s.queue, s.stack)
	s.queue, s.stack = nil, nil
	for _, j := range waiting {
		s.Complete(j, cancelled)
	}
	if s.Cur != nil {
		s.Complete(s.Cur, cancelled)
	}
	s.stopGrace()
	s.P.Unmap()
}

// Quit leaves once the current work is done.
func (s *Scheduler) Quit() {
	s.quitting = true
	if s.Cur == nil {
		s.stopGrace()
		s.OnIdle()
	}
}
