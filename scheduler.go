package main

import "slices"

// The daemon owns one window and shows one dialog in it at a time. The
// scheduler decides which: it knows nothing about GTK, so it can be tested
// without a display. A presenter does the drawing.
//
//	cur    the dialog on screen
//	stack  dialogs under cur, opened with --sub; the top one returns when cur closes
//	queue  dialogs waiting their turn (plain requests that arrived while cur was up)
//
// Whenever the next dialog is known the window's content is swapped in place:
// the layer surface stays mapped, so nothing re-maps and no fade restarts.
// When nothing is known yet the window lingers briefly, in case the caller is
// a shell script about to ask for the next menu (see idle).

const (
	// Defaults; daemon.toml can change both (grace, chain).
	//
	// graceMs: after a dialog closes the surface stays mapped, with the dialog
	// hidden, this long. A follow-up request inside it takes over the surface;
	// otherwise the surface is released.
	graceMs = 120
	// chainMs: with --chain the closed dialog stays visible instead, for up to
	// this long, so a menu hands over to the next without a gap.
	chainMs = 1500
)

// job is one request on its way through the daemon.
type job struct {
	req   request
	cfg   *Config
	css   string // absolute path of the stylesheet, or ""
	env   []string
	dir   string
	back  *backSpec
	reply chan response // buffered: the scheduler never blocks on a slow client
	dlg   *dialog       // set while the dialog exists

	finished bool
}

// presenter is the window as the scheduler sees it.
type presenter interface {
	present(j *job) // show j's dialog (building it) and map the window if needed
	suspend(j *job) // j goes under a --sub dialog: keep its state, stop showing it
	resume(j *job)  // j is back on top
	retire(j *job)  // j is done: stop it accepting input; the window keeps showing it until replaced
	conceal()       // hide the content, keep the surface
	unmap()         // release the surface
}

// timers schedules the grace periods.
type timers interface {
	after(ms int, f func()) (cancel func())
}

type scheduler struct {
	p presenter
	t timers

	cur   *job
	stack []*job
	queue []*job

	grace, chain int // ms: graceMs / chainMs, or what daemon.toml says

	cancelGrace func()
	quitting    bool   // opQuit received: leave once idle
	oneshot     bool   // a single-dialog run: leave as soon as it is over
	onIdle      func() // called when a quitting/oneshot scheduler runs out of work
}

// enqueue takes a new request.
func (s *scheduler) enqueue(j *job) {
	s.stopGrace()
	switch {
	case s.cur == nil:
		s.show(j)
	case j.req.Sub:
		s.p.suspend(s.cur)
		s.stack = append(s.stack, s.cur)
		s.show(j)
	default:
		s.queue = append(s.queue, j)
	}
}

func (s *scheduler) show(j *job) {
	s.cur = j
	s.p.present(j)
}

// complete ends j with r. It is safe to call for any job in any state and
// more than once.
func (s *scheduler) complete(j *job, r response) {
	if j.finished {
		return
	}
	j.finished = true
	j.reply <- r
	s.p.retire(j)

	switch {
	case j != s.cur: // a waiting or covered dialog ended early (client gone)
		s.stack = slices.DeleteFunc(s.stack, func(x *job) bool { return x == j })
		s.queue = slices.DeleteFunc(s.queue, func(x *job) bool { return x == j })
	case len(s.stack) > 0:
		parent := s.stack[len(s.stack)-1]
		s.stack = s.stack[:len(s.stack)-1]
		s.cur = parent
		s.p.resume(parent)
	case len(s.queue) > 0:
		next := s.queue[0]
		s.queue = s.queue[1:]
		s.show(next)
	default:
		s.cur = nil
		s.idle(j, r)
	}
}

// idle runs when the last dialog has closed and nothing is waiting. A chain is
// only expected after an answer: if the dialog was cancelled the caller is
// done, so the window gets the ordinary short grace instead. Back always
// expects a follow-up (the dialog it returns to is being started), so it holds
// the window like --chain does.
func (s *scheduler) idle(last *job, r response) {
	switch {
	case s.oneshot || s.quitting:
		s.onIdle()
	case last.req.Last && r.Code != exitBack:
		s.p.unmap()
	case last.req.Chain && r.Code == exitSubmitted, r.Code == exitBack:
		s.hold(s.chain)
	default:
		s.p.conceal()
		s.hold(s.grace)
	}
}

// hold keeps the surface for ms, then releases it. Once the timer has fired
// its cancel function must not be used again: GLib may have handed the source
// id to something else by then.
func (s *scheduler) hold(ms int) {
	s.cancelGrace = s.t.after(ms, func() {
		s.cancelGrace = nil
		s.p.unmap()
	})
}

func (s *scheduler) stopGrace() {
	if s.cancelGrace != nil {
		s.cancelGrace()
		s.cancelGrace = nil
	}
}

// dismissAll cancels the dialog on screen, everything under it and
// everything waiting, and releases the window at once, also when it was only
// being held open for a --chain that never came.
func (s *scheduler) dismissAll() {
	cancelled := response{Code: exitCancelled, Version: version}
	waiting := slices.Concat(s.queue, s.stack)
	s.queue, s.stack = nil, nil
	for _, j := range waiting {
		s.complete(j, cancelled)
	}
	if s.cur != nil {
		s.complete(s.cur, cancelled)
	}
	s.stopGrace()
	s.p.unmap()
}

// quit leaves once the current work is done.
func (s *scheduler) quit() {
	s.quitting = true
	if s.cur == nil {
		s.stopGrace()
		s.onIdle()
	}
}
