package main

import (
	"fmt"
	"sort"
	"strings"
)

// Collapsible lists ("collapsible = true" on a list item) group their options
// under headers that fold and unfold, e.g. a help menu with one section per app.
// An option line is "GROUP<TAB>ROW"; a line without the separator belongs to no
// group and is always shown, above the groups. This file is the logic only;
// dialog.go draws it.

const (
	markCollapsed = "▸"
	markExpanded  = "▾"
	groupSep      = "\t"
)

// groupSet is the parsed option lines of a collapsible list.
type groupSet struct {
	order     []string            // groups, in order of first appearance
	rows      map[string][]string // group -> its rows, in input order
	ungrouped []string
}

func parseGroups(opts []string, sep string) *groupSet {
	if sep == "" {
		sep = groupSep
	}
	g := &groupSet{rows: map[string][]string{}}
	for _, o := range opts {
		name, row, ok := strings.Cut(o, sep)
		if !ok || strings.TrimSpace(name) == "" {
			g.ungrouped = append(g.ungrouped, strings.TrimPrefix(o, sep))
			continue
		}
		if _, seen := g.rows[name]; !seen {
			g.order = append(g.order, name)
		}
		g.rows[name] = append(g.rows[name], row)
	}
	return g
}

// groupEntry is one line of the flattened list: a group header or a row.
type groupEntry struct {
	header bool
	group  string // the group (empty for ungrouped rows)
	text   string // what the list shows
	value  string // rows: the row text without its group (what submit returns)
}

// flatten turns the groups into the lines to show. With an empty query a
// collapsed group shows only its header. With a query every group that has a
// match is shown open with just its matches (the group name is part of what a
// row matches, so "yazi seek" finds the rows of the Yazi group), whatever the
// collapsed state; the header then counts the matches.
func (g *groupSet) flatten(collapsed map[string]bool, query, match string) []groupEntry {
	searching := strings.TrimSpace(query) != ""
	var out []groupEntry
	hit := func(group, row string) (int, bool) {
		if !searching {
			return 0, true
		}
		text := group + " " + row
		if match == "smart" {
			s := smartScore(text, query)
			return s, s >= 0
		}
		return 0, strings.Contains(strings.ToLower(text), strings.ToLower(query))
	}
	pick := func(group string, rows []string) []string {
		type scored struct {
			row   string
			score int
		}
		var got []scored
		for _, r := range rows {
			if s, ok := hit(group, r); ok {
				got = append(got, scored{r, s})
			}
		}
		if searching && match == "smart" {
			sort.SliceStable(got, func(i, j int) bool { return got[i].score > got[j].score })
		}
		res := make([]string, len(got))
		for i, s := range got {
			res[i] = s.row
		}
		return res
	}

	for _, r := range pick("", g.ungrouped) {
		out = append(out, groupEntry{text: r, value: r})
	}
	for _, name := range g.order {
		rows := pick(name, g.rows[name])
		if searching && len(rows) == 0 {
			continue
		}
		open := searching || !collapsed[name]
		mark := markCollapsed
		if open {
			mark = markExpanded
		}
		out = append(out, groupEntry{header: true, group: name, text: fmt.Sprintf("%s %s  (%d)", mark, name, len(rows))})
		if !open {
			continue
		}
		for _, r := range rows {
			out = append(out, groupEntry{group: name, text: "    " + r, value: r})
		}
	}
	return out
}

// headerIndex returns the position of group's header in entries, or -1.
func headerIndex(entries []groupEntry, group string) int {
	for i, e := range entries {
		if e.header && e.group == group {
			return i
		}
	}
	return -1
}

// entryTexts is what the list shows for entries.
func entryTexts(entries []groupEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.text
	}
	return out
}
