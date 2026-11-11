package main

import (
	"reflect"
	"strings"
	"testing"
)

// fakePresenter records what the scheduler asks of the window.
type fakePresenter struct{ log []string }

func (f *fakePresenter) rec(op string, j *job) {
	name := ""
	if j != nil {
		name = ":" + j.req.Config
	}
	f.log = append(f.log, op+name)
}
func (f *fakePresenter) present(j *job) { f.rec("present", j) }
func (f *fakePresenter) suspend(j *job) { f.rec("suspend", j) }
func (f *fakePresenter) resume(j *job)  { f.rec("resume", j) }
func (f *fakePresenter) retire(j *job)  { f.rec("retire", j) }
func (f *fakePresenter) conceal()       { f.rec("conceal", nil) }
func (f *fakePresenter) unmap()         { f.rec("unmap", nil) }

// fakeTimers holds timers until the test fires them.
type fakeTimers struct {
	pending map[int]func()
	next    int
	ms      map[int]int
}

func (t *fakeTimers) after(ms int, f func()) func() {
	if t.pending == nil {
		t.pending, t.ms = map[int]func(){}, map[int]int{}
	}
	t.next++
	id := t.next
	t.pending[id], t.ms[id] = f, ms
	return func() { delete(t.pending, id) }
}

// fire runs every timer that is still pending.
func (t *fakeTimers) fire() {
	for id, f := range t.pending {
		delete(t.pending, id)
		f()
	}
}

func newTestScheduler() (*scheduler, *fakePresenter, *fakeTimers) {
	p, t := &fakePresenter{}, &fakeTimers{}
	return &scheduler{p: p, t: t, onIdle: func() { p.log = append(p.log, "idle") }}, p, t
}

func mkJob(name string, mod ...func(*request)) *job {
	j := &job{req: request{Op: opShow, Config: name}, reply: make(chan response, 1)}
	for _, m := range mod {
		m(&j.req)
	}
	return j
}

func sub(r *request)   { r.Sub = true }
func chain(r *request) { r.Chain = true }
func last(r *request)  { r.Last = true }

func (f *fakePresenter) take() string {
	s := strings.Join(f.log, " ")
	f.log = nil
	return s
}

func TestShowsImmediatelyWhenIdle(t *testing.T) {
	s, p, _ := newTestScheduler()
	s.enqueue(mkJob("a"))
	if got := p.take(); got != "present:a" {
		t.Fatalf("got %q", got)
	}
}

func TestQueuedRequestsRunInOrderWithoutReleasingTheSurface(t *testing.T) {
	s, p, tm := newTestScheduler()
	a, b, c := mkJob("a"), mkJob("b"), mkJob("c")
	s.enqueue(a)
	s.enqueue(b)
	s.enqueue(c)
	p.take()

	s.complete(a, response{Code: 0})
	if got := p.take(); got != "retire:a present:b" {
		t.Fatalf("a done: got %q", got)
	}
	s.complete(b, response{})
	if got := p.take(); got != "retire:b present:c" {
		t.Fatalf("b done: got %q", got)
	}
	// No conceal/unmap while there was a next dialog: the surface was never released.
	s.complete(c, response{})
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
	s.enqueue(a)
	s.complete(a, response{})
	p.take() // retire:a conceal

	s.enqueue(mkJob("b")) // the next menu step arrives inside the grace period
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
	s.enqueue(a)
	s.complete(a, response{})
	p.take()
	tm.fire()
	if got := p.take(); got != "unmap" {
		t.Fatalf("got %q", got)
	}
}

func TestGraceLengths(t *testing.T) {
	cases := []struct {
		name string
		mod  []func(*request)
		want []string // after completing: presenter log
		ms   int
	}{
		{"default conceals then waits briefly", nil, []string{"retire:a", "conceal"}, graceMs},
		{"chain keeps content up and waits longer", []func(*request){chain}, []string{"retire:a"}, chainMs},
	}
	for _, tc := range cases {
		s, p, tm := newTestScheduler()
		a := mkJob("a", tc.mod...)
		s.enqueue(a)
		p.take()
		s.complete(a, response{})
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
	s.enqueue(a)
	p.take()
	s.complete(a, response{Code: exitCancelled}) // Esc on a menu opened with --chain
	if got := p.take(); got != "retire:a conceal" {
		t.Fatalf("got %q", got)
	}
	for _, ms := range tm.ms {
		if ms != graceMs {
			t.Fatalf("cancel should use the short grace, got %dms", ms)
		}
	}
}

func TestLastReleasesImmediately(t *testing.T) {
	s, p, tm := newTestScheduler()
	a := mkJob("a", last)
	s.enqueue(a)
	p.take()
	s.complete(a, response{})
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
	s.enqueue(parent)
	s.enqueue(child)
	if got := p.take(); got != "present:parent suspend:parent present:child" {
		t.Fatalf("open: got %q", got)
	}
	s.complete(child, response{})
	if got := p.take(); got != "retire:child resume:parent" {
		t.Fatalf("close: got %q", got)
	}
	if s.cur != parent {
		t.Fatal("parent should be on top again")
	}
}

func TestSubDialogsNest(t *testing.T) {
	s, p, _ := newTestScheduler()
	a, b, c := mkJob("a"), mkJob("b", sub), mkJob("c", sub)
	s.enqueue(a)
	s.enqueue(b)
	s.enqueue(c)
	p.take()
	s.complete(c, response{})
	s.complete(b, response{})
	if got := p.take(); got != "retire:c resume:b retire:b resume:a" {
		t.Fatalf("got %q", got)
	}
}

func TestPlainRequestWaitsBehindSubStack(t *testing.T) {
	s, p, _ := newTestScheduler()
	a, b, q := mkJob("a"), mkJob("b", sub), mkJob("q")
	s.enqueue(a)
	s.enqueue(b)
	s.enqueue(q)
	p.take()
	s.complete(b, response{})
	s.complete(a, response{})
	if got := p.take(); got != "retire:b resume:a retire:a present:q" {
		t.Fatalf("got %q", got)
	}
}

func TestSubWithNothingShowingIsAnOrdinaryRequest(t *testing.T) {
	s, p, _ := newTestScheduler()
	s.enqueue(mkJob("a", sub))
	if got := p.take(); got != "present:a" {
		t.Fatalf("got %q", got)
	}
}

func TestCoveredDialogCanEndEarly(t *testing.T) {
	s, p, _ := newTestScheduler()
	a, b := mkJob("a"), mkJob("b", sub)
	s.enqueue(a)
	s.enqueue(b)
	p.take()
	s.complete(a, response{}) // e.g. a's submit.exec finished while b is on top
	if got := p.take(); got != "retire:a" {
		t.Fatalf("got %q", got)
	}
	s.complete(b, response{})
	if got := p.take(); got != "retire:b conceal" {
		t.Fatalf("a must not be resumed after it ended: %q", got)
	}
}

func TestWaitingClientThatDisconnectsIsDropped(t *testing.T) {
	s, p, _ := newTestScheduler()
	a, b := mkJob("a"), mkJob("b")
	s.enqueue(a)
	s.enqueue(b)
	p.take()
	s.complete(b, response{Code: exitCancelled}) // b's client went away before its turn
	s.complete(a, response{})
	if got := p.take(); got != "retire:b retire:a conceal" {
		t.Fatalf("b must never be presented: %q", got)
	}
}

func TestCompleteIsIdempotent(t *testing.T) {
	s, p, _ := newTestScheduler()
	a := mkJob("a")
	s.enqueue(a)
	p.take()
	s.complete(a, response{Code: 0})
	s.complete(a, response{Code: 3}) // must not block or change the answer
	if got := (<-a.reply).Code; got != 0 {
		t.Fatalf("first answer wins, got %d", got)
	}
}

func TestDismissAllCancelsEverything(t *testing.T) {
	s, p, _ := newTestScheduler()
	a, b, q := mkJob("a"), mkJob("b", sub), mkJob("q")
	s.enqueue(a)
	s.enqueue(b)
	s.enqueue(q)
	p.take()
	s.dismissAll()
	for _, j := range []*job{a, b, q} {
		if got := (<-j.reply).Code; got != exitCancelled {
			t.Errorf("%s: code %d, want %d", j.req.Config, got, exitCancelled)
		}
	}
	if s.cur != nil || len(s.stack) != 0 || len(s.queue) != 0 {
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
	s.enqueue(a)
	s.complete(a, response{}) // closed; window held for the next dialog
	p.take()
	s.dismissAll()
	if got := p.take(); got != "unmap" {
		t.Fatalf("got %q", got)
	}
	if len(tm.pending) != 0 {
		t.Fatal("the chain timer must be cancelled")
	}
}

func TestOneshotLeavesWhenDone(t *testing.T) {
	s, p, tm := newTestScheduler()
	s.oneshot = true
	a := mkJob("a")
	s.enqueue(a)
	p.take()
	s.complete(a, response{})
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
	s.enqueue(a)
	p.take()
	s.quit()
	if got := p.take(); got != "" {
		t.Fatalf("must not leave while a dialog is up: %q", got)
	}
	s.complete(a, response{})
	if got := p.take(); got != "retire:a idle" {
		t.Fatalf("got %q", got)
	}
}

func TestQuitWhenAlreadyIdle(t *testing.T) {
	s, p, _ := newTestScheduler()
	s.quit()
	if got := p.take(); got != "idle" {
		t.Fatalf("got %q", got)
	}
}
