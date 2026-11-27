package luaenv

import (
	"errors"
	"fmt"
	"math"

	lua "github.com/yuin/gopher-lua"
)

// Plain data, in Go: nil, bool, float64, string, []any and map[string]any. It
// is all that crosses from one Lua state to another or out of a mod host.

// Limits bound a copy. Depth alone is not enough: one table referred to many
// times over is copied as many times, and a few lines can ask for billions.
type Limits struct {
	Depth  int // tables within tables
	Values int // values in all
	Bytes  int // of strings and keys in all
	String int // one string; 0 is no limit beyond Bytes
}

// Copy is the usual bound.
var Copy = Limits{Depth: 32, Values: 1 << 20, Bytes: 12 << 20}

type copier struct {
	lim    Limits
	values int
	bytes  int
	inside map[*lua.LTable]bool
}

// ToGo copies v out of Lua. It reads tables raw: no mod code runs while what a
// mod returned is being checked. An empty table comes out as an empty list.
func ToGo(v lua.LValue, lim Limits) (any, error) {
	c := &copier{lim: lim, inside: map[*lua.LTable]bool{}}
	return c.value(v, 0)
}

func (c *copier) str(s string) error {
	c.bytes += len(s)
	switch {
	case c.lim.String > 0 && len(s) > c.lim.String:
		return fmt.Errorf("a string of %d bytes (at most %d)", len(s), c.lim.String)
	case c.bytes > c.lim.Bytes:
		return fmt.Errorf("more than %d bytes of text", c.lim.Bytes)
	}
	return nil
}

func (c *copier) value(v lua.LValue, depth int) (any, error) {
	if c.values++; c.values > c.lim.Values {
		return nil, fmt.Errorf("more than %d values", c.lim.Values)
	}
	switch v := v.(type) {
	case *lua.LNilType:
		return nil, nil
	case lua.LBool:
		return bool(v), nil
	case lua.LNumber:
		f := float64(v)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, errors.New("a number that is not finite")
		}
		return f, nil
	case lua.LString:
		if err := c.str(string(v)); err != nil {
			return nil, err
		}
		return string(v), nil
	case *lua.LTable:
		return c.table(v, depth+1)
	}
	return nil, fmt.Errorf("a %s is not plain data", v.Type())
}

func (c *copier) table(t *lua.LTable, depth int) (any, error) {
	if depth > c.lim.Depth {
		return nil, fmt.Errorf("tables nested more than %d deep", c.lim.Depth)
	}
	if c.inside[t] {
		return nil, errors.New("a table that contains itself")
	}
	c.inside[t] = true
	defer delete(c.inside, t)

	var keys, values []lua.LValue
	ints, strs := 0, 0
	for k, v := t.Next(lua.LNil); k != lua.LNil; k, v = t.Next(k) {
		switch k := k.(type) {
		case lua.LNumber:
			if f := float64(k); f != math.Trunc(f) || f < 1 {
				return nil, fmt.Errorf("a table with the key %v", k)
			}
			ints++
		case lua.LString:
			if err := c.str(string(k)); err != nil {
				return nil, err
			}
			strs++
		default:
			return nil, fmt.Errorf("a table with a %s as a key", k.Type())
		}
		if len(keys) >= c.lim.Values {
			return nil, fmt.Errorf("more than %d values", c.lim.Values)
		}
		keys, values = append(keys, k), append(values, v)
	}
	switch {
	case ints > 0 && strs > 0:
		return nil, errors.New("a table that is both a list and a record")
	case strs > 0:
		m := make(map[string]any, len(keys))
		for i, k := range keys {
			x, err := c.value(values[i], depth)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", k, err)
			}
			m[string(k.(lua.LString))] = x
		}
		return m, nil
	}
	list := make([]any, len(keys))
	filled := make([]bool, len(keys))
	for i, k := range keys {
		n := int(k.(lua.LNumber))
		if n > len(list) {
			return nil, errors.New("a list with holes")
		}
		x, err := c.value(values[i], depth)
		if err != nil {
			return nil, fmt.Errorf("item %d: %w", n, err)
		}
		list[n-1], filled[n-1] = x, true
	}
	for _, f := range filled {
		if !f {
			return nil, errors.New("a list with holes")
		}
	}
	return list, nil
}

// FromGo copies plain data into L. A nil in a list is left out, so that lists
// never have holes; in a map the key is simply absent.
func FromGo(L *lua.LState, v any, lim Limits) (lua.LValue, error) {
	c := &copier{lim: lim}
	return c.into(L, v, 0)
}

func (c *copier) into(L *lua.LState, v any, depth int) (lua.LValue, error) {
	if c.values++; c.values > c.lim.Values {
		return nil, fmt.Errorf("more than %d values", c.lim.Values)
	}
	switch v := v.(type) {
	case nil:
		return lua.LNil, nil
	case bool:
		return lua.LBool(v), nil
	case float64:
		return lua.LNumber(v), nil
	case string:
		return lua.LString(v), c.str(v)
	case []any:
		if depth++; depth > c.lim.Depth {
			return nil, fmt.Errorf("nested more than %d deep", c.lim.Depth)
		}
		t := L.NewTable()
		for _, x := range v {
			l, err := c.into(L, x, depth)
			if err != nil {
				return nil, err
			}
			if l != lua.LNil {
				t.Append(l)
			}
		}
		return t, nil
	case map[string]any:
		if depth++; depth > c.lim.Depth {
			return nil, fmt.Errorf("nested more than %d deep", c.lim.Depth)
		}
		t := L.NewTable()
		for k, x := range v {
			if err := c.str(k); err != nil {
				return nil, err
			}
			l, err := c.into(L, x, depth)
			if err != nil {
				return nil, err
			}
			t.RawSetString(k, l)
		}
		return t, nil
	}
	return nil, fmt.Errorf("%T is not plain data", v)
}
