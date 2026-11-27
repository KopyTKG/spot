package provider

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"
)

// LaunchArg makes the host binary a launcher: it gives a program its normal
// memory limit back and becomes it. See Launch.
const LaunchArg = "launch"

const (
	maxOutput   = 8 << 20
	maxArgs     = 4096
	maxCommands = 8
	maxTimeout  = 60 * time.Second
)

// Launch runs in the child of the host, before the program: the host's data
// limit is not the program's (some do not start under it). The host's own is
// never raised.
func Launch(args []string) error {
	if len(args) < 2 {
		return errors.New("launch: no program")
	}
	var l syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_DATA, &l); err != nil {
		return err
	}
	l.Cur = l.Max
	if err := syscall.Setrlimit(syscall.RLIMIT_DATA, &l); err != nil {
		return err
	}
	return syscall.Exec(args[0], args[1:], os.Environ())
}

// output keeps what a program prints, and calls full once when it is too much.
type output struct {
	mu   sync.Mutex
	b    bytes.Buffer
	full func()
}

func (o *output) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.b.Len()+len(p) > maxOutput {
		if o.full != nil {
			o.full()
			o.full = nil
		}
		return len(p), nil
	}
	o.b.Write(p)
	return len(p), nil
}

func (o *output) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.b.String()
}

// Result of a command or a pipeline.
type Result struct {
	Status   int
	Statuses []int
	Stdout   string
	Stderr   string
}

type runOptions struct {
	timeout time.Duration
	stdin   string
	env     map[string]string
	dir     string
}

func (r *Runtime) environment(extra map[string]string) []string {
	env := []string{"PATH=/usr/bin"}
	for _, k := range []string{"HOME", "LANG", "XDG_RUNTIME_DIR"} {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	for k, v := range r.grants.Env {
		env = append(env, k+"="+v)
	}
	for k, v := range extra { // checked against setenv before
		env = append(env, k+"="+v)
	}
	return env
}

// pipeline runs the commands, each one's output into the next, without a shell.
// All must be permitted or none is started.
func (r *Runtime) pipeline(ctx context.Context, commands [][]string, o runOptions) (Result, error) {
	var res Result
	if len(commands) == 0 || len(commands) > maxCommands {
		return res, fmt.Errorf("between 1 and %d commands", maxCommands)
	}
	paths := make([]string, len(commands))
	for i, argv := range commands {
		if len(argv) == 0 || len(argv) > maxArgs {
			return res, errors.New("a command is a list of the program and its arguments")
		}
		if slices.ContainsFunc(argv, func(a string) bool { return strings.ContainsRune(a, 0) }) {
			return res, errors.New("an argument holds a NUL byte")
		}
		path, ok := r.grants.Programs[argv[0]]
		if !ok {
			r.audit("refused", "run "+argv[0])
			return res, fmt.Errorf("%q: %w to run it", argv[0], errRefused)
		}
		if err := r.unchanged(path); err != nil {
			r.audit("refused", "run "+argv[0]+": "+err.Error())
			return res, err
		}
		paths[i] = path
	}
	for name := range o.env {
		if !slices.Contains(r.grants.Setenv, name) {
			r.audit("refused", "setenv "+name)
			return res, fmt.Errorf("%s: %w to set it", name, errRefused)
		}
	}
	dir := r.emptyDir
	if o.dir != "" {
		real, err := r.real(o.dir, r.grants.Read)
		if err != nil {
			r.audit("refused", "dir "+o.dir)
			return res, fmt.Errorf("dir %q: %w", o.dir, err)
		}
		dir = real
	}
	if o.timeout <= 0 {
		o.timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, min(o.timeout, maxTimeout))
	defer cancel()

	env := r.environment(o.env)
	cmds := make([]*exec.Cmd, len(commands))
	errs := make([]*output, len(commands))
	var tooMuch bool
	out := &output{full: func() { tooMuch = true; cancel() }}
	var group int // the first program leads the group; all are killed together
	var pipes []*os.File
	defer func() {
		for _, p := range pipes {
			p.Close()
		}
	}()
	for i, argv := range commands {
		c := exec.Command("/proc/self/exe", append([]string{LaunchArg, paths[i]}, argv...)...)
		c.Args[0] = "spot-mod-host"
		c.Env, c.Dir = env, dir
		errs[i] = &output{full: func() { tooMuch = true; cancel() }}
		c.Stderr = errs[i]
		c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: group}
		c.WaitDelay = time.Second // a child of a child may keep a pipe open
		if i == 0 {
			c.Stdin = strings.NewReader(o.stdin)
		} else {
			read, write, err := os.Pipe()
			if err != nil {
				return res, err
			}
			pipes = append(pipes, read, write)
			cmds[i-1].Stdout = write
			c.Stdin = read
		}
		if i == len(commands)-1 {
			c.Stdout = out
		}
		cmds[i] = c
		if i > 0 {
			if err := cmds[i-1].Start(); err != nil {
				kill(group)
				return res, err
			}
			if group == 0 {
				group = cmds[0].Process.Pid
				c.SysProcAttr.Pgid = group
				track(group)
				defer untrack(group)
			}
		}
		r.audit("run", strings.Join(argv, " "))
	}
	last := cmds[len(cmds)-1]
	if err := last.Start(); err != nil {
		kill(group)
		for _, c := range cmds[:len(cmds)-1] {
			c.Wait()
		}
		return res, err
	}
	if group == 0 {
		group = last.Process.Pid
		track(group)
		defer untrack(group)
	}
	for _, p := range pipes {
		p.Close() // the children hold their own ends
	}
	pipes = nil

	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			kill(group)
		case <-done:
		}
	}()
	var stderr strings.Builder
	for i, c := range cmds {
		err := c.Wait()
		status := c.ProcessState.ExitCode()
		var exit *exec.ExitError
		if err != nil && !errors.As(err, &exit) && !errors.Is(err, exec.ErrWaitDelay) {
			status = -1
		}
		res.Statuses = append(res.Statuses, status)
		if status != 0 {
			res.Status = status
		}
		stderr.WriteString(errs[i].String())
	}
	close(done)
	switch {
	case tooMuch:
		kill(group)
		return res, fmt.Errorf("it printed more than %d bytes and was killed", maxOutput)
	case ctx.Err() != nil:
		kill(group)
		return res, errors.New("it took too long and was killed")
	}
	res.Stdout, res.Stderr = out.String(), stderr.String()
	return res, nil
}

// running are the process groups of programs not yet over.
var running = struct {
	sync.Mutex
	groups  map[int]bool
	closing bool
}{groups: map[int]bool{}}

func track(group int) {
	running.Lock()
	defer running.Unlock()
	running.groups[group] = true
	if running.closing {
		kill(group)
	}
}

func untrack(group int) {
	running.Lock()
	defer running.Unlock()
	delete(running.groups, group)
}

// KillRunning ends every program the mod is waiting for. The host calls it when
// its daemon is gone, so that they are not left behind.
func KillRunning() {
	running.Lock()
	defer running.Unlock()
	running.closing = true
	for g := range running.groups {
		kill(g)
	}
}

func kill(group int) {
	if group > 0 {
		_ = syscall.Kill(-group, syscall.SIGKILL)
	}
}

// unchanged checks a program of your own right before it is run: the daemon
// checks before each request, and a request can last. Something that already
// runs as you can still swap it between here and the start (S10).
func (r *Runtime) unchanged(path string) error {
	want, mine := r.approved[path]
	if !mine {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("%s cannot be checked: %w", path, err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, io.LimitReader(f, 17<<20)); err != nil {
		return fmt.Errorf("%s cannot be checked: %w", path, err)
	}
	if hex.EncodeToString(h.Sum(nil)) != want {
		return fmt.Errorf("%s has changed since you approved the mod", path)
	}
	return nil
}
