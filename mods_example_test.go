package main

import (
	"os"
	"strings"
	"testing"

	"gitlab.com/KopyTKG/spot/internal/config"
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
	if !c.UsesMods() || !strings.Contains(c.Submit.Exec, "$SPOT_VALUE_ROW") {
		t.Errorf("%+v", c.Submit)
	}
}
