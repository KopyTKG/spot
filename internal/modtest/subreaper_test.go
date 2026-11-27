package modtest

import (
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"gitlab.com/KopyTKG/spot/internal/modhost"
	"gitlab.com/KopyTKG/spot/internal/modload"
	"gitlab.com/KopyTKG/spot/internal/sandbox"
)

// Without a systemd user session there is no scope to kill: the supervisor has
// to find what detached itself (S29), however it did it.
func TestDetachedProgramsStopWithoutSystemd(t *testing.T) {
	for _, how := range []string{"stopping the mod", "the daemon dying"} {
		w := newWorld(t)
		w.file(".config/spot/mods/m/mod.toml", "[permissions.run]\nsh='r'\nsetsid='r'\nsleep='r'\n")
		w.file(".config/spot/mods/m/init.lua", `spot.on("detach", function()
  spot.run({ "sh", "-c", "setsid -f sh -c 'sleep 31.7 & sleep 31.7' >/dev/null 2>&1" })
  spot.run({ "setsid", "-f", "sleep", "31.7" })
  spot.run({ "sh", "-c", "(sleep 31.7 >/dev/null 2>&1 &)" })
  return true
end)`)
		c, l, err := modload.Provider(w.conf+"/spot/mods/m", modload.Options{Host: hostBin, Env: func(k string) string { return w.env[k] },
			Protected: sandbox.Protected{}, EmptyDir: w.dir + "/empty"})
		if err != nil {
			t.Fatal(err)
		}
		c.NoScope, c.Stderr = true, os.Stderr
		h, err := modhost.Start(c)
		if err != nil {
			t.Fatal(err)
		}
		if h.Limited {
			t.Fatal("it has a scope")
		}
		if _, err := h.Load(l); err != nil {
			t.Fatal(err)
		}
		if r, err := h.Call("detach", nil); err != nil || strings.TrimSpace(string(r.Result)) != "true" {
			t.Fatalf("%s %v", r.Result, err)
		}
		const args = "sleep\x0031.7\x00"
		time.Sleep(300 * time.Millisecond)
		if n := copiesOf(args); n < 4 {
			t.Fatalf("%s: only %d programs detached, this proves nothing", how, n)
		}
		if how == "stopping the mod" {
			h.Stop()
		} else {
			h.Abandon()
		}
		for range 200 {
			if copiesOf(args) == 0 {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if n := copiesOf(args); n != 0 {
			t.Errorf("%s: %d left running", how, n)
		}
	}
}

// The supervisor is outside the sandbox: a program of the mod cannot signal it.
func TestAModCannotKillItsSupervisor(t *testing.T) {
	w := newWorld(t)
	w.file(".config/spot/mods/m/mod.toml", "[permissions.run]\nkill='r'\n")
	w.file(".config/spot/mods/m/init.lua", `spot.on("kill", function(a) return spot.run({ "kill", "-9", a[1] }).status end)`)
	c, l, err := modload.Provider(w.conf+"/spot/mods/m", modload.Options{Host: hostBin, Env: func(k string) string { return w.env[k] },
		Protected: sandbox.Protected{}, EmptyDir: w.dir + "/empty"})
	if err != nil {
		t.Fatal(err)
	}
	c.NoScope = true
	h, err := modhost.Start(c)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Stop()
	if _, err := h.Load(l); err != nil {
		t.Fatal(err)
	}
	supervisor := 0
	entries, _ := os.ReadDir("/proc")
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		cmd, _ := os.ReadFile("/proc/" + e.Name() + "/cmdline")
		stat, _ := os.ReadFile("/proc/" + e.Name() + "/stat")
		if strings.HasSuffix(string(cmd), "\x00supervise\x00") && strings.Contains(string(stat), ") S "+strconv.Itoa(os.Getpid())+" ") {
			supervisor = pid
		}
	}
	if supervisor == 0 {
		t.Fatal("no supervisor found")
	}
	r, err := h.Call("kill", []string{strconv.Itoa(supervisor)})
	if err != nil || strings.TrimSpace(string(r.Result)) == "0" {
		t.Errorf("kill: %s %v", r.Result, err)
	}
	if syscall.Kill(supervisor, 0) != nil {
		t.Error("the supervisor is gone")
	}
}
