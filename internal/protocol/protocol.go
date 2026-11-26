// Package protocol is what a client and the daemon say to each other over the
// socket. No GTK.
package protocol

import (
	"encoding/json"
	"fmt"
	"net"
)

// Exit statuses, documented in the README.
const (
	ExitSubmitted = 0
	ExitCancelled = 1
	ExitUsage     = 2
	ExitError     = 3
	ExitBack      = 4 // the Back control was used
)

// Version is the build of this binary, set by main: a daemon and a client that
// differ do not talk.
var Version = "dev"

// The daemon and its clients talk over a Unix socket. One connection carries
// one request and one response, each a single JSON value.
//
//	client                            daemon
//	  | -- request{op:"show",...} -->   |   queued / stacked behind what is showing
//	  |            (blocks)             |   ... the user answers ...
//	  | <-- response{code,stdout,...} -- |

// Operations a request can ask for.
const (
	OpShow    = "show"    // display a dialog and wait for the answer
	OpDismiss = "dismiss" // cancel everything that is showing or waiting
	OpQuit    = "quit"    // exit once idle
	OpPing    = "ping"    // liveness + version
)

type Request struct {
	Op      string `json:"op"`
	Version string `json:"version"`

	// opShow: the caller's view of the world. The config is re-read by the
	// daemon on every request, so edits apply immediately; env and dir are
	// what submit.exec / submit.spawn commands run with.
	Config string   `json:"config,omitempty"` // absolute path
	Env    []string `json:"env,omitempty"`    // KEY=VALUE, the caller's environment
	Dir    string   `json:"dir,omitempty"`
	Stdin  string   `json:"stdin,omitempty"` // for list items with stdin = true

	// How this dialog relates to the ones around it.
	Chain bool `json:"chain,omitempty"` // another dialog follows: keep the window up for it
	Last  bool `json:"last,omitempty"`  // nothing follows: release the window at once
	Sub   bool `json:"sub,omitempty"`   // open on top of the current dialog, which returns when this one closes
	Back  bool `json:"back,omitempty"`  // show the Back control (SPOT_BACK in Env asks for it too)
}

type Response struct {
	Code    int    `json:"code"` // the exit status `spot` should end with
	Stdout  string `json:"stdout,omitempty"`
	Stderr  string `json:"stderr,omitempty"`
	Version string `json:"version,omitempty"` // the daemon's build
	Stale   bool   `json:"stale,omitempty"`   // daemon and client builds differ: restart the daemon
}

// RoundTrip sends req on conn and returns the daemon's answer.
func RoundTrip(conn net.Conn, req Request) (Response, error) {
	var resp Response
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return resp, fmt.Errorf("send: %w", err)
	}
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return resp, fmt.Errorf("receive: %w", err)
	}
	return resp, nil
}

// EnvLookup adapts a KEY=VALUE slice to the lookup parseConfig wants.
func EnvLookup(env []string) func(string) (string, bool) {
	m := make(map[string]string, len(env))
	for _, kv := range env {
		for i := 0; i < len(kv); i++ {
			if kv[i] == '=' {
				m[kv[:i]] = kv[i+1:]
				break
			}
		}
	}
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}
