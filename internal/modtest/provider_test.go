// Package modtest runs real providers in real sandboxed hosts: what their
// permissions give them, and what happens when they reach for more.
package modtest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"gitlab.com/KopyTKG/spot/internal/modhost"
	"gitlab.com/KopyTKG/spot/internal/modhost/channel"
	"gitlab.com/KopyTKG/spot/internal/modload"
	"gitlab.com/KopyTKG/spot/internal/sandbox"
)

var hostBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "spot-host")
	if err != nil {
		panic(err)
	}
	hostBin = filepath.Join(dir, "spot-mod-host")
	c := exec.Command("go", "build", "-o", hostBin, "gitlab.com/KopyTKG/spot/cmd/spot-mod-host")
	c.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := c.CombinedOutput(); err != nil {
		panic(fmt.Sprintf("building the host: %v\n%s", err, out))
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// world is a home folder with something to read, a folder to write and things
// no mod was granted.
type world struct {
	t    *testing.T
	dir  string
	env  map[string]string
	conf string
}

func newWorld(t *testing.T) *world {
	if sandbox.ABI() < sandbox.MinABI {
		t.Skipf("Landlock ABI %d here, %d needed", sandbox.ABI(), sandbox.MinABI)
	}
	d := t.TempDir()
	w := &world{t: t, dir: d, conf: d + "/.config"}
	w.env = map[string]string{"HOME": d, "XDG_CONFIG_HOME": d + "/.config", "XDG_STATE_HOME": d + "/state",
		"SECRET": "hunter2", "GREETING": "hello"}
	for _, sub := range []string{"readable", "writable", "outside", "empty", ".config/spot/mods", "state/spot"} {
		os.MkdirAll(filepath.Join(d, sub), 0o700)
	}
	w.file("readable/a", "alpha\nbeta\n")
	w.file("outside/secret", "SECRET")
	os.Symlink(d+"/outside/secret", d+"/readable/link-out")
	os.Link(d+"/outside/secret", d+"/readable/hard-out")
	syscall.Mkfifo(d+"/readable/pipe", 0o600)
	return w
}

func (w *world) file(rel, text string) {
	p := filepath.Join(w.dir, rel)
	os.MkdirAll(filepath.Dir(p), 0o700)
	if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
		w.t.Fatal(err)
	}
}

type loaded struct {
	t *testing.T
	h *modhost.Host
	r channel.Reply
}

// start writes the mod, prepares it and loads it into a host of its own.
func (w *world) start(toml, lua string) (*loaded, error) {
	w.file(".config/spot/mods/m/mod.toml", toml)
	w.file(".config/spot/mods/m/init.lua", lua)
	c, l, err := modload.Provider(w.conf+"/spot/mods/m", modload.Options{
		Host: hostBin, Env: func(k string) string { return w.env[k] },
		Protected: sandbox.Protected{Sockets: []string{w.dir + "/run/spot.sock"}, NoWrite: []string{w.conf + "/spot", w.dir + "/state/spot"}},
		EmptyDir:  w.dir + "/empty",
	})
	if err != nil {
		return nil, err
	}
	h, err := modhost.Start(c)
	if err != nil {
		return nil, err
	}
	w.t.Cleanup(h.Stop)
	r, err := h.Load(l)
	return &loaded{w.t, h, r}, err
}

func (w *world) must(toml, lua string) *loaded {
	w.t.Helper()
	l, err := w.start(toml, lua)
	if err != nil {
		w.t.Fatal(err)
	}
	return l
}

func (l *loaded) call(name string, args ...string) (string, error) {
	r, err := l.h.Call(name, args)
	return string(r.Result), err
}

// expect: out is the JSON result, or a part of the error when it starts "!".
func (l *loaded) expect(what, name, out string, args ...string) {
	l.t.Helper()
	got, err := l.call(name, args...)
	if want, fails := strings.CutPrefix(out, "!"); fails {
		if err == nil || !strings.Contains(err.Error(), want) {
			l.t.Errorf("%s: %s, %v; want an error with %q", what, got, err, want)
		}
	} else if err != nil || got != out {
		l.t.Errorf("%s: %s, %v; want %s", what, got, err, out)
	}
}

const reading = `
[permissions.read]
"~/readable" = "r"
"/proc/net" = "r"
"/proc/meminfo" = "r"
[permissions.write]
"~/writable" = "w"
[permissions.env]
GREETING = "r"
NOT_SET = "r"
`

const readingLua = `
spot.on("read", function(a) local s, e = spot.read(a[1]) return s or ("error: " .. e) end)
spot.on("write", function(a) local ok, e = spot.write(a[1], a[2]) return ok or ("error: " .. e) end)
spot.on("list", function(a) local t, e = spot.list(a[1]) if not t then return "error: " .. e end table.sort(t) return t end)
spot.on("exists", function(a) return spot.exists(a[1]) or "nothing" end)
spot.on("lines", function(a) local t = {} for l in spot.lines(a[1]) do t[#t+1] = l end return t end)
spot.on("env", function(a) return spot.env(a[1]) or "unset" end)
spot.on("net", function() return spot.read("/proc/net/dev") ~= nil end)
`

func TestFiles(t *testing.T) {
	w := newWorld(t)
	l := w.must(reading, readingLua)
	d := w.dir

	l.expect("a granted file", "read", `"alpha\nbeta\n"`, "~/readable/a")
	l.expect("by its full path", "lines", `["alpha","beta"]`, d+"/readable/a")
	l.expect("outside", "read", "!no permission", "~/outside/secret")
	l.expect("outside and not there", "read", "!no permission", "~/outside/zzz")
	l.expect("through dots", "read", "!no permission", "~/readable/../outside/secret")
	l.expect("a link out", "read", "!no permission", "~/readable/link-out")
	l.expect("a hard link out", "read", `"error: the file has more than one name"`, "~/readable/hard-out")
	l.expect("a pipe", "read", `"error: not a plain file"`, "~/readable/pipe")
	l.expect("a device", "read", "!no permission", "/dev/zero")
	l.expect("own environment", "read", "!no permission", "/proc/self/environ")
	l.expect("another's", "read", "!no permission", "/proc/1/cmdline")
	l.expect("all of the file system", "list", "!no permission", "/")
	if got, _ := l.call("read", "readable/a"); !strings.Contains(got, "not a full path") {
		t.Errorf("a relative path: %s", got)
	}
	if got, _ := l.call("read", "$SECRET/x"); !strings.Contains(got, "not one of") || strings.Contains(got, "hunter2") {
		t.Errorf("an unknown variable: %s", got)
	}
	l.expect("a proc file by name", "env", `"unset"`, "NOT_SET")
	l.expect("/proc/net, the exception", "net", `true`)
	l.expect("a folder", "list", `["a","hard-out","link-out","pipe"]`, "~/readable")
	l.expect("exists: a file", "exists", `"file"`, "~/readable/a")
	l.expect("exists: nothing", "exists", `"nothing"`, "~/readable/zzz")
	l.expect("exists outside", "exists", "!no permission", "~/outside/secret")
	l.expect("exists outside, missing", "exists", "!no permission", "~/outside/zzz")

	l.expect("write", "write", `true`, "~/writable/new", "one")
	l.expect("replace", "write", `true`, "~/writable/new", "two")
	if b, _ := os.ReadFile(d + "/writable/new"); string(b) != "two" {
		t.Errorf("written: %q", b)
	}
	os.Symlink(d+"/outside/secret", d+"/writable/link")
	os.Link(d+"/outside/secret", d+"/writable/hard")
	os.Symlink(d+"/outside", d+"/writable/dirlink")
	l.expect("write outside", "write", "!no permission", "~/outside/x", "x")
	l.expect("write where only reading", "write", "!no permission", "~/readable/a", "x")
	l.expect("write by dots", "write", "!no permission", "~/writable/../outside/secret", "x")
	l.expect("write over a link", "write", `"error: what is there is not a plain file"`, "~/writable/link", "x")
	l.expect("write over a hard link", "write", `"error: the file has more than one name"`, "~/writable/hard", "x")
	l.expect("write through a linked folder", "write", "!no permission", "~/writable/dirlink/secret", "x")
	l.expect("write spot's own mods", "write", "!no permission", "~/.config/spot/mods/m/init.lua", "x")
	if b, _ := os.ReadFile(d + "/outside/secret"); string(b) != "SECRET" {
		t.Errorf("the file outside was changed: %q", b)
	}

	l.expect("a permitted variable", "env", `"hello"`, "GREETING")
	l.expect("another variable", "env", "!no permission", "SECRET")
	l.expect("HOME unasked", "env", "!no permission", "HOME")
}

func TestPermissionsThatAreNeverGiven(t *testing.T) {
	for name, toml := range map[string]string{
		"all of /proc":                "[permissions.read]\n\"/proc\"='r'\n",
		"spot's configuration":        "[permissions.write]\n\"~/.config/spot\"='w'\n",
		"everything above it":         "[permissions.write]\n\"~\"='w'\n",
		"a single file":               "[permissions.write]\n\"~/readable/a\"='w'\n",
		"LD_PRELOAD":                  "[permissions.setenv]\nLD_PRELOAD='x'\n",
		"PATH":                        "[permissions.setenv]\nPATH='x'\n",
		"a program that is not there": "[permissions.run]\nnosuchprogram='x'\n",
		"port 0":                      "[permissions.net]\n\"0\"='x'\n",
	} {
		w := newWorld(t)
		if _, err := w.start(toml, "return 1"); err == nil {
			t.Errorf("%s: it started", name)
		}
	}
	w := newWorld(t)
	if _, err := w.start("", `local s = spot s.run({"ls"})`); err == nil || !strings.Contains(err.Error(), "would not be run") {
		t.Errorf("code that breaks the strict form: %v", err)
	}
}

const running = `
[permissions.run]
sh = "r"
cat = "r"
echo = "r"
yes = "r"
sleep = "r"
head = "r"
setsid = "r"
pwd = "r"
env = "r"
[permissions.read]
"~/readable" = "r"
[permissions.setenv]
GREETING = "g"
[permissions.env]
GREETING = "g"
`

const runningLua = `
local function out(r) return { r.status, r.stdout, r.stderr } end
spot.on("echo", function(a) return out(spot.run({ "echo", unpack(a) })) end)
spot.on("shell", function(a) return spot.run({ "sh", "-c", a[1] }).stdout end)
spot.on("status", function(a) local r = spot.run({ "sh", "-c", a[1] }) return { r.status, r.stderr } end)
spot.on("pwd", function(a) return spot.run({ "pwd" }, { dir = a[1] }).stdout end)
spot.on("stdin", function(a) return spot.run({ "cat" }, { stdin = a[1] }).stdout end)
spot.on("setenv", function(a) return spot.run({ "env" }, { env = { [a[1]] = a[2] } }).stdout end)
spot.on("slow", function(a) return spot.run({ "sleep", "30" }, { timeout = 200 }).status end)
spot.on("flood", function() return spot.run({ "yes" }).status end)
spot.on("pipe", function() local r = spot.pipe({ "echo", "one" }, { "cat" }, { "head", "-c", "3" }) return { r.status, r.stdout, r.statuses } end)
spot.on("pipefail", function() local r = spot.pipe({ "sh", "-c", "exit 3" }, { "cat" }) return { r.status, r.statuses } end)
spot.on("pipemissing", function() return spot.pipe({ "sh", "-c", "echo ran > ran" }, { "ls" }) end)
spot.on("unlisted", function() return spot.run({ "ls" }) end)
spot.on("path", function() return spot.run({ "/usr/bin/ls" }) end)
spot.on("detach", function() spot.run({ "sh", "-c", "setsid -f sleep 60 >/dev/null 2>&1" }) return true end)
`

func TestPrograms(t *testing.T) {
	w := newWorld(t)
	l := w.must(running, runningLua)

	l.expect("arguments are not a shell's", "echo", `[0,"$HOME; rm -rf /\n",""]`, "$HOME;", "rm", "-rf", "/")
	l.expect("stdin", "stdin", `"typed"`, "typed")
	l.expect("a status and stderr", "status", `[7,"oops\n"]`, "echo oops >&2; exit 7")
	l.expect("not listed", "unlisted", "!no permission")
	l.expect("not listed, by path", "path", "!no permission")
	l.expect("starts in an empty folder", "shell", `"`+w.dir+`/empty\n"`, "pwd")
	l.expect("dir inside read", "pwd", `"`+w.dir+`/readable\n"`, "~/readable")
	l.expect("dir outside", "pwd", "!no permission", "~/outside")
	l.expect("the kernel holds for programs too", "shell", `"1\n"`, "cat "+w.dir+"/outside/secret 2>/dev/null; echo $?")
	l.expect("an unlisted program from a shell", "shell", `"126\n"`, "ls >/dev/null 2>&1; echo $?")
	l.expect("the channel is not passed on", "shell", `"closed\n"`, "(echo x >&3) 2>/dev/null && echo open || echo closed")

	env, err := l.call("shell", "env")
	if err != nil {
		w.t.Fatal(err)
	}
	for _, secret := range []string{"hunter2", "SECRET=", "XDG_RUNTIME_DIR=", "DBUS_SESSION"} {
		if strings.Contains(env, secret) {
			t.Errorf("a program sees %q: %s", secret, env)
		}
	}
	if !strings.Contains(env, "PATH=/usr/bin") || !strings.Contains(env, "GREETING=hello") {
		t.Errorf("environment: %s", env)
	}
	if got, _ := l.call("setenv", "GREETING", "bonjour"); !strings.Contains(got, "GREETING=bonjour") {
		t.Errorf("setenv: %s", got)
	}
	l.expect("setting what was not asked", "setenv", "!no permission", "BASH_ENV", "/tmp/x")
	l.expect("setting PATH", "setenv", "!no permission", "PATH", "/tmp")

	start := time.Now()
	l.expect("too slow", "slow", "!took too long")
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("the timeout took %v", d)
	}
	l.expect("too much output", "flood", "!printed more than")
	l.expect("a pipe", "pipe", `[0,"one",[0,0,0]]`)
	l.expect("pipefail", "pipefail", `[3,[3,0]]`)
	l.expect("one command not allowed", "pipemissing", "!no permission")
	if _, err := os.Stat(w.dir + "/empty/ran"); err == nil {
		t.Error("part of a refused pipeline ran")
	}
	// A program that detaches itself goes with the mod.
	if _, err := l.call("detach"); err != nil {
		t.Fatal(err)
	}
	cg := l.h.Cgroup()
	if cg == "" {
		return
	}
	if procs, _ := os.ReadFile(cg + "/cgroup.procs"); len(strings.Fields(string(procs))) < 2 {
		t.Fatalf("expected the host and a detached program: %q", procs)
	}
	var pids []string
	procs, _ := os.ReadFile(cg + "/cgroup.procs")
	pids = strings.Fields(string(procs))
	l.h.Stop()
	time.Sleep(300 * time.Millisecond)
	for _, p := range pids {
		if b, err := os.ReadFile("/proc/" + p + "/stat"); err == nil && !strings.Contains(string(b), ") Z") {
			t.Errorf("process %s is still running: %s", p, b)
		}
	}
}

func TestValuesParsersAndFiles(t *testing.T) {
	w := newWorld(t)
	w.file(".config/spot/mods/up/mod.toml", "type='parser'\napi=1\nversion='1'\ndescription='d'\ninput='i'\n[returns]\nlabel='string'\n")
	w.file(".config/spot/mods/up/init.lua", `return function(i) return { { label = i:upper() } } end`)
	w.file(".config/spot/mods/m/lib/util.lua", `local n = 0 return { next = function() n = n + 1 return n end, say = spot.config.word }`)
	l := w.must("uses=['up']\n", `
local up = spot.use("up")
local util = spot.require("lib/util")
spot.value("rows", function() return up("x") end)
spot.value("count", function() return util.next() end)
spot.value("kept", { ttl = 60000 }, function() return util.next() end)
spot.publish("said", "now")
spot.on("require", function(a) return spot.require(a[1]) == util end)
spot.on("bad", function() return function() end end)
spot.on("long", function() return ("x"):rep(5000) end)
spot.on("rows", function() local t = {} for i = 1, 10001 do t[i] = { label = "x" } end return t end)
spot.on("again", function() spot.refresh("kept") return true end)
`)
	if got := strings.Join(l.r.Values, ",") + " / " + strings.Join(l.r.Requests, ","); got != "count,kept,rows,said / again,bad,long,require,rows" {
		t.Errorf("published: %s", got)
	}
	get := func(name, want string) {
		t.Helper()
		r, err := l.h.Get(name)
		if err != nil || string(r.Result) != want {
			t.Errorf("get %s: %s, %v; want %s", name, r.Result, err, want)
		}
	}
	get("rows", `[{"label":"X"}]`)
	get("said", `"now"`)
	get("count", `1`)
	get("count", `2`)
	get("kept", `3`)
	get("kept", `3`)
	l.expect("refresh", "again", `true`)
	get("kept", `4`)
	l.expect("the same file again", "require", `true`, "lib/util.lua")
	l.expect("a file the mod does not have", "require", "!the mod has no", "other")
	l.expect("out of the folder", "require", "!by its name", "../up/init")
	l.expect("by full path", "require", "!by its name", "/etc/passwd")
	l.expect("a function as an answer", "bad", "!not plain data")
	l.expect("a huge field", "long", "!5000 bytes")
	l.expect("too many rows", "rows", "!10001 rows")
}

func TestRefusalsAreRecorded(t *testing.T) {
	w := newWorld(t)
	l := w.must(reading, readingLua)
	r, _ := l.h.Call("read", []string{"~/outside/secret"})
	if len(r.Events) != 1 || r.Events[0].Kind != "refused" {
		t.Errorf("events: %+v", r.Events)
	}
	r, _ = l.h.Call("write", []string{"~/writable/x", "private-contents"})
	if len(r.Events) != 1 || strings.Contains(r.Events[0].What, "private-contents") {
		t.Errorf("events: %+v", r.Events)
	}
}

// Arguments are copied once, without metamethods (S42).
func TestAnArgumentCannotChangeAfterTheCheck(t *testing.T) {
	w := newWorld(t)
	l := w.must("[permissions.run]\necho='r'\n", `
spot.on("trick", function()
  local reads = 0
  local list = setmetatable({}, { __index = function() reads = reads + 1 return "ls" end, __len = function() return 1 end })
  local ok = pcall(function() return spot.run({ "echo", list }) end)
  return { ok, reads }
end)
`)
	l.expect("a table with metamethods", "trick", `[false,0]`)
}

func TestAModThatBreaksDoesNotStayStuck(t *testing.T) {
	w := newWorld(t)
	l := w.must("", `
spot.on("loop", function() while true do end end)
spot.on("fine", function() return 1 end)
spot.on("stuck", function() return ("a"):rep(100000):match(".-.-.-.-b$") end)
`)
	l.expect("an endless loop", "loop", "!took too long")
	l.expect("and it still works", "fine", `1`)
	start := time.Now()
	l.expect("a stuck one", "stuck", "!host is gone")
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("took %v", d)
	}
}
