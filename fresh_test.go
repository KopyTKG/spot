package main

import (
	"encoding/json"
	"io"
	"net"
	"os"
	"testing"

	"gitlab.com/KopyTKG/spot/internal/protocol"
)

// askOf runs `spot <verb> args` against a daemon that answers resp.
func askOf(t *testing.T, resp protocol.Response, verb string, args ...string) (string, int) {
	t.Helper()
	dir := t.TempDir()
	os.Chmod(dir, 0o700)
	l, err := net.Listen("unix", dir+"/s")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	t.Setenv("SPOT_SOCKET", dir+"/s")
	go func() {
		c, err := l.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		var req protocol.Request
		_ = json.NewDecoder(c).Decode(&req)
		_ = json.NewEncoder(c).Encode(resp)
	}()
	out, errs := os.Stdout, os.Stderr
	r, w, _ := os.Pipe()
	null, _ := os.Open(os.DevNull)
	os.Stdout, os.Stderr = w, null
	code := askMod(verb, args)
	os.Stdout, os.Stderr = out, errs
	w.Close()
	b, _ := io.ReadAll(r)
	return string(b), code
}

// get --fresh takes only what the mod says now: a script must not mistake an
// old value, or none, for the answer.
func TestGetFresh(t *testing.T) {
	now := protocol.Response{Result: json.RawMessage(`"on"`)}
	old := protocol.Response{Result: json.RawMessage(`"on"`), Stderr: "spot: this is from 5m0s ago: broken\n"}
	for name, c := range map[string]struct {
		resp protocol.Response
		args []string
		out  string
		code int
	}{
		"a value of now":          {now, []string{"--fresh", "m.v"}, "on\n", 0},
		"flags after the name":    {now, []string{"m.v", "--json", "--fresh"}, "\"on\"\n", 0},
		"old, asked as before":    {old, []string{"m.v"}, "on\n", 0},
		"old, refused":            {old, []string{"--fresh", "m.v"}, "", protocol.ExitError},
		"old, refused, as JSON":   {old, []string{"--fresh", "--json", "m.v"}, "", protocol.ExitError},
		"a failure stays its own": {protocol.Response{Code: protocol.ExitUsage, Stderr: "spot: no\n"}, []string{"--fresh", "m.v"}, "", protocol.ExitUsage},
	} {
		if out, code := askOf(t, c.resp, protocol.OpGet, c.args...); out != c.out || code != c.code {
			t.Errorf("%s: %q %d, want %q %d", name, out, code, c.out, c.code)
		}
	}
	// A bar shows that something is wrong instead of the old value.
	out, code := askOf(t, old, protocol.OpGet, "--fresh", "--waybar", "m.v")
	var bar map[string]string
	if json.Unmarshal([]byte(out), &bar) != nil || code == 0 || bar["class"] != "error" {
		t.Errorf("for a bar: %q %d", out, code)
	}
	// Only get has it: a watch says by itself what it cannot read.
	if code := askMod(protocol.OpWatch, []string{"--fresh", "m.v"}); code != protocol.ExitUsage {
		t.Errorf("watch --fresh: %d", code)
	}
}
