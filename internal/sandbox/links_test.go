package sandbox

import (
	"os"
	"path/filepath"
	"testing"
)

// Configurations are often links into a repository. Neither where they lead nor
// the links themselves may be writable: replacing a link replaces what is there.
func TestProtectedThroughLinks(t *testing.T) {
	d := t.TempDir()
	for _, sub := range []string{"home/.config", "repo/spot-config", "elsewhere/mods/m", "other", "dots/config", "home2"} {
		os.MkdirAll(filepath.Join(d, sub), 0o700)
	}
	os.WriteFile(d+"/other/daemon.toml", nil, 0o600)
	os.Symlink(d+"/repo/spot-config", d+"/home/.config/spot") // the folder is a link
	os.Symlink(d+"/other/daemon.toml", d+"/repo/spot-config/daemon.toml")
	os.Symlink(d+"/elsewhere/mods", d+"/repo/spot-config/mods")
	os.Symlink(d+"/dots/config", d+"/home2/.config") // a folder above is a link
	os.Mkdir(d+"/dots/config/spot", 0o700)

	for name, c := range map[string]struct{ own, write string }{
		"the folder that holds the link": {"home/.config/spot", "home/.config"},
		"where the link leads":           {"home/.config/spot", "repo/spot-config"},
		"above where it leads":           {"home/.config/spot", "repo"},
		"where a linked file leads":      {"home/.config/spot", "other"},
		"where the linked mods lead":     {"home/.config/spot", "elsewhere/mods"},
		"a mod inside them":              {"home/.config/spot", "elsewhere/mods/m"},
		"the holder of a link higher up": {"home2/.config/spot", "home2"},
		"where the higher link leads":    {"home2/.config/spot", "dots"},
	} {
		pr := Protected{NoWrite: []string{filepath.Join(d, c.own)}}
		if err := (Policy{Write: []string{filepath.Join(d, c.write)}}).Check(pr); err == nil {
			t.Errorf("%s: a write grant on %s was accepted", name, c.write)
		}
	}
	// A linked mod inside the linked mods folder.
	os.MkdirAll(d+"/deeper/mine", 0o700)
	os.Symlink(d+"/deeper/mine", d+"/elsewhere/mods/mine")
	if err := (Policy{Write: []string{d + "/deeper"}}).Check(Protected{NoWrite: []string{d + "/home/.config/spot"}}); err == nil {
		t.Error("where a mod linked from a linked folder leads: accepted")
	}
	pr := Protected{NoWrite: []string{d + "/home/.config/spot"}}
	os.MkdirAll(d+"/home/notes", 0o700)
	if err := (Policy{Write: []string{d + "/home/notes"}}).Check(pr); err != nil {
		t.Errorf("an unrelated folder: %v", err)
	}
}
