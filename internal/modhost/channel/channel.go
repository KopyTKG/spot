// Package channel is what the daemon and a mod host say to each other: one JSON
// value per line over a socket pair.
package channel

import "gitlab.com/KopyTKG/spot/internal/sandbox"

// Hello is the daemon's first message: what the host is to be restricted to.
type Hello struct {
	Policy sandbox.Policy `json:"policy"`
}

// OpPing asks whether the host still answers.
const OpPing = "ping"

// Message is any later message from the daemon.
type Message struct {
	Op string `json:"op"`
}

// Reply is a message from the host.
type Reply struct {
	Ready bool   `json:"ready,omitempty"`
	Error string `json:"error,omitempty"`
}
