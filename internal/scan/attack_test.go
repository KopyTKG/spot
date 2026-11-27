package scan

import (
	"strings"
	"testing"

	"gitlab.com/KopyTKG/spot/internal/mod"
)

// Attempts to get something past the reader of a scan. want is what has to be
// pointed at (or refused) for the attempt to have failed.
func TestAttacks(t *testing.T) {
	run := func(names ...string) *mod.Permissions {
		m := map[string]string{}
		for _, n := range names {
			m[n] = "looks useful"
		}
		return &mod.Permissions{Run: m}
	}
	read := func(p string) *mod.Permissions {
		return &mod.Permissions{Run: map[string]string{"cat": "r"}, Read: map[string]string{p: "r"}}
	}
	long := "~/" + strings.Repeat("a/", 60) + "../.ssh/id_rsa"
	for _, a := range []struct {
		name, src, want string
		perms           *mod.Permissions
	}{
		// hiding text from the reader
		{"reverse as a method", `local p = ("asr_di/hss./~"):reverse()`, "reverse", nil},
		{"reverse held in a variable", `local r = string.reverse local p = r("x")`, "reverse", nil},
		{"char through a string", `local p = ("").char(99, 117)`, "string.char", nil},
		{"the string library renamed", `local s = string local p = s.char(99)`, "string.char", nil},
		{"a computed library function", `local p = string["ch" .. "ar"](99)`, "string", nil},
		{"the library via a metatable", `local s = getmetatable("").__index local p = s["rep"]("a", 2)`, "metatable", nil},
		{"a path cut in the middle", `local p = table.concat({ "~/.s", "sh/i", "d_rsa" })`, "assembles", nil},
		{"the end cut off in the report", `local p = "` + long + `"`, "id_rsa", nil},
		// permissions that are far too wide
		{"all of home", `spot.run({"cat", "x"})`, `"~"`, read("~")},
		{"all of config", `spot.run({"cat", "x"})`, `"~/.config"`, read("~/.config")},
		{"all of /home", `spot.run({"cat", "x"})`, `"/home"`, read("/home")},
		// variables nobody sees
		{"options in a variable", `local o = { env = { LD_PRELOAD = "/tmp/x" } } spot.run({"ss"}, o)`, "options", run("ss")},
		{"computed option key", `local k = "env" spot.run({"ss"}, { [k] = { LD_PRELOAD = "x" } })`, "options", run("ss")},
		// programs that do more than their name says
		{"the loader", `spot.run({"/lib64/ld-linux-x86-64.so.2", "/usr/bin/curl"})`, "can run any other command", run("/lib64/ld-linux-x86-64.so.2")},
		{"busybox", `spot.run({"busybox", "nc"})`, "busybox", run("busybox")},
		{"flock", `spot.run({"flock", "/tmp/l", "x"})`, "flock", run("flock")},
		{"an editor", `spot.run({"nvim", "--headless", "+q"})`, "nvim", run("nvim")},
		{"a terminal", `spot.run({"foot", "x"})`, "foot", run("foot")},
		{"DNS as a way out", `spot.run({"getent", "hosts", "x"})`, "getent", run("getent")},
		{"ping as a way out", `spot.run({"ping", "-c1", "x"})`, "ping", run("ping")},
		{"a package manager", `spot.run({"npm", "exec", "x"})`, "npm", run("npm")},
		{"a look-alike name", `spot.run({"сurl", "x"})`, "unusual characters", run("сurl")},
		{"an IPv6 address", `local a = "2001:db8::dead:beef"`, "IP address", nil},
		// one harmless use first, the harmful one later
		{"a decoy first", "spot.run({\"curl\", \"-V\"})\n\n\nspot.run({\"curl\", \"-T\", \"f\", \"x\"})", "init.lua:4", run("curl")},
		// second round
		{"a method name joined", `local p = ("lruc")["rev" .. "erse"](x)`, "joins", nil},
		{"a name joined", `local a = "exam" .. "ple"`, "joins", nil},
		{"a name from a call", `local p = ("x")[("er"):rep(1)](x)`, "looks up", nil},
		{"escapes below the threshold", `local p = "~/.\115\115\104/\105d_rsa"`, "id_rsa", nil},
		{"the loader by another path", `spot.run({"/usr/lib/ld-linux-x86-64.so.2", "x"})`, "ld.so", run("/usr/lib/ld-linux-x86-64.so.2")},
		{"a versioned interpreter", `spot.run({"python3.14", "-c", "1"})`, "python3.14", run("python3.14")},
		{"home spelled long", `spot.run({"cat", "x"})`, "whole of", read("${HOME}/./")},
		{"home by dots", `spot.run({"cat", "x"})`, "whole of", read("~/.config/../")},
		{"a key folder by a trailing slash", `spot.run({"cat", "x"})`, ".ssh", read("~/.ssh/")},
	} {
		r := scanOf(a.src, a.perms)
		var all strings.Builder
		for _, n := range append(r.Refused, r.Flagged...) {
			all.WriteString(n.Where() + " " + n.What + " | " + n.Why + " " + n.Also() + "\n")
		}
		if !strings.Contains(all.String(), a.want) {
			t.Errorf("GOT PAST: %s", a.name)
		}
	}
}
