package notify

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fake puts a notify-send that records its arguments first in PATH.
func fake(t *testing.T) (log string) {
	t.Helper()
	dir := t.TempDir()
	log = filepath.Join(dir, "log")
	script := "#!/bin/sh\nfor a in \"$@\"; do echo \"$a\"; done >> " + log + "\n"
	if err := os.WriteFile(filepath.Join(dir, "notify-send"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	return log
}

func read(p string) string { b, _ := os.ReadFile(p); return string(b) }

func TestDefaultIsNotifySendWithEscapedBody(t *testing.T) {
	log := fake(t)
	send(nil, "spot: x.toml did not open", "-bad <value> & more")
	want := "--app-name=spot\n--urgency=critical\n--\nspot: x.toml did not open\n-bad &lt;value&gt; &amp; more\n"
	if got := read(log); got != want {
		t.Errorf("notify-send got %q, want %q", got, want)
	}
}

func TestOwnCommandGetsTheTextInTheEnvironment(t *testing.T) {
	log := fake(t)
	out := filepath.Join(t.TempDir(), "out")
	cmd := `printf '%s|%s' "$SPOT_NOTIFY_TITLE" "$SPOT_NOTIFY_BODY" > ` + out
	send(&cmd, "T $(false)", "<b> `x`")
	if got := read(out); got != "T $(false)|<b> `x`" {
		t.Errorf("got %q", got)
	}
	if read(log) != "" {
		t.Error("notify-send ran as well")
	}
}

func TestEmptyCommandAndEnvTurnItOff(t *testing.T) {
	log := fake(t)
	off := " "
	send(&off, "t", "b")
	t.Setenv(EnvOff, "1")
	Failure(nil, "t", "b")
	if read(log) != "" {
		t.Errorf("notified: %q", read(log))
	}
}

func TestAHangingCommandDoesNotHoldSpot(t *testing.T) {
	cmd := "sleep 30"
	start := time.Now()
	send(&cmd, "t", "b")
	if d := time.Since(start); d > limit+2*time.Second {
		t.Errorf("took %v", d)
	}
}

func TestPlainFileIsNotATerminal(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "f"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	null, _ := os.Open(os.DevNull)
	defer null.Close()
	if IsTerminal(f) || IsTerminal(null) {
		t.Error("a file or /dev/null taken for a terminal")
	}
}

// notify-send turns backslash codes in a body into what they name.
func TestBackslashesArriveAsWritten(t *testing.T) {
	if got := escape(`a "x\n b" <i>`); got != `a "x\\n b" &lt;i&gt;` {
		t.Errorf("%s", got)
	}
}
