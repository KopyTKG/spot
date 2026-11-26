package launcher

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"gitlab.com/KopyTKG/spot/internal/config"
)

const firefoxDesktop = `[Desktop Entry]
Version=1.0
Type=Application
Name=Firefox
Name[cs]=Firefox (cesky)
GenericName=Web Browser
Comment=Browse the World Wide Web
Exec=firefox %u
Icon=firefox
Terminal=false
Categories=Network;WebBrowser;
Keywords=internet;www;browser;web;
Actions=new-window;

[Desktop Action new-window]
Name=New Window
Exec=firefox --new-window
`

func TestParseDesktopEntry(t *testing.T) {
	e, ok := parseDesktopEntry("firefox", "/x/firefox.desktop", []byte(firefoxDesktop), nil)
	if !ok {
		t.Fatal("not parsed")
	}
	if e.Name != "Firefox" || e.GenericName != "Web Browser" || e.Exec != "firefox %u" || e.Icon != "firefox" {
		t.Errorf("fields: %+v", e)
	}
	if !reflect.DeepEqual(e.Keywords, []string{"internet", "www", "browser", "web"}) {
		t.Errorf("keywords = %v", e.Keywords)
	}
	// The [Desktop Action] group must not overwrite the main entry.
	if e.Exec != "firefox %u" || e.Name == "New Window" {
		t.Errorf("action group leaked: %+v", e)
	}
}

func TestParseDesktopEntryLocale(t *testing.T) {
	e, _ := parseDesktopEntry("firefox", "p", []byte(firefoxDesktop), []string{"cs_CZ", "cs"})
	if e.Name != "Firefox (cesky)" {
		t.Errorf("localised name = %q", e.Name)
	}
	e, _ = parseDesktopEntry("firefox", "p", []byte(firefoxDesktop), []string{"de"})
	if e.Name != "Firefox" {
		t.Errorf("fallback name = %q", e.Name)
	}
}

func TestParseDesktopEntryRejects(t *testing.T) {
	for name, src := range map[string]string{
		"link":      "[Desktop Entry]\nType=Link\nName=X\nURL=http://x\n",
		"no exec":   "[Desktop Entry]\nType=Application\nName=X\n",
		"no name":   "[Desktop Entry]\nType=Application\nExec=x\n",
		"not entry": "[Other]\nType=Application\nName=X\nExec=x\n",
	} {
		if _, ok := parseDesktopEntry("x", "p", []byte(src), nil); ok {
			t.Errorf("%s should be rejected", name)
		}
	}
}

func TestEscapesAndLists(t *testing.T) {
	if got := unescapeDesktop(`a\sb\\c\nd`); got != "a b\\c\nd" {
		t.Errorf("unescape = %q", got)
	}
	if got := splitDesktopList(`a;b\;c;;d;`); !reflect.DeepEqual(got, []string{"a", "b;c", "d"}) {
		t.Errorf("list = %q", got)
	}
}

func TestLocalePrefs(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	if got := localePrefs(env(map[string]string{"LANG": "cs_CZ.UTF-8"})); !reflect.DeepEqual(got, []string{"cs_CZ", "cs"}) {
		t.Errorf("cs_CZ = %v", got)
	}
	if got := localePrefs(env(map[string]string{"LANG": "C"})); got != nil {
		t.Errorf("C = %v", got)
	}
	if got := localePrefs(env(map[string]string{"LC_ALL": "de_AT@euro", "LANG": "cs_CZ"})); !reflect.DeepEqual(got, []string{"de_AT", "de"}) {
		t.Errorf("LC_ALL wins: %v", got)
	}
}

func TestExecArgs(t *testing.T) {
	e := &AppEntry{Name: "My App", Icon: "myicon", Path: "/p/app.desktop"}
	for _, c := range []struct {
		exec string
		want []string
	}{
		{"firefox %u", []string{"firefox"}},
		{"vlc --started-from-file %U", []string{"vlc", "--started-from-file"}},
		{`env FOO=bar app --name "My App" %F`, []string{"env", "FOO=bar", "app", "--name", "My App"}},
		{`app "a \"quoted\" arg" %f`, []string{"app", `a "quoted" arg`}},
		{"app %i", []string{"app", "--icon", "myicon"}},
		{"app --title=%c --desktop-file=%k", []string{"app", "--title=My App", "--desktop-file=/p/app.desktop"}},
		{"app 100%% --flag=%f", []string{"app", "100%", "--flag="}},
		{`app "" x`, []string{"app", "", "x"}},
	} {
		if got := execArgs(c.exec, e); !reflect.DeepEqual(got, c.want) {
			t.Errorf("execArgs(%q) = %q, want %q", c.exec, got, c.want)
		}
	}
	if got := execArgs("app %i", &AppEntry{}); !reflect.DeepEqual(got, []string{"app"}) {
		t.Errorf("%%i without an icon: %q", got)
	}
}

func TestResolvedExecQuoting(t *testing.T) {
	e := &AppEntry{Exec: `sh -c "echo 'hi there'; ls"`}
	got := e.resolvedExec()
	want := `sh -c 'echo '\''hi there'\''; ls'`
	if got != want {
		t.Errorf("resolvedExec = %s, want %s", got, want)
	}
	if got := shellQuote("plain-word_1.0/x"); got != "plain-word_1.0/x" {
		t.Errorf("plain word quoted: %s", got)
	}
}

func TestBuildLaunch(t *testing.T) {
	e := &AppEntry{ID: "firefox", Name: "Firefox", Exec: "firefox %u", WorkDir: "/work"}
	// Defaults: just the command.
	if s := buildLaunch(e, nil, "/home/u"); s.cmd != "firefox" || s.dir != "/work" {
		t.Errorf("default: %+v", s)
	}
	cfg := &config.AppsConfig{Launch: config.AppsLaunch{Command: "uwsm-app -- {exec}", Cwd: "~/x", Env: map[string]string{"B": "2", "A": "1"}}}
	s := buildLaunch(e, cfg, "/home/u")
	if s.cmd != "uwsm-app -- firefox" || s.dir != "/home/u/x" || !reflect.DeepEqual(s.env, []string{"A=1", "B=2"}) {
		t.Errorf("template: %+v", s)
	}
	// Terminal apps: the terminal template wraps the app, then the command wraps both.
	term := &AppEntry{ID: "htop", Name: "htop", Exec: "htop", Terminal: true}
	if s := buildLaunch(term, cfg, "/h"); s.cmd != "uwsm-app -- xdg-terminal-exec htop" {
		t.Errorf("terminal default: %q", s.cmd)
	}
	cfg.Launch.Terminal = "alacritty -e {exec}"
	if s := buildLaunch(term, cfg, "/h"); s.cmd != "uwsm-app -- alacritty -e htop" {
		t.Errorf("terminal custom: %q", s.cmd)
	}
	// An override is the whole command.
	cfg.Override = []config.AppOverride{{ID: "firefox", Command: "firefox --new-window --profile {exec}"}}
	if s := buildLaunch(e, cfg, "/h"); s.cmd != "firefox --new-window --profile firefox" {
		t.Errorf("override: %q", s.cmd)
	}
}

func TestVisibleApp(t *testing.T) {
	look := func(s string) error {
		if s == "present" {
			return nil
		}
		return os.ErrNotExist
	}
	base := AppEntry{ID: "org.x.App", Name: "App"}
	cases := []struct {
		name string
		mod  func(*AppEntry)
		c    *config.AppsConfig
		want bool
	}{
		{"plain", func(e *AppEntry) {}, nil, true},
		{"nodisplay", func(e *AppEntry) { e.NoDisplay = true }, nil, false},
		{"hidden", func(e *AppEntry) { e.Hidden = true }, nil, false},
		{"onlyshowin other", func(e *AppEntry) { e.OnlyShowIn = []string{"GNOME"} }, nil, false},
		{"onlyshowin match", func(e *AppEntry) { e.OnlyShowIn = []string{"hyprland"} }, nil, true},
		{"notshowin", func(e *AppEntry) { e.NotShowIn = []string{"Hyprland"} }, nil, false},
		{"tryexec missing", func(e *AppEntry) { e.TryExec = "absent" }, nil, false},
		{"tryexec present", func(e *AppEntry) { e.TryExec = "present" }, nil, true},
		{"exclude id", func(e *AppEntry) {}, &config.AppsConfig{Exclude: []string{"org.x.*"}}, false},
		{"exclude name", func(e *AppEntry) {}, &config.AppsConfig{Exclude: []string{"app"}}, false},
		{"exclude other", func(e *AppEntry) {}, &config.AppsConfig{Exclude: []string{"nope*"}}, true},
	}
	for _, c := range cases {
		e := base
		c.mod(&e)
		if got := visibleApp(&e, c.c, []string{"Hyprland"}, look); got != c.want {
			t.Errorf("%s: visible = %v, want %v", c.name, got, c.want)
		}
	}
}

func writeDesktop(t *testing.T, dir, file, body string) {
	t.Helper()
	p := filepath.Join(dir, file)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanAppsPrecedenceAndIDs(t *testing.T) {
	user, sys := t.TempDir(), t.TempDir()
	writeDesktop(t, user, "tool.desktop", "[Desktop Entry]\nType=Application\nName=User Tool\nExec=tool\n")
	writeDesktop(t, sys, "tool.desktop", "[Desktop Entry]\nType=Application\nName=System Tool\nExec=tool\n")
	writeDesktop(t, sys, "kde/dolphin.desktop", "[Desktop Entry]\nType=Application\nName=Files\nExec=files\n")
	writeDesktop(t, sys, "notes.txt", "ignored")
	apps := scanApps([]string{user, sys}, nil)
	byID := map[string]*AppEntry{}
	for _, a := range apps {
		byID[a.ID] = a
	}
	if byID["tool"] == nil || byID["tool"].Name != "User Tool" {
		t.Errorf("earlier dir should win: %+v", byID["tool"])
	}
	if byID["kde-dolphin"] == nil {
		t.Errorf("nested id should be kde-dolphin, got %v", len(byID))
	}
	if len(apps) != 2 {
		t.Errorf("apps = %d, want 2", len(apps))
	}
}

func TestLoadAppsCachesUntilDirChanges(t *testing.T) {
	dir := t.TempDir()
	writeDesktop(t, dir, "a.desktop", "[Desktop Entry]\nType=Application\nName=Alpha\nExec=a\n")
	env := func(k string) string { return "" }
	cfg := &config.AppsConfig{Paths: []string{dir}, Desktop: []string{"Hyprland"}}
	first := loadApps(cfg, env, "/home/u")
	if len(first) != 1 {
		t.Fatalf("first = %d", len(first))
	}
	// A second dialog gets the same scan.
	appIndex.Lock()
	cached := appIndex.apps
	appIndex.Unlock()
	if loadApps(cfg, env, "/home/u"); len(cached) != 1 {
		t.Errorf("cache lost")
	}
	// A new file bumps the directory mtime: rescanned.
	time.Sleep(20 * time.Millisecond)
	writeDesktop(t, dir, "b.desktop", "[Desktop Entry]\nType=Application\nName=Beta\nExec=b\n")
	if got := loadApps(cfg, env, "/home/u"); len(got) != 2 {
		t.Errorf("after adding a file: %d apps, want 2", len(got))
	}
}

func TestDefaultAppPaths(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	got := defaultAppPaths(env(map[string]string{}), "/h")
	for _, want := range []string{"/h/.local/share/applications", "/usr/share/applications", "/var/lib/flatpak/exports/share/applications", "/h/.local/share/flatpak/exports/share/applications"} {
		found := false
		for _, g := range got {
			if g == want {
				found = true
			}
		}
		if !found {
			t.Errorf("missing %s in %v", want, got)
		}
	}
	got = defaultAppPaths(env(map[string]string{"XDG_DATA_DIRS": "/a:/b", "XDG_DATA_HOME": "/d"}), "/h")
	if got[0] != "/d/applications" || got[1] != "/a/applications" || got[2] != "/b/applications" {
		t.Errorf("custom dirs: %v", got)
	}
}

func appsFixture() []*AppEntry {
	return []*AppEntry{
		{ID: "firefox", Name: "Firefox", Comment: "Browse the web", Keywords: []string{"internet"}},
		{ID: "files", Name: "Files", Comment: "Browse your files"},
		{ID: "foot", Name: "Foot", Comment: "Terminal emulator"},
		{ID: "gimp", Name: "GIMP", GenericName: "Image Editor", Comment: "Create images"},
		{ID: "btop", Name: "btop++", Comment: "Resource monitor", Keywords: []string{"cpu", "memory"}},
	}
}

func names(es []*AppEntry) []string {
	var out []string
	for _, e := range es {
		out = append(out, e.Name)
	}
	return out
}

func TestRankAppsNoQuerySortsByNameThenFrecency(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	got := names(rankApps(appsFixture(), "", nil, now, 0))
	if !reflect.DeepEqual(got, []string{"btop++", "Files", "Firefox", "Foot", "GIMP"}) {
		t.Errorf("by name: %v", got)
	}
	s := &frecencyStore{data: map[string]frecRecord{"gimp": {Count: 5, Last: now.Unix()}, "foot": {Count: 1, Last: now.Unix()}}}
	got = names(rankApps(appsFixture(), "", s, now, 0))
	if got[0] != "GIMP" || got[1] != "Foot" {
		t.Errorf("frecency order: %v", got)
	}
}

func TestRankAppsQuery(t *testing.T) {
	now := time.Now()
	if got := names(rankApps(appsFixture(), "fire", nil, now, 0)); !reflect.DeepEqual(got, []string{"Firefox"}) {
		t.Errorf("fire: %v", got)
	}
	// Name matches beat matches in the comment / keywords.
	if got := names(rankApps(appsFixture(), "files", nil, now, 0)); got[0] != "Files" {
		t.Errorf("files: %v", got)
	}
	// Secondary text finds apps the name does not: "internet" is a keyword.
	if got := names(rankApps(appsFixture(), "internet", nil, now, 0)); !reflect.DeepEqual(got, []string{"Firefox"}) {
		t.Errorf("keyword: %v", got)
	}
	// Abbreviations work as in every smart list.
	if got := names(rankApps(appsFixture(), "ff", nil, now, 0)); len(got) == 0 || got[0] != "Firefox" {
		t.Errorf("ff: %v", got)
	}
	if got := rankApps(appsFixture(), "zzzzqq", nil, now, 0); len(got) != 0 {
		t.Errorf("no match: %v", names(got))
	}
	if got := rankApps(appsFixture(), "", nil, now, 2); len(got) != 2 {
		t.Errorf("max_results: %d", len(got))
	}
}

func TestFrecencyScoreAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "frecency.json")
	now := time.Unix(1_700_000_000, 0)
	s := openFrecency(path)
	s.bump("a", now)
	s.bump("a", now)
	s.bump("b", now.Add(-30*24*time.Hour))
	if got := s.score("a", now); got != 8 { // 2 launches x4 (within the hour)
		t.Errorf("recent score = %v", got)
	}
	if got := s.score("b", now); got != 0.5 {
		t.Errorf("old score = %v", got)
	}
	if got := s.score("none", now); got != 0 {
		t.Errorf("unknown = %v", got)
	}
	re := openFrecency(path) // survives a restart
	if re.data["a"].Count != 2 {
		t.Errorf("persisted: %+v", re.data)
	}
	var nilStore *frecencyStore
	nilStore.bump("x", now) // a disabled store is a no-op, not a crash
	if nilStore.score("x", now) != 0 {
		t.Error("nil store score")
	}
}

func TestAppResultsLinesAndLookup(t *testing.T) {
	r := &Results{
		Cfg:     &config.AppsConfig{},
		apps:    append(appsFixture(), &AppEntry{ID: "files2", Name: "Files", Comment: "Browse your files"}),
		byLine:  map[string]*AppEntry{},
		nowFunc: time.Now,
	}
	lines := r.lines("")
	seen := map[string]bool{}
	for _, l := range lines {
		if seen[l] {
			t.Errorf("duplicate line %q", l)
		}
		seen[l] = true
		if r.EntryOf(l) == nil {
			t.Errorf("line %q maps to nothing", l)
		}
	}
	hasID := false
	for _, l := range lines {
		if strings.HasSuffix(l, "(files)") || strings.HasSuffix(l, "(files2)") {
			hasID = true
		}
	}
	if !hasID {
		t.Errorf("colliding lines should be disambiguated by id: %q", lines)
	}
	no := false
	r.Cfg = &config.AppsConfig{ShowComment: &no}
	if l := r.lines("fire"); len(l) != 1 || l[0] != "Firefox" {
		t.Errorf("without comments: %q", l)
	}
	r.Cfg = &config.AppsConfig{}
	if l := r.lines("fire"); len(l) != 1 || l[0] != "Firefox  —  Browse the web" {
		t.Errorf("with comment: %q", l)
	}
}

// BenchmarkScanApps measures a cold scan of the real applications directories
// (the launcher plan wants this to stay fast with ~500 apps).
//
//	go test -run X -bench ScanApps ./...
func BenchmarkScanApps(b *testing.B) {
	home, _ := os.UserHomeDir()
	paths := defaultAppPaths(os.Getenv, home)
	var n int
	for i := 0; i < b.N; i++ {
		n = len(scanApps(paths, localePrefs(os.Getenv)))
	}
	b.ReportMetric(float64(n), "apps")
}

func TestAppsIconConfig(t *testing.T) {
	var none *config.AppsConfig
	if !none.IconsShown() || none.IconPx() != 32 {
		t.Errorf("defaults: icons=%v size=%d", none.IconsShown(), none.IconPx())
	}
	off := false
	c := &config.AppsConfig{Icons: &off, IconSize: 48}
	if c.IconsShown() || c.IconPx() != 48 {
		t.Errorf("explicit: icons=%v size=%d", c.IconsShown(), c.IconPx())
	}
	cfg, err := config.ParseConfig([]byte("[[items]]\ntype=\"apps\"\nid=\"a\"\n[apps]\nicons=false\nicon_size=64\n"), noEnv)
	if err != nil || cfg.Apps.IconsShown() || cfg.Apps.IconPx() != 64 {
		t.Errorf("parsed: err=%v apps=%+v", err, cfg.Apps)
	}
	if _, err := config.ParseConfig([]byte("[[items]]\ntype=\"apps\"\nid=\"a\"\n[apps]\nicon_size=-1\n"), noEnv); err == nil {
		t.Error("a negative icon_size should be rejected")
	}
}

// noEnv is an environment with nothing in it.
func noEnv(string) (string, bool) { return "", false }
