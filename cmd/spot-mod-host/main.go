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
	"runtime/debug"
	"syscall"
	"time"

	"gitlab.com/KopyTKG/spot/internal/modhost/channel"
	"gitlab.com/KopyTKG/spot/internal/parser"
	"gitlab.com/KopyTKG/spot/internal/sandbox"
	"golang.org/x/sys/unix"
)

const (
	chanFD = 3
	second = "restricted"
)

func main() {
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
	in := bufio.NewReader(ch)
	for {
		line, err := channel.ReadLine(in)
		if err != nil {
			return // the daemon is gone, or a stream that cannot be trusted
		}
		var m channel.Message
		if json.Unmarshal(line, &m) != nil {
			os.Exit(1)
		}
		switch {
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
