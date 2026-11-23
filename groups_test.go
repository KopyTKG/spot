package main

import (
	"reflect"
	"testing"
)

func texts(es []groupEntry) []string {
	var out []string
	for _, e := range es {
		out = append(out, e.text)
	}
	return out
}

var helpLines = []string{
	"Yazi\tseek forward",
	"Yazi\tquit",
	"mpv\tseek 5 seconds",
	"no group line",
	"Yazi\tnew tab",
	"\tleading separator, no group",
}

func TestParseGroups(t *testing.T) {
	g := parseGroups(helpLines, "")
	if !reflect.DeepEqual(g.order, []string{"Yazi", "mpv"}) {
		t.Errorf("order = %v", g.order)
	}
	if got := g.rows["Yazi"]; !reflect.DeepEqual(got, []string{"seek forward", "quit", "new tab"}) {
		t.Errorf("Yazi rows = %v", got)
	}
	if !reflect.DeepEqual(g.ungrouped, []string{"no group line", "leading separator, no group"}) {
		t.Errorf("ungrouped = %v", g.ungrouped)
	}
}

func TestFlattenCollapsedAndExpanded(t *testing.T) {
	g := parseGroups(helpLines, "")
	all := map[string]bool{"Yazi": true, "mpv": true}
	got := texts(g.flatten(all, "", "contains"))
	want := []string{"no group line", "leading separator, no group", "▸ Yazi  (3)", "▸ mpv  (1)"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("collapsed = %q, want %q", got, want)
	}
	open := map[string]bool{"Yazi": false, "mpv": true}
	got = texts(g.flatten(open, "", "contains"))
	want = []string{"no group line", "leading separator, no group",
		"▾ Yazi  (3)", "    seek forward", "    quit", "    new tab", "▸ mpv  (1)"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("expanded = %q, want %q", got, want)
	}
}

func TestFlattenQueryOpensMatchingGroups(t *testing.T) {
	g := parseGroups(helpLines, "")
	all := map[string]bool{"Yazi": true, "mpv": true}
	es := g.flatten(all, "seek", "smart")
	got := texts(es)
	want := []string{"▾ Yazi  (1)", "    seek forward", "▾ mpv  (1)", "    seek 5 seconds"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("seek = %q, want %q", got, want)
	}
	// The group name is searchable: "yazi seek" only finds Yazi's row.
	got = texts(g.flatten(all, "yazi seek", "smart"))
	want = []string{"▾ Yazi  (1)", "    seek forward"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("yazi seek = %q, want %q", got, want)
	}
	// Nothing matches: nothing is shown, not even headers.
	if got := g.flatten(all, "zzzz", "smart"); len(got) != 0 {
		t.Errorf("zzzz = %q", texts(got))
	}
}

func TestFlattenValuesAndHeaders(t *testing.T) {
	g := parseGroups(helpLines, "")
	es := g.flatten(map[string]bool{}, "", "contains")
	for _, e := range es {
		if e.header && e.value != "" {
			t.Errorf("header %q has a value %q", e.text, e.value)
		}
		if !e.header && e.value == "" {
			t.Errorf("row %q has no value", e.text)
		}
	}
	if i := headerIndex(es, "mpv"); i < 0 || !es[i].header || es[i].group != "mpv" {
		t.Errorf("headerIndex(mpv) = %d", i)
	}
	if headerIndex(es, "none") != -1 {
		t.Error("headerIndex of an unknown group should be -1")
	}
}

func TestParseGroupsCustomSeparator(t *testing.T) {
	g := parseGroups([]string{"a|one", "a|two", "b|three"}, "|")
	if !reflect.DeepEqual(g.order, []string{"a", "b"}) || len(g.rows["a"]) != 2 {
		t.Errorf("custom separator: %+v", g)
	}
}
