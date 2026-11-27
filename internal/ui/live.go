package ui

// Live controls apply at once and leave the dialog open. Each may have a state
// command (what is the value now) and an on_change command (make it this). The
// state is read when the dialog opens and again after every change, of any
// control: one setting may well change another.

import (
	"context"
	"errors"
	"os/exec"
	"slices"
	"strings"
	"syscall"
	"time"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"

	"gitlab.com/KopyTKG/spot/internal/capture"
	"gitlab.com/KopyTKG/spot/internal/config"
	"gitlab.com/KopyTKG/spot/internal/launcher"
)

// fill puts shell-quoted values into a command template.
func fill(tmpl, id, key, value string) string {
	return strings.NewReplacer(
		"{id}", launcher.ShellQuote(id),
		"{key}", launcher.ShellQuote(key),
		"{value}", launcher.ShellQuote(value),
	).Replace(tmpl)
}

// runBounded is runCommand with a time limit; on expiry the whole process group
// is killed.
func runBounded(cmd string, env []string, dir string, limit time.Duration) (stdout, stderr string, status int) {
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	var so, se capture.Buffer
	c := exec.CommandContext(ctx, "sh", "-c", cmd)
	c.Env, c.Dir = env, dir
	c.Stdout, c.Stderr = &so, &se
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error { return syscall.Kill(-c.Process.Pid, syscall.SIGKILL) }
	c.WaitDelay = 300 * time.Millisecond
	err := c.Run()
	var ee *exec.ExitError
	switch {
	case ctx.Err() != nil:
		return so.String(), "command timed out after " + limit.String() + "\n", 124
	case err == nil || errors.Is(err, exec.ErrWaitDelay): // it left something running behind
		return so.String(), se.String(), 0
	case errors.As(err, &ee):
		return so.String(), se.String(), ee.ExitCode()
	}
	return so.String(), err.Error() + "\n", 127
}

// command runs a control's command off the main loop and hands the result back
// on it, unless the dialog has ended meanwhile.
func (b *builder) command(cmd, value string, limit time.Duration, done func(stdout, stderr string, status int)) {
	env := append(slices.Clip(b.env), "SPOT_VALUE="+value)
	go func() {
		stdout, stderr, status := runBounded(cmd, env, b.dir, limit)
		coreglib.IdleAdd(func() {
			if !b.d.done {
				done(stdout, stderr, status)
			}
		})
	}()
}

// report shows a failed command under the dialog; success clears it.
func (b *builder) report(stderr string, status int) {
	if status == 0 {
		b.errLabel.SetVisible(false)
		return
	}
	msg := strings.TrimSpace(stderr)
	if msg == "" {
		msg = "command failed"
	}
	b.errLabel.SetText(msg)
	b.errLabel.SetVisible(true)
}

// refreshAll re-reads every control's state.
func (b *builder) refreshAll() {
	for _, f := range b.refreshers {
		f()
	}
}

// control is the logic of one switch, segmented control or slider. The widget
// only has to show a value and call set when the user picks one.
type control struct {
	b     *builder
	item  config.Item
	show  func(value string)              // display without reporting a change
	read  func(out string) (string, bool) // a state command's output as a value
	busy  func(bool)                      // mark the widget while a change runs
	value string

	changing bool
	pending  *string     // picked while a change was still running
	reads    int         // only the newest state read counts
	holding  func() bool // the user is still moving it (a slider); nil = never
}

// set applies a value the user picked.
func (c *control) set(v string) {
	c.reads++ // a state read from before this pick is out of date
	if c.changing {
		c.pending = &v
		return
	}
	if v == c.value {
		return
	}
	prev := c.value
	c.value = v
	c.show(v)
	if c.item.OnChange == "" {
		return
	}
	c.changing = true
	c.busy(true)
	cmd := fill(c.item.OnChange, c.item.ID, c.item.ID, v)
	c.b.command(cmd, v, c.item.CommandTimeout(), func(stdout, stderr string, status int) {
		c.changing = false
		c.busy(false)
		c.b.report(stderr, status)
		switch {
		case status != 0:
			c.value, c.pending = prev, nil // it did not happen: say so
			c.show(prev)
		case c.item.State == "":
			if got, ok := c.read(stdout); ok { // on_change may print what it settled on
				c.value = got
				c.show(got)
			}
		}
		if next := c.pending; next != nil {
			c.pending = nil
			c.set(*next)
			return
		}
		c.b.refreshAll()
	})
}

// refresh reads the state command, if there is one.
func (c *control) refresh() {
	if c.item.State == "" || c.changing || c.holding != nil && c.holding() {
		return
	}
	c.reads++
	n := c.reads
	cmd := fill(c.item.State, c.item.ID, c.item.ID, c.value)
	c.b.command(cmd, c.value, c.item.CommandTimeout(), func(stdout, stderr string, status int) {
		if n != c.reads || c.changing {
			return
		}
		if status != 0 {
			c.b.report(stderr, status)
			return
		}
		if v, ok := c.read(stdout); ok {
			c.value = v
			c.show(v)
		}
	})
}
