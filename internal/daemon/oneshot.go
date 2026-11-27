package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"gitlab.com/KopyTKG/spot/internal/config"
	"gitlab.com/KopyTKG/spot/internal/protocol"
	"gitlab.com/KopyTKG/spot/internal/scheduler"
	"gitlab.com/KopyTKG/spot/internal/ui"
)

// appID builds the per-invocation GApplication ID for a config name. Every
// dot-separated part of a valid ID must start with a letter, so a name such as
// "01-form" cannot be used as is.
func appID(name string, pid int) string {
	return fmt.Sprintf("dev.kopy.spot.c_%s.p%d", config.SanitizeID(name), pid)
}

// RunOneshot shows the dialog in a window of its own and exits with the answer.
func RunOneshot(req protocol.Request) protocol.Response {
	j, err := scheduler.BuildJob(req)
	if err != nil {
		return protocol.Response{Code: protocol.ExitError, Stderr: fmt.Sprintf("spot: %v\n", err), Fault: true}
	}
	if j.Cfg.HasPassword() {
		noCoreDump()
	}
	// Without a display GTK would end the process itself, with the status that
	// means "cancelled" and no word to anyone.
	if !gtk.InitCheck() {
		return protocol.Response{Code: protocol.ExitError, Stderr: "spot: the dialog could not be shown: no Wayland display\n", Fault: true}
	}
	// Per-invocation app ID so concurrent `spot` calls don't dedupe.
	base := strings.TrimSuffix(filepath.Base(req.Config), filepath.Ext(req.Config))
	app := gtk.NewApplication(appID(base, os.Getpid()), gio.ApplicationFlagsNone)
	sh := ui.NewShell(app, true, &config.DaemonConfig{})
	app.ConnectActivate(func() {
		sh.Init()
		if j.Cfg.HasPassword() {
			sh.Raise() // it may be asked for while a dialog of the daemon is up
		}
		sh.Sched.Enqueue(j)
	})
	app.Run(nil)

	select {
	case r := <-j.Reply:
		return r
	default:
		return protocol.Response{Code: protocol.ExitError, Stderr: "spot: the dialog could not be shown\n", Fault: true}
	}
}

// noCoreDump keeps a password out of a crash dump on disk. Only the soft limit,
// and only in a process that asks for a password: the daemon starts your
// programs, which would inherit it.
func noCoreDump() {
	var l syscall.Rlimit
	if syscall.Getrlimit(syscall.RLIMIT_CORE, &l) == nil {
		l.Cur = 0
		syscall.Setrlimit(syscall.RLIMIT_CORE, &l)
	}
}
