package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// A mod host and what it starts live in a scope of its own; the daemon takes
// nothing from them (S21).
func TestAConnectionFromAModsScopeIsRefused(t *testing.T) {
	if _, err := os.Stat(filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "systemd/private")); err != nil {
		t.Skip("no systemd user session")
	}
	if fromAMod(os.Getpid()) {
		t.Error("an ordinary process taken for a mod")
	}
	unit := fmt.Sprintf("spot-mod-test-%d-1.scope", os.Getpid())
	c := exec.Command("/usr/bin/systemd-run", "--user", "--scope", "--quiet", "--collect", "--unit="+unit, "--", "/usr/bin/sleep", "20")
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { c.Process.Kill(); c.Wait() }()
	for range 200 {
		if fromAMod(c.Process.Pid) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("a process in a mod's scope was not recognised")
}
