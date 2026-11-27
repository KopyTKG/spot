package modd

import (
	"context"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var scopeName = regexp.MustCompile(`^spot-mod-[a-z0-9_-]+-([0-9]+)-[0-9]+\.scope$`)

// StaleScopes are the scopes, among units, of daemons that are no more: what a
// mod of theirs detached is still running in them.
func StaleScopes(units []string, alive func(pid int) bool) []string {
	var out []string
	for _, u := range units {
		if m := scopeName.FindStringSubmatch(u); m != nil {
			if pid, err := strconv.Atoi(m[1]); err == nil && !alive(pid) {
				out = append(out, u)
			}
		}
	}
	return out
}

// CleanStale stops them.
func CleanStale() {
	if os.Getenv("XDG_RUNTIME_DIR") == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/bin/systemctl", "--user", "list-units", "--plain", "--no-legend", "--type=scope", "spot-mod-*").Output()
	if err != nil {
		return
	}
	var units []string
	for _, l := range strings.Split(string(out), "\n") {
		if f := strings.Fields(l); len(f) > 0 {
			units = append(units, f[0])
		}
	}
	for _, u := range StaleScopes(units, func(pid int) bool { return syscall.Kill(pid, 0) == nil }) {
		_ = exec.CommandContext(ctx, "/usr/bin/systemctl", "--user", "stop", u).Run()
	}
}
