// Package parser runs parser mods by their contract: one function, text and
// options in, a list of records out, checked on every call. Each call has a Lua
// state of its own. It runs inside a mod host, which is sandboxed; this package
// keeps parsers from each other and from their callers.
package parser

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	lua "github.com/yuin/gopher-lua"

	"gitlab.com/KopyTKG/spot/internal/luaenv"
	"gitlab.com/KopyTKG/spot/internal/mod"
)

const (
	maxRows  = 10_000
	maxField = 4 << 10
	maxLogs  = 100
)

// Violation is a parser breaking its contract, as opposed to saying with
// error() that it could not read its input.
type Violation struct{ msg string }

func (v *Violation) Error() string { return v.msg }

// output bounds what a parser returns.
var output = luaenv.Limits{Depth: 32, Values: 1 << 19, Bytes: 8 << 20, String: maxField}

// Call is one use of a set of parsers: the one named by a caller and those it
// uses.
type Call struct {
	Mods map[string]mod.Mod
	Log  []string // what the parsers said, in order
}

// Run calls the parser name. ctx bounds the whole call, nested parsers
// included; it cannot interrupt a slow string pattern, so the process running
// this must also be killable.
func (c *Call) Run(ctx context.Context, name, input string, options any) ([]any, error) {
	return c.run(ctx, name, input, options, nil)
}

// Load checks that the parser name loads: its init.lua gives one function.
func (c *Call) Load(ctx context.Context, name string) error {
	m, ok := c.Mods[name]
	if !ok {
		return fmt.Errorf("parser %s: there is none", name)
	}
	L := luaenv.New(true)
	defer L.Close()
	L.SetContext(ctx)
	_, err := c.function(ctx, L, m, nil)
	return err
}

func (c *Call) run(ctx context.Context, name, input string, options any, chain []string) (_ []any, err error) {
	m, ok := c.Mods[name]
	if !ok {
		return nil, fmt.Errorf("parser %s: there is none", name)
	}
	if slices.Contains(chain, name) {
		return nil, fmt.Errorf("parser %s: uses goes in a circle", name)
	}
	if len(chain) >= mod.MaxDepth {
		return nil, fmt.Errorf("parser %s: uses is more than %d deep", name, mod.MaxDepth)
	}
	chain = append(slices.Clone(chain), name)

	L := luaenv.New(true) // fresh for this call: nothing is kept, nothing shared
	defer L.Close()
	L.SetContext(ctx)
	fn, err := c.function(ctx, L, m, chain)
	if err != nil {
		return nil, err
	}
	if options == nil {
		options = map[string]any{}
	}
	opts, err := luaenv.FromGo(L, options, luaenv.Copy)
	if err != nil {
		return nil, fmt.Errorf("parser %s: options: %w", name, err)
	}
	if err := L.CallByParam(lua.P{Fn: fn, NRet: 1, Protect: true}, lua.LString(input), opts); err != nil {
		msg := fmt.Sprintf("parser %s: %s", name, message(ctx, err))
		if ctx.Err() != nil || crashed(msg) {
			return nil, &Violation{msg}
		}
		return nil, errors.New(msg)
	}
	ret := L.Get(-1)
	L.Pop(1)
	return check(m, ret)
}

// function loads a parser into L and returns the one thing it must give.
func (c *Call) function(ctx context.Context, L *lua.LState, m mod.Mod, chain []string) (*lua.LFunction, error) {
	L.SetGlobal("spot", c.api(ctx, L, m, chain))
	chunk, err := L.Load(bytes.NewReader(m.Source), m.Name+"/init.lua")
	if err != nil {
		return nil, fmt.Errorf("parser %s: %s", m.Name, message(ctx, err))
	}
	base := L.GetTop()
	L.Push(chunk)
	if err := L.PCall(0, lua.MultRet, nil); err != nil {
		return nil, fmt.Errorf("parser %s: %s", m.Name, message(ctx, err))
	}
	n := L.GetTop() - base
	fn, ok := L.Get(-1).(*lua.LFunction)
	if n != 1 || !ok {
		return nil, fmt.Errorf("parser %s: init.lua must return exactly one function", m.Name)
	}
	L.Pop(1)
	return fn, nil
}

// api is a parser's whole `spot`: use, json and log.
func (c *Call) api(ctx context.Context, L *lua.LState, m mod.Mod, chain []string) lua.LValue {
	raise := func(L *lua.LState, format string, args ...any) int {
		L.Error(lua.LString(fmt.Sprintf(format, args...)), 0)
		return 0
	}
	use := L.NewFunction(func(L *lua.LState) int {
		name := L.CheckString(1)
		if !mod.ValidName(name) || !slices.Contains(m.Manifest.Uses, name) {
			return raise(L, "spot.use(%q): not listed in this mod's uses", name)
		}
		L.Push(L.NewFunction(func(L *lua.LState) int {
			input := L.CheckString(1)
			var options any
			if v := L.Get(2); v != lua.LNil {
				var err error
				if options, err = luaenv.ToGo(v, luaenv.Copy); err != nil {
					return raise(L, "options of %s: %v", name, err)
				}
				if list, ok := options.([]any); ok && len(list) == 0 {
					options = map[string]any{}
				}
			}
			rows, err := c.run(ctx, name, input, options, chain)
			if err != nil {
				return raise(L, "%v", err)
			}
			v, err := luaenv.FromGo(L, rows, luaenv.Copy)
			if err != nil {
				return raise(L, "parser %s: %v", name, err)
			}
			L.Push(v)
			return 1
		}))
		return 1
	})

	enc := L.NewTable()
	enc.RawSetString("encode", L.NewFunction(func(L *lua.LState) int {
		v, err := luaenv.ToGo(L.CheckAny(1), luaenv.Copy)
		if err != nil {
			return raise(L, "spot.json.encode: %v", err)
		}
		b, err := json.Marshal(v)
		if err != nil {
			return raise(L, "spot.json.encode: %v", err)
		}
		L.Push(lua.LString(b))
		return 1
	}))
	enc.RawSetString("decode", L.NewFunction(func(L *lua.LState) int {
		var v any
		if err := json.Unmarshal([]byte(L.CheckString(1)), &v); err != nil {
			return raise(L, "spot.json.decode: %v", err)
		}
		l, err := luaenv.FromGo(L, v, luaenv.Copy)
		if err != nil {
			return raise(L, "spot.json.decode: %v", err)
		}
		L.Push(l)
		return 1
	}))

	log := L.NewFunction(func(L *lua.LState) int {
		if len(c.Log) < maxLogs {
			s := L.CheckString(1)
			c.Log = append(c.Log, m.Name+": "+s[:min(len(s), maxField)])
		}
		return 0
	})
	return luaenv.ReadOnly(L, map[string]lua.LValue{"use": use, "json": luaenv.ReadOnly(L, map[string]lua.LValue{
		"encode": enc.RawGetString("encode"), "decode": enc.RawGetString("decode"),
	}), "log": log})
}

// crashed recognises the library's own errors: a mistake in the parser rather
// than its error(). The library does not tell the two apart.
func crashed(msg string) bool {
	return slices.ContainsFunc([]string{"attempt to ", "bad argument", "stack overflow", "cannot perform ", "registry overflow"},
		func(sign string) bool { return strings.Contains(msg, sign) })
}

// message is the text of a Lua error, without running any code of the mod.
func message(ctx context.Context, err error) string {
	if ctx.Err() != nil {
		return "took too long"
	}
	var api *lua.ApiError
	if errors.As(err, &api) {
		if s, ok := api.Object.(lua.LString); ok {
			return string(s)
		}
		if api.Object != nil && api.Object != lua.LNil {
			return "failed with a " + api.Object.Type().String() + " as the error"
		}
		if api.Cause != nil {
			return api.Cause.Error()
		}
	}
	return strings.TrimSpace(err.Error())
}

// check copies what a parser returned and holds it to its declaration.
func check(m mod.Mod, ret lua.LValue) ([]any, error) {
	fail := func(format string, args ...any) ([]any, error) {
		return nil, &Violation{fmt.Sprintf("parser %s: "+format, append([]any{m.Name}, args...)...)}
	}
	if t, ok := ret.(*lua.LTable); ok && t.Len() > maxRows {
		return fail("%d records (at most %d)", t.Len(), maxRows)
	}
	v, err := luaenv.ToGo(ret, output)
	if err != nil {
		return fail("what it returned has %v", err)
	}
	rows, ok := v.([]any)
	if !ok {
		return fail("it must return a list of records, got a %s", ret.Type())
	}
	if len(rows) > maxRows {
		return fail("%d records (at most %d)", len(rows), maxRows)
	}
	fields := make([]string, 0, len(m.Manifest.Returns))
	for f := range m.Manifest.Returns {
		fields = append(fields, f)
	}
	sort.Strings(fields) // the same complaint every time
	for i, row := range rows {
		rec, ok := row.(map[string]any)
		if empty, isList := row.([]any); isList && len(empty) == 0 {
			rec, ok = map[string]any{}, true
			rows[i] = rec
		}
		if !ok {
			return fail("record %d is not a record (a table with named fields)", i+1)
		}
		for _, f := range fields {
			typ, optional := mod.FieldType(m.Manifest.Returns[f])
			x, present := rec[f]
			if !present {
				if optional {
					continue
				}
				return fail("record %d, field %q: expected %s, got nil", i+1, f, typ)
			}
			if list, isList := x.([]any); typ == "map" && isList && len(list) == 0 {
				x = map[string]any{}
				rec[f] = x
			}
			if got := kind(x); got != typ {
				return fail("record %d, field %q: expected %s, got %s", i+1, f, typ, got)
			}
		}
	}
	return rows, nil
}

func kind(v any) string {
	switch v.(type) {
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "boolean"
	case []any:
		return "list"
	case map[string]any:
		return "map"
	}
	return "nil"
}
