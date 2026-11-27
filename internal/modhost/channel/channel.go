// Package channel is what the daemon and a mod host say to each other: one JSON
// value per line over a socket pair. Neither side trusts the other.
package channel

import (
	"bufio"
	"encoding/json"
	"errors"

	"gitlab.com/KopyTKG/spot/internal/mod"
	"gitlab.com/KopyTKG/spot/internal/sandbox"
)

// MaxMessage is the longest line either side reads.
const MaxMessage = 16 << 20

// Hello is the daemon's first message: what the host is to be restricted to.
type Hello struct {
	Policy sandbox.Policy `json:"policy"`
}

// Operations.
const (
	OpPing  = "ping"  // does the host still answer
	OpParse = "parse" // run a parser
)

// Message is any later message from the daemon.
type Message struct {
	Op    string `json:"op"`
	Parse *Parse `json:"parse,omitempty"`
}

// Parse asks for one parser call. The mods' files travel with it: the host
// cannot read the mods folder.
type Parse struct {
	Name    string             `json:"name"`
	Mods    map[string]mod.Mod `json:"mods"`
	Input   []byte             `json:"input"`
	Options json.RawMessage    `json:"options,omitempty"`
	Millis  int                `json:"millis"`         // time allowed inside Lua
	Load    bool               `json:"load,omitempty"` // only check that it loads
}

// Reply is a message from the host. Ready says the host is fine; Error with
// Ready is the mod's fault, without it the host's.
type Reply struct {
	Ready  bool            `json:"ready,omitempty"`
	Error  string          `json:"error,omitempty"`
	Broken bool            `json:"broken,omitempty"` // the error is a broken contract
	Result json.RawMessage `json:"result,omitempty"`
	Log    []string        `json:"log,omitempty"`
}

// ReadLine reads one message without ever holding more than MaxMessage of it.
func ReadLine(r *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		part, err := r.ReadSlice('\n')
		if len(line)+len(part) > MaxMessage {
			return nil, errors.New("a message that is too long")
		}
		line = append(line, part...)
		if err == nil {
			return line, nil
		}
		if err != bufio.ErrBufferFull {
			return nil, err
		}
	}
}
