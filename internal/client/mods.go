package client

import (
	"encoding/json"
	"errors"
	"strings"

	"gitlab.com/KopyTKG/spot/internal/protocol"
)

// Mods reaches mods through the daemon, for a dialog that is not shown by it.
type Mods struct{}

func ask(req protocol.Request) (protocol.Response, error) {
	req.Version = protocol.Version
	r, ok := ViaDaemon(req)
	switch {
	case !ok:
		return r, errors.New("mods run in the daemon, and it could not be reached")
	case r.Code != 0:
		return r, errors.New(strings.TrimPrefix(strings.TrimSpace(r.Stderr), "spot: "))
	}
	return r, nil
}

// Get is a value of a mod.
func (Mods) Get(full string) (json.RawMessage, string, error) {
	r, err := ask(protocol.Request{Op: protocol.OpGet, Name: full})
	return r.Result, strings.TrimPrefix(strings.TrimSpace(r.Stderr), "spot: "), err
}

// Call runs a request of a mod.
func (Mods) Call(full string, args []string) error {
	_, err := ask(protocol.Request{Op: protocol.OpCall, Name: full, Args: args})
	return err
}

// Parse runs a parser.
func (Mods) Parse(parser string, input []byte) (json.RawMessage, error) {
	r, err := ask(protocol.Request{Op: protocol.OpParse, Name: parser, Stdin: string(input)})
	return r.Result, err
}
