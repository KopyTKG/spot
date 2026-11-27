package match

import (
	"math/rand"
	"strings"
	"testing"
	"time"
)

// Rows come from outside (file names, window titles) and matching runs on the UI
// thread at every keystroke.
func TestMatchingARepetitiveRowIsQuick(t *testing.T) {
	row := strings.Repeat("a", 40000)
	start := time.Now()
	SmartScore(row, "aaaab")
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Errorf("one row took %v", d)
	}
}

// slowSpan is the obvious way, trying every start: what the fast one must equal.
func slowSpan(s, q string) (int, bool) {
	sr, qr := []rune(s), []rune(q)
	best, found := 0, false
	for start := range sr {
		if sr[start] != qr[0] {
			continue
		}
		qi, end := 0, start
		for i := start; i < len(sr) && qi < len(qr); i++ {
			if sr[i] == qr[qi] {
				qi, end = qi+1, i
			}
		}
		if qi == len(qr) && (!found || end-start+1 < best) {
			best, found = end-start+1, true
		}
	}
	return best, found
}

func TestSubsequenceSpanEqualsTheObviousOne(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	word := func(n int) string {
		b := make([]rune, n)
		for i := range b {
			b[i] = []rune("abcč ")[r.Intn(5)]
		}
		return string(b)
	}
	for i := 0; i < 20000; i++ {
		s, q := word(r.Intn(20)), word(1+r.Intn(4))
		got, ok := subsequenceSpan(s, q)
		want, wok := slowSpan(s, q)
		if got != want || ok != wok {
			t.Fatalf("subsequenceSpan(%q, %q) = %d,%v want %d,%v", s, q, got, ok, want, wok)
		}
	}
}
