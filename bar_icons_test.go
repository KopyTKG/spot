package main

import (
	"encoding/json"
	"strings"
	"testing"

	"gitlab.com/KopyTKG/spot/internal/protocol"
)

// Icon fonts, such as Nerd Fonts, use private-use characters. They are what a
// bar module shows, and cannot steer a terminal: they pass as they are.
func TestIconFontCharactersPass(t *testing.T) {
	const icons = "\U000F0565 \uf023" // shield (beyond four hex digits), lock
	raw, _ := json.Marshal(map[string]string{"text": icons, "tooltip": "on " + icons})
	var m map[string]string
	if err := json.Unmarshal([]byte(forBar(protocol.Response{Result: raw})), &m); err != nil {
		t.Fatal(err)
	}
	if m["text"] != icons || m["tooltip"] != "on "+icons {
		t.Errorf("for a bar: %q %q", m["text"], m["tooltip"])
	}
	if got := field(icons); got != icons {
		t.Errorf("as a field: %q", got)
	}
	if got := line(raw); !strings.Contains(got, icons) {
		t.Errorf("as JSON: %s", got)
	}
	// What can steer a terminal, or reorder what is read, still does not pass.
	for _, bad := range []string{"\x1b", "\u202e", "\u0085"} {
		if strings.Contains(field(bad), bad) || strings.Contains(line(json.RawMessage(`"`+bad+`"`)), bad) {
			t.Errorf("%q passed", bad)
		}
	}
}
