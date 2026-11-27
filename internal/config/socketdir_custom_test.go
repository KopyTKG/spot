package config

import (
	"os"
	"testing"
)

func TestACustomSocketFolder(t *testing.T) {
	d := t.TempDir()
	for mode, ok := range map[os.FileMode]bool{0o700: true, 0o755: true, 0o775: false, 0o777: false, 0o777 | os.ModeSticky: true} {
		if err := os.Chmod(d, mode); err != nil {
			t.Fatal(err)
		}
		if err := PrepareSocket(d + "/spot.sock"); (err == nil) != ok {
			t.Errorf("mode %v: %v", mode, err)
		}
	}
	os.Chmod(d, 0o700)
	if err := PrepareSocket(d + "/not/there/spot.sock"); err != nil {
		t.Errorf("a missing folder: %v", err)
	}
	if checkSocketDir(d, os.Getuid()+1) == nil {
		t.Error("another user's folder")
	}
}
