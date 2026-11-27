package sandbox

import "testing"

func TestCapabilitiesAreRefused(t *testing.T) {
	if capable(0) != nil {
		t.Error("none is refused")
	}
	if capable(1<<23) == nil { // CAP_SYS_NICE, which a compositor may pass on
		t.Error("a capability was accepted")
	}
	if err := Privileged(); err != nil {
		t.Logf("this test runs privileged: %v", err)
	}
}
