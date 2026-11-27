package main

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// superviseArg makes this binary the supervisor of one host. It is used where
// there is no systemd to give a mod a scope: without one, a program that
// detaches itself from its parent would outlive the mod. As a "subreaper" the
// supervisor adopts such programs, and so can find and kill them (S29). It
// never reads anything of the mod's, and the mod cannot signal it: it is
// outside the sandbox.
const superviseArg = "supervise"

func supervise() int {
	if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0); err != nil {
		return 1
	}
	ch := os.NewFile(chanFD, "daemon")
	host := exec.Command("/proc/self/exe")
	host.Args[0] = "spot-mod-host"
	host.ExtraFiles = []*os.File{ch}
	host.Stderr = os.Stderr
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	if err := host.Start(); err != nil {
		return 1
	}
	ch.Close() // so that the daemon sees the host's end close when it goes
	ended := make(chan error, 1)
	go func() { ended <- host.Wait() }()
	code := 0
	select {
	case err := <-ended:
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			code = exit.ExitCode()
		}
	case <-stop:
		host.Process.Kill()
		<-ended
	}
	killAll()
	return code
}

// descendants are the processes below us.
func descendants() []int {
	parent := map[int]int{}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		b, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue
		}
		// "pid (name) state ppid ...": the name may hold anything.
		s := string(b)
		f := strings.Fields(s[strings.LastIndexByte(s, ')')+1:])
		if len(f) > 1 && f[0] != "Z" {
			parent[pid], _ = strconv.Atoi(f[1])
		}
	}
	me := os.Getpid()
	var out []int
	for pid := range parent {
		for p, steps := parent[pid], 0; p > 1 && steps < 64; p, steps = parent[p], steps+1 {
			if p == me {
				out = append(out, pid)
				break
			}
		}
	}
	return out
}

// killAll ends everything below us, again and again: what is killed may have
// just started something.
func killAll() {
	end := time.Now().Add(2 * time.Second)
	for {
		var status syscall.WaitStatus
		for {
			if pid, err := syscall.Wait4(-1, &status, syscall.WNOHANG, nil); pid <= 0 || err != nil {
				break
			}
		}
		left := descendants()
		if len(left) == 0 || time.Now().After(end) {
			return
		}
		for _, pid := range left {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
