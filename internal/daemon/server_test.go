package daemon

import (
	"net"
	"strings"
	"testing"
)

func TestRequestSizeIsBounded(t *testing.T) {
	for name, c := range map[string]struct {
		size int
		ok   bool
	}{"ordinary": {1000, true}, "far too big": {maxRequest + 1000, false}} {
		client, server := net.Pipe()
		go func() {
			client.Write([]byte(`{"op":"ping","stdin":"` + strings.Repeat("x", c.size) + `"}`))
			client.Close()
		}()
		_, err := readRequest(server)
		server.Close()
		if (err == nil) != c.ok {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}
