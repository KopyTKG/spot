// Package client is the command line's side of the daemon: start it, ask it for a
// dialog, control it. No GTK.
package client

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"gitlab.com/KopyTKG/spot/internal/config"
	"gitlab.com/KopyTKG/spot/internal/notify"
	"gitlab.com/KopyTKG/spot/internal/protocol"
)

// ViaDaemon shows req through the daemon, starting one if none is running.
// ok is false when that is not possible and the caller should run the dialog
// itself.
func ViaDaemon(req protocol.Request) (resp protocol.Response, ok bool) {
	conn, err := dialDaemon(true)
	if err != nil {
		return resp, false
	}
	resp, err = protocol.RoundTrip(conn, req)
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
		if resp, err = protocol.RoundTrip(conn, req); err != nil || resp.Stale {
			return resp, false
		}
	}
	return resp, true
}

// dialDaemon connects to the running daemon. With start set it launches one
// first if needed.
func dialDaemon(start bool) (net.Conn, error) {
	sock := config.SocketPath()
	if err := config.PrepareSocket(sock); err != nil {
		return nil, untrusted(err)
	}
	conn, err := dialOwn(sock)
	if err == nil || !start || errors.Is(err, protocol.ErrForeignPeer) {
		if errors.Is(err, protocol.ErrForeignPeer) {
			err = untrusted(err)
		}
		return conn, err
	}
	if err := spawnDaemon(); err != nil {
		return nil, err
	}
	// The first start has to bring up GTK; give it a moment.
	deadline := time.Now().Add(3 * time.Second)
	for {
		if conn, err = dialOwn(sock); err == nil {
			return conn, nil
		}
		if time.Now().After(deadline) {
			return nil, err
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// dialOwn connects to the socket and makes sure a process of ours answers,
// before anything is sent to it.
func dialOwn(sock string) (net.Conn, error) {
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil, err
	}
	if err := protocol.CheckPeer(conn); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

// untrusted reports a socket that must not be used: someone else's doing, so it
// is said where it will be seen. The dialog is then shown without the daemon.
func untrusted(err error) error {
	msg := fmt.Sprintf("not using the daemon socket: %v", err)
	fmt.Fprintf(os.Stderr, "spot: %s\n", msg)
	notify.Failure(config.Daemon().Notify, "spot: the daemon socket cannot be trusted", msg)
	return err
}

// openLog opens the daemon's log, readable by its owner only: it can quote
// commands and configs. A log from an earlier release is tightened too.
func openLog(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		return nil, err
	}
	// Only a plain file: log = "/dev/null" must not have its mode changed.
	if st, err := f.Stat(); err == nil && st.Mode().IsRegular() {
		_ = f.Chmod(0o600)
	}
	return f, nil
}

// spawnDaemon starts `spot daemon` detached from this process and terminal.
func spawnDaemon() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	logf := os.NewFile(0, os.DevNull)
	if path := config.LogPath(); path != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err == nil {
			if f, err := openLog(path); err == nil {
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
		protocol.RoundTrip(conn, protocol.Request{Op: protocol.OpQuit, Version: protocol.Version})
		conn.Close()
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.Dial("unix", config.SocketPath())
		if err != nil {
			return
		}
		conn.Close()
		time.Sleep(25 * time.Millisecond)
	}
}

// Control sends a one-off command to a running daemon (never starts one).
// It returns the exit status for `spot`.
func Control(op string) int {
	conn, err := dialDaemon(false)
	if err != nil {
		if op == protocol.OpPing {
			fmt.Println("spot daemon: not running")
			return protocol.ExitCancelled
		}
		return protocol.ExitSubmitted // nothing to dismiss or stop
	}
	defer conn.Close()
	resp, err := protocol.RoundTrip(conn, protocol.Request{Op: op, Version: protocol.Version})
	if err != nil {
		fmt.Fprintf(os.Stderr, "spot: %v\n", err)
		return protocol.ExitError
	}
	if op == protocol.OpPing {
		fmt.Printf("spot daemon: running (%s), client %s\n", resp.Version, protocol.Version)
	}
	return protocol.ExitSubmitted
}
