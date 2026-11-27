package launcher

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"gitlab.com/KopyTKG/spot/internal/config"
)

func TestNewTarget(t *testing.T) {
	for _, c := range []struct{ raw, path, uri string }{
		{"", "", ""},
		{"/tmp/a.txt", "/tmp/a.txt", "file:///tmp/a.txt"},
		{"a.txt", "/work/a.txt", "file:///work/a.txt"},
		{"../a.txt", "/a.txt", "file:///a.txt"},
		{"-rf", "/work/-rf", "file:///work/-rf"},
		{"/tmp/a b#?%.txt", "/tmp/a b#?%.txt", "file:///tmp/a%20b%23%3F%25.txt"},
		{"notes: final.txt", "/work/notes: final.txt", "file:///work/notes:%20final.txt"},
		{"https://example.org/x?q=1&y=2", "https://example.org/x?q=1&y=2", "https://example.org/x?q=1&y=2"},
		{"mailto:someone@example.org", "mailto:someone@example.org", "mailto:someone@example.org"},
		{"file:///tmp/a%20b", "/tmp/a b", "file:///tmp/a%20b"},
		{"file://otherhost/share/a", "file://otherhost/share/a", "file://otherhost/share/a"},
	} {
		got := newTarget(c.raw, "/work")
		if got.path != c.path || got.uri != c.uri {
			t.Errorf("newTarget(%q) = {%q %q}, want {%q %q}", c.raw, got.path, got.uri, c.path, c.uri)
		}
	}
}

func TestExecArgsWithTarget(t *testing.T) {
	tg := newTarget("/tmp/a b", "/")
	for exec, want := range map[string][]string{
		"app %f":             {"app", "/tmp/a b"},
		"app %F --end":       {"app", "/tmp/a b", "--end"},
		"app %u":             {"app", "file:///tmp/a%20b"},
		"app %U":             {"app", "file:///tmp/a%20b"},
		"app --open=%f":      {"app", "--open=/tmp/a b"},
		"app --new":          {"app", "--new", "/tmp/a b"},
		"app %%f":            {"app", "%f", "/tmp/a b"},
		`app "two words" %u`: {"app", "two words", "file:///tmp/a%20b"},
	} {
		if got := execArgs(exec, &AppEntry{}, tg); !reflect.DeepEqual(got, want) {
			t.Errorf("execArgs(%q) = %q, want %q", exec, got, want)
		}
	}
}

// Whatever the file is called, the shell must hand it over as one argument and
// run nothing in it, also through the terminal and launch templates.
func TestTargetSurvivesTheShell(t *testing.T) {
	for _, name := range []string{
		"a b", "it's", `say "hi"`, "$(touch pwned)", "`touch pwned`", "a; touch pwned", "x\ny", `back\slash`, "*", "{exec}", "~", "#c",
	} {
		dir := t.TempDir()
		path := filepath.Join(dir, name)
		for _, e := range []*AppEntry{
			{Exec: "printf [%%s] %f"},
			{Exec: "printf [%%s]", Terminal: true},
		} {
			cfg := termCfg()
			spec := buildLaunch(e, cfg, "/h", newTarget(path, "/"))
			c := exec.Command("sh", "-c", spec.cmd)
			c.Dir = dir
			out, err := c.Output()
			if err != nil {
				t.Fatalf("%q: %v (%s)", name, err, spec.cmd)
			}
			if string(out) != "["+path+"]" {
				t.Errorf("%q arrived as %q (%s)", name, out, spec.cmd)
			}
		}
		if _, err := os.Stat(filepath.Join(dir, "pwned")); err == nil {
			t.Errorf("%q ran a command", name)
		}
	}
}

func TestLaunchHandsOverTheTargetAndDoesNotPassItOn(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	r := &Results{Cfg: noFrecency(), nowFunc: time.Now}
	e := &AppEntry{ID: "x", Exec: `sh -c "echo $1 inherited=$SPOT_TARGET > ` + out + `" sh %f`}
	r.Launch(e, []string{"PATH=" + os.Getenv("PATH"), targetVar + "=doc.txt"}, dir)
	var got []byte
	for i := 0; i < 200 && len(got) == 0; i++ {
		time.Sleep(10 * time.Millisecond)
		got, _ = os.ReadFile(out)
	}
	if want := filepath.Join(dir, "doc.txt") + " inherited="; strings.TrimSpace(string(got)) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDesktopEntryMimeTypes(t *testing.T) {
	parse := func(extra string) []string {
		e, ok := parseDesktopEntry("x", "/x.desktop", []byte("[Desktop Entry]\nType=Application\nName=X\nExec=x\n"+extra), nil)
		if !ok {
			t.Fatal("not parsed")
		}
		return e.MimeTypes
	}
	if got, want := parse("MimeType=text/plain;image/png;x-a/b"+string(rune(92))+";c;\n"), []string{"text/plain", "image/png", "x-a/b;c"}; !reflect.DeepEqual(got, want) {
		t.Errorf("MimeTypes = %q, want %q", got, want)
	}
	if got := parse(""); got != nil {
		t.Errorf("without the key: %q", got)
	}
}

func termCfg() *config.AppsConfig {
	return &config.AppsConfig{Launch: config.AppsLaunch{Command: "env {exec}", Terminal: "env {exec}"}}
}

func noFrecency() *config.AppsConfig {
	off := false
	return &config.AppsConfig{Frecency: &off}
}

func TestTargetWithoutAFolderIsStillAbsolute(t *testing.T) {
	if got := newTarget("-rf", "").path; !filepath.IsAbs(got) {
		t.Errorf("%q could be read as an option", got)
	}
}
