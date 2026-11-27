//go:build gui

package ui

import (
	"testing"
	"time"

	"gitlab.com/KopyTKG/spot/internal/config"
)

// stdin may hold the same line thousands of times; the rows are built on the UI
// thread.
func TestManyEqualSwitchRowsAreQuick(t *testing.T) {
	lines := make([]string, 10000)
	for i := range lines {
		lines[i] = "same\tk\ton"
	}
	start := time.Now()
	s := parseSwitchRows(&config.Item{}, lines)
	if d := time.Since(start); d > 300*time.Millisecond {
		t.Errorf("took %v", d)
	}
	if len(s.rows) != len(lines) {
		t.Errorf("%d rows told apart, want %d", len(s.rows), len(lines))
	}
}
