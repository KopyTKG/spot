package scheduler

import (
	"reflect"
	"strings"
	"testing"

	"gitlab.com/KopyTKG/spot/internal/config"
	"gitlab.com/KopyTKG/spot/internal/protocol"
)

// fakePresenter records what the scheduler asks of the window.
type fakePresenter struct{ log []string }

func (f *fakePresenter) rec(op string, j *Job) {
	name := ""
	if j != nil {
		name = ":" + j.req.Config
	}
	f.log = append(f.log, op+name)
}

func (f *fakePresenter) Present(j *Job) { f.rec("present", j) }

func (f *fakePresenter) Suspend(j *Job) { f.rec("suspend", j) }

func (f *fakePresenter) Resume(j *Job) { f.rec("resume", j) }

func (f *fakePresenter) Retire(j *Job) { f.rec("retire", j) }

func (f *fakePresenter) Conceal() { f.rec("conceal", nil) }

func (f *fakePresenter) Unmap() { f.rec("unmap", nil) }

// fakeTimers holds timers until the test fires them.
type fakeTimers struct {
	pending map[int]func()
	next    int
	ms      map[int]int

	fired       map[int]bool
	staleCancel int // cancels of a timer that had already fired
}

func (t *fakeTimers) After(ms int, f func()) func() {
	if t.pending == nil {
		t.pending, t.ms = map[int]func(){}, map[int]int{}
	}
	t.next++
	id := t.next
	t.pending[id], t.ms[id] = f, ms
	return func() {
		if t.fired[id] {
			t.staleCancel++
		}
		delete(t.pending, id)
	}
}

// fire runs every timer that is still pending.
func (t *fakeTimers) fire() {
	if t.fired == nil {
		t.fired = map[int]bool{}
	}
	for id, f := range t.pending {
		delete(t.pending, id)
		t.fired[id] = true
		f()
	}
}

func newTestScheduler() (*Scheduler, *fakePresenter, *fakeTimers) {
	p, t := &fakePresenter{}, &fakeTimers{}
	return &Scheduler{P: p, T: t, Grace: config.DefaultGraceMs, Chain: config.DefaultChainMs, OnIdle: func() { p.log = append(p.log, "idle") }}, p, t
}

func mkJob(name string, mod ...func(*protocol.Request)) *Job {
	j := &Job{req: protocol.Request{Op: protocol.OpShow, Config: name}, Reply: make(chan protocol.Response, 1)}
	for _, m := range mod {
		m(&j.req)
	}
	return j
}

func sub(r *protocol.Request) { r.Sub = true }

func chain(r *protocol.Request) { r.Chain = true }

func last(r *protocol.Request) { r.Last = true }

func (f *fakePresenter) take() string {
	s := strings.Join(f.log, " ")
	f.log = nil
	return s
}

func TestShowsImmediatelyWhenIdle(t *testing.T) {
	s, p, _ := newTestScheduler()
	s.Enqueue(mkJob("a"))
	if got := p.take(); got != "present:a" {
		t.Fatalf("got %q", got)
	}
}

func TestQueuedRequestsRunInOrderWithoutReleasingTheSurface(t *testing.T) {
	s, p, tm := newTestScheduler()
	a, b, c := mkJob("a"), mkJob("b"), mkJob("c")
	s.Enqueue(a)
	s.Enqueue(b)
	s.Enqueue(c)
	p.take()

	s.Complete(a, protocol.Response{Code: 0})
	if got := p.take(); got != "retire:a present:b" {
		t.Fatalf("a done: got %q", got)
	}
	s.Complete(b, protocol.Response{})
	if got := p.take(); got != "retire:b present:c" {
		t.Fatalf("b done: got %q", got)
	}
	// No conceal/unmap while there was a next dialog: the surface was never released.
	s.Complete(c, protocol.Response{})
	if got := p.take(); got != "retire:c conceal" {
		t.Fatalf("c done: got %q", got)
	}
	if len(tm.pending) != 1 {
		t.Fatalf("want one grace timer, got %d", len(tm.pending))
	}
}

func TestFollowUpInsideGraceKeepsTheSurface(t *testing.T) {
	s, p, tm := newTestScheduler()
	a := mkJob("a")
	s.Enqueue(a)
	s.Complete(a, protocol.Response{})
	p.take() // retire:a conceal

	s.Enqueue(mkJob("b")) // the next menu step arrives inside the grace period
	if got := p.take(); got != "present:b" {
		t.Fatalf("got %q", got)
	}
	tm.fire() // the old grace timer must be dead
	if got := p.take(); got != "" {
		t.Fatalf("surface released although b is showing: %q", got)
	}
}

func TestSurfaceReleasedWhenGraceRunsOut(t *testing.T) {
	s, p, tm := newTestScheduler()
	a := mkJob("a")
	s.Enqueue(a)
	s.Complete(a, protocol.Response{})
	p.take()
	tm.fire()
	if got := p.take(); got != "unmap" {
		t.Fatalf("got %q", got)
	}
}

func TestFiredTimerIsNeverCancelled(t *testing.T) {
	s, p, tm := newTestScheduler()
	a := mkJob("a")
	s.Enqueue(a)
	s.Complete(a, protocol.Response{})
	tm.fire() // grace ran out, surface released
	p.take()
	s.Enqueue(mkJob("b")) // would cancel the dead timer
	s.DismissAll()        // and so would this
	s.Quit()
	if tm.staleCancel != 0 {
		t.Fatalf("cancelled an already-fired timer %d time(s): GLib would complain, or hit a reused source id", tm.staleCancel)
	}
}

func TestGraceLengths(t *testing.T) {
	cases := []struct {
		name string
		mod  []func(*protocol.Request)
		want []string // after completing: presenter log
		ms   int
	}{
		{"default conceals then waits briefly", nil, []string{"retire:a", "conceal"}, config.DefaultGraceMs},
		{"chain keeps content up and waits longer", []func(*protocol.Request){chain}, []string{"retire:a"}, config.DefaultChainMs},
	}
	for _, tc := range cases {
		s, p, tm := newTestScheduler()
		a := mkJob("a", tc.mod...)
		s.Enqueue(a)
		p.take()
		s.Complete(a, protocol.Response{})
		if got := p.log; !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: log %v, want %v", tc.name, got, tc.want)
		}
		for _, ms := range tm.ms {
			if ms != tc.ms {
				t.Errorf("%s: grace %dms, want %dms", tc.name, ms, tc.ms)
			}
		}
	}
}

func TestCancelledChainDialogDoesNotHoldTheWindow(t *testing.T) {
	s, p, tm := newTestScheduler()
	a := mkJob("a", chain)
	s.Enqueue(a)
	p.take()
	s.Complete(a, protocol.Response{Code: protocol.ExitCancelled}) // Esc on a menu opened with --chain
	if got := p.take(); got != "retire:a conceal" {
		t.Fatalf("got %q", got)
	}
	for _, ms := range tm.ms {
		if ms != config.DefaultGraceMs {
			t.Fatalf("cancel should use the short grace, got %dms", ms)
		}
	}
}

func TestBackHoldsTheWindowForTheDialogItReturnsTo(t *testing.T) {
	// Back spawns the previous menu; it takes a moment to start. The window
	// must wait for it instead of closing after the short grace period.
	for _, name := range []string{"plain", "chain", "last"} {
		var mods []func(*protocol.Request)
		switch name {
		case "chain":
			mods = append(mods, chain)
		case "last":
			mods = append(mods, last)
		}
		s, p, tm := newTestScheduler()
		a := mkJob("a", mods...)
		s.Enqueue(a)
		p.take()
		s.Complete(a, protocol.Response{Code: protocol.ExitBack})
		if got := p.take(); got != "retire:a" {
			t.Errorf("%s: got %q (content stays up, surface kept)", name, got)
		}
		for _, ms := range tm.ms {
			if ms != config.DefaultChainMs {
				t.Errorf("%s: waits %dms, want %dms", name, ms, config.DefaultChainMs)
			}
		}
		s.Enqueue(mkJob("menu")) // the menu Back opened
		if got := p.take(); got != "present:menu" {
			t.Errorf("%s: got %q", name, got)
		}
	}
}

func TestLastReleasesImmediately(t *testing.T) {
	s, p, tm := newTestScheduler()
	a := mkJob("a", last)
	s.Enqueue(a)
	p.take()
	s.Complete(a, protocol.Response{})
	if got := p.take(); got != "retire:a unmap" {
		t.Fatalf("got %q", got)
	}
	if len(tm.pending) != 0 {
		t.Fatal("--last must not leave a timer behind")
	}
}

func TestSubDialogReturnsToParent(t *testing.T) {
	s, p, _ := newTestScheduler()
	parent, child := mkJob("parent"), mkJob("child", sub)
	s.Enqueue(parent)
	s.Enqueue(child)
	if got := p.take(); got != "present:parent suspend:parent present:child" {
		t.Fatalf("open: got %q", got)
	}
	s.Complete(child, protocol.Response{})
	if got := p.take(); got != "retire:child resume:parent" {
		t.Fatalf("close: got %q", got)
	}
	if s.Cur != parent {
		t.Fatal("parent should be on top again")
	}
}

func TestSubDialogsNest(t *testing.T) {
	s, p, _ := newTestScheduler()
	a, b, c := mkJob("a"), mkJob("b", sub), mkJob("c", sub)
	s.Enqueue(a)
	s.Enqueue(b)
	s.Enqueue(c)
	p.take()
	s.Complete(c, protocol.Response{})
	s.Complete(b, protocol.Response{})
	if got := p.take(); got != "retire:c resume:b retire:b resume:a" {
		t.Fatalf("got %q", got)
	}
}

func TestPlainRequestWaitsBehindSubStack(t *testing.T) {
	s, p, _ := newTestScheduler()
	a, b, q := mkJob("a"), mkJob("b", sub), mkJob("q")
	s.Enqueue(a)
	s.Enqueue(b)
	s.Enqueue(q)
	p.take()
	s.Complete(b, protocol.Response{})
	s.Complete(a, protocol.Response{})
	if got := p.take(); got != "retire:b resume:a retire:a present:q" {
		t.Fatalf("got %q", got)
	}
}

func TestSubWithNothingShowingIsAnOrdinaryRequest(t *testing.T) {
	s, p, _ := newTestScheduler()
	s.Enqueue(mkJob("a", sub))
	if got := p.take(); got != "present:a" {
		t.Fatalf("got %q", got)
	}
}

func TestCoveredDialogCanEndEarly(t *testing.T) {
	s, p, _ := newTestScheduler()
	a, b := mkJob("a"), mkJob("b", sub)
	s.Enqueue(a)
	s.Enqueue(b)
	p.take()
	s.Complete(a, protocol.Response{}) // e.g. a's submit.exec finished while b is on top
	if got := p.take(); got != "retire:a" {
		t.Fatalf("got %q", got)
	}
	s.Complete(b, protocol.Response{})
	if got := p.take(); got != "retire:b conceal" {
		t.Fatalf("a must not be resumed after it ended: %q", got)
	}
}

func TestWaitingClientThatDisconnectsIsDropped(t *testing.T) {
	s, p, _ := newTestScheduler()
	a, b := mkJob("a"), mkJob("b")
	s.Enqueue(a)
	s.Enqueue(b)
	p.take()
	s.Complete(b, protocol.Response{Code: protocol.ExitCancelled}) // b's client went away before its turn
	s.Complete(a, protocol.Response{})
	if got := p.take(); got != "retire:b retire:a conceal" {
		t.Fatalf("b must never be presented: %q", got)
	}
}

func TestCompleteIsIdempotent(t *testing.T) {
	s, p, _ := newTestScheduler()
	a := mkJob("a")
	s.Enqueue(a)
	p.take()
	s.Complete(a, protocol.Response{Code: 0})
	s.Complete(a, protocol.Response{Code: 3}) // must not block or change the answer
	if got := (<-a.Reply).Code; got != 0 {
		t.Fatalf("first answer wins, got %d", got)
	}
}

func TestDismissAllCancelsEverything(t *testing.T) {
	s, p, _ := newTestScheduler()
	a, b, q := mkJob("a"), mkJob("b", sub), mkJob("q")
	s.Enqueue(a)
	s.Enqueue(b)
	s.Enqueue(q)
	p.take()
	s.DismissAll()
	for _, j := range []*Job{a, b, q} {
		if got := (<-j.Reply).Code; got != protocol.ExitCancelled {
			t.Errorf("%s: code %d, want %d", j.req.Config, got, protocol.ExitCancelled)
		}
	}
	if s.Cur != nil || len(s.stack) != 0 || len(s.queue) != 0 {
		t.Fatal("scheduler should be empty")
	}
	log := p.take()
	if strings.Contains(log, "present") {
		t.Fatal("dismissing must not promote anything")
	}
	if !strings.HasSuffix(log, "unmap") {
		t.Fatalf("the window must be released at the end: %q", log)
	}
}

func TestDismissReleasesAWindowHeldForAChain(t *testing.T) {
	s, p, tm := newTestScheduler()
	a := mkJob("a", chain)
	s.Enqueue(a)
	s.Complete(a, protocol.Response{}) // closed; window held for the next dialog
	p.take()
	s.DismissAll()
	if got := p.take(); got != "unmap" {
		t.Fatalf("got %q", got)
	}
	if len(tm.pending) != 0 {
		t.Fatal("the chain timer must be cancelled")
	}
}

func TestOneshotLeavesWhenDone(t *testing.T) {
	s, p, tm := newTestScheduler()
	s.Oneshot = true
	a := mkJob("a")
	s.Enqueue(a)
	p.take()
	s.Complete(a, protocol.Response{})
	if got := p.take(); got != "retire:a idle" {
		t.Fatalf("got %q", got)
	}
	if len(tm.pending) != 0 {
		t.Fatal("oneshot has no grace period")
	}
}

func TestQuitWaitsForWork(t *testing.T) {
	s, p, _ := newTestScheduler()
	a := mkJob("a")
	s.Enqueue(a)
	p.take()
	s.Quit()
	if got := p.take(); got != "" {
		t.Fatalf("must not leave while a dialog is up: %q", got)
	}
	s.Complete(a, protocol.Response{})
	if got := p.take(); got != "retire:a idle" {
		t.Fatalf("got %q", got)
	}
}

func TestQuitWhenAlreadyIdle(t *testing.T) {
	s, p, _ := newTestScheduler()
	s.Quit()
	if got := p.take(); got != "idle" {
		t.Fatalf("got %q", got)
	}
}

// --- toggle: a keybind pressed again ------------------------------------------

func toggleJob(name string, mod ...func(*protocol.Request)) *Job {
	j := mkJob(name, mod...)
	j.Cfg = &config.Config{Toggle: true}
	return j
}

func TestToggleClosesTheDialogThatIsOnScreen(t *testing.T) {
	s, p, _ := newTestScheduler()
	a := toggleJob("launcher")
	s.Enqueue(a)
	p.take()
	again := toggleJob("launcher")
	s.Enqueue(again)
	if got := p.take(); got != "retire:launcher conceal" {
		t.Fatalf("pressed again: got %q", got)
	}
	if r := <-a.Reply; r.Code != protocol.ExitCancelled {
		t.Errorf("the open dialog should end cancelled: %+v", r)
	}
	if r := <-again.Reply; r.Code != protocol.ExitCancelled {
		t.Errorf("the repeat should be answered cancelled: %+v", r)
	}
	if s.Cur != nil || len(s.queue) != 0 {
		t.Errorf("nothing should be left: cur=%v queue=%d", s.Cur, len(s.queue))
	}
}

func TestToggleRepeatsNeverQueueUp(t *testing.T) {
	s, p, _ := newTestScheduler()
	other := mkJob("menu") // a plain dialog, no toggle, on screen
	s.Enqueue(other)
	first := toggleJob("launcher")
	s.Enqueue(first) // waits its turn
	for i := 0; i < 5; i++ {
		s.Enqueue(toggleJob("launcher")) // key held down
	}
	if len(s.queue) != 1 {
		t.Fatalf("queue = %d, want 1: repeats must be dropped", len(s.queue))
	}
	p.take()
	s.Complete(other, protocol.Response{})
	if got := p.take(); got != "retire:menu present:launcher" {
		t.Fatalf("after the first closes: got %q", got)
	}
}

func TestToggleOnlyMatchesTheSameConfig(t *testing.T) {
	s, p, _ := newTestScheduler()
	s.Enqueue(toggleJob("launcher"))
	s.Enqueue(toggleJob("power")) // another toggle dialog: queued, not a repeat
	if len(s.queue) != 1 {
		t.Fatalf("a different config should queue normally: %d", len(s.queue))
	}
	// A plain request for the same file is not a toggle either.
	s.Enqueue(mkJob("launcher"))
	if len(s.queue) != 2 {
		t.Fatalf("a request without toggle keeps today's behaviour: %d", len(s.queue))
	}
	_ = p
}

func TestToggleSubDialogIsDeliberate(t *testing.T) {
	s, p, _ := newTestScheduler()
	s.Enqueue(toggleJob("launcher"))
	p.take()
	s.Enqueue(toggleJob("launcher", sub))
	if got := p.take(); got != "suspend:launcher present:launcher" {
		t.Fatalf("--sub must nest, not toggle: %q", got)
	}
}

func TestWithoutToggleRepeatsStillQueue(t *testing.T) {
	s, _, _ := newTestScheduler()
	for i := 0; i < 3; i++ {
		j := mkJob("menu")
		j.Cfg = &config.Config{}
		s.Enqueue(j)
	}
	if len(s.queue) != 2 {
		t.Fatalf("default behaviour changed: queue = %d, want 2", len(s.queue))
	}
}
