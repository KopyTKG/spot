package launcher

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLaunchHistoryIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spot", "frecency.json")
	mode := func(when string) {
		t.Helper()
		st, err := os.Stat(path)
		if err != nil || st.Mode().Perm() != 0o600 {
			t.Errorf("%s: %v %v", when, st.Mode().Perm(), err)
		}
	}
	openFrecency(path).bump("a", time.Now())
	mode("new")

	os.Chmod(path, 0o644) // as an earlier release left it
	s := openFrecency(path)
	mode("opened")
	if s.score("a", time.Now()) == 0 {
		t.Error("the history was lost")
	}
	os.WriteFile(path+".tmp", nil, 0o644)
	s.bump("a", time.Now())
	mode("saved over a leftover")
}
