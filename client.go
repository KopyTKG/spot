package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// viaDaemon shows req through the daemon, starting one if none is running.
// ok is false when that is not possible and the caller should run the dialog
// itself.
func viaDaemon(req request) (resp response, ok bool) {
	conn, err := dialDaemon(true)
	if err != nil {
		return resp, false
	}
	resp, err = roundTrip(conn, req)
	conn.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "spot: daemon: %v (showing the dialog directly)\n", err)
		return resp, false
	}

	if resp.Stale {
		// Built from a different version than us: replace it and ask again.
		stopDaemon()
		if conn, err = dialDaemon(true); err != nil {
			return resp, false
		}
		defer conn.Close()
		if resp, err = roundTrip(conn, req); err != nil || resp.Stale {
			return resp, false
		}
	}
	return resp, true
}

// dialDaemon connects to the running daemon. With start set it launches one
// first if needed.
func dialDaemon(start bool) (net.Conn, error) {
	sock := socketPath()
	conn, err := net.Dial("unix", sock)
	if err == nil || !start {
		return conn, err
	}
	if err := spawnDaemon(); err != nil {
		return nil, err
	}
	// The first start has to bring up GTK; give it a moment.
	deadline := time.Now().Add(3 * time.Second)
	for {
		if conn, err = net.Dial("unix", sock); err == nil {
			return conn, nil
		}
		if time.Now().After(deadline) {
			return nil, err
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// spawnDaemon starts `spot daemon` detached from this process and terminal.
func spawnDaemon() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	logf := os.NewFile(0, os.DevNull)
	if path := logPath(); path != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
			if f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644); err == nil {
				defer f.Close()
				logf = f
			}
		}
	}
	c := exec.Command(exe, "daemon")
	c.Stdout, c.Stderr = logf, logf
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := c.Start(); err != nil {
		return err
	}
	return c.Process.Release()
}

// stopDaemon asks the daemon to exit once idle and waits until its socket is gone.
func stopDaemon() {
	if conn, err := dialDaemon(false); err == nil {
		roundTrip(conn, request{Op: opQuit, Version: version})
		conn.Close()
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.Dial("unix", socketPath())
		if err != nil {
			return
		}
		conn.Close()
		time.Sleep(25 * time.Millisecond)
	}
}

// control sends a one-off command to a running daemon (never starts one).
// It returns the exit status for `spot`.
func control(op string) int {
	conn, err := dialDaemon(false)
	if err != nil {
		if op == opPing {
			fmt.Println("spot daemon: not running")
			return exitCancelled
		}
		return exitSubmitted // nothing to dismiss or stop
	}
	defer conn.Close()
	resp, err := roundTrip(conn, request{Op: op, Version: version})
	if err != nil {
		fmt.Fprintf(os.Stderr, "spot: %v\n", err)
		return exitError
	}
	if op == opPing {
		fmt.Printf("spot daemon: running (%s), client %s\n", resp.Version, version)
	}
	return exitSubmitted
}
