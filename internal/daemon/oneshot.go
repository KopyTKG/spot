package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
