// spot-mod-host runs one daemon mod, inside a sandbox it puts itself in. It is
// started by the daemon with a socket to it as file descriptor 3 and is of no
// use started by hand: it refuses to go on unless it is restricted.
//
// It must stay a static program with no GTK: the sandbox grants it nothing but
// its own file.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"syscall"
	"time"
	"unsafe"

	"gitlab.com/KopyTKG/spot/internal/modhost/channel"
	"gitlab.com/KopyTKG/spot/internal/parser"
	"gitlab.com/KopyTKG/spot/internal/provider"
	"gitlab.com/KopyTKG/spot/internal/sandbox"
	"golang.org/x/sys/unix"
)

const (
	chanFD = 3
	second = "restricted"
)

// The main goroutine stays on the main thread: its name is the process's name.
func init() { runtime.LockOSThread() }

func main() {
	// A child of the host, on its way to being a program a mod runs.
	if len(os.Args) > 1 && os.Args[1] == provider.LaunchArg {
		if err := provider.Launch(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "spot: %v\n", err)
		}
		os.Exit(126)
	}
	if len(os.Args) == 2 && os.Args[1] == second {
		run()
		return
	}
	// Stage one: take the policy, restrict this thread, execute ourselves.
	ch := os.NewFile(chanFD, "daemon")
	var hello channel.Hello
	// The daemon sends this and nothing more until we report ready, so the
	// decoder cannot swallow anything meant for stage two.
	if err := json.NewDecoder(ch).Decode(&hello); err != nil {
		fail(ch, fmt.Errorf("reading the policy: %w", err))
	}
	// Nothing but the channel is passed on.
	if err := unix.CloseRange(chanFD+1, ^uint(0), unix.CLOSE_RANGE_CLOEXEC); err != nil {
		fail(ch, fmt.Errorf("closing files: %w", err))
	}
	env := environment(hello.Policy)
	if err := sandbox.Restrict(hello.Policy); err != nil {
		fail(ch, err)
	}
	// Same goroutine, same thread: what it has now covers the new program.
	err := syscall.Exec("/proc/self/exe", []string{os.Args[0], second}, env)
	fail(ch, fmt.Errorf("executing again: %w", err))
}

// environment is the small one a mod host and its programs get (S11): not what
// was needed to start us.
func environment(p sandbox.Policy) []string {
	env := []string{"PATH=" + sandbox.SystemPath}
	keep := []string{"HOME", "LANG"}
	if len(p.Sockets) > 0 {
		keep = append(keep, "XDG_RUNTIME_DIR")
	}
	for _, k := range keep {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	return env
}

// run is stage two. No mod code is read before it has proved where it is.
func run() {
	ch := os.NewFile(chanFD, "daemon")
	syscall.CloseOnExec(chanFD) // programs a mod starts do not get the channel (S12)
	// Executed as /proc/self/exe we would show up as "exe".
	name := [16]byte{}
	copy(name[:], "spot-mod-host")
	unix.Prctl(unix.PR_SET_NAME, uintptr(unsafe.Pointer(&name[0])), 0, 0, 0)
	if err := unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0); err != nil {
		fail(ch, fmt.Errorf("protecting memory: %w", err))
	}
	if err := sandbox.Verify(); err != nil {
		fail(ch, fmt.Errorf("not in the sandbox: %w", err))
	}
	if err := limitMemory(); err != nil {
		fail(ch, fmt.Errorf("limiting memory: %w", err))
	}
	out := json.NewEncoder(ch)
	out.Encode(channel.Reply{Ready: true})
	// Reading goes on while a mod runs: when the daemon is gone, also a mod
	// stuck where nothing can interrupt it must go.
	messages := make(chan channel.Message)
	go func() {
		in := bufio.NewReader(ch)
		for {
			line, err := channel.ReadLine(in)
			if err != nil {
				// the daemon is gone, or a stream that cannot be trusted
				provider.KillRunning()
				os.Exit(0)
			}
			var m channel.Message
			if json.Unmarshal(line, &m) != nil {
				os.Exit(1)
			}
			messages <- m
		}
	}()
	var mods *provider.Runtime
	for m := range messages {
		switch {
		case m.Op == channel.OpLoad && m.Load != nil && mods == nil:
			var r channel.Reply
			mods, r = provider.Open(*m.Load)
			out.Encode(r)
		case m.Op == channel.OpGet && mods != nil:
			out.Encode(mods.Get(m.Name))
		case m.Op == channel.OpCall && mods != nil:
			out.Encode(mods.Call(m.Name, m.Args))
		case m.Op == channel.OpTick && mods != nil:
			out.Encode(mods.Tick())
		case m.Op == channel.OpPing:
			out.Encode(channel.Reply{Ready: true})
		case m.Op == channel.OpParse && m.Parse != nil:
			out.Encode(parse(m.Parse))
		default:
			os.Exit(1)
		}
	}
}

// memoryLimit stops a mod that allocates without end. A data limit: an
// address-space limit does not work with Go.
const memoryLimit = 256 << 20

func limitMemory() error {
	var l unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_DATA, &l); err != nil {
		return err
	}
	l.Cur = min(memoryLimit, l.Max)
	debug.SetMemoryLimit(memoryLimit * 3 / 4) // collect before reaching it
	return unix.Setrlimit(unix.RLIMIT_DATA, &l)
}

func parse(p *channel.Parse) channel.Reply {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(p.Millis)*time.Millisecond)
	defer cancel()
	c := &parser.Call{Mods: p.Mods}
	bad := func(err error) channel.Reply {
		var v *parser.Violation
		return channel.Reply{Ready: true, Error: err.Error(), Broken: errors.As(err, &v), Log: c.Log}
	}
	if p.Load {
		if err := c.Load(ctx, p.Name); err != nil {
			return bad(err)
		}
		return channel.Reply{Ready: true, Log: c.Log}
	}
	var options any
	if len(p.Options) > 0 {
		if err := json.Unmarshal(p.Options, &options); err != nil {
			return bad(fmt.Errorf("options: %w", err))
		}
	}
	rows, err := c.Run(ctx, p.Name, string(p.Input), options)
	if err != nil {
		return bad(err)
	}
	b, err := json.Marshal(rows)
	if err != nil {
		return bad(err)
	}
	// Room for the rest of the message: JSON can be longer than the text in it.
	if len(b) > channel.MaxMessage-1<<20 {
		return bad(fmt.Errorf("parser %s: the result is too large", p.Name))
	}
	return channel.Reply{Ready: true, Result: b, Log: c.Log}
}

func fail(ch *os.File, err error) {
	json.NewEncoder(ch).Encode(channel.Reply{Error: err.Error()})
	fmt.Fprintf(os.Stderr, "spot-mod-host: %v\n", err)
	os.Exit(1)
}
