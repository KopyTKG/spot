// Package capture keeps what a command prints, up to a limit: a command that
// never stops printing must not use up the memory of the daemon.
package capture

import "bytes"

// Limit is how much of one stream of one command is kept.
const Limit = 8 << 20

// Buffer is an io.Writer that keeps the first Limit bytes and drops the rest.
// Writing never fails, so the command is not disturbed.
type Buffer struct{ b bytes.Buffer }

func (c *Buffer) Write(p []byte) (int, error) {
	if room := Limit - c.b.Len(); room > 0 {
		c.b.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}

func (c *Buffer) String() string { return c.b.String() }
func (c *Buffer) Bytes() []byte  { return c.b.Bytes() }
