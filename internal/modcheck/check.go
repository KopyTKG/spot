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
	"gitlab.com/KopyTKG/spot/internal/sandbox"
)

const (
	limit    = 5 * time.Second
	junk     = "\x00\x01\x1b[31m ]}{[ <<>> \xff\xfe 0 -1 NaN null\n\n\t\t\n%s%n"
	maxInput = 4 << 20
)

type checker struct {
	host   string
	name   string
	set    map[string]mod.Mod
	out    io.Writer
	h      *modhost.Host
	failed bool
	seen   map[string]bool // fields returned by any record
	rows   int
	slow   time.Duration
}

// clean makes text that came from a mod safe to print (S32): no control
// characters to move the cursor or recolour what spot says.
func clean(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || !unicode.IsControl(r) && !unicode.Is(unicode.Cf, r) {
			return r
		}
		return '?'
	}, s)
	if len(s) > 600 {
		s = s[:600] + "..."
	}
	return strings.ReplaceAll(s, "\n", "\n         ")
}

func (c *checker) say(mark, format string, args ...any) {
	if mark == "FAIL" {
		c.failed = true
	}
	fmt.Fprintf(c.out, "%-5s %s\n", mark, clean(fmt.Sprintf(format, args...)))
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
		c.say("log", "%s", l)
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
		c.say("warn", "no examples: put example/<name>.input and its expected <name>.json, so the result is checked")
		return
	}
	for _, in := range inputs {
		name := strings.TrimSuffix(filepath.Base(in), ".input")
		input, err := os.ReadFile(in)
		if err != nil {
			c.say("FAIL", "example %s: %v", name, err)
			continue
		}
		data, err := os.ReadFile(strings.TrimSuffix(in, ".input") + ".json")
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

// Check checks the parser in dir with the host binary at host, writing to out.
// With live not nil, that text is parsed too. It reports whether all passed.
func Check(host, dir string, live []byte, out io.Writer) bool {
	c := &checker{host: host, out: out, seen: map[string]bool{}}
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
	return !c.failed
}
