package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseDaemonConfig(t *testing.T) {
	t.Setenv("HOME", "/home/me")
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	c, err := parseDaemonConfig([]byte(`
socket = "$XDG_RUNTIME_DIR/spot-work.sock"
log    = "~/spot.log"
grace  = 0
chain  = 3000
`), "/home/me/.config/spot")
	if err != nil {
		t.Fatal(err)
	}
	if c.Socket != "/run/user/1000/spot-work.sock" {
		t.Errorf("socket = %q", c.Socket)
	}
	if c.Log != "/home/me/spot.log" {
		t.Errorf("log = %q", c.Log)
	}
	if c.graceMs() != 0 || c.chainMs() != 3000 {
		t.Errorf("grace %d chain %d, want 0 3000", c.graceMs(), c.chainMs())
	}
}

func TestDaemonConfigDefaults(t *testing.T) {
	c, err := parseDaemonConfig(nil, "/x")
	if err != nil {
		t.Fatal(err)
	}
	if c.graceMs() != graceMs || c.chainMs() != chainMs || c.Socket != "" || c.Log != "" {
		t.Errorf("empty file: %+v", c)
	}
}

func TestDaemonConfigRelativePath(t *testing.T) {
	c, err := parseDaemonConfig([]byte(`log = "logs/daemon.log"`), "/etc/spot")
	if err != nil {
		t.Fatal(err)
	}
	if c.Log != "/etc/spot/logs/daemon.log" {
		t.Errorf("log = %q", c.Log)
	}
}

func TestDaemonConfigErrors(t *testing.T) {
	cases := map[string]string{
		`grace = -1`:     "grace",
		`chain = 100000`: "chain",
		`grase = 50`:     "grase", // typo: unknown keys are errors
		`grace = "50"`:   "",
	}
	for in, want := range cases {
		_, err := parseDaemonConfig([]byte(in), "/x")
		if err == nil {
			t.Errorf("%s: no error", in)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%s: error %q does not mention %q", in, err, want)
		}
	}
}

func TestSocketPathPrecedence(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	os.MkdirAll(filepath.Join(dir, "spot"), 0o755)
	os.WriteFile(filepath.Join(dir, "spot", "daemon.toml"), []byte(`socket = "/tmp/from-file.sock"`), 0o644)

	c := loadDaemonConfig()
	if c.Socket != "/tmp/from-file.sock" {
		t.Fatalf("loaded socket %q", c.Socket)
	}
	// socketPath reads the cached config; SPOT_SOCKET must win over any file.
	t.Setenv("SPOT_SOCKET", "/tmp/env.sock")
	if got := socketPath(); got != "/tmp/env.sock" {
		t.Errorf("SPOT_SOCKET ignored: %q", got)
	}
}

func TestBrokenDaemonConfigFallsBackToDefaults(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	os.MkdirAll(filepath.Join(dir, "spot"), 0o755)
	os.WriteFile(filepath.Join(dir, "spot", "daemon.toml"), []byte(`grace = [`), 0o644)
	if c := loadDaemonConfig(); c.graceMs() != graceMs || c.Socket != "" {
		t.Errorf("broken file should give defaults, got %+v", c)
	}
}
