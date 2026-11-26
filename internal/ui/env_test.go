package ui

import (
	"testing"
)

func TestWithoutEnv(t *testing.T) {
	got := withoutEnv([]string{"A=1", "SPOT_BACK=x y", "SPOT_BACKUP=keep", "B=2"}, "SPOT_BACK")
	want := []string{"A=1", "SPOT_BACKUP=keep", "B=2"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}
