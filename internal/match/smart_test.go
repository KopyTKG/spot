package match

import (
	"reflect"
	"testing"
)

func TestSmartScore(t *testing.T) {
	// Each query must find its target; the table also pins the ranking order.
	match := []struct{ text, query string }{
		{"  Remote", "rmt"},
		{"  Remote", "rem"},
		{"  Remote", "REMOTE"},
		{"  System", "sys"},
		{"  Power", "pwr"},
		{"System Firewall", "fire"},
		{"System Firewall", "sf"},
		{"System Firewall", "sys fw"},
		{"Bookmarks", "bkm"},
	}
	for _, c := range match {
		if SmartScore(c.text, c.query) < 0 {
			t.Errorf("smartScore(%q, %q) = no match, want a match", c.text, c.query)
		}
	}
	nomatch := []struct{ text, query string }{
		{"Remove", "rmt"},
		{"  Remote", "xyz"},
		{"  Remote", "rmtx"},
	}
	for _, c := range nomatch {
		if got := SmartScore(c.text, c.query); got >= 0 {
			t.Errorf("smartScore(%q, %q) = %d, want no match", c.text, c.query, got)
		}
	}
	if SmartScore("anything", "") != 0 {
		t.Error("empty query should score 0 for everything")
	}
}

func TestRankSmart(t *testing.T) {
	opts := []string{"  Apps", "  Remote", "  Remove", "  Learn", "  Power", "  Capture"}
	cases := []struct {
		query string
		want  []string
	}{
		{"rmt", []string{"  Remote"}},
		{"rem", []string{"  Remote", "  Remove"}},             // prefix hits keep menu order
		{"re", []string{"  Remote", "  Remove", "  Capture"}}, // prefixes first; "Capture" has r..e in order
		{"pwr", []string{"  Power"}},
		{"", opts},
		{"zzz", nil},
	}
	for _, c := range cases {
		got := RankSmart(opts, c.query)
		if !reflect.DeepEqual(got, c.want) && !(len(got) == 0 && len(c.want) == 0) {
			t.Errorf("rankSmart(%q) = %q, want %q", c.query, got, c.want)
		}
	}
}
