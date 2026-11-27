// Package luaenv makes the Lua states mod code runs in and copies plain data in
// and out of them. A state has an allow-list of globals and nothing else.
package luaenv

import (
	"slices"

	lua "github.com/yuin/gopher-lua"
)

// Allowed are the globals of every mod, besides what spot adds. What is not
// here is not there, also whatever a later version of the Lua library adds.
var Allowed = []string{
	"assert", "error", "getmetatable", "ipairs", "next", "pairs", "pcall", "rawequal", "rawget", "rawset",
	"select", "setmetatable", "tonumber", "tostring", "type", "unpack", "xpcall",
	"string", "table", "math", "coroutine",
}

// New is a state with nothing that reaches outside it. With deterministic, also
// nothing that differs from one run to the next.
func New(deterministic bool) *lua.LState {
	L := lua.NewState(lua.Options{SkipOpenLibs: true, CallStackSize: 200, RegistrySize: 1 << 12, RegistryMaxSize: 1 << 18, RegistryGrowStep: 1 << 10})
	for _, lib := range []struct {
		name string
		open lua.LGFunction
	}{{lua.BaseLibName, lua.OpenBase}, {lua.TabLibName, lua.OpenTable}, {lua.StringLibName, lua.OpenString},
		{lua.MathLibName, lua.OpenMath}, {lua.CoroutineLibName, lua.OpenCoroutine}} {
		L.Push(L.NewFunction(lib.open))
		L.Push(lua.LString(lib.name))
		L.Call(1, 0)
	}
	var remove []lua.LValue
	L.G.Global.ForEach(func(k, _ lua.LValue) {
		if s, ok := k.(lua.LString); !ok || !slices.Contains(Allowed, string(s)) {
			remove = append(remove, k)
		}
	})
	for _, k := range remove {
		L.G.Global.RawSet(k, lua.LNil)
	}

	str := L.G.Global.RawGetString("string").(*lua.LTable)
	str.RawSetString("dump", lua.LNil)
	if deterministic {
		m := L.G.Global.RawGetString("math").(*lua.LTable)
		m.RawSetString("random", lua.LNil)
		m.RawSetString("randomseed", lua.LNil)
		// The library prints the address of a table or function, which differs
		// every time and says something about our memory.
		L.G.Global.RawSetString("tostring", L.NewFunction(func(L *lua.LState) int {
			L.Push(lua.LString(show(L, L.CheckAny(1))))
			return 1
		}))
		format := str.RawGetString("format")
		str.RawSetString("format", L.NewFunction(func(L *lua.LState) int {
			n := L.GetTop()
			args := make([]lua.LValue, n)
			for i := range args {
				v := L.Get(i + 1)
				switch v.Type() {
				case lua.LTString, lua.LTNumber:
					args[i] = v
				default:
					args[i] = lua.LString(show(L, v))
				}
			}
			L.Push(format)
			for _, a := range args {
				L.Push(a)
			}
			L.Call(n, 1)
			return 1
		}))
	}
	return L
}

// show is tostring without addresses.
func show(L *lua.LState, v lua.LValue) string {
	switch v.Type() {
	case lua.LTNil, lua.LTBool, lua.LTNumber, lua.LTString:
		return v.String()
	}
	if f, ok := L.GetMetaField(v, "__tostring").(*lua.LFunction); ok {
		L.Push(f)
		L.Push(v)
		L.Call(1, 1)
		r := L.Get(-1)
		L.Pop(1)
		if s, ok := r.(lua.LString); ok {
			return string(s)
		}
	}
	return v.Type().String()
}

// ReadOnly is what mods see as `spot`: a userdata, so that not even rawset can
// change it, giving read access to fields and hiding its metatable.
func ReadOnly(L *lua.LState, fields map[string]lua.LValue) *lua.LUserData {
	index := L.NewTable()
	for k, v := range fields {
		index.RawSetString(k, v)
	}
	mt := L.NewTable()
	mt.RawSetString("__index", L.NewFunction(func(L *lua.LState) int {
		L.Push(index.RawGet(L.CheckAny(2)))
		return 1
	}))
	mt.RawSetString("__newindex", L.NewFunction(func(L *lua.LState) int {
		L.RaiseError("it cannot be changed")
		return 0
	}))
	mt.RawSetString("__metatable", lua.LFalse)
	u := L.NewUserData()
	L.SetMetatable(u, mt)
	return u
}
