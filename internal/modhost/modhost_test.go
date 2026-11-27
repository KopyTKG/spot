package modhost

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"gitlab.com/KopyTKG/spot/internal/sandbox"
)

func buildHost(t *testing.T) string {
	if sandbox.ABI() < sandbox.MinABI {
		t.Skipf("Landlock ABI %d here, %d needed", sandbox.ABI(), sandbox.MinABI)
	}
	bin := filepath.Join(t.TempDir(), "spot-mod-host")
	c := exec.Command("go", "build", "-o", bin, "gitlab.com/KopyTKG/spot/cmd/spot-mod-host")
	c.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("building the host: %v\n%s", err, out)
	}
	return bin
}

func status(t *testing.T, pid int) map[string]string {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status")
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]string{}
	for _, l := range strings.Split(string(b), "\n") {
		k, v, _ := strings.Cut(l, ":")
		m[k] = strings.TrimSpace(v)
	}
	return m
}

func gone(pid int) bool {
	for range 200 {
		if _, err := os.Stat("/proc/" + strconv.Itoa(pid)); err != nil {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// The whole chain: a scope with limits, the host restricting itself and
// executing again, and the channel surviving both.
func TestAHostStartsRestrictedAndLimited(t *testing.T) {
	bin := buildHost(t)
	h, err := Start(Config{Name: "probe", Policy: sandbox.Policy{Self: bin}, Stderr: os.Stderr})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Stop()
	if err := h.Ping(2 * time.Second); err != nil {
		t.Fatal(err)
	}
	pid := h.PID()
	st := status(t, pid)
	if st["NoNewPrivs"] != "1" || st["Seccomp"] != "2" || st["PPid"] != strconv.Itoa(os.Getpid()) {
		t.Errorf("NoNewPrivs %s, Seccomp %s, parent %s", st["NoNewPrivs"], st["Seccomp"], st["PPid"])
	}
	if _, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/environ"); err == nil {
		t.Error("its environment can be read")
	}
	if !h.Limited {
		t.Skip("no systemd user session: limits not checked")
	}
	for file, want := range map[string]string{"memory.max": "268435456", "memory.swap.max": "0", "pids.max": "128"} {
		b, err := os.ReadFile(filepath.Join(h.Cgroup(), file))
		if got := strings.TrimSpace(string(b)); got != want {
			t.Errorf("%s = %q (%v), want %s", file, got, err, want)
		}
	}
	cg := h.Cgroup()
	h.Stop()
	if !gone(pid) {
		t.Error("the host is still there")
	}
	if b, err := os.ReadFile(filepath.Join(cg, "cgroup.procs")); err == nil && len(b) > 0 {
		t.Errorf("left in its scope: %s", b)
	}
	h.Stop() // twice is fine
}

func TestAHostStartedByHandRunsNothing(t *testing.T) {
	bin := buildHost(t)
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	ours, theirs := os.NewFile(uintptr(fds[0]), "a"), os.NewFile(uintptr(fds[1]), "b")
	defer ours.Close()
	c := exec.Command(bin, "restricted") // stage two without stage one
	c.ExtraFiles = []*os.File{theirs}
	err = c.Run()
	theirs.Close()
	line, _ := bufio.NewReader(ours).ReadString('\n')
	if err == nil || !strings.Contains(line, "not in the sandbox") {
		t.Errorf("exit %v, said %q", err, line)
	}
}

func TestARefusedPolicyStartsNothing(t *testing.T) {
	d := t.TempDir()
	_, err := Start(Config{Name: "bad", Policy: sandbox.Policy{Self: "/nonexistent", Sockets: []string{d}}, Protected: sandbox.Protected{Socket: d + "/spot.sock"}})
	if err == nil || !strings.Contains(err.Error(), "socket") {
		t.Errorf("err = %v", err)
	}
}
