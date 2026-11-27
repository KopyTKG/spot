package approval

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"gitlab.com/KopyTKG/spot/internal/scan"
)

// Item is one permission as the review shows it.
type Item struct {
	Kind, What, Why string // Why is the author's reason
	New             bool
	Danger          string // "" when nothing is known against it
}

var ordinarySetenv = func(name string) bool {
	return name == "LANG" || name == "TZ" || name == "NO_COLOR" || name == "TERM" || strings.HasPrefix(name, "LC_")
}

func secretName(name string) bool {
	for _, end := range []string{"_TOKEN", "_KEY", "_SECRET", "_PASSWORD", "_PASS"} {
		if strings.HasSuffix(strings.ToUpper(name), end) {
			return true
		}
	}
	return name == "SSH_AUTH_SOCK"
}

func danger(kind, what string) string {
	switch kind {
	case "socket":
		return "whatever listens there does what it is asked, the user bus and the compositor any command: this mod can do anything you can"
	case "net":
		return "can reach any machine on this port, and send data out through name lookups"
	case "netlink":
		return "reads the kernel's network tables"
	case "run":
		return scan.ProgramRisk(what)
	case "read":
		return scan.PlaceRisk(what, false)
	case "write":
		return scan.PlaceRisk(what, true)
	case "env":
		if secretName(what) {
			return "its name says it holds a secret"
		}
	case "setenv":
		if !ordinarySetenv(what) {
			return "a variable like this can make a program load or run something else"
		}
	}
	return ""
}

var kinds = []string{"socket", "net", "netlink", "run", "write", "read", "env", "setenv"}

// Items are the permissions of m: what is new first, then what is dangerous.
func Items(m *Mod, old *Record) []Item {
	perms := m.Provider.Manifest.Permissions
	if perms == nil {
		return nil
	}
	var before map[string]map[string]string
	if old != nil && old.Permissions != nil {
		before = old.Permissions.Kinds()
	}
	var out []Item
	all := perms.Kinds()
	for _, kind := range kinds {
		for _, what := range keys(all[kind]) {
			_, had := before[kind][what]
			out = append(out, Item{Kind: kind, What: what, Why: all[kind][what], New: old != nil && !had, Danger: danger(kind, what)})
		}
	}
	rank := func(i Item) int {
		switch {
		case i.New:
			return 0
		case i.Danger != "":
			return 1
		}
		return 2
	}
	slices.SortStableFunc(out, func(a, b Item) int { return rank(a) - rank(b) })
	return out
}

// Combinations are what several permissions allow together.
func Combinations(items []Item) []string {
	var private, out, later bool
	for _, i := range items {
		switch {
		case i.Kind == "net" || i.Kind == "socket":
			out = true
		case (i.Kind == "read" || i.Kind == "env") && i.Danger != "":
			private = true
		case i.Kind == "write" && i.Danger != "":
			later = true
		}
	}
	var c []string
	if private && out {
		c = append(c, "It can read private things AND reach the network: it could send them away.")
	}
	if later && out {
		c = append(c, "It can reach the network AND write what runs later: what it fetches could be run.")
	}
	return c
}

// Words are what has to be typed to approve: every program it may run, and each
// kind of permission that has something new or dangerous. With neither, the
// mod's name.
func Words(m *Mod, items []Item) []string {
	var out []string
	for _, i := range items {
		w := i.Kind
		if i.Kind == "run" {
			w = i.What
		} else if !i.New && i.Danger == "" {
			continue
		}
		if !slices.Contains(out, w) {
			out = append(out, w)
		}
	}
	if len(out) == 0 {
		return []string{m.Name}
	}
	return out
}

// Accepted reports whether line holds every word, and nothing else.
func Accepted(line string, words []string) bool {
	typed := strings.FieldsFunc(line, func(r rune) bool { return r == ' ' || r == ',' || r == '\t' || r == '\r' || r == '\n' })
	for _, w := range words {
		if !slices.Contains(typed, w) {
			return false
		}
	}
	for _, t := range typed {
		if !slices.Contains(words, t) {
			return false
		}
	}
	return len(typed) > 0
}

// Review writes what there is to know before approving m.
func Review(w io.Writer, m *Mod, old *Record) []Item {
	man := m.Provider.Manifest
	title := m.Name
	if man.Version != "" {
		title += " " + Oneline(man.Version)
	}
	if man.Description != "" {
		title += " — " + fmt.Sprintf("%q", Oneline(man.Description))
	}
	fmt.Fprintf(w, "\n%s (%s)\n", title, man.Type)
	if old != nil && old.Hash != m.Hash {
		fmt.Fprintf(w, "CHANGED since you approved it on %s\n", old.Approved.Format("2006-01-02"))
	}
	fmt.Fprintln(w)

	items := Items(m, old)
	if len(items) == 0 {
		fmt.Fprintln(w, "  It asks for no permissions.")
	}
	for _, i := range items {
		mark := "   "
		if i.New {
			mark = "NEW"
		}
		fmt.Fprintf(w, "  %s  %-7s %s", mark, i.Kind, scan.Quote(i.What))
		if i.Danger != "" {
			fmt.Fprintf(w, "\n                 !! DANGER: %s", i.Danger)
		}
		fmt.Fprintf(w, "\n                 the author says: %q\n", Oneline(i.Why))
	}
	for _, c := range Combinations(items) {
		fmt.Fprintf(w, "\n  !! %s\n", c)
	}
	if len(m.Parsers) > 0 {
		fmt.Fprintf(w, "\nUses the parsers: %s\n", strings.Join(keys(m.Parsers), ", "))
	}
	for _, p := range keys(m.Programs) {
		fmt.Fprintf(w, "\nRuns your own program %s, approved as it is now.\n", scan.Quote(p))
	}

	flagged := findings(m.Name, scan.Mod(m.Files, man.Permissions))
	for _, n := range keys(m.Parsers) {
		flagged = append(flagged, findings(n, scan.Mod(map[string][]byte{"init.lua": m.Parsers[n].Source}, nil))...)
	}
	if len(flagged) > 0 {
		fmt.Fprintln(w, "\nFlagged in the code (each also has honest uses):")
		for _, f := range flagged {
			fmt.Fprintln(w, "  "+f)
		}
	} else {
		fmt.Fprintf(w, "\nNothing flagged in the code. %s\n", scan.Verdict)
	}

	if old != nil && old.Hash != m.Hash {
		var changed []string
		now := m.Texts()
		for _, f := range keys(now) {
			if before, ok := old.Files[f]; !ok {
				changed = append(changed, f+" (new)")
			} else if before != now[f] {
				changed = append(changed, f)
			}
		}
		for _, f := range keys(old.Files) {
			if _, ok := now[f]; !ok {
				changed = append(changed, f+" (removed)")
			}
		}
		if len(changed) > 0 {
			fmt.Fprintf(w, "\nChanged files: %s\n  see them: spot mods diff %s\n", Visible(strings.Join(changed, ", ")), m.Name)
		}
	}
	fmt.Fprintf(w, "\nRead the mod before you approve it:  spot mods show %s\n", m.Name)
	return items
}

// findings leaves out what is about mod.toml: the permissions are shown above.
func findings(name string, r scan.Report) []string {
	var out []string
	for _, n := range r.Flagged {
		if n.Line == 0 {
			continue
		}
		s := name + "/" + n.Where() + "  " + n.What
		if also := n.Also(); also != "" {
			s += " (also at " + also + ")"
		}
		out = append(out, strings.ReplaceAll(s, "\n", " "))
	}
	return out
}
