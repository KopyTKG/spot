// Package notify tells the user that spot failed when nobody would otherwise
// see it: a dialog started from a keybind, a bar or a menu has its stderr thrown
// away, and a broken config just looks like nothing happened. No GTK: the
// desktop's notification daemon does the showing.
package notify

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// EnvOff turns notifications off for one call, whatever daemon.toml says.
const EnvOff = "SPOT_NO_NOTIFY"

// IsTerminal reports whether f is a terminal, where an error message is seen.
func IsTerminal(f *os.File) bool {
	var t syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TCGETS, uintptr(unsafe.Pointer(&t)))
	return errno == 0
}

// Failure sends a notification unless stderr is a terminal. command is the
// `notify` key of daemon.toml: nil for notify-send, "" for off, otherwise a
// shell command that finds the text in $SPOT_NOTIFY_TITLE and $SPOT_NOTIFY_BODY.
// Not being able to notify is not an error: the message is on stderr already.
func Failure(command *string, title, body string) {
	if os.Getenv(EnvOff) != "" || IsTerminal(os.Stderr) {
		return
	}
	send(command, title, body)
}

// A notification daemon that does not answer must not keep spot from exiting.
const limit = 3 * time.Second

func send(command *string, title, body string) {
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	var c *exec.Cmd
	switch {
	case command == nil:
		// "--": a title or message may start with a dash.
		c = exec.CommandContext(ctx, "notify-send", "--app-name=spot", "--urgency=critical", "--",
			title, escape(body))
	case strings.TrimSpace(*command) == "":
		return
	default:
		c = exec.CommandContext(ctx, "sh", "-c", *command)
		c.Env = append(os.Environ(), "SPOT_NOTIFY_TITLE="+title, "SPOT_NOTIFY_BODY="+body)
	}
	c.WaitDelay = time.Second
	_ = c.Run()
}

// escape keeps a notification daemon from reading the text as markup: error
// messages quote config text, which may contain anything.
var escape = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace
