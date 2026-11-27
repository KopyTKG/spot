package scan

import (
	"strings"
	"testing"

	"gitlab.com/KopyTKG/spot/internal/config"
)

// A command in every place spot runs one from a dialog.
const everyPlace = `
[[items]]
type = "switch"
id = "s"
state = "echo C1"
on_change = "echo C2"
[[items]]
type = "button"
label = "b"
exec = "echo C3"
[[items]]
type = "list"
id = "l"
run = "echo C4"
parse = "ss"
[[items]]
type = "apps"
id = "a"
[apps.launch]
command = "echo C5"
terminal = "echo C6"
env = { LD_PRELOAD = "C7" }
[[apps.override]]
id = "x"
command = "echo C8"
[[sources]]
name = "n"
prefix = "="
exec = "echo C9"
activate = "echo C10"
[back]
cmd = "echo C11"
[submit]
exec = "echo C12"
`

func TestEveryCommandOfADialogIsShown(t *testing.T) {
	if _, err := config.ParseConfig([]byte(everyPlace), func(string) (string, bool) { return "", false }); err != nil {
		t.Fatalf("the sample must be a config spot accepts: %v", err)
	}
	out := Beside(map[string][]byte{"d.toml": []byte(everyPlace)})
	if len(out) != 1 {
		t.Fatalf("%+v", out)
	}
	all := strings.Join(out[0].Runs, "\n")
	for _, n := range []string{"C1\"", "C2", "C3", "C4", "C5", "C6", "C7", "C8", "C9", "C10", "C11", "C12"} {
		if !strings.Contains(all, n) {
			t.Errorf("not shown: %s", n)
		}
	}
}

func TestWhatRunsOutside(t *testing.T) {
	out := Beside(map[string][]byte{
		"init.lua": []byte("x"), "mod.toml": []byte("[permissions.run]\nss='x'\n"), "lib/a.lua": []byte("x"),
		"style.css": []byte("* {}"), "README.md": []byte("run: x"), "quiet.toml": []byte("[[items]]\ntype='field'\n"), "broken.toml": []byte("exec = ("),
		"d.toml": []byte("[submit]\nexec = \"\\u001b[2Jrm\\nfake\"\n"),
		"a.sh":   []byte("echo"), "noext": []byte("#!/bin/sh\n"), "bin": []byte("\x7fELF..."), "x.py": []byte("print(1)"),
	})
	var names []string
	for _, o := range out {
		names = append(names, o.File)
		for _, r := range o.Runs {
			if strings.ContainsAny(r, "\x1b\n") {
				t.Errorf("a command can disturb the screen: %q", r)
			}
		}
	}
	if got := strings.Join(names, " "); got != "a.sh bin d.toml noext x.py" {
		t.Errorf("%s", got)
	}
}
