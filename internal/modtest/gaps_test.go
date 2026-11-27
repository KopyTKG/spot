package modtest

import (
	"net"
	"os"
	"strings"
	"testing"
)

// The daemon's socket makes things happen. No mod, and no program of a mod, may
// reach it (S7).
func TestAProgramCannotReachTheDaemon(t *testing.T) {
	w := newWorld(t)
	if err := os.MkdirAll(w.dir+"/run", 0o700); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", w.dir+"/run/spot.sock")
	if err != nil {
		t.Skipf("no socket here: %v", err)
	}
	defer ln.Close()
	abstract, err := net.Listen("unix", "@spot-test-"+w.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer abstract.Close()
	// Unsandboxed both can be reached: otherwise this proves nothing.
	for _, addr := range []string{w.dir + "/run/spot.sock", "@spot-test-" + w.dir} {
		c, err := net.Dial("unix", addr)
		if err != nil {
			t.Fatal(err)
		}
		c.Close()
	}
	l := w.must("[permissions.run]\npython3='r'\n", `
spot.on("connect", function(a)
  local code = "import socket,sys; a=sys.argv[1]; a=chr(0)+a[1:] if a[0]=='@' else a; s=socket.socket(socket.AF_UNIX); s.connect(a); print('connected')"
  local r = spot.run({ "python3", "-I", "-c", code, a[1] })
  return r.stdout ~= "" and "connected" or r.stderr:match("[%a]+Error[^\n]*")
end)`)
	for name, addr := range map[string]string{"the socket": w.dir + "/run/spot.sock", "an abstract socket": "@spot-test-" + w.dir} {
		got, err := l.call("connect", addr)
		if err != nil || !strings.Contains(got, "PermissionError") {
			t.Errorf("%s: %s, %v", name, got, err)
		}
	}
}

func TestTheRestOfTheRefusals(t *testing.T) {
	w := newWorld(t)
	os.Symlink(w.dir+"/outside", w.dir+"/readable/dir-out")
	l := w.must(`
[permissions.run]
env = "r"
pwd = "r"
cat = "r"
[permissions.read]
"~/readable" = "r"
"/proc/net" = "r"
"/sys/class/net" = "r"
[permissions.setenv]
LANG = "r"
`, `
spot.on("setenv", function(a) return spot.run({ "env" }, { env = { [a[1]] = "x" } }).status end)
spot.on("dir", function(a) return spot.run({ "pwd" }, { dir = a[1] }).stdout end)
spot.on("read", function(a) return spot.read(a[1]) end)
spot.on("catnet", function() return spot.run({ "cat", "/proc/net/dev" }).status end)
`)
	for _, name := range []string{"GIT_SSH_COMMAND", "HOME", "BASH_ENV", "LD_LIBRARY_PATH"} {
		l.expect("setting "+name, "setenv", "!no permission", name)
	}
	l.expect("a folder reached by a link out", "dir", "!no permission", "~/readable/dir-out")
	l.expect("a device through its class", "read", `"65536\n"`, "/sys/class/net/lo/mtu")
	l.expect("elsewhere in /sys", "read", "!no permission", "/sys/kernel/uevent_seqnum")
	// The host's own view only: a program has a folder of its own.
	l.expect("/proc/net from a program", "catnet", `1`)
	if got, err := l.call("read", "/proc/net/dev"); err != nil || !strings.Contains(got, "lo:") {
		t.Errorf("/proc/net from spot.read: %.60s, %v", got, err)
	}
	if _, err := w.start("[permissions.setenv]\nHOME='x'\n", "return 1"); err == nil {
		t.Error("a mod asking to set HOME started")
	}
}
