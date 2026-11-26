package protocol

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckPeer(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "s")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		c, _ := ln.Accept()
		accepted <- c
	}()
	client, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server := <-accepted
	defer server.Close()

	for name, c := range map[string]net.Conn{"client": client, "server": server} {
		if err := CheckPeer(c); err != nil {
			t.Errorf("%s: our own peer refused: %v", name, err)
		}
		if err := checkPeer(c, uint32(os.Getuid())+1); !errors.Is(err, ErrForeignPeer) {
			t.Errorf("%s: a foreign peer was accepted: %v", name, err)
		}
	}
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	if CheckPeer(a) == nil {
		t.Error("something that is not a Unix socket was accepted")
	}
}
