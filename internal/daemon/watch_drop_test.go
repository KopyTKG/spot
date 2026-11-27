package daemon

import (
	"net"
	"testing"
	"time"

	"gitlab.com/KopyTKG/spot/internal/protocol"
)

// A caller of watch that stops reading is dropped, and does not hold the daemon.
func TestAWatcherThatDoesNotReadIsDropped(t *testing.T) {
	old := watchPatience
	watchPatience = 100 * time.Millisecond
	defer func() { watchPatience = old }()
	client, server := net.Pipe() // a write blocks until the other end reads
	defer client.Close()
	done := make(chan error, 1)
	go func() {
		done <- send(server, func(protocol.Response) error {
			_, err := server.Write([]byte("value\n"))
			return err
		}, protocol.Response{})
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Error("written to a caller that does not read")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("still waiting for it")
	}
}
