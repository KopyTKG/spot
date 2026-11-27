package scheduler

import (
	"testing"

	"gitlab.com/KopyTKG/spot/internal/protocol"
)

func TestTheWaitingLineHasAnEnd(t *testing.T) {
	// The current job is set by hand: nothing here is ever shown.
	s := &Scheduler{Cur: &Job{}}
	var last *Job
	for i := 0; i <= maxWaiting; i++ {
		last = &Job{Reply: make(chan protocol.Response, 1)}
		s.stopGrace()
		s.Enqueue(last)
	}
	if len(s.queue) != maxWaiting {
		t.Errorf("%d waiting", len(s.queue))
	}
	select {
	case r := <-last.Reply:
		if r.Code != protocol.ExitError {
			t.Errorf("code %d", r.Code)
		}
	default:
		t.Error("the one too many got no answer")
	}
}
