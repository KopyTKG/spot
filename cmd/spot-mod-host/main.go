// spot-mod-host runs one daemon mod, inside a sandbox it puts itself in. It is
// started by the daemon with a socket to it as file descriptor 3 and is of no
// use started by hand: it refuses to go on unless it is restricted.
//
// It must stay a static program with no GTK: the sandbox grants it nothing but
// its own file.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"syscall"

	"gitlab.com/KopyTKG/spot/internal/modhost/channel"
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
	out := json.NewEncoder(ch)
	out.Encode(channel.Reply{Ready: true})
	in := bufio.NewScanner(ch)
	for in.Scan() {
		var m channel.Message
		if json.Unmarshal(in.Bytes(), &m) != nil || m.Op != channel.OpPing {
			os.Exit(1) // a stream that cannot be trusted any more
		}
		out.Encode(channel.Reply{Ready: true})
	}
}

func fail(ch *os.File, err error) {
	json.NewEncoder(ch).Encode(channel.Reply{Error: err.Error()})
	fmt.Fprintf(os.Stderr, "spot-mod-host: %v\n", err)
	os.Exit(1)
}
