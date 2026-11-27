// Package scan reads a mod's code before it is approved, without running it. It
// does two things: it holds the code to a strict form, so that which of spot's
// functions it calls and which programs it starts can be known from reading it
// (S40), and it points at things known to be used for harm that also have honest
// uses. It never says that a mod is safe (S39).
package scan

import (
	"bytes"
	"fmt"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/yuin/gopher-lua/ast"
	"github.com/yuin/gopher-lua/parse"

	"gitlab.com/KopyTKG/spot/internal/mod"
)

// Verdict is what to say when there is nothing to point at. Said after findings
// too.
const Verdict = "This is not a safety check: read the mod before you enable it."

// Note is one thing found. Line 0 is mod.toml.
type Note struct {
	File string
	Line int
	What string
	Why  string   // "" for a broken rule of the strict form
	More []string // further places with the same finding
}

func (n Note) Where() string {
	if n.Line == 0 {
		return n.File
	}
	return n.File + ":" + strconv.Itoa(n.Line)
}

// Also lists the further places, shortened when there are many.
func (n Note) Also() string {
	if len(n.More) > 8 {
		return strings.Join(n.More[:8], ", ") + fmt.Sprintf(" and %d more", len(n.More)-8)
	}
	return strings.Join(n.More, ", ")
}

// Report of one mod. A mod with anything in Refused does not run.
type Report struct {
	Refused []Note
	Flagged []Note
}

// Quote makes text from a mod safe to show (S32). Of a long text both ends are
// shown: the end of a path says as much as its start.
func Quote(s string) string {
	if r := []rune(s); len(r) > 100 {
		s = string(r[:55]) + " ... " + string(r[len(r)-40:])
	}
	s = strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Cf, r) { // direction marks and other invisibles
			return unicode.ReplacementChar
		}
		return r
	}, s)
	return strconv.Quote(s)
}

type scanner struct {
	perms *mod.Permissions
	r     Report
	file  string
	seen  map[string]int // what was found, and where in the list

	okSpot   map[*ast.IdentExpr]bool
	okString map[*ast.IdentExpr]bool
	ran      map[string]bool // programs named in spot.run / spot.pipe
	calls    map[string]bool // spot functions called
	envRead  map[string]bool
}

func (s *scanner) refuse(line int, format string, args ...any) {
	n := Note{File: s.file, Line: line, What: fmt.Sprintf(format, args...)}
	s.add(&s.r.Refused, "!"+n.What, n)
}

// add reports a thing once, with every place it is: a harmless first use must
// not hide a later one.
func (s *scanner) add(list *[]Note, key string, n Note) {
	i, ok := s.seen[key]
	if !ok {
		s.seen[key] = len(*list)
		*list = append(*list, n)
		return
	}
	first := &(*list)[i]
	if where := n.Where(); where != first.Where() && !slices.Contains(first.More, where) {
		first.More = append(first.More, where)
	}
}

func (s *scanner) flag(line int, why, format string, args ...any) {
	what := fmt.Sprintf(format, args...)
	s.add(&s.r.Flagged, what, Note{File: s.file, Line: line, What: what, Why: why})
}

// Mod scans the Lua files and permissions of one mod. perms may be nil.
func Mod(files map[string][]byte, perms *mod.Permissions) Report {
	if perms == nil {
		perms = &mod.Permissions{}
	}
	s := &scanner{perms: perms, seen: map[string]int{}, ran: map[string]bool{}, calls: map[string]bool{}, envRead: map[string]bool{}}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		// A file's name is the mod's text as well.
		s.file = name
		if strings.ContainsFunc(name, func(r rune) bool { return !unicode.IsPrint(r) }) {
			s.file = Quote(name)
		}
		s.source(files[name])
	}
	s.file = "mod.toml"
	s.permissions()
	return s.r
}

func (s *scanner) source(src []byte) {
	if bytes.HasPrefix(src, []byte("\x1b")) {
		s.refuse(0, "compiled code, not text")
		return
	}
	chunk, err := parse.Parse(bytes.NewReader(src), s.file)
	if err != nil {
		s.refuse(0, "does not parse: %s", Quote(err.Error()))
		return
	}
	s.okSpot, s.okString = map[*ast.IdentExpr]bool{}, map[*ast.IdentExpr]bool{}
	s.walk(reflect.ValueOf(chunk), 0)
	s.rawText(src)
}

// walk visits every node of the tree. It goes by reflection, so that no kind of
// node can be forgotten and hide what is inside it.
func (s *scanner) walk(v reflect.Value, line int) {
	switch v.Kind() {
	case reflect.Interface:
		if !v.IsNil() {
			s.walk(v.Elem(), line)
		}
	case reflect.Pointer:
		if v.IsNil() {
			return
		}
		if n, ok := v.Interface().(ast.PositionHolder); ok && n.Line() > 0 {
			line = n.Line()
		}
		s.node(v.Interface(), line)
		s.walk(v.Elem(), line)
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				s.walk(v.Field(i), line)
			}
		}
	case reflect.Slice:
		for i := range v.Len() {
			s.walk(v.Index(i), line)
		}
	}
}

// reserved are names a mod may not take for its own.
func (s *scanner) reserved(line int, names []string) {
	for _, n := range names {
		if n == "spot" || n == "_G" {
			s.refuse(line, "%q used as a name of the mod's own", n)
		}
	}
}

func literal(e ast.Expr) (string, bool) {
	if s, ok := e.(*ast.StringExpr); ok {
		return s.Value, true
	}
	return "", false
}

// spotName is "run" for spot.run, "json.encode" for spot.json.encode: a chain
// of written-out names rooted at spot, with the identifier at its root.
func spotName(e ast.Expr) (string, *ast.IdentExpr) {
	var parts []string
	for {
		at, ok := e.(*ast.AttrGetExpr)
		if !ok {
			return "", nil
		}
		key, ok := literal(at.Key)
		if !ok {
			return "", nil
		}
		parts = append(parts, key)
		if root, ok := at.Object.(*ast.IdentExpr); ok {
			if root.Value != "spot" {
				return "", nil
			}
			slices.Reverse(parts)
			return strings.Join(parts, "."), root
		}
		e = at.Object
	}
}

func (s *scanner) node(n any, line int) {
	switch n := n.(type) {
	case *ast.FuncCallExpr:
		s.call(n, line)
	case *ast.AttrGetExpr:
		key, isName := literal(n.Key)
		s.hiding(line, key)
		root, ok := n.Object.(*ast.IdentExpr)
		if !ok {
			break
		}
		switch {
		case root.Value == "spot" && key == "config": // reading spot.config
			s.okSpot[root] = true
		case root.Value == "string":
			s.okString[root] = true
			if !isName {
				s.flag(line, "which function is only known when it runs", "takes a computed function of the string library")
			}
		}
	case *ast.IdentExpr:
		if n.Value == "string" && !s.okString[n] {
			s.flag(line, "its functions can then be called under other names", "uses the string library as a value")
		}
		if n.Value == "_G" || n.Value == "spot" && !s.okSpot[n] {
			s.refuse(line, "%q used other than as spot.name(...) or spot.config", n.Value)
		}
	case *ast.StringExpr:
		s.text(line, n.Value)
	case *ast.StringConcatOpExpr:
		// Two written-out pieces with no space between: why not one string?
		a, aok := literal(n.Lhs)
		b, bok := literal(n.Rhs)
		if aok && bok && min(len(a), len(b)) <= 8 && len(a) > 0 && len(b) > 0 && !strings.ContainsFunc(a+b, unicode.IsSpace) {
			s.flag(line, "a name or an address cut up cannot be searched for; honest use is rare", "joins written-out pieces, %s and %s", Quote(a), Quote(b))
		}
		for _, side := range []ast.Expr{n.Lhs, n.Rhs} {
			if v, ok := literal(side); ok && pathPiece(v) {
				s.flag(line, "the full path is only known when it runs; honest use: a file in a folder of its own", "assembles a path from pieces, with %s", Quote(v))
			}
		}
	case *ast.LocalAssignStmt:
		s.reserved(line, n.Names)
	case *ast.ParList:
		s.reserved(line, n.Names)
	case *ast.NumberForStmt:
		s.reserved(line, []string{n.Name})
	case *ast.GenericForStmt:
		s.reserved(line, n.Names)
	}
}

// hiding points at the ways of making text that cannot be read as written, by
// the function's name, however the library was reached.
func (s *scanner) hiding(line int, name string) {
	switch name {
	case "char":
		s.flag(line, "can build names and addresses so that they cannot be read; honest use: control characters", "builds text from character codes (string.char)")
	case "reverse":
		s.flag(line, "can hide what a text says; honest use is rare", "reverses text (string.reverse)")
	}
}

func (s *scanner) call(n *ast.FuncCallExpr, line int) {
	if n.Func == nil { // object:method(...)
		s.hiding(line, n.Method)
		return
	}
	if id, ok := n.Func.(*ast.IdentExpr); ok && id.Value == "getmetatable" {
		s.flag(line, "reaches how a value behaves, the string library among it; honest use: classes", "calls getmetatable")
	}
	if at, ok := n.Func.(*ast.AttrGetExpr); ok { // table.concat of path pieces
		switch at.Key.(type) {
		case *ast.FuncCallExpr, *ast.StringConcatOpExpr:
			s.flag(line, "which function is called cannot be read from the code", "looks up a function by a name made while it runs")
		}
		if lib, ok := at.Object.(*ast.IdentExpr); ok && lib.Value == "table" {
			if key, _ := literal(at.Key); key == "concat" && len(n.Args) > 0 {
				s.pieces(line, n.Args[0])
			}
		}
	}
	name, root := spotName(n.Func)
	if root == nil {
		return
	}
	s.okSpot[root] = true
	s.calls[name] = true
	switch name {
	case "run":
		if len(n.Args) < 1 || len(n.Args) > 2 {
			s.refuse(line, "spot.run takes its list, written out, and options")
			return
		}
		s.command(n.Args[0], line)
		if len(n.Args) == 2 {
			s.options(n.Args[1], line)
		}
	case "pipe":
		for i, a := range n.Args {
			t, ok := a.(*ast.TableExpr)
			if !ok {
				s.refuse(line, "spot.pipe takes every list, and its options, written out")
				return
			}
			if i == len(n.Args)-1 && i > 0 && keyedOnly(t) {
				s.options(t, line)
				return
			}
			s.command(a, line)
		}
		if len(n.Args) == 0 {
			s.refuse(line, "spot.pipe without a command")
		}
	case "read", "lines", "list", "exists":
		s.access(n, line, "read", s.perms.Read)
	case "write":
		s.access(n, line, "write", s.perms.Write)
	case "env":
		if len(n.Args) > 0 {
			if v, ok := literal(n.Args[0]); !ok {
				s.flag(line, "which variable is only known when it runs", "reads an environment variable with a computed name")
			} else {
				s.envRead[v] = true
				if _, ok := s.perms.Env[v]; !ok {
					s.flag(line, "spot will refuse it; a mod that asks for what it did not declare deserves a look", "reads the variable %s without permission", Quote(v))
				}
			}
		}
	}
}

func pathPiece(v string) bool {
	return strings.Contains(v, "/") && (strings.HasPrefix(v, "~") || strings.HasPrefix(v, "/") || strings.Contains(v, "/."))
}

func (s *scanner) pieces(line int, arg ast.Expr) {
	t, ok := arg.(*ast.TableExpr)
	if !ok {
		return
	}
	for _, f := range t.Fields {
		if v, ok := literal(f.Value); ok && pathPiece(v) {
			s.flag(line, "the full path is only known when it runs; honest use: a file in a folder of its own", "assembles a path from pieces, with %s", Quote(v))
		}
	}
}

func keyedOnly(t *ast.TableExpr) bool {
	for _, f := range t.Fields {
		if f.Key == nil {
			return false
		}
	}
	return true
}

// command checks one argv: written out in the call, plain entries only, the
// program first and literal.
func (s *scanner) command(arg ast.Expr, line int) {
	t, ok := arg.(*ast.TableExpr)
	if !ok || len(t.Fields) == 0 {
		s.refuse(line, "the command of spot.run / spot.pipe must be a list written out in the call")
		return
	}
	for _, f := range t.Fields {
		if f.Key != nil {
			s.refuse(line, "a command list with a keyed entry: a later [1] would replace the program")
			return
		}
	}
	program, ok := literal(t.Fields[0].Value)
	if !ok {
		s.refuse(line, "the program to run must be written out, first in the list")
		return
	}
	s.ran[program] = true
	s.program(line, program, "runs")
	if !s.permitted(program) {
		s.flag(line, "spot will refuse it; a mod that asks for what it did not declare deserves a look", "runs %s without permission", Quote(program))
	}
	for _, f := range t.Fields[1:] {
		switch v := f.Value.(type) {
		case *ast.StringExpr, *ast.NumberExpr:
		case *ast.UnaryMinusOpExpr:
			if _, ok := v.Expr.(*ast.NumberExpr); !ok {
				s.computed(line, program)
			}
		case *ast.FuncCallExpr:
			if id, ok := v.Func.(*ast.IdentExpr); ok && id.Value == "unpack" {
				s.flag(line, "any number of arguments from data", "unpacks a list into the arguments of %s", Quote(program))
				continue
			}
			s.computed(line, program)
		default:
			s.computed(line, program)
		}
	}
}

func (s *scanner) computed(line int, program string) {
	s.flag(line, "what it is given is only known when it runs; honest use: a name or number chosen by you", "passes computed arguments to %s", Quote(program))
}

// options flags variables set for a program without permission.
func (s *scanner) options(arg ast.Expr, line int) {
	unseen := func() {
		s.flag(line, "they can set variables that make a program load or run something else", "gives a program options that are built elsewhere")
	}
	t, ok := arg.(*ast.TableExpr)
	if !ok {
		unseen()
		return
	}
	for _, f := range t.Fields {
		k, ok := literal(f.Key)
		if !ok {
			unseen()
			continue
		}
		if k != "env" {
			continue
		}
		env, ok := f.Value.(*ast.TableExpr)
		if !ok {
			s.flag(line, "which variables is only known when it runs", "sets computed variables for a program")
			continue
		}
		for _, e := range env.Fields {
			k, ok := literal(e.Key)
			if _, allowed := s.perms.Setenv[k]; !ok || !allowed {
				s.flag(line, "variables can make a program load or run something else", "sets the variable %s without permission", Quote(k))
			}
		}
	}
}

func (s *scanner) permitted(program string) bool {
	_, ok := s.perms.Run[program]
	return ok
}

func covered(path string, entries map[string]string) bool {
	for e := range entries {
		e = strings.TrimSuffix(e, "/")
		if path == e || strings.HasPrefix(path, e+"/") {
			return true
		}
	}
	return false
}

func (s *scanner) access(n *ast.FuncCallExpr, line int, kind string, entries map[string]string) {
	if len(n.Args) == 0 {
		return
	}
	path, ok := literal(n.Args[0])
	switch {
	case !ok:
		s.flag(line, "which file is only known when it runs; honest use: files you choose", "%ss a computed path", kind)
	case !covered(path, entries) || strings.Contains(path, ".."):
		s.flag(line, "spot will refuse it; a mod that asks for what it did not declare deserves a look", "%ss %s without permission", kind, Quote(path))
	}
}

// permissions flags what is asked for: dangerous entries, and ones no code
// uses.
func (s *scanner) permissions() {
	each := func(m map[string]string, f func(string)) {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			f(k)
		}
	}
	runs := s.calls["run"] || s.calls["pipe"]
	unused := func(kind, what string) {
		s.flag(0, "asked for and not used: why does it want it?", "unused permission %s %s", kind, Quote(what))
	}
	each(s.perms.Run, func(p string) {
		if !s.ran[p] { // what the code runs has been flagged where it does
			s.program(0, p, "may run")
			unused("run", p)
		}
	})
	reads := runs || s.calls["read"] || s.calls["lines"] || s.calls["list"] || s.calls["exists"]
	each(s.perms.Read, func(p string) {
		s.wide(p, "read")
		s.path(0, p, true)
		if !reads {
			unused("read", p)
		}
	})
	each(s.perms.Write, func(p string) {
		s.wide(p, "write")
		s.path(0, p, false)
		s.flag(0, "changes files; honest use: its own state", "may write %s", Quote(p))
		if !runs && !s.calls["write"] {
			unused("write", p)
		}
	})
	each(s.perms.Env, func(v string) {
		if !s.envRead[v] {
			unused("env", v)
		}
	})
	each(s.perms.Setenv, func(v string) {
		if !runs {
			unused("setenv", v)
		}
	})
	each(s.perms.Net, func(p string) {
		s.flag(0, "can send anything it can read to any machine", "may use the network, port %s", Quote(p))
		if !runs && !s.calls["fetch"] {
			unused("net", p)
		}
	})
	each(s.perms.Netlink, func(n string) {
		if !runs {
			unused("netlink", n)
		}
	})
	each(s.perms.Socket, func(p string) {
		s.flag(0, "whatever listens there does what it is asked: with the user bus or the compositor, anything you can", "may talk to the socket %s", Quote(p))
		if !runs && !s.calls["fetch"] {
			unused("socket", p)
		}
	})
}

// wideFolders hold far more than any mod needs, keys and passwords among it.
var wideFolders = regexp.MustCompile(`^(~|\$\{?HOME\}?|/home(/[^/]+)?|(~|\$\{?HOME\}?|/home/[^/]+)/\.(config|local|local/share|local/state|cache)|\$\{?XDG_(CONFIG_HOME|DATA_HOME|STATE_HOME|RUNTIME_DIR)\}?|/etc|/var|/run|/run/user(/[0-9]+)?|/tmp)$`)

func (s *scanner) wide(p, kind string) {
	if wideFolders.MatchString(filepath.Clean(p)) {
		s.flag(0, "everything under it, with keys, tokens and other programs' files; ask for the folder it needs", "may %s the whole of %s", kind, Quote(p))
	}
}
