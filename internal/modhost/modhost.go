// Package modhost is the daemon's side of a mod host: it starts one in a scope
// of its own with memory and task limits, talks to it and stops it with
// everything it started. It touches no GTK.
package modhost

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"gitlab.com/KopyTKG/spot/internal/modhost/channel"
	"gitlab.com/KopyTKG/spot/internal/sandbox"
)

const (
	DefaultMemory = 256 << 20 // of the whole mod, no swap
	DefaultTasks  = 128       // threads count

	startWait = 10 * time.Second
)

// Config says what to start.
type Config struct {
	Name      string // the mod's
	Policy    sandbox.Policy
	Protected sandbox.Protected
	Memory    int64    // bytes; 0 is DefaultMemory
	Tasks     int      // 0 is DefaultTasks
	Stderr    *os.File // where the host and its programs complain
}

// Host is a running mod host.
type Host struct {
	// Limited is false without a systemd user session: no memory or task limit,
	// and a program that detaches itself is not stopped with the mod.
	Limited bool

	cmd    *exec.Cmd
	conn   net.Conn
	in     *bufio.Reader
	cgroup string // of the scope; "" when not Limited
}

var (
	modName = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
	count   atomic.Int64
)

func userSystemd() bool {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, "systemd/private"))
	return err == nil
}

// Start runs a host and returns once it is restricted and ready.
func Start(c Config) (*Host, error) {
	if !modName.MatchString(c.Name) {
		return nil, fmt.Errorf("mod name %q: want [a-z][a-z0-9_-]*", c.Name)
	}
	if err := c.Policy.Check(c.Protected); err != nil {
		return nil, fmt.Errorf("mod %s: %w", c.Name, err)
	}
	if c.Memory == 0 {
		c.Memory = DefaultMemory
	}
	if c.Tasks == 0 {
		c.Tasks = DefaultTasks
	}

	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("socketpair: %w", err)
	}
	ours, theirs := os.NewFile(uintptr(fds[0]), "mod"), os.NewFile(uintptr(fds[1]), "daemon")
	defer theirs.Close()
	conn, err := net.FileConn(ours) // a copy, with deadlines
	ours.Close()
	if err != nil {
		return nil, err
	}

	h := &Host{conn: conn, in: bufio.NewReader(conn), Limited: userSystemd()}
	unit := fmt.Sprintf("spot-mod-%s-%d-%d.scope", c.Name, os.Getpid(), count.Add(1))
	argv := []string{c.Policy.Self}
	if h.Limited {
		argv = append([]string{filepath.Join(sandbox.SystemPath, "systemd-run"), "--user", "--scope", "--quiet", "--collect",
			"--unit=" + unit,
			"-p", "MemoryMax=" + strconv.FormatInt(c.Memory, 10),
			"-p", "MemorySwapMax=0", // without it a memory limit is none where there is swap
			"-p", "TasksMax=" + strconv.Itoa(c.Tasks),
			"--"}, argv...)
	}
	h.cmd = exec.Command(argv[0], argv[1:]...)
	h.cmd.Dir = "/"
	h.cmd.Stderr = c.Stderr
	h.cmd.ExtraFiles = []*os.File{theirs} // becomes fd 3
	h.cmd.Env = []string{"PATH=" + sandbox.SystemPath}
	// The last two are for systemd-run; the host drops them.
	for _, k := range []string{"HOME", "LANG", "XDG_RUNTIME_DIR", "DBUS_SESSION_BUS_ADDRESS"} {
		if v, ok := os.LookupEnv(k); ok {
			h.cmd.Env = append(h.cmd.Env, k+"="+v)
		}
	}
	if err := h.cmd.Start(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("mod %s: starting the host: %w", c.Name, err)
	}
	theirs.Close() // so that the host's end closing is seen

	err = h.exchange(channel.Hello{Policy: c.Policy}, startWait)
	if err == nil && h.Limited {
		h.cgroup, err = cgroupOf(h.cmd.Process.Pid, unit)
	}
	if err != nil {
		h.Stop()
		return nil, fmt.Errorf("mod %s: %w", c.Name, err)
	}
	return h, nil
}

// exchange sends v and waits for the host to say it is ready.
func (h *Host) exchange(v any, wait time.Duration) error {
	h.conn.SetDeadline(time.Now().Add(wait))
	defer h.conn.SetDeadline(time.Time{})
	if err := json.NewEncoder(h.conn).Encode(v); err != nil {
		return fmt.Errorf("the host does not take messages: %w", err)
	}
	line, err := h.in.ReadSlice('\n') // bounded by the reader's buffer
	if err != nil {
		return fmt.Errorf("the host did not answer: %w", err)
	}
	var r channel.Reply
	if err := json.Unmarshal(line, &r); err != nil {
		return fmt.Errorf("the host answered nonsense: %w", err)
	}
	if !r.Ready {
		return errors.New(r.Error)
	}
	return nil
}

// Ping reports whether the host answers in time.
func (h *Host) Ping(wait time.Duration) error {
	return h.exchange(channel.Message{Op: channel.OpPing}, wait)
}

// PID is the host's process.
func (h *Host) PID() int { return h.cmd.Process.Pid }

// Cgroup is the folder of the host's scope, "" when not Limited.
func (h *Host) Cgroup() string { return h.cgroup }

// cgroupOf is the folder of pid's scope, which must be the one we made: it is
// later written to in order to kill everything in it.
func cgroupOf(pid int, unit string) (string, error) {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cgroup")
	if err != nil {
		return "", err
	}
	path, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "0::")
	if !ok || filepath.Base(path) != unit {
		return "", fmt.Errorf("the host is not in its scope %s but in %q", unit, path)
	}
	return filepath.Join("/sys/fs/cgroup", path), nil
}

// Stop kills the host and everything in its scope. It is safe to call twice.
func (h *Host) Stop() {
	if h.cgroup != "" {
		// The scope is gone already when its last process left.
		_ = os.WriteFile(filepath.Join(h.cgroup, "cgroup.kill"), []byte("1"), 0)
		h.cgroup = ""
	}
	h.conn.Close()
	if h.cmd.ProcessState == nil {
		_ = h.cmd.Process.Kill()
		_ = h.cmd.Wait()
	}
}
