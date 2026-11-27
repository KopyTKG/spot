package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"

	"gitlab.com/KopyTKG/spot/internal/approval"
	"gitlab.com/KopyTKG/spot/internal/daemon"
	"gitlab.com/KopyTKG/spot/internal/mod"
	"gitlab.com/KopyTKG/spot/internal/modd"
	"gitlab.com/KopyTKG/spot/internal/modload"
	"gitlab.com/KopyTKG/spot/internal/modperm"
	"gitlab.com/KopyTKG/spot/internal/protocol"
)

func modsError(format string, a ...any) int {
	fmt.Fprintf(os.Stderr, "spot: "+format+"\n", a...)
	return protocol.ExitError
}

// approvable reports why a mod could not run even if approved.
func approvable(s approval.Store, m *approval.Mod) error {
	if why := m.Refused(); len(why) > 0 {
		return errors.New("its code would not be run:\n  " + strings.Join(why, "\n  "))
	}
	grants, err := modperm.Resolve(m.Provider.Manifest.Permissions, s.Getenv)
	if err != nil {
		return err
	}
	return grants.Policy("/nonexistent").Check(modload.Protected(grants.Vars))
}

// allowMod is the one way a mod gets approved: by someone at a terminal, typing
// what they accept. There is no flag that skips any of it.
func allowMod(s approval.Store, name string) int {
	if daemon.IsRoot() {
		return modsError("mods do not run as root, and are not approved as root")
	}
	state, m, old, err := s.State(name)
	if err != nil {
		return modsError("%v", err)
	}
	if state == approval.Approved {
		fmt.Printf("%s is approved, and has not changed since\n", name)
		return 0
	}
	if err := approvable(s, m); err != nil {
		return modsError("%s cannot be approved: %s", name, approval.Visible(err.Error()))
	}
	tty, err := approval.Terminal()
	if err != nil {
		return modsError("%v", err)
	}
	defer tty.Close()
	items := approval.Review(tty, m, old)
	ok, err := approval.Ask(tty, items, approval.Words(m, items), name)
	if err != nil {
		return modsError("%v", err)
	}
	if !ok {
		fmt.Fprintf(tty, "Not approved: %s will not run.\n", name)
		return protocol.ExitError
	}
	if err := s.Approve(m); err != nil {
		return modsError("%v", err)
	}
	if m.Provider.Manifest.Type == "provider" {
		fmt.Fprintf(tty, "Approved. It runs once \"%s\" is in mods in mods.toml.\n", name)
	} else {
		fmt.Fprintf(tty, "Approved.\n")
	}
	return 0
}

func describe(state approval.State, r *approval.Record) string {
	switch state {
	case approval.Approved:
		return "approved " + r.Approved.Format("2006-01-02")
	case approval.Changed:
		return "CHANGED since it was approved: will not run"
	}
	return "not approved: will not run"
}

func showMod(s approval.Store, name string) int {
	state, m, r, err := s.State(name)
	if m == nil {
		return modsError("%v", err)
	}
	fmt.Printf("%s: %s\n", name, describe(state, r))
	if err != nil {
		fmt.Println("  " + err.Error())
	}
	texts := m.Texts()
	files := make([]string, 0, len(texts))
	for f := range texts {
		files = append(files, f)
	}
	sort.Strings(files)
	for _, f := range files {
		fmt.Printf("\n===== %s\n", approval.Visible(f))
		for i, l := range strings.Split(strings.TrimSuffix(texts[f], "\n"), "\n") {
			fmt.Printf("%4d  %s\n", i+1, approval.Visible(l))
		}
	}
	if len(m.Programs) > 0 {
		fmt.Println("\n===== programs of your own")
		for p, d := range m.Programs {
			fmt.Printf("  %s  %s\n", d[:16], approval.Visible(p))
		}
	}
	return 0
}

func diffMod(s approval.Store, name string) int {
	state, m, r, err := s.State(name)
	if err != nil {
		return modsError("%v", err)
	}
	if r == nil {
		return modsError("%s was never approved: read it with spot mods show %s", name, name)
	}
	if state == approval.Approved {
		fmt.Printf("%s has not changed since you approved it\n", name)
		return 0
	}
	now := m.Texts()
	all := map[string]bool{}
	for f := range now {
		all[f] = true
	}
	for f := range r.Files {
		all[f] = true
	}
	files := make([]string, 0, len(all))
	for f := range all {
		files = append(files, f)
	}
	sort.Strings(files)
	changed := false
	for _, f := range files {
		before, had := r.Files[f]
		after, has := now[f]
		if had && has && before == after {
			continue
		}
		changed = true
		fmt.Printf("===== %s%s\n", approval.Visible(f), map[bool]string{!had: " (new)", !has: " (removed)"}[true])
		for _, l := range approval.Diff(before, after) {
			fmt.Println(l)
		}
	}
	if !changed {
		fmt.Println("No file differs: a program of your own that it runs has changed.")
	}
	return 0
}

func listMods() int {
	s, err := modStore()
	if err != nil {
		return modsError("%v", err)
	}
	entries, err := os.ReadDir(s.Mods)
	if errors.Is(err, fs.ErrNotExist) || err == nil && len(entries) == 0 {
		fmt.Printf("No mods in %s\n", s.Mods)
		return 0
	}
	if err != nil {
		return modsError("%v", err)
	}
	doing := modStates()
	// A parser is approved on its own, or as part of each mod that uses it.
	with := map[string][]string{}
	for _, e := range entries {
		if state, m, _, err := s.State(e.Name()); err == nil && state == approval.Approved {
			for parser := range m.Parsers {
				with[parser] = append(with[parser], e.Name())
			}
		}
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if st, err := os.Stat(s.Mods + "/" + name); err != nil || !st.IsDir() {
			continue
		}
		if !mod.ValidName(name) {
			fmt.Printf("%-20q cannot be a mod: small letters, digits, - and _\n", name)
			continue
		}
		state, m, r, err := s.State(name)
		switch {
		case err != nil:
			fmt.Printf("%-20s broken: %s\n", name, approval.Oneline(err.Error()))
		default:
			now := ""
			switch d, known := doing[name]; {
			case known && d.Error != "":
				now = ", FAILING since " + d.Since + ": " + approval.Oneline(d.Error)
			case known && d.Running:
				now = ", running"
			}
			said := describe(state, r)
			if users := with[name]; len(users) > 0 && state != approval.Approved {
				said = "approved as part of " + strings.Join(users, ", ")
			}
			fmt.Printf("%-20s %-9s %s%s\n", name, m.Provider.Manifest.Type, said, now)
		}
	}
	return 0
}

func auditMod(s approval.Store, name string) int {
	b, err := os.ReadFile(modd.AuditPath(s, name))
	if errors.Is(err, fs.ErrNotExist) {
		fmt.Printf("Nothing recorded for %s\n", name)
		return 0
	}
	if err != nil {
		return modsError("%v", err)
	}
	fmt.Print(approval.Visible(string(b)))
	return 0
}
