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
	"time"

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
	if os.Geteuid() == 0 {
		fmt.Fprintln(os.Stderr, "spot: the daemon does not run as root")
		return protocol.ExitError
	}
	// Not PR_SET_DUMPABLE 0, tempting as it is: the desktop portal then cannot
	// identify the process and refuses GTK the desktop's settings.
	sock := config.SocketPath()
	if err := config.PrepareSocket(sock); err != nil {
		fmt.Fprintf(os.Stderr, "spot: %v\n", err)
		return protocol.ExitError
	}
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

	os.Remove(sock)             // a leftover from a crash; the lock says nobody is using it
	old := syscall.Umask(0o177) // the socket is never open to others, not even briefly
	ln, err := net.Listen("unix", sock)
	syscall.Umask(old)
	if err != nil {
		fmt.Fprintf(os.Stderr, "spot: %v\n", err)
		return protocol.ExitError
	}
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

// A request is a config's path, the caller's environment and its stdin lines.
const (
	maxRequest  = 32 << 20
	requestWait = 10 * time.Second
)

// readRequest reads one request, bounded in size and in the time it may take.
// Afterwards the connection waits without limit: a dialog can stay up for long.
func readRequest(conn net.Conn) (req protocol.Request, err error) {
	conn.SetReadDeadline(time.Now().Add(requestWait))
	defer conn.SetReadDeadline(time.Time{})
	err = json.NewDecoder(io.LimitReader(conn, maxRequest)).Decode(&req)
	return req, err
}

func handle(conn net.Conn, sh *ui.Shell) {
	defer conn.Close()
	if err := protocol.CheckPeer(conn); err != nil {
		fmt.Fprintf(os.Stderr, "spot: refused a connection: %v\n", err)
		return
	}
	reply := func(r protocol.Response) {
		r.Version = protocol.Version
		json.NewEncoder(conn).Encode(r)
	}

	req, err := readRequest(conn)
	if err != nil {
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
