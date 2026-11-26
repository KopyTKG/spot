package config

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
	Socket string  `toml:"socket"` // listen here (SPOT_SOCKET wins)
	Log    string  `toml:"log"`    // stdout/stderr of a daemon a client starts
	Grace  *int    `toml:"grace"`  // ms the surface waits for a follow-up dialog (default graceMs)
	Chain  *int    `toml:"chain"`  // ms an answered --chain dialog stays up (default chainMs)
	Notify *string `toml:"notify"` // command reporting a failure nobody would see; "" = off (default notify-send)
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

var Daemon = sync.OnceValue(loadDaemonConfig)

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
func (c *DaemonConfig) GraceMillis() int { return orDefault(c.Grace, DefaultGraceMs) }

func (c *DaemonConfig) ChainMillis() int { return orDefault(c.Chain, DefaultChainMs) }

func orDefault(p *int, def int) int {
	if p == nil {
		return def
	}
	return *p
}

// SocketPath is where the daemon listens: SPOT_SOCKET (tests, or a second
// daemon), then daemon.toml, then the runtime directory.
func SocketPath() string {
	if p := os.Getenv("SPOT_SOCKET"); p != "" {
		return p
	}
	if p := Daemon().Socket; p != "" {
		return p
	}
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "spot.sock")
	}
	return filepath.Join(os.TempDir(), "spot-"+strconv.Itoa(os.Getuid())+".sock")
}

// LogPath is where a daemon started by a client writes its output, or "" for
// nowhere. A daemon started by systemd logs to the journal instead.
func LogPath() string {
	if p := Daemon().Log; p != "" {
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

// The daemon owns one window and shows one dialog in it at a time. The
// scheduler decides which: it knows nothing about GTK, so it can be tested
// without a display. A presenter does the drawing.
//
//	cur    the dialog on screen
//	stack  dialogs under cur, opened with --sub; the top one returns when cur closes
//	queue  dialogs waiting their turn (plain requests that arrived while cur was up)
//
// Whenever the next dialog is known the window's content is swapped in place:
// the layer surface stays mapped, so nothing re-maps and no fade restarts.
// When nothing is known yet the window lingers briefly, in case the caller is
// a shell script about to ask for the next menu (see idle).

const (
	// Defaults; daemon.toml can change both (grace, chain).
	//
	// graceMs: after a dialog closes the surface stays mapped, with the dialog
	// hidden, this long. A follow-up request inside it takes over the surface;
	// otherwise the surface is released.
	DefaultGraceMs = 120
	// chainMs: with --chain the closed dialog stays visible instead, for up to
	// this long, so a menu hands over to the next without a gap.
	DefaultChainMs = 1500
)
