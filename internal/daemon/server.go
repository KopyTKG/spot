// Package daemon serves dialogs: on the socket, or once for a single call.
package daemon

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"syscall"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"gitlab.com/KopyTKG/spot/internal/config"
	"gitlab.com/KopyTKG/spot/internal/protocol"
	"gitlab.com/KopyTKG/spot/internal/scheduler"
	"gitlab.com/KopyTKG/spot/internal/ui"
)

// onMain runs f on the GTK main thread.
func onMain(f func()) { coreglib.IdleAdd(f) }

// RunDaemon serves dialogs until told to quit. It returns the process exit status.
func RunDaemon() int {
	sock := config.SocketPath()
	lock, err := os.OpenFile(sock+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "spot: %v\n", err)
		return protocol.ExitError
	}
	defer lock.Close()
	// Held for the daemon's life: two clients racing to start one end up with one.
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		fmt.Fprintln(os.Stderr, "spot: daemon already running")
		return protocol.ExitSubmitted
	}

	os.Remove(sock) // a leftover from a crash; the lock says nobody is using it
	ln, err := net.Listen("unix", sock)
	if err != nil {
		fmt.Fprintf(os.Stderr, "spot: %v\n", err)
		return protocol.ExitError
	}
	os.Chmod(sock, 0o600)
	defer os.Remove(sock)
	defer ln.Close()

	// NonUnique: one daemon per socket is enforced by the lock above. With a
	// unique application ID GTK would hand a second daemon (another SPOT_SOCKET)
	// over to the first and exit, re-activating the first one.
	app := gtk.NewApplication("dev.kopy.spot.daemon", gio.ApplicationNonUnique)
	sh := ui.NewShell(app, false, config.Daemon())
	app.ConnectActivate(func() {
		sh.Init()
		app.Hold() // no window yet is not a reason to exit
		go serve(ln, sh)
	})

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		onMain(func() {
			sh.Sched.DismissAll()
			app.Quit()
		})
	}()

	if code := app.Run(nil); code > 0 {
		return protocol.ExitError
	}
	return protocol.ExitSubmitted
}

func serve(ln net.Listener, sh *ui.Shell) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go handle(conn, sh)
	}
}

func handle(conn net.Conn, sh *ui.Shell) {
	defer conn.Close()
	reply := func(r protocol.Response) {
		r.Version = protocol.Version
		json.NewEncoder(conn).Encode(r)
	}

	var req protocol.Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		return
	}

	switch req.Op {
	case protocol.OpPing:
		reply(protocol.Response{})
	case protocol.OpQuit:
		onMain(sh.Sched.Quit)
		reply(protocol.Response{})
	case protocol.OpDismiss:
		onMain(sh.Sched.DismissAll)
		reply(protocol.Response{})
	case protocol.OpShow:
		show(conn, sh, req, reply)
	default:
		reply(protocol.Response{Code: protocol.ExitUsage, Stderr: fmt.Sprintf("spot: unknown operation %q\n", req.Op)})
	}
}

func show(conn net.Conn, sh *ui.Shell, req protocol.Request, reply func(protocol.Response)) {
	// A daemon left over from before an upgrade would run old code on new configs.
	if req.Version != protocol.Version {
		reply(protocol.Response{Stale: true})
		return
	}
	j, err := scheduler.BuildJob(req)
	if err != nil {
		reply(protocol.Response{Code: protocol.ExitError, Stderr: fmt.Sprintf("spot: %v\n", err), Fault: true})
		return
	}

	// If the caller goes away (Ctrl-C, killed script) its dialog goes with it.
	gone := make(chan struct{})
	go func() {
		io.Copy(io.Discard, conn)
		close(gone)
	}()

	onMain(func() { sh.Sched.Enqueue(j) })
	select {
	case r := <-j.Reply:
		reply(r)
	case <-gone:
		onMain(func() {
			sh.Sched.Complete(j, protocol.Response{Code: protocol.ExitCancelled, Version: protocol.Version})
		})
	}
}
