package approval

import (
	"fmt"
	"strings"
	"unicode"
)

// Oneline makes a reason or a description safe on the screen: one line of at
// most 200 characters, with nothing invisible and nothing that reorders text.
func Oneline(s string) string {
	s = strings.Join(strings.FieldsFunc(s, unicode.IsSpace), " ")
	s = strings.Map(func(r rune) rune {
		if !unicode.IsPrint(r) {
			return -1
		}
		return r
	}, s)
	if r := []rune(s); len(r) > 200 {
		s = string(r[:200]) + "..."
	}
	return s
}

// Visible shows a mod's code as it is: what a terminal would obey or hide is
// written out, since removing it would hide what the reader should see.
func Visible(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t':
			b.WriteRune(r)
		case r == unicode.ReplacementChar:
			b.WriteString(`\u{fffd}`)
		case r < 0x80 && !unicode.IsPrint(r):
			fmt.Fprintf(&b, `\x%02x`, r)
		case !unicode.IsPrint(r):
			fmt.Fprintf(&b, `\u{%04x}`, r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

const maxDiffLines = 4000

// Diff is the lines that differ between old and new, with their numbers.
func Diff(old, new string) []string {
	a, b := strings.Split(old, "\n"), strings.Split(new, "\n")
	if len(a) > maxDiffLines || len(b) > maxDiffLines {
		return []string{"  (too long to compare here: read it with spot mods show)"}
	}
	// The longest common subsequence of lines.
	n, m := len(a), len(b)
	l := make([][]int32, n+1)
	for i := range l {
		l[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				l[i][j] = l[i+1][j+1] + 1
			} else {
				l[i][j] = max(l[i+1][j], l[i][j+1])
			}
		}
	}
	var out []string
	for i, j := 0, 0; i < n || j < m; {
		switch {
		case i < n && j < m && a[i] == b[j]:
			i, j = i+1, j+1
		case j == m || i < n && l[i+1][j] >= l[i][j+1]:
			out = append(out, fmt.Sprintf("-%4d  %s", i+1, Visible(a[i])))
			i++
		default:
			out = append(out, fmt.Sprintf("+%4d  %s", j+1, Visible(b[j])))
			j++
		}
	}
	return out
}
