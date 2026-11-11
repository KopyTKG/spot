package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// buildJob reads the config named by req and prepares it for display. It
// touches no GTK, so it may run on any goroutine.
func buildJob(req request) (*job, error) {
	data, err := os.ReadFile(req.Config)
	if err != nil {
		return nil, err
	}
	cfg, err := parseConfig(data, envLookup(req.Env))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", req.Config, err)
	}

	for i := range cfg.Items {
		if cfg.Items[i].Type == "list" && cfg.Items[i].Stdin {
			cfg.Items[i].Options = stdinLines(req.Stdin)
		}
	}

	css := ""
	if cfg.Style != "" {
		css = cfg.Style
		if !filepath.IsAbs(css) {
			css = filepath.Join(filepath.Dir(req.Config), css)
		}
	}
	return &job{req: req, cfg: cfg, css: css, env: req.Env, dir: req.Dir, back: cfg.backSpec(req.Back), reply: make(chan response, 1)}, nil
}

// stdinLines splits newline-delimited list options.
func stdinLines(s string) []string {
	sc := bufio.NewScanner(strings.NewReader(s))
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines
}

// onMain runs f on the GTK main thread.
func onMain(f func()) { coreglib.IdleAdd(f) }

// runDaemon serves dialogs until told to quit. It returns the process exit status.
func runDaemon() int {
	sock := socketPath()
	lock, err := os.OpenFile(sock+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "spot: %v\n", err)
		return exitError
	}
	defer lock.Close()
	// Held for the daemon's life: two clients racing to start one end up with one.
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		fmt.Fprintln(os.Stderr, "spot: daemon already running")
		return exitSubmitted
	}

	os.Remove(sock) // a leftover from a crash; the lock says nobody is using it
	ln, err := net.Listen("unix", sock)
	if err != nil {
		fmt.Fprintf(os.Stderr, "spot: %v\n", err)
		return exitError
	}
	os.Chmod(sock, 0o600)
	defer os.Remove(sock)
	defer ln.Close()

	// NonUnique: one daemon per socket is enforced by the lock above. With a
	// unique application ID GTK would hand a second daemon (another SPOT_SOCKET)
	// over to the first and exit, re-activating the first one.
	app := gtk.NewApplication("dev.kopy.spot.daemon", gio.ApplicationNonUnique)
	sh := newShell(app, false)
	app.ConnectActivate(func() {
		sh.init()
		app.Hold() // no window yet is not a reason to exit
		go serve(ln, sh)
	})

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		onMain(func() {
			sh.sched.dismissAll()
			app.Quit()
		})
	}()

	if code := app.Run(nil); code > 0 {
		return exitError
	}
	return exitSubmitted
}

func serve(ln net.Listener, sh *shell) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go handle(conn, sh)
	}
}

func handle(conn net.Conn, sh *shell) {
	defer conn.Close()
	reply := func(r response) {
		r.Version = version
		json.NewEncoder(conn).Encode(r)
	}

	var req request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		return
	}

	switch req.Op {
	case opPing:
		reply(response{})
	case opQuit:
		onMain(sh.sched.quit)
		reply(response{})
	case opDismiss:
		onMain(sh.sched.dismissAll)
		reply(response{})
	case opShow:
		show(conn, sh, req, reply)
	default:
		reply(response{Code: exitUsage, Stderr: fmt.Sprintf("spot: unknown operation %q\n", req.Op)})
	}
}

func show(conn net.Conn, sh *shell, req request, reply func(response)) {
	// A daemon left over from before an upgrade would run old code on new configs.
	if req.Version != version {
		reply(response{Stale: true})
		return
	}
	j, err := buildJob(req)
	if err != nil {
		reply(response{Code: exitError, Stderr: fmt.Sprintf("spot: %v\n", err)})
		return
	}

	// If the caller goes away (Ctrl-C, killed script) its dialog goes with it.
	gone := make(chan struct{})
	go func() {
		io.Copy(io.Discard, conn)
		close(gone)
	}()

	onMain(func() { sh.sched.enqueue(j) })
	select {
	case r := <-j.reply:
		reply(r)
	case <-gone:
		onMain(func() { sh.sched.complete(j, response{Code: exitCancelled, Version: version}) })
	}
}
