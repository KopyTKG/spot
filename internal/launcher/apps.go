// Package launcher is the application index behind type = "apps": .desktop
// entries, launching, frecency and the [[sources]] commands. No GTK.
package launcher

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"gitlab.com/KopyTKG/spot/internal/config"
	"gitlab.com/KopyTKG/spot/internal/match"
	"gitlab.com/KopyTKG/spot/internal/protocol"
)

// The apps module: spot as an application launcher framework. `type = "apps"`
// shows the installed applications (freedesktop .desktop entries) as a searchable
// list; Enter launches the selected one through a command template the user
// controls, so how apps are started (uwsm-app, systemd-run, a terminal wrapper)
// stays in the config, not in Go code. This file is the logic (parse, index,
// rank, launch); package ui draws it.

// AppEntry is one installed application.
type AppEntry struct {
	ID          string // desktop file id: "org.mozilla.firefox"
	Path        string // the .desktop file
	Name        string
	GenericName string
	Comment     string
	Exec        string
	Icon        string
	WorkDir     string // Path= key
	TryExec     string
	Terminal    bool
	NoDisplay   bool
	Hidden      bool
	Keywords    []string
	Categories  []string
	MimeTypes   []string // MimeType=: what the application says it can open
	OnlyShowIn  []string
	NotShowIn   []string
}

// parseDesktopEntry reads the [Desktop Entry] group of a .desktop file. locales
// is the preference order for localised keys ("cs_CZ", "cs"). It returns false
// for anything that is not an Application.
func parseDesktopEntry(id, path string, data []byte, locales []string) (*AppEntry, bool) {
	e := &AppEntry{ID: id, Path: path}
	in := false
	rank := map[string]int{} // localised key -> best locale rank so far (lower is better)
	typ := ""
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			in = line == "[Desktop Entry]"
			continue
		}
		if !in {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key, val = strings.TrimSpace(key), strings.TrimSpace(val)
		base, loc := key, ""
		if i := strings.Index(key, "["); i > 0 && strings.HasSuffix(key, "]") {
			base, loc = key[:i], key[i+1:len(key)-1]
		}
		switch base {
		case "Name", "GenericName", "Comment", "Keywords":
			r := 1 << 20 // unlocalised: worst rank
			if loc != "" {
				r = -1
				for i, l := range locales {
					if l == loc {
						r = i
						break
					}
				}
				if r < 0 {
					continue
				}
			}
			if prev, seen := rank[base]; seen && prev <= r {
				continue
			}
			rank[base] = r
			switch base {
			case "Name":
				e.Name = unescapeDesktop(val)
			case "GenericName":
				e.GenericName = unescapeDesktop(val)
			case "Comment":
				e.Comment = unescapeDesktop(val)
			case "Keywords":
				e.Keywords = splitDesktopList(val)
			}
		}
		if loc != "" {
			continue
		}
		switch key {
		case "Type":
			typ = val
		case "Exec":
			e.Exec = val
		case "Icon":
			e.Icon = val
		case "Path":
			e.WorkDir = val
		case "TryExec":
			e.TryExec = val
		case "Terminal":
			e.Terminal = val == "true"
		case "NoDisplay":
			e.NoDisplay = val == "true"
		case "Hidden":
			e.Hidden = val == "true"
		case "Categories":
			e.Categories = splitDesktopList(val)
		case "MimeType":
			e.MimeTypes = splitDesktopList(val)
		case "OnlyShowIn":
			e.OnlyShowIn = splitDesktopList(val)
		case "NotShowIn":
			e.NotShowIn = splitDesktopList(val)
		}
	}
	if typ != "Application" || e.Name == "" || e.Exec == "" {
		return nil, false
	}
	return e, true
}

// unescapeDesktop resolves the value escapes of the Desktop Entry spec.
func unescapeDesktop(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			switch s[i] {
			case 's':
				b.WriteByte(' ')
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			default:
				b.WriteByte(s[i])
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// splitDesktopList splits a ;-separated list (a \; is a literal semicolon).
func splitDesktopList(s string) []string {
	var out []string
	var cur strings.Builder
	flush := func() {
		if v := strings.TrimSpace(cur.String()); v != "" {
			out = append(out, v)
		}
		cur.Reset()
	}
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\\' && i+1 < len(s) && s[i+1] == ';':
			cur.WriteByte(';')
			i++
		case s[i] == ';':
			flush()
		default:
			cur.WriteByte(s[i])
		}
	}
	flush()
	return out
}

// localePrefs turns $LC_MESSAGES / $LANG into the locale preference list.
func localePrefs(env func(string) string) []string {
	v := env("LC_ALL")
	if v == "" {
		v = env("LC_MESSAGES")
	}
	if v == "" {
		v = env("LANG")
	}
	if i := strings.IndexAny(v, ".@"); i >= 0 {
		v = v[:i]
	}
	if v == "" || v == "C" || v == "POSIX" {
		return nil
	}
	out := []string{v}
	if i := strings.Index(v, "_"); i > 0 {
		out = append(out, v[:i])
	}
	return out
}

// --- visibility -------------------------------------------------------------

func globMatch(patterns []string, names ...string) bool {
	for _, p := range patterns {
		for _, n := range names {
			if ok, _ := filepath.Match(strings.ToLower(p), strings.ToLower(n)); ok {
				return true
			}
		}
	}
	return false
}

func intersects(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if strings.EqualFold(x, y) {
				return true
			}
		}
	}
	return false
}

// visibleApp applies Hidden / NoDisplay, OnlyShowIn / NotShowIn for the given
// desktops, TryExec (looked up with lookPath) and the user's exclude globs.
func visibleApp(e *AppEntry, c *config.AppsConfig, desktops []string, lookPath func(string) error) bool {
	if e.Hidden || e.NoDisplay {
		return false
	}
	if len(e.OnlyShowIn) > 0 && !intersects(e.OnlyShowIn, desktops) {
		return false
	}
	if intersects(e.NotShowIn, desktops) {
		return false
	}
	if e.TryExec != "" && lookPath != nil && lookPath(e.TryExec) != nil {
		return false
	}
	if c != nil && globMatch(c.Exclude, e.ID, e.Name) {
		return false
	}
	return true
}

// --- Exec resolution --------------------------------------------------------

// execArgs splits an Exec line into arguments (quoting rules of the Desktop
// Entry spec) and resolves the field codes: %f %F become the target's path and
// %u %U its URI (both vanish without a target), %i becomes --icon <Icon>, %c the
// name, %k the .desktop path, %% a percent sign; the deprecated codes vanish. A
// line with no file or URL code gets the target's path as its last argument.
func execArgs(exec string, e *AppEntry, t target) []string {
	var args []string
	var cur strings.Builder
	has, quoted := false, false
	flush := func() {
		if has {
			args = append(args, cur.String())
		}
		cur.Reset()
		has = false
	}
	for i := 0; i < len(exec); i++ {
		c := exec[i]
		switch {
		case quoted && c == '\\' && i+1 < len(exec) && strings.IndexByte("\"`$\\", exec[i+1]) >= 0:
			i++
			cur.WriteByte(exec[i])
		case c == '"':
			quoted, has = !quoted, true
		case !quoted && (c == ' ' || c == '\t'):
			flush()
		default:
			cur.WriteByte(c)
			has = true
		}
	}
	flush()

	var out []string
	placed := false // the line has a file or URL code
	for _, a := range args {
		if a == "%i" {
			if e.Icon != "" {
				out = append(out, "--icon", e.Icon)
			}
			continue
		}
		var b strings.Builder
		for i := 0; i < len(a); i++ {
			if a[i] != '%' || i+1 == len(a) {
				b.WriteByte(a[i])
				continue
			}
			i++
			switch a[i] {
			case '%':
				b.WriteByte('%')
			case 'c':
				b.WriteString(e.Name)
			case 'k':
				b.WriteString(e.Path)
			case 'f', 'F':
				b.WriteString(t.path)
				placed = true
			case 'u', 'U':
				b.WriteString(t.uri)
				placed = true
			}
			// every other code (%d %D %n %N %v %m, %i inside an argument) is dropped
		}
		if b.Len() > 0 || a == "" { // an argument that was written empty ("") stays; one a code expanded to nothing goes
			out = append(out, b.String())
		}
	}
	if !placed && !t.empty() {
		out = append(out, t.path)
	}
	return out
}

// ShellQuote makes s one safe word for sh.
func ShellQuote(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r == '_' || r == '-' || r == '.' || r == '/' || r == ':' || r == '=' || r == '+' || r == ',' || r == '@' ||
			r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z')
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// resolvedExec is the entry's Exec line as one shell-safe command: {exec}.
func (e *AppEntry) resolvedExec(t target) string {
	args := execArgs(e.Exec, e, t)
	q := make([]string, len(args))
	for i, a := range args {
		q[i] = ShellQuote(a)
	}
	return strings.Join(q, " ")
}

// launchSpec is what to run for an entry: the command line, the working
// directory ("" = the caller's) and extra environment.
type launchSpec struct {
	cmd string
	dir string
	env []string
}

// buildLaunch applies the [apps.launch] templates and a per-app override.
func buildLaunch(e *AppEntry, c *config.AppsConfig, home string, t target) launchSpec {
	var l config.AppsLaunch
	var overrides []config.AppOverride
	if c != nil {
		l, overrides = c.Launch, c.Override
	}
	run := e.resolvedExec(t)
	var cmd string
	for _, o := range overrides {
		if o.ID == e.ID && o.Command != "" {
			cmd = strings.ReplaceAll(o.Command, "{exec}", run)
		}
	}
	if cmd == "" {
		if e.Terminal {
			term := l.Terminal
			if term == "" {
				term = "xdg-terminal-exec {exec}"
			}
			run = strings.ReplaceAll(term, "{exec}", run)
		}
		tmpl := l.Command
		if tmpl == "" {
			tmpl = "{exec}"
		}
		cmd = strings.ReplaceAll(tmpl, "{exec}", run)
	}
	dir := l.Cwd
	if dir == "" {
		dir = e.WorkDir
	}
	dir = expandHome(dir, home) // not ~someone-else, which is no folder of ours
	var env []string
	for k, v := range l.Env {
		env = append(env, k+"="+v)
	}
	sort.Strings(env)
	return launchSpec{cmd: cmd, dir: dir, env: env}
}

// SpawnDetached starts a launched app in its own session with no stdio, so it
// outlives the dialog (and the daemon) and never receives its signals.
func SpawnDetached(cmd string, env []string, dir string) {
	c := exec.Command("sh", "-c", cmd)
	c.Env, c.Dir = env, dir
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := c.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "spot: launch: %v\n", err)
		return
	}
	go c.Wait()
}

// --- the index --------------------------------------------------------------

// defaultAppPaths is every applications directory the session can see: the XDG
// data dirs, plus the flatpak export dirs, which a daemon started before the
// session environment was complete often lacks.
func defaultAppPaths(env func(string) string, home string) []string {
	var dirs []string
	data := env("XDG_DATA_HOME")
	if data == "" {
		data = filepath.Join(home, ".local/share")
	}
	dirs = append(dirs, data)
	sys := env("XDG_DATA_DIRS")
	if sys == "" {
		sys = "/usr/local/share:/usr/share"
	}
	dirs = append(dirs, strings.Split(sys, ":")...)
	dirs = append(dirs, "/var/lib/flatpak/exports/share", filepath.Join(home, ".local/share/flatpak/exports/share"))
	seen := map[string]bool{}
	var out []string
	for _, d := range dirs {
		if d == "" {
			continue
		}
		p := filepath.Join(d, "applications")
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

func expandHome(p, home string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		return home + p[1:]
	}
	return p
}

// scanApps reads every .desktop file under paths. A desktop id that appears in
// several directories is taken from the first one (user dirs come first).
func scanApps(paths []string, locales []string) []*AppEntry {
	seen := map[string]bool{}
	var out []*AppEntry
	for _, root := range paths {
		_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".desktop") {
				return nil
			}
			rel, _ := filepath.Rel(root, p)
			id := strings.ReplaceAll(strings.TrimSuffix(rel, ".desktop"), string(filepath.Separator), "-")
			if seen[id] {
				return nil
			}
			seen[id] = true
			data, rerr := os.ReadFile(p)
			if rerr != nil {
				return nil
			}
			if e, ok := parseDesktopEntry(id, p, data, locales); ok {
				out = append(out, e)
			}
			return nil
		})
	}
	return out
}

// appIndex caches the scan across dialogs: the daemon keeps it warm, and it is
// rebuilt only when an applications directory changes (a file added or removed).
var appIndex struct {
	sync.Mutex
	key  string
	apps []*AppEntry
}

func dirStamp(paths []string) string {
	var b strings.Builder
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			b.WriteString(p + "|-;")
			continue
		}
		fmt.Fprintf(&b, "%s|%d;", p, st.ModTime().UnixNano())
	}
	return b.String()
}

// appPaths is the list of directories to scan: [apps].paths, else the defaults.
func appPaths(c *config.AppsConfig, env func(string) string, home string) []string {
	if c != nil && len(c.Paths) > 0 {
		var paths []string
		for _, p := range c.Paths {
			paths = append(paths, expandHome(p, home))
		}
		return paths
	}
	return defaultAppPaths(env, home)
}

// loadApps returns the visible applications for c, from the cache when nothing
// changed on disk.
func loadApps(c *config.AppsConfig, env func(string) string, home string) []*AppEntry {
	paths := appPaths(c, env, home)
	desktops := []string(nil)
	if c != nil {
		desktops = c.Desktop
	}
	if len(desktops) == 0 {
		desktops = strings.Split(env("XDG_CURRENT_DESKTOP"), ":")
	}
	key := dirStamp(paths) + strings.Join(localePrefs(env), ",")

	appIndex.Lock()
	if appIndex.key != key {
		appIndex.apps = scanApps(paths, localePrefs(env))
		appIndex.key = key
	}
	all := appIndex.apps
	appIndex.Unlock()

	var out []*AppEntry
	for _, e := range all {
		if visibleApp(e, c, desktops, func(s string) error { _, err := exec.LookPath(s); return err }) {
			out = append(out, e)
		}
	}
	return out
}

// --- ranking ----------------------------------------------------------------

// frecencyStore remembers how often and how recently each app was launched.
type frecencyStore struct {
	path string
	mu   sync.Mutex
	data map[string]frecRecord
}

type frecRecord struct {
	Count int   `json:"count"`
	Last  int64 `json:"last"`
}

func openFrecency(path string) *frecencyStore {
	s := &frecencyStore{path: path, data: map[string]frecRecord{}}
	// It is a history of what was launched: the owner's business only, also when
	// an earlier release left it open to others.
	if st, err := os.Stat(path); err == nil && st.Mode().IsRegular() && st.Mode().Perm() != 0o600 {
		_ = os.Chmod(path, 0o600)
	}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &s.data)
	}
	return s
}

// frecencyPath is $XDG_STATE_HOME/spot/frecency.json.
func frecencyPath(env func(string) string, home string) string {
	dir := env("XDG_STATE_HOME")
	if dir == "" {
		dir = filepath.Join(home, ".local/state")
	}
	return filepath.Join(dir, "spot", "frecency.json")
}

// score weighs launches by age: a launch in the last hour counts four times,
// the last day twice, the last week once, older ones half.
func (s *frecencyStore) score(id string, now time.Time) float64 {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	r, ok := s.data[id]
	s.mu.Unlock()
	if !ok {
		return 0
	}
	age := now.Sub(time.Unix(r.Last, 0))
	w := 0.5
	switch {
	case age < time.Hour:
		w = 4
	case age < 24*time.Hour:
		w = 2
	case age < 7*24*time.Hour:
		w = 1
	}
	return float64(r.Count) * w
}

func (s *frecencyStore) bump(id string, now time.Time) {
	if s == nil {
		return
	}
	s.mu.Lock()
	r := s.data[id]
	r.Count++
	r.Last = now.Unix()
	s.data[id] = r
	b, _ := json.Marshal(s.data)
	s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err == nil {
		// A name of its own (mode 0600): two one-shot runs may write at once.
		f, err := os.CreateTemp(filepath.Dir(s.path), "frecency-*.tmp")
		if err != nil {
			return
		}
		_, werr := f.Write(b)
		if cerr := f.Close(); werr == nil && cerr == nil && os.Rename(f.Name(), s.path) == nil {
			return
		}
		_ = os.Remove(f.Name())
	}
}

// rankApps orders apps for query: best match first, launches you make often
// nudging ties and near-ties; with no query, most-used first, then by name.
// max > 0 caps the result.
func rankApps(apps []*AppEntry, query string, frec *frecencyStore, now time.Time, max int) []*AppEntry {
	type hit struct {
		e     *AppEntry
		score float64
	}
	var hits []hit
	searching := strings.TrimSpace(query) != ""
	for _, e := range apps {
		f := frec.score(e.ID, now)
		if !searching {
			hits = append(hits, hit{e, f})
			continue
		}
		s := match.SmartScore(e.Name, query)
		if s2 := match.SmartScore(strings.Join(append([]string{e.GenericName, e.Comment, e.ID}, e.Keywords...), " "), query); s2 >= 0 && s2-300 > s {
			s = s2 - 300 // matched only through the secondary text: rank below name matches
		}
		if s < 0 {
			continue
		}
		hits = append(hits, hit{e, float64(s) + minF(f*20, 300)})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return strings.ToLower(hits[i].e.Name) < strings.ToLower(hits[j].e.Name)
	})
	if max > 0 && len(hits) > max {
		hits = hits[:max]
	}
	out := make([]*AppEntry, len(hits))
	for i, h := range hits {
		out[i] = h.e
	}
	return out
}

func minF(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// appLine is how an app shows in the list.
func appLine(e *AppEntry, comment bool) string {
	if comment && e.Comment != "" {
		return e.Name + "  —  " + e.Comment
	}
	return e.Name
}

// Results ties the list lines back to the apps they stand for.
type Results struct {
	// Source mode (see sources.go): while the query starts with a source's prefix
	// the list shows that source's rows instead of apps.
	sources   []config.SourceConfig
	Src       *config.SourceConfig // the active source, nil = app search
	SrcQuery  string               // the query without its prefix
	SrcRows   []sourceRow
	srcSeq    int // bumped whenever the query changes: a late result for an old one is dropped
	SrcCancel context.CancelFunc
	Pick      func() int // the selected position in the list (set by the dialog)

	Paths   []string // the directories scanned (icons are looked up next to them)
	Cfg     *config.AppsConfig
	apps    []*AppEntry
	frec    *frecencyStore
	byLine  map[string]*AppEntry
	Home    string
	nowFunc func() time.Time
}

func NewResults(c *config.AppsConfig, sources []config.SourceConfig, env func(string) string, home string) *Results {
	r := &Results{Cfg: c, Home: home, apps: loadApps(c, env, home), byLine: map[string]*AppEntry{}, nowFunc: time.Now}
	r.Paths = appPaths(c, env, home)
	r.sources = sources
	if c.UsesFrecency() {
		r.frec = openFrecency(frecencyPath(env, home))
	}
	return r
}

// lines ranks for query and returns the list lines. Two apps that would show the
// same line get their desktop id appended.
func (r *Results) lines(query string) []string {
	max := 0
	if r.Cfg != nil {
		max = r.Cfg.MaxResults
	}
	ranked := rankApps(r.apps, query, r.frec, r.nowFunc(), max)
	r.byLine = make(map[string]*AppEntry, len(ranked))
	out := make([]string, 0, len(ranked))
	for _, e := range ranked {
		line := appLine(e, r.Cfg.CommentShown())
		if _, dup := r.byLine[line]; dup {
			line += "  (" + e.ID + ")"
		}
		r.byLine[line] = e
		out = append(out, line)
	}
	return out
}

func (r *Results) EntryOf(line string) *AppEntry { return r.byLine[line] }

// Launch starts e as configured and records the launch.
func (r *Results) Launch(e *AppEntry, callerEnv []string, callerDir string) {
	spec := buildLaunch(e, r.Cfg, r.Home, newTarget(EnvGetter(callerEnv)(targetVar), callerDir))
	// The application must not inherit the target: a launcher opened from it
	// later would hand the same file to whatever is picked there.
	var env []string
	for _, kv := range callerEnv {
		if !strings.HasPrefix(kv, targetVar+"=") {
			env = append(env, kv)
		}
	}
	env = append(env, spec.env...)
	dir := spec.dir
	if dir == "" {
		dir = callerDir
	}
	SpawnDetached(spec.cmd, env, dir)
	if r.Cfg.UsesFrecency() {
		r.frec.bump(e.ID, r.nowFunc())
	}
}

func (r *Results) EntryByID(id string) *AppEntry {
	for _, e := range r.apps {
		if e.ID == id {
			return e
		}
	}
	return nil
}

// EnvGetter reads a variable from an "K=V" environment slice (the plain-string
// sibling of envLookup, which also reports presence).
func EnvGetter(env []string) func(string) string {
	look := protocol.EnvLookup(env)
	return func(k string) string { v, _ := look(k); return v }
}

// HomeOf is $HOME from that environment, falling back to the process's.
func HomeOf(env func(string) string) string {
	if h := env("HOME"); h != "" {
		return h
	}
	h, _ := os.UserHomeDir()
	return h
}

// --- source mode ------------------------------------------------------------

// sourceRequest asks the dialog to run src for query, then hand the rows to
// finishSource with seq.
type sourceRequest struct {
	Seq   int
	Src   *config.SourceConfig
	Query string
}

// SetQuery reacts to the search text. For plain app search it returns the list
// lines. When the query starts with a source's prefix it switches to that source
// and returns a request to run (nil while the query is still too short: the list
// is just empty). Any run that is still going for an older query is cancelled.
func (r *Results) SetQuery(query string) (lines []string, req *sourceRequest) {
	r.cancelSource()
	src, rest := matchSource(r.sources, query)
	if src == nil {
		r.Src, r.SrcRows, r.SrcQuery = nil, nil, ""
		return r.lines(query), nil
	}
	r.Src, r.SrcRows, r.SrcQuery = src, nil, rest
	r.srcSeq++
	if len([]rune(rest)) < src.MinQuery() {
		return nil, nil
	}
	return nil, &sourceRequest{Seq: r.srcSeq, Src: src, Query: rest}
}

func (r *Results) cancelSource() {
	if r.SrcCancel != nil {
		r.SrcCancel()
		r.SrcCancel = nil
	}
}

// FinishSource stores a run's rows and returns the list lines, unless the query
// has moved on since the run started (then ok is false and nothing changes).
func (r *Results) FinishSource(seq int, rows []sourceRow) (lines []string, ok bool) {
	if seq != r.srcSeq || r.Src == nil {
		return nil, false
	}
	r.SrcRows = rows
	lines = make([]string, len(rows))
	for i, row := range rows {
		lines[i] = row.Label
	}
	return lines, true
}

// StartLines are the lines a new dialog opens with: the apps, or nothing when a
// default source owns the search (the list fills once something is typed).
func (r *Results) StartLines() []string {
	if config.HasDefaultSource(r.sources) {
		r.SetQuery("")
		return nil
	}
	return r.lines("")
}

// PickedRow is the selected source row, nil when there is none.
func (r *Results) PickedRow() *sourceRow {
	if r.Src == nil || r.Pick == nil {
		return nil
	}
	if i := r.Pick(); i >= 0 && i < len(r.SrcRows) {
		return &r.SrcRows[i]
	}
	return nil
}
