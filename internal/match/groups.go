package match

import (
	"fmt"
	"sort"
	"strings"
)

// Collapsible lists ("collapsible = true" on a list item) group their options
// under headers that fold and unfold, e.g. a help menu with one section per app.
// An option line is "GROUP<TAB>ROW"; a line without the separator belongs to no
// group and is always shown, above the groups. This file is the logic only;
// package ui draws it.

const (
	markCollapsed   = "▸"
	markExpanded    = "▾"
	DefaultGroupSep = "\t"
)

// GroupSet is the parsed option lines of a collapsible list.
type GroupSet struct {
	Order     []string            // groups, in order of first appearance
	rows      map[string][]string // group -> its rows, in input order
	ungrouped []string
}

func ParseGroups(opts []string, sep string) *GroupSet {
	if sep == "" {
		sep = DefaultGroupSep
	}
	g := &GroupSet{rows: map[string][]string{}}
	for _, o := range opts {
		name, row, ok := strings.Cut(o, sep)
		if !ok || strings.TrimSpace(name) == "" {
			g.ungrouped = append(g.ungrouped, strings.TrimPrefix(o, sep))
			continue
		}
		if _, seen := g.rows[name]; !seen {
			g.Order = append(g.Order, name)
		}
		g.rows[name] = append(g.rows[name], row)
	}
	return g
}

// GroupEntry is one line of the flattened list: a group header or a row.
type GroupEntry struct {
	Header bool
	Group  string // the group (empty for ungrouped rows)
	text   string // what the list shows
	Value  string // rows: the row text without its group (what submit returns)
}

// Flatten turns the groups into the lines to show. With an empty query a
// collapsed group shows only its header. With a query every group that has a
// match is shown open with just its matches (the group name is part of what a
// row matches, so "yazi seek" finds the rows of the Yazi group), whatever the
// collapsed state; the header then counts the matches.
func (g *GroupSet) Flatten(collapsed map[string]bool, query, match string) []GroupEntry {
	searching := strings.TrimSpace(query) != ""
	var out []GroupEntry
	hit := func(group, row string) (int, bool) {
		if !searching {
			return 0, true
		}
		text := group + " " + row
		if match == "smart" {
			s := SmartScore(text, query)
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
		out = append(out, GroupEntry{text: r, Value: r})
	}
	for _, name := range g.Order {
		rows := pick(name, g.rows[name])
		if searching && len(rows) == 0 {
			continue
		}
		open := searching || !collapsed[name]
		mark := markCollapsed
		if open {
			mark = markExpanded
		}
		out = append(out, GroupEntry{Header: true, Group: name, text: fmt.Sprintf("%s %s  (%d)", mark, name, len(rows))})
		if !open {
			continue
		}
		for _, r := range rows {
			out = append(out, GroupEntry{Group: name, text: "    " + r, Value: r})
		}
	}
	return out
}

// HeaderIndex returns the position of group's header in entries, or -1.
func HeaderIndex(entries []GroupEntry, group string) int {
	for i, e := range entries {
		if e.Header && e.Group == group {
			return i
		}
	}
	return -1
}

// EntryTexts is what the list shows for entries.
func EntryTexts(entries []GroupEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.text
	}
	return out
}
