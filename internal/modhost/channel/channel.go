// Package channel is what the daemon and a mod host say to each other: one JSON
// value per line over a socket pair. Neither side trusts the other.
package channel

import (
	"bufio"
	"encoding/json"
	"errors"

	"gitlab.com/KopyTKG/spot/internal/mod"
	"gitlab.com/KopyTKG/spot/internal/modperm"
	"gitlab.com/KopyTKG/spot/internal/sandbox"
)

// MaxMessage is the longest line either side reads.
const MaxMessage = 16 << 20

// Protocol is the version of what is said here. A package upgrade can put a new
// host beside a daemon that is still running: they then do not talk.
const Protocol = 1

// Hello is the daemon's first message: what the host is to be restricted to.
type Hello struct {
	Protocol int            `json:"protocol"`
	Policy   sandbox.Policy `json:"policy"`
}

// Operations.
const (
	OpPing  = "ping"  // does the host still answer
	OpParse = "parse" // run a parser
	OpLoad  = "load"  // load the provider this host is for
	OpGet   = "get"   // a value of it
	OpCall  = "call"  // a request of it
	OpTick  = "tick"  // run the timers that are due
)

// Message is any later message from the daemon.
type Message struct {
	Op    string   `json:"op"`
	Parse *Parse   `json:"parse,omitempty"`
	Load  *Load    `json:"load,omitempty"`
	Name  string   `json:"name,omitempty"` // of the value or request
	Args  []string `json:"args,omitempty"`
}

// Load is a provider: the very bytes that were checked, its parsers, and what
// it was granted.
type Load struct {
	Mod      mod.Mod            `json:"mod"`
	Files    map[string][]byte  `json:"files"` // what spot.require may load
	Parsers  map[string]mod.Mod `json:"parsers,omitempty"`
	Grants   modperm.Grants     `json:"grants"`
	Config   json.RawMessage    `json:"config,omitempty"`
	EmptyDir string             `json:"empty_dir"` // where programs start
}

// Event is one thing a mod did or was refused, for the audit log. It never
// holds what was written to a program or the value of a variable.
type Event struct {
	Kind string `json:"kind"`
	What string `json:"what"`
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
	Events []Event         `json:"events,omitempty"`

	Values   []string `json:"values,omitempty"`   // after load
	Requests []string `json:"requests,omitempty"` // after load

	// The host never speaks on its own. Each reply says what the daemon has to
	// come back for.
	Every   map[string]int64 `json:"every,omitempty"`   // values to recompute while watched, ms
	Next    int64            `json:"next,omitempty"`    // ms until a timer is due, 0 for none
	Changed []string         `json:"changed,omitempty"` // values published or refreshed
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
