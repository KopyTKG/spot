package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The example's dialog script, with everything it calls faked: nothing is shown
// and the clipboard is not touched.
func TestSocketsScript(t *testing.T) {
	d := t.TempDir()
	fake := func(name, body string) {
		if err := os.WriteFile(filepath.Join(d, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	read := func(name string) string { b, _ := os.ReadFile(filepath.Join(d, name)); return string(b) }
	fake("notify-send", `printf '%s|' "$@" >> `+d+`/notified`)
	fake("wl-copy", `cat >> `+d+`/copied`)
	for name, c := range map[string]struct {
		get, dialog                       string
		exit                              int
		shown, copied, says, notification string
	}{
		"get fails":  {`echo 'spot: sockets is not enabled' >&2; exit 3`, `echo x`, 1, "", "", "not enabled", "not enabled"},
		"a choice":   {`printf 'tcp  LISTEN    127.0.0.1:22  0.0.0.0:*\t127.0.0.1:22\t\n'`, `head -1`, 0, "tcp  LISTEN    127.0.0.1:22  0.0.0.0:*\n", "127.0.0.1:22", "", ""},
		"cancelled":  {`printf 'a b c d\ta\t\n'`, `cat >/dev/null; exit 1`, 0, "a b c d\n", "", "", ""},
		"old values": {`echo 'spot: this is from 3s ago' >&2; printf 'a b c d\ta\t\n'`, `cat >/dev/null; exit 1`, 0, "a b c d\n", "", "", ""},
	} {
		for _, f := range []string{"notified", "copied", "shown"} {
			os.Remove(filepath.Join(d, f))
		}
		fake("spot", `if [ "$1" = get ]; then `+c.get+`; else tee `+d+`/shown | { `+c.dialog+`; }; fi`)
		cmd := exec.Command("examples/mods/sockets/sockets.sh")
		cmd.Env = []string{"PATH=" + d + ":/usr/bin", "SPOT=" + d + "/spot"}
		out, err := cmd.CombinedOutput()
		code := cmd.ProcessState.ExitCode()
		if code != c.exit || read("shown") != c.shown || read("copied") != c.copied || !strings.Contains(string(out), c.says) {
			t.Errorf("%s: exit %d (%v), shown %q, copied %q, said %q", name, code, err, read("shown"), read("copied"), out)
		}
		if n := read("notified"); c.notification == "" && n != "" || c.notification != "" && !strings.Contains(n, c.notification) {
			t.Errorf("%s: notified %q", name, n)
		}
	}
}
