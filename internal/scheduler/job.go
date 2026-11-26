package scheduler

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gitlab.com/KopyTKG/spot/internal/config"
	"gitlab.com/KopyTKG/spot/internal/protocol"
)

// BuildJob reads the config named by req and prepares it for display. It
// touches no GTK, so it may run on any goroutine.
func BuildJob(req protocol.Request) (*Job, error) {
	data, err := os.ReadFile(req.Config)
	if err != nil {
		return nil, err
	}
	cfg, err := config.ParseConfig(data, protocol.EnvLookup(req.Env))
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
	return &Job{req: req, Cfg: cfg, CSS: css, Env: req.Env, Dir: req.Dir, Back: cfg.BackSpec(req.Back), Reply: make(chan protocol.Response, 1)}, nil
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
