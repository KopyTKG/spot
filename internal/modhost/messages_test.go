package modhost

import (
	"bufio"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"gitlab.com/KopyTKG/spot/internal/modhost/channel"
	"gitlab.com/KopyTKG/spot/internal/sandbox"
)

// A stream that cannot be trusted ends the host.
func TestGarbageEndsTheHost(t *testing.T) {
	bin := buildHost(t)
	for name, junk := range map[string]string{
		"not JSON":         "}{ nonsense\n",
		"an unknown order": `{"op":"format-disk"}` + "\n",
		"too long":         `{"op":"ping","name":"` + strings.Repeat("x", channel.MaxMessage) + "\"}\n",
	} {
		h, err := Start(Config{Name: "junk", Policy: sandbox.Policy{Self: bin}})
		if err != nil {
			t.Fatal(err)
		}
		h.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		io.WriteString(h.conn, junk)
		pid := h.PID()
		done := make(chan struct{})
		h.abandoned = true // nobody else may wait for it
		go func() { h.cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Errorf("%s: host %d goes on", name, pid)
			h.cmd.Process.Kill()
			<-done
		}
	}
}

// And the daemon's side does not believe a host either.
func TestTheDaemonRejectsGarbageFromAHost(t *testing.T) {
	for name, answer := range map[string]string{"not JSON": "}{\n", "too long": strings.Repeat("x", channel.MaxMessage+10) + "\n", "nothing": ""} {
		ours, theirs := net.Pipe()
		h := &Host{conn: ours, in: bufio.NewReader(ours)}
		go func() {
			bufio.NewReader(theirs).ReadString('\n')
			io.WriteString(theirs, answer)
			if answer == "" {
				theirs.Close()
			}
		}()
		if _, err := h.exchange(channel.Message{Op: channel.OpPing}, 5*time.Second); err == nil {
			t.Errorf("%s was believed", name)
		}
		ours.Close()
		theirs.Close()
	}
}

func TestAHostRefusesAnotherProtocol(t *testing.T) {
	bin := buildHost(t)
	h, err := Start(Config{Name: "v", Policy: sandbox.Policy{Self: bin}})
	if err != nil {
		t.Fatal(err)
	}
	h.Stop()
	c := hostByHand(t, bin)
	c.send(`{"protocol":999,"policy":{"self":"` + bin + `"}}`)
	if got := c.line(); !strings.Contains(got, "protocol") {
		t.Errorf("%q", got)
	}
}

type byHand struct {
	t    *testing.T
	file *os.File
	in   *bufio.Reader
}

func (b byHand) send(s string) { b.file.WriteString(s + "\n") }
func (b byHand) line() string  { s, _ := b.in.ReadString('\n'); return s }

func hostByHand(t *testing.T, bin string) byHand {
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	ours, theirs := os.NewFile(uintptr(fds[0]), "a"), os.NewFile(uintptr(fds[1]), "b")
	c := exec.Command(bin)
	c.ExtraFiles = []*os.File{theirs}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	theirs.Close()
	t.Cleanup(func() { c.Process.Kill(); c.Wait(); ours.Close() })
	return byHand{t, ours, bufio.NewReader(ours)}
}
