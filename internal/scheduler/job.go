package scheduler

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"gitlab.com/KopyTKG/spot/internal/approval"
	"gitlab.com/KopyTKG/spot/internal/capture"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"gitlab.com/KopyTKG/spot/internal/config"
	"gitlab.com/KopyTKG/spot/internal/protocol"
)

// BuildJob reads the config named by req and prepares it for display. It
// touches no GTK, so it may run on any goroutine.
func BuildJob(req protocol.Request, mods config.ModSource) (*Job, error) {
	data, err := config.ReadFile(req.Config)
	if err != nil {
		return nil, err
	}
	cfg, err := config.ParseConfigAt(req.Config, data, protocol.EnvLookup(req.Env))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", req.Config, err)
	}

	if req.Page != "" && cfg.Pages[req.Page] == nil {
		return nil, fmt.Errorf("%s has no page %q", req.Config, req.Page)
	}

	for i := range cfg.Items {
		if cfg.Items[i].Type == "list" && cfg.Items[i].Stdin {
			cfg.Items[i].Options = stdinLines(req.Stdin)
		}
	}

	if cfg.UsesMods() {
		// A dialog that can call mods must not be someone else's to change (S17).
		if err := approval.Trusted(req.Config); err != nil {
			return nil, fmt.Errorf("a dialog that uses mods: %w", err)
		}
		for i := range cfg.Items {
			fetch(&cfg.Items[i], req, mods)
		}
	}

	css := ""
	if cfg.Style != "" {
		css = cfg.Style
		if !filepath.IsAbs(css) {
			css = filepath.Join(filepath.Dir(req.Config), css)
		}
	}
	return &Job{req: req, Config: req.Config, Page: req.Page, Mods: mods, Cfg: cfg, CSS: css, Env: req.Env, Dir: req.Dir, Back: cfg.BackSpec(req.Back), Reply: make(chan protocol.Response, 1)}, nil
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

// fetch gets what an item shows from a mod, before the dialog is drawn. A
// failure does not keep the dialog from opening: it is said under the item.
func fetch(it *config.Item, req protocol.Request, mods config.ModSource) {
	rows := func(raw []byte, err error) {
		if err == nil {
			it.Rows, err = config.Rows(raw)
		}
		if err != nil {
			it.Note = err.Error()
		}
	}
	switch {
	case it.Type != "list" || it.From == "" && it.Parse == "":
	case mods == nil:
		it.Note = "mods are not available here"
	case it.From != "":
		raw, warning, err := mods.Get(it.From)
		rows(raw, err)
		if warning != "" {
			// Old rows may be shown, an old switch position never.
			for i := range it.Rows {
				it.Rows[i].State = nil
			}
			it.Note = warning
		}
	default:
		out, err := output(it.Run, req.Env, req.Dir, it.CommandTimeout())
		if err != nil {
			it.Note = fmt.Sprintf("%s: %v", it.Run, err)
			return
		}
		rows(mods.Parse(it.Parse, out))
	}
}

// output is what the dialog's own command prints. It runs as you, like state
// and exec; only the parser that reads it is sandboxed.
func output(cmd string, env []string, dir string, limit time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	var out, errs capture.Buffer
	c := exec.CommandContext(ctx, "sh", "-c", cmd)
	c.Env, c.Dir = env, dir
	c.Stdout, c.Stderr = &out, &errs
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error { return syscall.Kill(-c.Process.Pid, syscall.SIGKILL) }
	c.WaitDelay = 300 * time.Millisecond
	err := c.Run()
	switch {
	case ctx.Err() != nil:
		return nil, errors.New("took too long")
	case err != nil && !errors.Is(err, exec.ErrWaitDelay):
		if msg := strings.TrimSpace(errs.String()); msg != "" {
			err = errors.New(msg)
		}
		return nil, err
	}
	return out.Bytes(), nil
}
