// Package provider runs a provider mod inside its host: the Lua state that
// lives as long as the mod, and its `spot`, which is its only way to anything.
// Every call checks the mod's grants, and the kernel checks again.
package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	lua "github.com/yuin/gopher-lua"

	"gitlab.com/KopyTKG/spot/internal/luaenv"
	"gitlab.com/KopyTKG/spot/internal/modhost/channel"
	"gitlab.com/KopyTKG/spot/internal/modperm"
	"gitlab.com/KopyTKG/spot/internal/parser"
)

const (
	functionTime = 5 * time.Second
	maxValues    = 100
	maxRows      = 10_000
	maxLines     = 200
	maxEvents    = 500
	maxText      = 4 << 10
	maxTimers    = 64
	maxKept      = 32 << 20 // of all values together
	minTimer     = 100 * time.Millisecond
	minEvery     = 250 * time.Millisecond
)

type timer struct {
	fn    *lua.LFunction
	due   time.Time
	every time.Duration // 0: once
}

// published bounds a value or the answer of a request.
var published = luaenv.Limits{Depth: 32, Values: 1 << 19, Bytes: 8 << 20, String: maxText}

// arguments bounds what a mod passes to spot.
var arguments = luaenv.Limits{Depth: 8, Values: 1 << 16, Bytes: maxFile + 1<<20}

type value struct {
	fn     *lua.LFunction // nil for a published one
	ttl    time.Duration
	cached any
	size   int // of cached, as JSON
	at     time.Time
	has    bool
	every  time.Duration // recompute this often, while somebody watches
}

// Runtime is a loaded provider.
type Runtime struct {
	L        *lua.LState
	name     string
	grants   modperm.Grants
	files    map[string][]byte
	parsers  parser.Call
	uses     []string
	emptyDir string

	values   map[string]*value
	requests map[string]*lua.LFunction
	required map[string]lua.LValue
	loading  map[string]bool

	timers  []*timer
	changed map[string]bool // published or refreshed since the last reply
	written int             // bytes since writtenSince
	since   time.Time

	ctx     context.Context // of the function running now
	log     []string
	events  []channel.Event
	dropped int
	serial  int
}

func (r *Runtime) next() int { r.serial++; return r.serial }

func (r *Runtime) audit(kind, what string) {
	if len(r.events) >= maxEvents {
		r.dropped++
		return
	}
	r.events = append(r.events, channel.Event{Kind: kind, What: what[:min(len(what), maxText)]})
}

// reply is what a call ends with: its result or error, and what was logged.
func (r *Runtime) reply(result any, err error) channel.Reply {
	out := channel.Reply{Ready: true, Log: r.log, Events: r.events}
	if r.dropped > 0 {
		out.Events = append(out.Events, channel.Event{Kind: "dropped", What: fmt.Sprintf("%d more events", r.dropped)})
	}
	r.log, r.events, r.dropped = nil, nil, 0
	for name := range r.changed {
		out.Changed = append(out.Changed, name)
	}
	sort.Strings(out.Changed)
	clear(r.changed)
	for _, t := range r.timers {
		in := max(time.Until(t.due).Milliseconds(), 1)
		if out.Next == 0 || in < out.Next {
			out.Next = in
		}
	}
	if err == nil && result != nil {
		out.Result, err = json.Marshal(result)
		if len(out.Result) > channel.MaxMessage-2<<20 {
			out.Result, err = nil, errors.New("the result is too large")
		}
	}
	if err != nil {
		out.Error = r.name + ": " + err.Error()
	}
	return out
}

// within5 runs f with the time a function may take.
func (r *Runtime) within5(f func() error) error {
	ctx, cancel := context.WithTimeout(context.Background(), functionTime)
	defer cancel()
	r.ctx = ctx
	r.L.SetContext(ctx)
	defer r.L.RemoveContext()
	err := f()
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return errors.New("took too long")
	}
	var api *lua.ApiError
	if errors.As(err, &api) {
		if s, ok := api.Object.(lua.LString); ok {
			return errors.New(string(s))
		}
		if api.Object != nil && api.Object != lua.LNil {
			return fmt.Errorf("failed with a %s as the error", api.Object.Type())
		}
	}
	return err
}

// Open runs the provider's init.lua, which says what it publishes.
func Open(l channel.Load) (*Runtime, channel.Reply) {
	r := &Runtime{L: luaenv.New(false), name: l.Mod.Name, grants: l.Grants, files: l.Files,
		parsers: parser.Call{Mods: l.Parsers}, uses: l.Mod.Manifest.Uses, emptyDir: l.EmptyDir,
		values: map[string]*value{}, requests: map[string]*lua.LFunction{},
		required: map[string]lua.LValue{}, loading: map[string]bool{}, ctx: context.Background(),
		changed: map[string]bool{}, since: time.Now()}
	err := r.within5(func() error {
		// A parser that does not load fails the mod now, not at its first use.
		names := make([]string, 0, len(l.Parsers))
		for name := range l.Parsers {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if err := r.parsers.Load(r.ctx, name); err != nil {
				return err
			}
		}
		r.parsers.Log = nil
		if err := r.install(l.Config); err != nil {
			return err
		}
		chunk, err := r.L.Load(bytes.NewReader(l.Mod.Source), r.name+"/init.lua")
		if err != nil {
			return err
		}
		r.L.Push(chunk)
		return r.L.PCall(0, 0, nil)
	})
	out := r.reply(nil, err)
	out.Every = map[string]int64{}
	for name, v := range r.values {
		out.Values = append(out.Values, name)
		if v.every > 0 {
			out.Every[name] = v.every.Milliseconds()
		}
	}
	for name := range r.requests {
		out.Requests = append(out.Requests, name)
	}
	sort.Strings(out.Values)
	sort.Strings(out.Requests)
	return r, out
}

func shape(v any) error {
	if rows, ok := v.([]any); ok && len(rows) > maxRows {
		return fmt.Errorf("%d rows (at most %d)", len(rows), maxRows)
	}
	return nil
}

func (r *Runtime) copyOut(v lua.LValue) (any, error) {
	if t, ok := v.(*lua.LTable); ok && t.Len() > maxRows {
		return nil, fmt.Errorf("%d rows (at most %d)", t.Len(), maxRows)
	}
	out, err := luaenv.ToGo(v, published)
	if err != nil {
		return nil, fmt.Errorf("what it returned has %v", err)
	}
	return out, shape(out)
}

// keep checks that name may hold out beside the other values, and says how much
// it is.
func (r *Runtime) keep(name string, out any) (int, error) {
	b, err := json.Marshal(out)
	if err != nil {
		return 0, err
	}
	total := len(b)
	for other, v := range r.values {
		if other != name {
			total += v.size
		}
	}
	if total > maxKept {
		return 0, fmt.Errorf("the mod's values come to more than %d MB", maxKept>>20)
	}
	return len(b), nil
}

// Get is a value: computed when asked for, or kept for its ttl.
func (r *Runtime) Get(name string) channel.Reply {
	v, ok := r.values[name]
	if !ok {
		return r.reply(nil, fmt.Errorf("there is no value %q", name))
	}
	if v.fn == nil || v.has && v.ttl > 0 && time.Since(v.at) < v.ttl {
		return r.reply(v.cached, nil)
	}
	err := r.within5(func() error {
		if err := r.L.CallByParam(lua.P{Fn: v.fn, NRet: 1, Protect: true}); err != nil {
			return err
		}
		ret := r.L.Get(-1)
		r.L.Pop(1)
		out, err := r.copyOut(ret)
		if err != nil {
			return err
		}
		size, err := r.keep(name, out)
		if err != nil {
			return err
		}
		v.cached, v.size, v.at, v.has = out, size, time.Now(), true
		return nil
	})
	if err != nil {
		return r.reply(nil, fmt.Errorf("value %q: %w", name, err))
	}
	return r.reply(v.cached, nil)
}

// Tick runs every timer that is due. One that fails does not stop the others;
// the first failure is reported.
func (r *Runtime) Tick() channel.Reply {
	now := time.Now()
	var due []*timer
	keep := r.timers[:0]
	for _, t := range r.timers {
		if t.due.After(now) {
			keep = append(keep, t)
			continue
		}
		due = append(due, t)
		if t.every > 0 {
			t.due = now.Add(t.every)
			keep = append(keep, t)
		}
	}
	r.timers = keep
	var failed error
	for _, t := range due {
		err := r.within5(func() error { return r.L.CallByParam(lua.P{Fn: t.fn, Protect: true}) })
		if err != nil && failed == nil {
			failed = fmt.Errorf("a timer: %w", err)
		}
	}
	return r.reply(nil, failed)
}

// Call runs a request with its arguments, a list of strings.
func (r *Runtime) Call(name string, args []string) channel.Reply {
	fn, ok := r.requests[name]
	if !ok {
		return r.reply(nil, fmt.Errorf("there is no request %q", name))
	}
	var out any
	err := r.within5(func() error {
		t := r.L.NewTable()
		for _, a := range args {
			t.Append(lua.LString(a))
		}
		if err := r.L.CallByParam(lua.P{Fn: fn, NRet: 1, Protect: true}, t); err != nil {
			return err
		}
		ret := r.L.Get(-1)
		r.L.Pop(1)
		var err error
		out, err = r.copyOut(ret)
		return err
	})
	if err != nil {
		return r.reply(nil, fmt.Errorf("request %q: %w", name, err))
	}
	return r.reply(out, nil)
}

func raise(L *lua.LState, format string, args ...any) int {
	L.Error(lua.LString(fmt.Sprintf(format, args...)), 0)
	return 0
}

// arg copies one argument once, without metamethods: what is checked is what is
// used (S42).
func arg(L *lua.LState, i int) any {
	v, err := luaenv.ToGo(L.Get(i), arguments)
	if err != nil {
		raise(L, "argument %d: %v", i, err)
	}
	return v
}

func push(L *lua.LState, v any) int {
	l, err := luaenv.FromGo(L, v, luaenv.Copy)
	if err != nil {
		return raise(L, "%v", err)
	}
	L.Push(l)
	return 1
}

// failed ends a file call: a refusal is an error of the mod, anything else is
// returned for it to deal with.
func (r *Runtime) failed(L *lua.LState, what, path string, err error) int {
	if errors.Is(err, errRefused) {
		r.audit("refused", what+" "+path)
		return raise(L, "%s %q: no permission", what, path)
	}
	L.Push(lua.LNil)
	L.Push(lua.LString(err.Error()))
	return 2
}

var itemName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
var fileName = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_/-]*(\.lua)?$`)

func words(v any) ([]string, error) {
	list, ok := v.([]any)
	if !ok {
		return nil, errors.New("a command is a list: the program, then its arguments")
	}
	out := make([]string, len(list))
	for i, x := range list {
		switch x := x.(type) {
		case string:
			out[i] = x
		case float64:
			out[i] = lua.LNumber(x).String()
		default:
			return nil, fmt.Errorf("argument %d of a command is not a string or a number", i)
		}
	}
	return out, nil
}

func options(v any) (runOptions, error) {
	var o runOptions
	if v == nil {
		return o, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		if list, isList := v.([]any); isList && len(list) == 0 {
			return o, nil
		}
		return o, errors.New("options are a table of named things")
	}
	for k, x := range m {
		switch s, isText := x.(string); {
		case k == "timeout":
			ms, ok := x.(float64)
			if !ok || ms <= 0 {
				return o, errors.New("timeout is a number of milliseconds")
			}
			o.timeout = time.Duration(ms) * time.Millisecond
		case k == "stdin" && isText:
			o.stdin = s
		case k == "dir" && isText:
			o.dir = s
		case k == "env":
			env, ok := x.(map[string]any)
			if !ok {
				return o, errors.New("env is a table of variables")
			}
			o.env = map[string]string{}
			for name, val := range env {
				s, ok := val.(string)
				if !ok || strings.ContainsRune(s, 0) {
					return o, fmt.Errorf("env %s is not a string", name)
				}
				o.env[name] = s
			}
		default:
			return o, fmt.Errorf("unknown or wrong option %q", k)
		}
	}
	return o, nil
}

// install makes `spot`.
func (r *Runtime) install(config json.RawMessage) error {
	L := r.L
	fields := map[string]lua.LValue{}
	add := func(name string, f lua.LGFunction) { fields[name] = L.NewFunction(f) }

	command := func(L *lua.LState, commands [][]string, opts any) int {
		o, err := options(opts)
		if err != nil {
			return raise(L, "%v", err)
		}
		res, err := r.pipeline(r.ctx, commands, o)
		if err != nil {
			return raise(L, "%v", err)
		}
		t := L.NewTable()
		t.RawSetString("status", lua.LNumber(res.Status))
		statuses := L.NewTable()
		for _, s := range res.Statuses {
			statuses.Append(lua.LNumber(s))
		}
		t.RawSetString("statuses", statuses)
		t.RawSetString("stdout", lua.LString(res.Stdout))
		t.RawSetString("stderr", lua.LString(res.Stderr))
		L.Push(t)
		return 1
	}
	add("run", func(L *lua.LState) int {
		argv, err := words(arg(L, 1))
		if err != nil {
			return raise(L, "spot.run: %v", err)
		}
		return command(L, [][]string{argv}, arg(L, 2))
	})
	add("pipe", func(L *lua.LState) int {
		var commands [][]string
		var opts any
		for i := 1; i <= L.GetTop(); i++ {
			v := arg(L, i)
			if m, ok := v.(map[string]any); ok && i == L.GetTop() {
				opts = m
				break
			}
			argv, err := words(v)
			if err != nil {
				return raise(L, "spot.pipe: %v", err)
			}
			commands = append(commands, argv)
		}
		return command(L, commands, opts)
	})

	add("read", func(L *lua.LState) int {
		path := L.CheckString(1)
		b, err := r.readFile(path)
		if err != nil {
			return r.failed(L, "read", path, err)
		}
		r.audit("read", path)
		L.Push(lua.LString(b))
		return 1
	})
	add("lines", func(L *lua.LState) int {
		path := L.CheckString(1)
		b, err := r.readFile(path)
		if err != nil {
			if errors.Is(err, errRefused) {
				return r.failed(L, "read", path, err)
			}
			return raise(L, "%s: %v", path, err)
		}
		r.audit("read", path)
		rest := strings.TrimSuffix(string(b), "\n")
		done := len(b) == 0
		L.Push(L.NewFunction(func(L *lua.LState) int {
			if done {
				L.Push(lua.LNil)
				return 1
			}
			line, more, found := strings.Cut(rest, "\n")
			rest, done = more, !found
			L.Push(lua.LString(line))
			return 1
		}))
		return 1
	})
	add("write", func(L *lua.LState) int {
		path, data := L.CheckString(1), L.CheckString(2)
		if err := r.writeFile(path, []byte(data)); err != nil {
			return r.failed(L, "write", path, err)
		}
		r.audit("write", path)
		L.Push(lua.LTrue)
		return 1
	})
	add("list", func(L *lua.LState) int {
		path := L.CheckString(1)
		names, err := r.listDir(path)
		if err != nil {
			return r.failed(L, "list", path, err)
		}
		r.audit("read", path)
		t := L.NewTable()
		for _, n := range names {
			t.Append(lua.LString(n))
		}
		L.Push(t)
		return 1
	})
	add("exists", func(L *lua.LState) int {
		path := L.CheckString(1)
		kind, err := r.kindOf(path)
		if err != nil {
			return r.failed(L, "read", path, err)
		}
		if kind == "" {
			L.Push(lua.LNil)
			return 1
		}
		L.Push(lua.LString(kind))
		return 1
	})
	add("env", func(L *lua.LState) int {
		name := L.CheckString(1)
		if v, ok := r.grants.Env[name]; ok {
			L.Push(lua.LString(v))
			return 1
		}
		// Permitted and not set looks the same as not set.
		if slices.Contains(r.grants.EnvNames, name) {
			L.Push(lua.LNil)
			return 1
		}
		r.audit("refused", "env "+name)
		return raise(L, "env %q: no permission", name)
	})
	add("time", func(L *lua.LState) int {
		L.Push(lua.LNumber(float64(time.Now().UnixMilli()) / 1000))
		return 1
	})
	add("log", func(L *lua.LState) int {
		if len(r.log) < maxLines {
			s := L.CheckString(1)
			r.log = append(r.log, s[:min(len(s), maxText)])
		}
		return 0
	})

	enc := luaenv.ReadOnly(L, map[string]lua.LValue{
		"encode": L.NewFunction(func(L *lua.LState) int {
			b, err := json.Marshal(arg(L, 1))
			if err != nil {
				return raise(L, "spot.json.encode: %v", err)
			}
			L.Push(lua.LString(b))
			return 1
		}),
		"decode": L.NewFunction(func(L *lua.LState) int {
			var v any
			if err := json.Unmarshal([]byte(L.CheckString(1)), &v); err != nil {
				return raise(L, "spot.json.decode: %v", err)
			}
			return push(L, v)
		}),
	})
	fields["json"] = enc

	add("use", func(L *lua.LState) int {
		name := L.CheckString(1)
		if !slices.Contains(r.uses, name) {
			return raise(L, "spot.use(%q): not listed in this mod's uses", name)
		}
		L.Push(L.NewFunction(func(L *lua.LState) int {
			input := L.CheckString(1)
			var opts any
			if L.Get(2) != lua.LNil {
				if opts = arg(L, 2); opts != nil {
					if list, ok := opts.([]any); ok && len(list) == 0 {
						opts = map[string]any{}
					}
				}
			}
			rows, err := r.parsers.Run(r.ctx, name, input, opts)
			r.log = append(r.log, r.parsers.Log...)
			r.parsers.Log = nil
			if err != nil {
				return raise(L, "%v", err)
			}
			return push(L, rows)
		}))
		return 1
	})
	add("require", func(L *lua.LState) int {
		name := L.CheckString(1)
		if !fileName.MatchString(name) {
			return raise(L, "spot.require(%q): a file of this mod, by its name", name)
		}
		file := strings.TrimSuffix(name, ".lua") + ".lua"
		if v, ok := r.required[file]; ok {
			L.Push(v)
			return 1
		}
		// Only the bytes that came with the mod: the ones that were scanned.
		src, ok := r.files[file]
		if !ok {
			return raise(L, "spot.require(%q): the mod has no %s", name, file)
		}
		if r.loading[file] {
			return raise(L, "spot.require(%q): goes in a circle", name)
		}
		r.loading[file] = true
		defer delete(r.loading, file)
		chunk, err := L.Load(bytes.NewReader(src), r.name+"/"+file)
		if err != nil {
			return raise(L, "%v", err)
		}
		L.Push(chunk)
		L.Call(0, 1)
		r.required[file] = L.Get(-1)
		return 1
	})

	named := func(L *lua.LState) string {
		name := L.CheckString(1)
		if !itemName.MatchString(name) {
			raise(L, "%q: names are small letters, digits and _", name)
		}
		return name
	}
	room := func(L *lua.LState, name string) {
		if _, ok := r.values[name]; !ok && len(r.values) >= maxValues {
			raise(L, "more than %d values", maxValues)
		}
	}
	add("value", func(L *lua.LState) int {
		name := named(L)
		room(L, name)
		fn := L.CheckFunction(L.GetTop())
		v := &value{fn: fn}
		if L.GetTop() > 2 {
			m, _ := arg(L, 2).(map[string]any)
			if ms, ok := m["ttl"].(float64); ok && ms > 0 {
				v.ttl = time.Duration(ms) * time.Millisecond
			}
			if ms, ok := m["every"].(float64); ok && ms > 0 {
				v.every = max(time.Duration(ms)*time.Millisecond, minEvery)
			}
		}
		r.values[name] = v
		return 0
	})
	add("publish", func(L *lua.LState) int {
		name := named(L)
		room(L, name)
		out, err := r.copyOut(L.Get(2))
		if err != nil {
			return raise(L, "publish %q: %v", name, err)
		}
		size, err := r.keep(name, out)
		if err != nil {
			return raise(L, "publish %q: %v", name, err)
		}
		r.values[name] = &value{cached: out, size: size, has: true}
		r.changed[name] = true
		return 0
	})
	add("refresh", func(L *lua.LState) int {
		name := named(L)
		if v, ok := r.values[name]; ok && v.fn != nil {
			v.has = false
			r.changed[name] = true
		}
		return 0
	})
	add("on", func(L *lua.LState) int {
		name := named(L)
		if len(r.requests) >= maxValues {
			return raise(L, "more than %d requests", maxValues)
		}
		r.requests[name] = L.CheckFunction(2)
		return 0
	})
	schedule := func(repeat bool) lua.LGFunction {
		return func(L *lua.LState) int {
			ms := float64(L.CheckNumber(1))
			fn := L.CheckFunction(2)
			if !(ms >= 0 && ms <= 1e10) {
				return raise(L, "a timer takes a number of milliseconds")
			}
			if len(r.timers) >= maxTimers {
				return raise(L, "more than %d timers", maxTimers)
			}
			d := max(time.Duration(ms)*time.Millisecond, minTimer)
			t := &timer{fn: fn, due: time.Now().Add(d)}
			if repeat {
				t.every = d
			}
			r.timers = append(r.timers, t)
			return 0
		}
	}
	add("every", schedule(true))
	add("after", schedule(false))

	var cfg any = map[string]any{}
	if len(config) > 0 {
		if err := json.Unmarshal(config, &cfg); err != nil {
			return fmt.Errorf("config: %w", err)
		}
	}
	c, err := luaenv.FromGo(L, cfg, luaenv.Copy)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	fields["config"] = c
	L.SetGlobal("spot", luaenv.ReadOnly(L, fields))
	return nil
}
