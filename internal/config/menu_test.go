package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const smallMenu = `
title = "Menu"
[[page.main.entry]]
label = "A"
page = "a"
[page.a]
title = "Apples"
[[page.a.entry]]
label = "B"
page = "b"
[[page.a.entry]]
label = "Back round"
page = "main"
[[page.b.entry]]
label = "run"
exec = "true"
`

func TestMenuParses(t *testing.T) {
	c, err := ParseConfig([]byte(smallMenu), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if !c.IsMenu() || c.StartPage() != "main" || c.PageTitle("main") != "Menu" || c.PageTitle("a") != "Apples" || c.PageTitle("b") != "b" {
		t.Errorf("%v %q %q", c.IsMenu(), c.PageTitle("main"), c.PageTitle("a"))
	}
	if got := c.PagePath("b"); !slices.Equal(got, []string{"main", "a", "b"}) {
		t.Errorf("path %v", got)
	}
	if got := c.PagePath("nowhere"); got != nil {
		t.Errorf("path to nowhere %v", got)
	}
	if w := UnknownKeys([]byte(smallMenu)); len(w) != 0 {
		t.Errorf("warnings %v", w)
	}
	if w := UnknownKeys([]byte("[[page.main.entry]]\nlabel='x'\nexce='y'\n")); len(w) != 1 {
		t.Errorf("a misspelling is not warned about: %v", w)
	}
}

func TestMenuRefuses(t *testing.T) {
	entry := func(body string) string { return "[[page.main.entry]]\n" + body }
	for name, c := range map[string]struct{ toml, want string }{
		"no label":       {entry("exec='x'"), "no label"},
		"two lines":      {entry("label=\"a\\nb\"\nexec='x'"), "one line"},
		"no action":      {entry("label='a'"), "exactly one"},
		"two actions":    {entry("label='a'\nexec='x'\nopen='y'"), "exactly one"},
		"unknown page":   {entry("label='a'\npage='nope'"), "no page"},
		"hold alone":     {entry("label='a'\nexec='x'\nhold=true"), "hold goes"},
		"back alone":     {entry("label='a'\nterminal='x'\nback=true"), "back goes"},
		"open an option": {entry("label='a'\nopen='--help'"), "not an option"},
		"no start":       {"[[page.other.entry]]\nlabel='a'\nexec='x'", "to start on"},
		"empty page":     {"[page.main]\ntitle='x'", "no entries"},
		"with items":     {entry("label='a'\nexec='x'") + "\n[[items]]\ntype='list'\nid='l'", "pages only"},
		"with submit":    {"[submit]\nexec='x'\n" + entry("label='a'\nexec='x'"), "pages only"},
		"menu alone":     {"[menu]\nheight=1", "has no entries"},
		"misspelt entry": {"[[page.main.entries]]\nlabel='a'\nexec='x'", "has no entries"},
		"pages":          {"[[pages.main.entry]]\nlabel='a'\nexec='x'", "has no entries"},
		"one bracket":    {"[page.main]\n[page.main.entry]\nlabel='a'\nexec='x'", "two brackets"},
		"include here":   {"include=['x.toml']", "only a config file"},
	} {
		if _, err := ParseConfig([]byte(c.toml), noEnv); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", name, err, c.want)
		}
	}
}

func TestMenuIncludes(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	write := func(path, text string) {
		os.MkdirAll(filepath.Dir(path), 0o700)
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(root+"/menu/a.toml", "[[page.a.entry]]\nlabel='x'\nexec='true'\n")
	write(outside+"/evil.toml", "[[page.a.entry]]\nlabel='x'\nexec='true'\n")
	load := func(include, extra string) (*Config, error) {
		data := "include=[" + include + "]\n" + extra + "[[page.main.entry]]\nlabel='A'\npage='a'\n"
		return ParseConfigAt(root+"/menu.toml", []byte(data), noEnv)
	}
	if c, err := load("'menu/*.toml'", ""); err != nil || c.Pages["a"] == nil {
		t.Fatalf("a glob: %v", err)
	}
	// The config's folder is itself reached through a link, as ~/.config/spot is.
	link := t.TempDir() + "/spot"
	os.Symlink(root, link)
	if _, err := ParseConfigAt(link+"/menu.toml", []byte("include=['menu/a.toml']\n[[page.main.entry]]\nlabel='A'\npage='a'\n"), noEnv); err != nil {
		t.Errorf("through a link: %v", err)
	}
	os.Symlink(outside+"/evil.toml", root+"/menu/link.toml")
	os.Symlink(outside, root+"/away")
	write(root+"/twice.toml", "[[page.main.entry]]\nlabel='x'\nexec='true'\n")
	write(root+"/other.toml", "title='no'\n")
	os.Mkdir(root+"/dir.toml", 0o700)
	for name, c := range map[string]struct{ include, want string }{
		"missing":             {"'gone.toml'", "no such file"},
		"parent":              {"'../x.toml'", "below the config"},
		"absolute":            {"'" + outside + "/evil.toml'", "below the config"},
		"a link out":          {"'menu/link.toml'", "outside"},
		"a linked folder out": {"'away/evil.toml'", "outside"},
		"defined twice":       {"'twice.toml'", "twice"},
		"other keys":          {"'other.toml'", "only [page.*]"},
		"a folder":            {"'dir.toml'", "not a regular file"},
	} {
		if _, err := load(c.include, ""); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", name, err, c.want)
		}
	}
	// A glob that matches nothing is fine: a folder may be empty.
	if _, err := ParseConfigAt(root+"/menu.toml", []byte("include=['none/*.toml']\n[[page.main.entry]]\nlabel='A'\nexec='x'\n"), noEnv); err != nil {
		t.Errorf("empty glob: %v", err)
	}
}
