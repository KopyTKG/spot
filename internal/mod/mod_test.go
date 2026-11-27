package mod

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// What someone gets wrong must be named in their words, not in Go's.
func TestManifestMistakes(t *testing.T) {
	ok := "type='parser'\napi=1\nversion='1'\ndescription='d'\ninput='i'\n[returns]\na='string'\n"
	for name, c := range map[string]struct{ toml, says string }{
		"misspelled":     {strings.Replace(ok, "description", "descripton", 1), "unknown key descripton"},
		"no type":        {strings.Replace(ok, "type='parser'\n", "", 1), `add type = "parser"`},
		"no api":         {strings.Replace(ok, "api=1\n", "", 1), "api = 1 is missing"},
		"api in quotes":  {strings.Replace(ok, "api=1", "api='1'", 1), "api: a TOML string"},
		"a list":         {"[permissions]\nrun=['ss']\n", "run: a TOML array"},
		"no reason":      {"[permissions.run]\nss=''\n", "needs the reason"},
		"singular table": {"[permission.run]\nss='x'\n", "unknown key permission"},
		"not TOML":       {"type = parser\n", "line 1:"},
	} {
		_, err := parseManifest([]byte(c.toml))
		switch {
		case err == nil || !strings.Contains(err.Error(), c.says):
			t.Errorf("%s: %v; want %q", name, err, c.says)
		case strings.Contains(err.Error(), "mod.Manifest") || strings.Contains(err.Error(), "struct"):
			t.Errorf("%s: Go's words: %v", name, err)
		}
	}
	if _, err := parseManifest([]byte(ok)); err != nil {
		t.Errorf("a correct one: %v", err)
	}
}

func TestFileNamesThatCouldForgeALine(t *testing.T) {
	for _, name := range []string{"x\nsock: approved.lua", "a\x1b[31m.lua", "b\u202ec.lua", "sub\n/d.lua"} {
		dir := t.TempDir()
		p := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(p), 0o700)
		os.WriteFile(p, []byte("return 1"), 0o600)
		_, err := Files(dir)
		if err == nil || strings.ContainsAny(err.Error(), "\n\x1b\u202e") {
			t.Errorf("%q: %v", name, err)
		}
	}
}
