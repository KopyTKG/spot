package main

import (
	"os"
	"testing"

	"gitlab.com/KopyTKG/spot/internal/config"
	"gitlab.com/KopyTKG/spot/internal/scan"
)

// The example's dialog must stay a valid config.
func TestTheExampleDialog(t *testing.T) {
	data, err := os.ReadFile("examples/mods/sockets/sockets.toml")
	if err != nil {
		t.Fatal(err)
	}
	c, err := config.ParseConfig(data, func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatal(err)
	}
	// The example is mod and dialog: no shell anywhere in it.
	if !c.UsesMods() || c.Submit.Copy != "{row}" || c.Submit.Exec != "" || len(scan.Beside(map[string][]byte{"sockets.toml": data})) != 0 {
		t.Errorf("%+v", c.Submit)
	}
}
