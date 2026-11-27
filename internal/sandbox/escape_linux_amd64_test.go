package sandbox

// These tests restrict real processes the way a mod host is restricted and then
// try to get out. The test binary is its own helper, and must stay a static
// program (no package net, no cgo): without programs a policy grants nothing
// but the binary itself.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

type probeEnv struct {
	Policy    Policy
	Dir       string
	Good, Bad int // TCP ports with a listener
}

func TestMain(m *testing.M) {
	switch os.Getenv("SPOT_PROBE") {
	case "restrict":
		var e probeEnv
		json.Unmarshal([]byte(os.Getenv("SPOT_PROBE_ENV")), &e)
		if err := Restrict(e.Policy); err != nil {
			os.Stdout.WriteString(`{"error":"` + err.Error() + `"}`)
			os.Exit(1)
		}
		next := os.Getenv("SPOT_PROBE_NEXT")
		if next == "" {
			next = "probe"
		}
		os.Setenv("SPOT_PROBE", next)
		syscall.Exec("/proc/self/exe", os.Args, os.Environ())
		os.Exit(3)
	case "probe":
		var e probeEnv
		json.Unmarshal([]byte(os.Getenv("SPOT_PROBE_ENV")), &e)
		json.NewEncoder(os.Stdout).Encode(map[string]any{"got": probes(e)})
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func tcpSocket(proto int) (int, error) {
	return unix.Socket(unix.AF_INET, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, proto)
}

func probes(e probeEnv) map[string]bool {
	ok := func(err error) bool { return err == nil }
	read := func(p string) bool { _, err := os.ReadFile(p); return ok(err) }
	write := func(p string) bool { return ok(os.WriteFile(p, []byte("x"), 0o600)) }
	sock := func(domain, typ, proto int) bool {
		fd, err := unix.Socket(domain, typ, proto)
		if err == nil {
			unix.Close(fd)
		}
		return ok(err)
	}
	connect := func(port int) bool {
		fd, err := tcpSocket(0)
		if err != nil {
			return false
		}
		defer unix.Close(fd)
		return ok(unix.Connect(fd, &unix.SockaddrInet4{Port: port, Addr: [4]byte{127, 0, 0, 1}}))
	}
	call := func(nr uintptr, a ...uintptr) bool {
		a = append(a, 0, 0, 0, 0, 0, 0)
		_, _, errno := unix.Syscall6(nr, a[0], a[1], a[2], a[3], a[4], a[5])
		return errno == 0
	}

	got := map[string]bool{
		"verified":                    ok(Verify()),
		"read a granted file":         read(e.Dir + "/readable/f"),
		"read outside":                read(e.Dir + "/outside/f"),
		"write in a writable folder":  write(e.Dir + "/writable/new"),
		"write in a readable folder":  write(e.Dir + "/readable/new"),
		"write outside":               write(e.Dir + "/outside/new"),
		"change a mode outside":       ok(os.Chmod(e.Dir+"/outside/f", 0o644)),
		"signal the parent":           ok(unix.Kill(os.Getppid(), 0)),
		"run a listed program":        ok(exec.Command("/usr/bin/true").Run()),
		"run an unlisted program":     ok(exec.Command("/usr/bin/cat", "/dev/null").Run()),
		"make a TCP socket":           sock(unix.AF_INET, unix.SOCK_STREAM, 0),
		"connect to the granted port": connect(e.Good),
		"connect to another port":     connect(e.Bad),
		"Multipath TCP":               sock(unix.AF_INET, unix.SOCK_STREAM, unix.IPPROTO_MPTCP),
		"ICMP socket":                 sock(unix.AF_INET, unix.SOCK_DGRAM, unix.IPPROTO_ICMP),
		"VM socket":                   sock(unix.AF_VSOCK, unix.SOCK_STREAM, 0),
		"netlink diag":                sock(unix.AF_NETLINK, unix.SOCK_RAW, 4),
		"netlink route":               sock(unix.AF_NETLINK, unix.SOCK_RAW, 0),
		"shared memory":               call(unix.SYS_SHMGET, 0, 4096, 0o1600),
		"add a key":                   func() bool { _, err := unix.AddKey("user", "spot-test", []byte("x"), -3); return ok(err) }(),
		"a program from memory":       func() bool { _, err := unix.MemfdCreate("t", 0); return ok(err) }(),
		"a user namespace":            ok(unix.Unshare(unix.CLONE_NEWUSER)),
		"io_uring":                    call(unix.SYS_IO_URING_SETUP, 4, uintptr(unsafe.Pointer(new([256]byte)))),
	}
	if len(e.Policy.Read) > 1 { // the /proc and /sys cases
		battery, _ := filepath.Glob("/sys/class/power_supply/*/type")
		got["read /proc/net/dev"] = read("/proc/net/dev")
		got["read a device through /sys/class"] = len(battery) == 0 || read(battery[0])
		got["read another /proc file"] = read("/proc/meminfo")
		got["read elsewhere in /sys"] = read("/sys/kernel/notes")
	}
	got["listen without bind"] = func() bool {
		fd, err := tcpSocket(0)
		if err != nil {
			return false
		}
		defer unix.Close(fd)
		return ok(unix.Listen(fd, 1))
	}()

	// Every thread of the process is restricted, not just the one that was.
	var wg sync.WaitGroup
	var mu sync.Mutex
	all := true
	for range 64 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			runtime.LockOSThread()
			if Verify() != nil {
				mu.Lock()
				all = false
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	got["every thread verified"] = all
	return got
}

func listener(t *testing.T) int {
	fd, err := tcpSocket(0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { unix.Close(fd) })
	if err := unix.Bind(fd, &unix.SockaddrInet4{Addr: [4]byte{127, 0, 0, 1}}); err != nil {
		t.Fatal(err)
	}
	if err := unix.Listen(fd, 16); err != nil {
		t.Fatal(err)
	}
	sa, err := unix.Getsockname(fd)
	if err != nil {
		t.Fatal(err)
	}
	return sa.(*unix.SockaddrInet4).Port
}

func sandboxed(t *testing.T, change func(*Policy)) map[string]bool {
	if ABI() < MinABI {
		t.Skipf("Landlock ABI %d here, %d needed", ABI(), MinABI)
	}
	d := t.TempDir()
	for _, sub := range []string{"readable", "writable", "outside"} {
		os.Mkdir(filepath.Join(d, sub), 0o700)
		os.WriteFile(filepath.Join(d, sub, "f"), []byte("x"), 0o600)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	e := probeEnv{Dir: d, Good: listener(t), Bad: listener(t)}
	e.Policy = Policy{Self: self, Programs: []string{"/usr/bin/true"}, Read: []string{d + "/readable"}, Write: []string{d + "/writable"}}
	change(&e.Policy)
	if len(e.Policy.Ports) > 0 {
		e.Policy.Ports = []uint16{uint16(e.Good)}
	}
	js, _ := json.Marshal(e)
	c := exec.Command(self)
	c.Env = []string{"SPOT_PROBE=restrict", "SPOT_PROBE_ENV=" + string(js)}
	out, err := c.Output()
	var r struct {
		Got   map[string]bool
		Error string
	}
	if jerr := json.Unmarshal(out, &r); err != nil || jerr != nil || r.Error != "" {
		t.Fatalf("helper: %v %v %q %s", err, jerr, r.Error, out)
	}
	return r.Got
}

func expect(t *testing.T, got map[string]bool, allowed ...string) {
	t.Helper()
	want := map[string]bool{}
	for _, a := range allowed {
		want[a] = true
		if _, known := got[a]; !known {
			t.Errorf("no probe %q", a)
		}
	}
	for what, did := range got {
		if did != want[what] {
			t.Errorf("%-30s possible = %v, want %v", what, did, want[what])
		}
	}
}

var always = []string{"verified", "every thread verified", "read a granted file", "write in a writable folder", "run a listed program"}

func TestNoEscapeWithoutNetwork(t *testing.T) {
	expect(t, sandboxed(t, func(*Policy) {}), always...)
}

func TestNoEscapeWithNetworkAndNetlink(t *testing.T) {
	got := sandboxed(t, func(p *Policy) { p.Ports = []uint16{1}; p.Netlink = []string{"diag"} })
	expect(t, got, append([]string{"make a TCP socket", "connect to the granted port", "netlink diag"}, always...)...)
}

// /proc/net leads into the host's own folder, and the entries of a /sys/class
// folder are links to the real devices elsewhere under /sys.
func TestProcNetAndSysClass(t *testing.T) {
	got := sandboxed(t, func(p *Policy) { p.Read = append(p.Read, "/proc/net", "/sys/class/power_supply") })
	expect(t, got, append([]string{"read /proc/net/dev", "read a device through /sys/class"}, always...)...)
}

func TestUnsandboxedIsNotVerified(t *testing.T) {
	if Verify() == nil {
		t.Error("an unrestricted process passed")
	}
}
