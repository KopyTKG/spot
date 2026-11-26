// Package match ranks and groups list rows against what is typed. No GTK.
package match

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// SmartScore ranks how well query matches text, for list items with
// match = "smart". It returns -1 for no match; higher is better. Each
// whitespace-separated term of the query has to match, and the scores add up.
//
// A term matches, best first, as: a prefix of the text ("rem" ~ "Remote"), a
// prefix of one of its words ("fire" ~ "System Firewall"), a run of word
// initials ("sf"), a contiguous substring ("mot"), or letters in order with
// gaps ("rmt" ~ "Remote"). Case and non-alphanumerics (icons, spaces) are
// ignored, like an abbreviation typed on a network device CLI.
func SmartScore(text, query string) int {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return 0
	}
	t := strings.TrimLeftFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	words := strings.FieldsFunc(t, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	total := 0
	for _, term := range terms {
		s := termScore(t, words, term)
		if s < 0 {
			return -1
		}
		total += s
	}
	return total
}

func termScore(t string, words []string, q string) int {
	if strings.HasPrefix(t, q) {
		return 1000
	}
	for i, w := range words {
		if strings.HasPrefix(w, q) {
			return 900 - i
		}
	}
	initials := make([]rune, 0, len(words))
	for _, w := range words {
		r, _ := utf8.DecodeRuneInString(w)
		initials = append(initials, r)
	}
	if isSubsequence(string(initials), q) {
		return 800
	}
	if i := strings.Index(t, q); i >= 0 {
		return 700 - min(i, 99)
	}
	flat := []rune(strings.Join(words, ""))
	qr := []rune(q)
	if span, ok := subsequenceSpan(string(flat), q); ok {
		bonus := 0
		if len(flat) > 0 && flat[0] == qr[0] {
			bonus = 50
		}
		return 500 + bonus - min(span-len(qr), 99)
	}
	return -1
}

func isSubsequence(s, q string) bool {
	_, ok := subsequenceSpan(s, q)
	return ok
}

// subsequenceSpan reports whether q's letters occur in s in order, and how
// wide the tightest greedy match starting at the first hit is.
func subsequenceSpan(s, q string) (int, bool) {
	sr, qr := []rune(s), []rune(q)
	best, found := 0, false
	for start := range sr {
		if sr[start] != qr[0] {
			continue
		}
		qi, end := 0, start
		for i := start; i < len(sr) && qi < len(qr); i++ {
			if sr[i] == qr[qi] {
				qi++
				end = i
			}
		}
		if qi == len(qr) {
			if span := end - start + 1; !found || span < best {
				best, found = span, true
			}
		}
	}
	return best, found
}

// RankSmart returns the options that match query, best match first (ties keep
// their original order). An empty query returns them all, unchanged.
func RankSmart(opts []string, query string) []string {
	if strings.TrimSpace(query) == "" {
		return opts
	}
	type hit struct {
		s     string
		score int
	}
	var hits []hit
	for _, o := range opts {
		if sc := SmartScore(o, query); sc >= 0 {
			hits = append(hits, hit{o, sc})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.s
	}
	return out
}
