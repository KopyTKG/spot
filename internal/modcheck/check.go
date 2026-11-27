// Package modcheck is `spot mods check`: it holds a parser to its contract
// while it is being written. The parser runs in a sandboxed host that is
// granted nothing.
package modcheck

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"gitlab.com/KopyTKG/spot/internal/mod"
	"gitlab.com/KopyTKG/spot/internal/modhost"
	"gitlab.com/KopyTKG/spot/internal/modhost/channel"
	"gitlab.com/KopyTKG/spot/internal/modload"
	"gitlab.com/KopyTKG/spot/internal/modperm"
	"gitlab.com/KopyTKG/spot/internal/sandbox"
	"gitlab.com/KopyTKG/spot/internal/scan"
)

const (
	limit    = 5 * time.Second
	junk     = "\x00\x01\x1b[31m ]}{[ <<>> \xff\xfe 0 -1 NaN null\n\n\t\t\n%s%n"
	maxInput = 4 << 20
)

type checker struct {
	host    string
	name    string
	set     map[string]mod.Mod
	out     io.Writer
	h       *modhost.Host
	failed  bool
	seen    map[string]bool // fields returned by any record
	rows    int
	slow    time.Duration
	flagged int
	hinted  bool
}

// clean makes text that may hold a mod's own safe to print (S32): one line, no
// control or invisible characters, and no run of spaces to push words to where
// spot's own would stand.
func clean(s string) string {
	s = strings.Join(strings.FieldsFunc(s, func(r rune) bool { return unicode.IsSpace(r) }), " ")
	s = strings.Map(func(r rune) rune {
		if !unicode.IsPrint(r) {
			return '?'
		}
		return r
	}, s)
	if r := []rune(s); len(r) > 300 {
		s = string(r[:300]) + "..."
	}
	return s
}

// say writes one finding. Newlines of the format are spot's and are kept;
// whatever is put into it is cleaned.
func (c *checker) say(mark, format string, args ...any) {
	if mark == "FAIL" {
		c.failed = true
		defer c.hint(args)
	}
	for i, a := range args {
		switch v := a.(type) {
		case string:
			args[i] = clean(v)
		case []byte:
			args[i] = clean(string(v))
		case error:
			args[i] = clean(v.Error())
		}
	}
	text := strings.ReplaceAll(fmt.Sprintf(format, args...), "\n", "\n         ")
	fmt.Fprintf(c.out, "%-5s %s\n", mark, text)
}

// hint explains the Lua error someone gets who expects the whole language.
func (c *checker) hint(args []any) {
	text := fmt.Sprint(args...)
	if c.hinted || !strings.Contains(text, "non-function object") && !strings.Contains(text, "non-table object(nil)") {
		return
	}
	c.hinted = true
	fmt.Fprintf(c.out, "%-5s %s\n", "hint", "a parser has no print, io, os or require, and its spot has only use, json and log (print: use spot.log)")
}

// call runs the parser once, in a new host if the last one died.
func (c *checker) call(input []byte, load bool) ([]map[string]any, []string, error) {
	if c.h == nil {
		h, err := modhost.Start(modhost.Config{Name: c.name, Policy: sandbox.Policy{Self: c.host}, Stderr: nil})
		if err != nil {
			return nil, nil, err
		}
		c.h = h
	}
	start := time.Now()
	raw, log, err := c.h.Parse(channel.Parse{Name: c.name, Mods: c.set, Input: input, Load: load}, limit)
	c.slow = max(c.slow, time.Since(start))
	var fault *modhost.ModError
	if err != nil {
		if !errors.As(err, &fault) {
			c.h = nil
		}
		return nil, log, err
	}
	var rows []map[string]any
	if !load {
		if err := json.Unmarshal(raw, &rows); err != nil {
			return nil, log, fmt.Errorf("the host sent no list: %w", err)
		}
		c.rows += len(rows)
		for _, r := range rows {
			for k := range r {
				c.seen[k] = true
			}
		}
	}
	return rows, log, nil
}

func (c *checker) peak() string {
	if c.h == nil {
		return "unknown"
	}
	b, err := os.ReadFile("/proc/" + strconv.Itoa(c.h.PID()) + "/status")
	if err != nil {
		return "unknown"
	}
	for _, l := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(l, "VmHWM:"); ok {
			return strings.TrimSpace(v)
		}
	}
	return "unknown"
}

// twice runs the parser two times on the same input and requires the same out.
func (c *checker) twice(what string, input []byte) ([]map[string]any, error) {
	rows, log, err := c.call(input, false)
	for _, l := range log {
		c.say("log", "the mod says %s", scan.Quote(l))
	}
	again, _, err2 := c.call(input, false)
	if !reflect.DeepEqual(rows, again) || fmt.Sprint(err) != fmt.Sprint(err2) {
		c.say("FAIL", "%s: the same input twice gives different results", what)
	}
	return rows, err
}

// broken reports whether err is more than the parser saying it cannot read it:
// a broken contract, or a host that died.
func broken(err error) bool {
	var fault *modhost.ModError
	return !errors.As(err, &fault) || fault.Broken
}

func (c *checker) examples(dir string) {
	inputs, _ := filepath.Glob(filepath.Join(dir, "example", "*.input"))
	if len(inputs) == 0 {
		if other, _ := filepath.Glob(filepath.Join(dir, "example", "*")); len(other) > 0 {
			c.say("warn", "example/ has files, but none ends in .input: none of them is checked")
		}
		c.say("warn", "no examples: put example/<name>.input and its expected <name>.json, so the result is checked")
		return
	}
	for _, in := range inputs {
		name := strings.TrimSuffix(filepath.Base(in), ".input")
		input, err := mod.ReadExample(in)
		if err != nil {
			c.say("FAIL", "example %s: %v", name, err)
			continue
		}
		data, err := mod.ReadExample(strings.TrimSuffix(in, ".input") + ".json")
		if err != nil {
			c.say("FAIL", "example %s: %v", name, err)
			continue
		}
		var want []map[string]any
		if err := json.Unmarshal(data, &want); err != nil {
			c.say("FAIL", "example %s: %s.json is not a list of records: %v", name, name, err)
			continue
		}
		got, err := c.twice("example "+name, input)
		switch {
		case err != nil:
			c.say("FAIL", "example %s: %v", name, err)
		case len(got) != len(want):
			c.say("FAIL", "example %s: %d records, expected %d", name, len(got), len(want))
		default:
			same := true
			for i := range got {
				if !reflect.DeepEqual(got[i], want[i]) {
					g, _ := json.Marshal(got[i])
					w, _ := json.Marshal(want[i])
					c.say("FAIL", "example %s, record %d:\ngot      %s\nexpected %s", name, i+1, g, w)
					same = false
					break
				}
			}
			if same {
				c.say("ok", "example %s: %d records as expected", name, len(got))
			}
		}
	}
}

// scan reads a mod's code and says what it found. It reports how many things
// it flagged and whether the code keeps to the strict form.
func (c *checker) scan(dir string, m mod.Mod) (int, bool) {
	files, err := mod.Files(dir)
	if err != nil {
		c.say("FAIL", "mod %s: %v", m.Name, err)
		return 0, false
	}
	// What is read must be what runs, byte for byte (S38).
	files["init.lua"] = m.Source
	r := scan.Mod(files, m.Manifest.Permissions)
	for _, n := range r.Refused {
		c.say("FAIL", "%s/%s  %s", m.Name, n.Where(), n.What)
	}
	for _, n := range r.Flagged {
		c.say("flag", "%s/%s  %s", m.Name, n.Where(), n.What)
		c.say("", "%s", n.Why)
		if also := n.Also(); also != "" {
			c.say("", "also at %s", also)
		}
	}
	return len(r.Flagged), len(r.Refused) == 0
}

// permissions says what in them could never be granted on this machine.
func (c *checker) permissions(m mod.Mod) bool {
	grants, err := modperm.Resolve(m.Manifest.Permissions, os.Getenv)
	if err == nil {
		err = grants.Policy(c.host).Check(modload.Protected(grants.Vars))
	}
	if err != nil {
		c.say("FAIL", "permissions of %s: %v", m.Name, err)
		return false
	}
	return true
}

// verdict never calls a mod safe (S39), and says nothing of the kind about code
// it could not read or would not accept.
func (c *checker) verdict(flagged int, accepted bool) {
	c.flagged = flagged
	if !accepted {
		c.say("scan", "Refused: this mod would not be run, and what it does is not known. %d flagged in what was read.", flagged)
		return
	}
	if flagged == 0 {
		c.say("scan", "Nothing flagged. %s", scan.Verdict)
		return
	}
	c.say("scan", "%d flagged; each also has honest uses. %s", flagged, scan.Verdict)
}

// Check checks the parser in dir with the host binary at host, writing to out.
// With live not nil, that text is parsed too. It reports whether all passed.
func Check(host, dir string, live []byte, out io.Writer) bool {
	c := &checker{host: host, out: out, seen: map[string]bool{}}
	first, err := mod.Load(dir)
	if err != nil {
		c.say("FAIL", "%v", err)
		return false
	}
	if first.Manifest.Type == "provider" {
		flagged, ok := c.scan(dir, first)
		c.verdict(flagged, ok)
		ok = c.permissions(first) && ok
		c.say("note", "this is a provider: its code was read, not run (starting providers waits for approval)")
		return ok
	}
	name, set, err := mod.LoadSet(dir)
	if err != nil {
		c.say("FAIL", "%v", err)
		return false
	}
	c.name, c.set = name, set
	defer func() {
		if c.h != nil {
			c.h.Stop()
		}
	}()

	used := make([]string, 0, len(set))
	for n := range set {
		if n != name {
			used = append(used, n)
		}
	}
	slices.Sort(used)

	// Read the code first: what breaks the strict form is not run.
	flagged, strict := 0, true
	for _, n := range append([]string{name}, used...) {
		at := dir
		if n != name {
			at = filepath.Join(filepath.Dir(filepath.Clean(dir)), n)
		}
		found, ok := c.scan(at, set[n])
		flagged, strict = flagged+found, strict && ok
	}
	c.verdict(flagged, strict)
	if !strict {
		return false
	}
	for _, n := range append(used, name) {
		c.name = n
		_, _, err := c.call(nil, true)
		if err != nil {
			c.say("FAIL", "%v", err)
			return false
		}
	}
	with := ""
	if len(used) > 0 {
		with = ", uses " + strings.Join(used, ", ")
	}
	c.say("ok", "%s %s loads: one function, a complete mod.toml%s", name, set[name].Manifest.Version, with)

	c.examples(dir)

	for _, t := range []struct{ what, input string }{{"empty input", ""}, {"junk input", junk}} {
		rows, err := c.twice(t.what, []byte(t.input))
		switch {
		case err != nil && broken(err):
			c.say("FAIL", "%s: %v", t.what, err)
		case err != nil:
			c.say("ok", "%s: refused with an error", t.what)
		case len(rows) > 0:
			c.say("FAIL", "%s: gives %d records; want {} or error()", t.what, len(rows))
		default:
			c.say("ok", "%s: {}", t.what)
		}
	}

	if live != nil {
		if len(live) > maxInput {
			c.say("FAIL", "live input: more than %d MB", maxInput>>20)
		} else if rows, err := c.twice("live input", live); err != nil {
			c.say("FAIL", "live input: %v", err)
		} else {
			c.say("ok", "live input: %d records", len(rows))
			if len(rows) > 0 {
				first, _ := json.Marshal(rows[0])
				c.say("", "first: %s", first)
			}
		}
	}

	returns := set[name].Manifest.Returns
	var undeclared, never []string
	for f := range c.seen {
		if _, ok := returns[f]; !ok {
			undeclared = append(undeclared, f)
		}
	}
	for f, decl := range returns {
		if _, optional := mod.FieldType(decl); optional && !c.seen[f] && c.rows > 0 {
			never = append(never, f)
		}
	}
	sort.Strings(undeclared)
	sort.Strings(never)
	if len(undeclared) > 0 {
		c.say("warn", "returned but not declared in [returns]: %s", strings.Join(undeclared, ", "))
	}
	if len(never) > 0 {
		c.say("warn", "optional fields never seen: %s", strings.Join(never, ", "))
	}
	c.say("", "slowest call %s, host memory at most %s (limits %s, 256 MB)", c.slow.Round(time.Millisecond), c.peak(), limit)
	// Repeated at the end: whatever a mod printed above, this comes after it.
	c.verdict(c.flagged, true)
	return !c.failed
}
