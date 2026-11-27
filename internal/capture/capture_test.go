package capture

import (
	"os/exec"
	"testing"
)

func TestKeepsOnlyTheLimit(t *testing.T) {
	var b Buffer
	c := exec.Command("sh", "-c", "head -c 20000000 /dev/zero")
	c.Stdout = &b
	if err := c.Run(); err != nil {
		t.Fatalf("the command must run to its end: %v", err)
	}
	if len(b.Bytes()) != Limit {
		t.Errorf("kept %d bytes", len(b.Bytes()))
	}
}
