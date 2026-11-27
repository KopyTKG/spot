package main

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode"

	"gitlab.com/KopyTKG/spot/internal/protocol"
)

func TestTextOutputCannotForgeAndStaysJSON(t *testing.T) {
	got := asText(json.RawMessage(`[{"label":"a\tb\nc\u001b[31m","key":"k\\n","state":true},{"label":"x"}]`))
	if want := "a\\tb\\nc\\u{001b}[31m\tk\\\\n\ttrue\nx\t\t\n"; got != want {
		t.Errorf("rows: %q, want %q", got, want)
	}
	for _, raw := range []string{`{"a":"<b>","q":"say \"hi\"\n"}`, "{\"bidi\":\"‮\U000e0001\"}", `[1,"two"]`} {
		out := asText(json.RawMessage(raw))
		var a, b any
		if err := json.Unmarshal([]byte(out), &b); err != nil {
			t.Errorf("%q: no longer JSON: %q", raw, out)
			continue
		}
		json.Unmarshal([]byte(raw), &a)
		x, _ := json.Marshal(a)
		y, _ := json.Marshal(b)
		if string(x) != string(y) {
			t.Errorf("%q: changed to %q", raw, out)
		}
		if strings.ContainsFunc(strings.TrimSuffix(out, "\n"), func(r rune) bool { return !unicode.IsPrint(r) }) {
			t.Errorf("%q: raw characters: %q", raw, out)
		}
	}
}

// A bar reads markup: nothing of a mod's may act as such, and a class is a name.
func TestForABar(t *testing.T) {
	parse := func(r protocol.Response) map[string]string {
		var m map[string]string
		if err := json.Unmarshal([]byte(forBar(r)), &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	m := parse(protocol.Response{Result: json.RawMessage(`{"text":"<b>&","tooltip":"<span size='99999'>","class":"a\" b{}-1"}`)})
	if m["text"] != "&lt;b&gt;&amp;" || strings.ContainsAny(m["tooltip"], "<>") || m["class"] != "ab-1" {
		t.Errorf("%q", m)
	}
	m = parse(protocol.Response{Code: protocol.ExitError, Stderr: "spot: <x> broke\n"})
	if m["text"] != "!" || m["class"] != "error" || m["tooltip"] != "&lt;x&gt; broke" {
		t.Errorf("an error: %q", m)
	}
	m = parse(protocol.Response{Result: json.RawMessage(`[{"label":"one"},{"label":"two"}]`), Stderr: "spot: old\n"})
	if m["text"] != "2" || m["class"] != "stale" || !strings.Contains(m["tooltip"], "one") {
		t.Errorf("old rows: %q", m)
	}
}
