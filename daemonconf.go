package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/pelletier/go-toml/v2"
)

// DaemonConfig is ~/.config/spot/daemon.toml. Every key is optional. The
// daemon reads it once, at start; clients read it on every call, because they
// need the socket and log paths too.
type DaemonConfig struct {
	Socket string `toml:"socket"` // listen here (SPOT_SOCKET wins)
	Log    string `toml:"log"`    // stdout/stderr of a daemon a client starts
	Grace  *int   `toml:"grace"`  // ms the surface waits for a follow-up dialog (default graceMs)
	Chain  *int   `toml:"chain"`  // ms an answered --chain dialog stays up (default chainMs)
}

// daemonConfigPath is where daemon.toml is looked for.
func daemonConfigPath() string {
	dir := configDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "daemon.toml")
}

// parseDaemonConfig decodes daemon.toml. Unknown keys are errors: with four
// keys a typo is far more likely than a key from a newer version. Relative
// paths resolve against base, the file's directory.
func parseDaemonConfig(data []byte, base string) (*DaemonConfig, error) {
	var c DaemonConfig
	d := toml.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		var strict *toml.StrictMissingError
		if errors.As(err, &strict) && len(strict.Errors) > 0 {
			return nil, fmt.Errorf("unknown key %q", strings.Join(strict.Errors[0].Key(), "."))
		}
		return nil, err
	}
	for name, ms := range map[string]*int{"grace": c.Grace, "chain": c.Chain} {
		if ms != nil && (*ms < 0 || *ms > 60000) {
			return nil, fmt.Errorf("%s = %d: must be 0-60000 (ms)", name, *ms)
		}
	}
	c.Socket = expandPath(c.Socket, base)
	c.Log = expandPath(c.Log, base)
	return &c, nil
}

// expandPath expands $VARS and a leading ~/ and makes p absolute against base.
func expandPath(p, base string) string {
	if p == "" {
		return ""
	}
	p = os.ExpandEnv(p)
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, p[1:])
		}
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(base, p)
	}
	return p
}

var daemonConf = sync.OnceValue(loadDaemonConfig)

// loadDaemonConfig reads daemon.toml. A missing file means defaults; a broken
// one is reported and also means defaults, so a typo there never stops a
// dialog from showing.
func loadDaemonConfig() *DaemonConfig {
	path := daemonConfigPath()
	if path == "" {
		return &DaemonConfig{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "spot: %v (using daemon defaults)\n", err)
		}
		return &DaemonConfig{}
	}
	c, err := parseDaemonConfig(data, filepath.Dir(path))
	if err != nil {
		fmt.Fprintf(os.Stderr, "spot: %s: %v (using daemon defaults)\n", path, err)
		return &DaemonConfig{}
	}
	return c
}

// graceMs and chainMs with daemon.toml applied.
func (c *DaemonConfig) graceMs() int { return orDefault(c.Grace, graceMs) }
func (c *DaemonConfig) chainMs() int { return orDefault(c.Chain, chainMs) }

func orDefault(p *int, def int) int {
	if p == nil {
		return def
	}
	return *p
}

// socketPath is where the daemon listens: SPOT_SOCKET (tests, or a second
// daemon), then daemon.toml, then the runtime directory.
func socketPath() string {
	if p := os.Getenv("SPOT_SOCKET"); p != "" {
		return p
	}
	if p := daemonConf().Socket; p != "" {
		return p
	}
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "spot.sock")
	}
	return filepath.Join(os.TempDir(), "spot-"+strconv.Itoa(os.Getuid())+".sock")
}

// logPath is where a daemon started by a client writes its output, or "" for
// nowhere. A daemon started by systemd logs to the journal instead.
func logPath() string {
	if p := daemonConf().Log; p != "" {
		return p
	}
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		return filepath.Join(d, "spot", "daemon.log")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".local", "state", "spot", "daemon.log")
	}
	return ""
}
