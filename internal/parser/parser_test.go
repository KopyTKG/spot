package parser

import (
	"context"
	"encoding/json"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	lua "github.com/yuin/gopher-lua"

	"gitlab.com/KopyTKG/spot/internal/luaenv"
	"gitlab.com/KopyTKG/spot/internal/mod"
)

func parserOf(name, returns, source string, uses ...string) mod.Mod {
	r := map[string]string{}
	for _, f := range strings.Fields(returns) {
		k, v, _ := strings.Cut(f, ":")
		r[k] = v
	}
	return mod.Mod{Name: name, Manifest: mod.Manifest{Type: "parser", API: 1, Returns: r, Uses: uses}, Source: []byte(source)}
}

func run(t *testing.T, input string, mods ...mod.Mod) (string, error) {
	t.Helper()
	set := map[string]mod.Mod{}
	for _, m := range mods {
		set[m.Name] = m
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	rows, err := (&Call{Mods: set}).Run(ctx, mods[0].Name, input, nil)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	return string(b), nil
}

func TestOnlyTheAllowedGlobalsExist(t *testing.T) {
	L := luaenv.New(true)
	defer L.Close()
	var got []string
	L.G.Global.ForEach(func(k, _ lua.LValue) { got = append(got, k.String()) })
	sort.Strings(got)
	want := slices.Clone(luaenv.Allowed)
	sort.Strings(want)
	if !slices.Equal(got, want) {
		t.Errorf("globals: %v\nwant:    %v", got, want)
	}
}

func TestTheContract(t *testing.T) {
	for name, c := range map[string]struct{ returns, source, out, errs string }{
		"records":          {"a:string n:number? t:list?", `return function(i) return {{a=i, extra={1,2}}, {a="y", n=2, t={}}} end`, `[{"a":"in","extra":[1,2]},{"a":"y","n":2,"t":[]}]`, ""},
		"nothing found":    {"a:string", `return function() return {} end`, `[]`, ""},
		"an empty map":     {"m:map", `return function() return {{m={}}} end`, `[{"m":{}}]`, ""},
		"a failure":        {"a:string", `return function() error("cannot read it") end`, "", "cannot read it"},
		"error with table": {"a:string", `return function() error({}) end`, "", "a table as the error"},
		"nil":              {"a:string", `return function() end`, "", "list of records"},
		"a string":         {"a:string", `return function() return "x" end`, "", "list of records"},
		"one record":       {"a:string", `return function() return {a="x"} end`, "", "list of records"},
		"strings in list":  {"a:string", `return function() return {"x"} end`, "", "record 1 is not a record"},
		"missing field":    {"a:string", `return function() return {{a="x"}, {}} end`, "", `record 2, field "a": expected string, got nil`},
		"wrong type":       {"a:number", `return function() return {{a="x"}} end`, "", `expected number, got string`},
		"a function":       {"a:string?", `return function() return {{f=print or type}} end`, "", "a function is not plain data"},
		"itself":           {"a:string?", `return function() local t = {} t.t = t return {t} end`, "", "contains itself"},
		"holes":            {"a:string?", `return function() return {{l={[1]=1, [3]=3}}} end`, "", "holes"},
		"huge index":       {"a:string?", `return function() return {{l={[1e9]=1}}} end`, "", "holes"},
		"list and record":  {"a:string?", `return function() return {{1, a="x"}} end`, "", "both a list and a record"},
		"not a number":     {"a:number", `return function() return {{a=0/0}} end`, "", "not finite"},
		"infinite":         {"a:number", `return function() return {{a=1/0}} end`, "", "not finite"},
		"too deep":         {"a:string?", `return function() local t = {} for i=1,50 do t = {t=t} end return {t} end`, "", "more than 32 deep"},
		// 2^30 values from 30 tables: depth is fine, the copy is what explodes.
		"shared tables":     {"a:string?", `return function() local t = {1} for i=1,30 do t = {t, t} end return {{t=t}} end`, "", "more than"},
		"too many records":  {"a:string?", `return function() local r = {} for i=1,10001 do r[i] = {} end return r end`, "", "10001 records"},
		"a huge field":      {"a:string", `return function() return {{a=("x"):rep(5000)}} end`, "", "5000 bytes"},
		"endless loop":      {"a:string?", `return function() while true do end end`, "", "took too long"},
		"endless recursion": {"a:string?", `return function() local function f() return 1 + f() end return f() end`, "", "stack overflow"},
		"two functions":     {"a:string?", `return function() end, function() end`, "", "exactly one function"},
		"not a function":    {"a:string?", `return {}`, "", "exactly one function"},
		"syntax":            {"a:string?", `return function(`, "", "parser p:"},
		"no io":             {"a:string?", `return function() io.open("/etc/passwd") end`, "", "nil"},
		"no os":             {"a:string?", `return function() os.execute("true") end`, "", "nil"},
		"no dofile":         {"a:string?", `return function() dofile("/etc/hostname") end`, "", "non-function"},
		"no load":           {"a:string?", `return function() loadstring("return 1")() end`, "", "non-function"},
		"no require":        {"a:string?", `return function() require("os") end`, "", "non-function"},
		"no random":         {"a:string", `return function() return {{a=tostring(math.random)}} end`, `[{"a":"nil"}]`, ""},
		"no addresses":      {"a:string b:string c:string", `return function() local t = {} return {{a=tostring(t), b=("%s"):format(t), c=string.format("%v %s", print or type, coroutine.create(type))}} end`, `[{"a":"table","b":"table","c":"function thread"}]`, ""},
		"__tostring works":  {"a:string", `return function() return {{a=tostring(setmetatable({}, {__tostring=function() return "me" end}))}} end`, `[{"a":"me"}]`, ""},
		// Output is read raw: a metamethod does not run while it is checked.
		"metamethods":      {"a:string", `return function() return {setmetatable({}, {__index=function() error("ran") end})} end`, "", `expected string, got nil`},
		"spot is fixed":    {"a:string?", `return function() spot.log = nil end`, "", "cannot be changed"},
		"rawset on spot":   {"a:string?", `return function() rawset(spot, "log", 1) end`, "", "parser p:"},
		"spot's metatable": {"a:string", `return function() return {{a=tostring(getmetatable(spot))}} end`, `[{"a":"false"}]`, ""},
		"no provider api":  {"a:string", `return function() return {{a=tostring(spot.run)..tostring(spot.read)..tostring(spot.env)}} end`, `[{"a":"nilnilnil"}]`, ""},
		"json":             {"a:string n:number?", `return function() local v = spot.json.decode('[{"a":"x","n":null}, null]') v[1].a = spot.json.encode(v[1]) return v end`, `[{"a":"{\"a\":\"x\"}"}]`, ""},
		"use undeclared":   {"a:string?", `return function() spot.use("other") end`, "", "not listed in this mod's uses"},
		"use a path":       {"a:string?", `return function() spot.use("../x") end`, "", "not listed"},
	} {
		got, err := run(t, "in", parserOf("p", c.returns, c.source))
		switch {
		case c.errs == "" && (err != nil || got != c.out):
			t.Errorf("%s: %s, %v; want %s", name, got, err, c.out)
		case c.errs != "" && (err == nil || !strings.Contains(err.Error(), c.errs)):
			t.Errorf("%s: %s, %v; want an error with %q", name, got, err, c.errs)
		case err != nil && strings.Contains(err.Error(), "0x"):
			t.Errorf("%s: the error shows an address: %v", name, err)
		}
	}
}

// A parser used by another must not be able to reach or change anything of its
// caller, and keeps nothing between calls.
func TestAUsedParserCannotTouchItsCaller(t *testing.T) {
	hostile := parserOf("bad", "a:string", `
count = (count or 0) + 1
return function(input)
  string.format = function() return "hijacked" end
  string.upper = nil
  getmetatable("").__index = {}
  tostring = nil
  table.insert = nil
  return {{a = tostring == nil and "own " .. count or "?"}}
end`)
	caller := parserOf("p", "a:string b:string", `
local parse = spot.use("bad")
return function()
  local first, second = parse("")[1].a, parse("")[1].a
  local t = {}
  table.insert(t, 1)
  return {{a = first .. "," .. second, b = string.format("%d", #t) .. ("ok"):upper()}}
end`, "bad")
	got, err := run(t, "", caller, hostile)
	if want := `[{"a":"own 1,own 1","b":"1OK"}]`; err != nil || got != want {
		t.Errorf("%s, %v; want %s", got, err, want)
	}
}

func TestNestedErrorsAndCircles(t *testing.T) {
	inner := parserOf("inner", "n:number", `return function() return {{n="x"}} end`)
	outer := parserOf("p", "a:string?", `return function() return spot.use("inner")("") end`, "inner")
	if _, err := run(t, "", outer, inner); err == nil || !strings.Contains(err.Error(), `parser inner: record 1, field "n"`) {
		t.Errorf("a nested parser's output is not checked: %v", err)
	}
	caught := parserOf("p", "a:string", `return function() local ok, e = pcall(spot.use("inner"), "") return {{a=tostring(ok)}} end`, "inner")
	if got, err := run(t, "", caught, inner); got != `[{"a":"false"}]` {
		t.Errorf("the caller cannot catch it: %s, %v", got, err)
	}
	a := parserOf("p", "a:string?", `return function() return spot.use("b")("") end`, "b")
	b := parserOf("b", "a:string?", `return function() return spot.use("p")("") end`, "p")
	if _, err := run(t, "", a, b); err == nil || !strings.Contains(err.Error(), "circle") {
		t.Errorf("a circle: %v", err)
	}
}
