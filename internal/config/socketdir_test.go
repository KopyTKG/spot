package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFallbackSocketLivesInAPrivateFolder(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	sock := filepath.Join(sharedSocketDir(), "spot.sock")
	if err := PrepareSocket(sock); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Dir(sock))
	if err != nil || st.Mode().Perm() != 0o700 {
		t.Fatalf("folder: %v %v", st, err)
	}
	if err := PrepareSocket(sock); err != nil {
		t.Errorf("a second time: %v", err)
	}
	os.Chmod(filepath.Dir(sock), 0o755)
	if PrepareSocket(sock) == nil {
		t.Error("a folder open to others was accepted")
	}
}

func TestCheckPrivateDirRefuses(t *testing.T) {
	tmp := t.TempDir()
	good := filepath.Join(tmp, "good")
	os.Mkdir(good, 0o700)
	link := filepath.Join(tmp, "link")
	os.Symlink(good, link)
	file := filepath.Join(tmp, "file")
	os.WriteFile(file, nil, 0o600)
	if err := checkPrivateDir(good, os.Getuid()); err != nil {
		t.Errorf("our own folder: %v", err)
	}
	for name, err := range map[string]error{
		"another owner": checkPrivateDir(good, os.Getuid()+1),
		"a symlink":     checkPrivateDir(link, os.Getuid()),
		"a file":        checkPrivateDir(file, os.Getuid()),
		"nothing there": checkPrivateDir(filepath.Join(tmp, "no"), os.Getuid()),
	} {
		if err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}
