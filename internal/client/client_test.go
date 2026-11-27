package client

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLogIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.log")
	for _, earlier := range []bool{false, true} {
		if earlier { // left behind by a release that created it 0644
			if err := os.Chmod(path, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		f, err := openLog(path)
		if err != nil {
			t.Fatal(err)
		}
		f.Close()
		st, err := os.Stat(path)
		if err != nil || st.Mode().Perm() != 0o600 {
			t.Errorf("earlier=%v: mode %v, %v", earlier, st.Mode().Perm(), err)
		}
	}
}
