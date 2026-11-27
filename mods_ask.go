package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode"

	"gitlab.com/KopyTKG/spot/internal/client"
	"gitlab.com/KopyTKG/spot/internal/modd"
	"gitlab.com/KopyTKG/spot/internal/protocol"
)

const askUsage = `usage: spot get <mod.value> [--json|--waybar]    print a value of a mod
       spot watch <mod.value> [--json|--waybar]  print it now and on every change
       spot call <mod.request> [args...]         run a request of a mod
`

type format int

const (
	asPlain format = iota
	asJSON
	asBar
)

// askMod is `spot get`, `spot watch` and `spot call`.
func askMod(verb string, args []string) int {
	how := asPlain
	if verb != protocol.OpCall {
		var rest []string
		for _, a := range args {
			switch {
			case a == "--json" && how != asBar:
				how = asJSON
			case a == "--waybar" && how != asJSON:
				how = asBar
			default:
				rest = append(rest, a)
			}
		}
		args = rest
	}
	// Everything after a request's name is its arguments, dashes and all.
	if len(args) == 0 || strings.HasPrefix(args[0], "-") || verb != protocol.OpCall && len(args) != 1 {
		fmt.Fprint(os.Stderr, askUsage)
		return protocol.ExitUsage
	}
	req := protocol.Request{Op: verb, Version: protocol.Version, Name: args[0], Args: args[1:]}
	if verb == protocol.OpWatch {
		return watchMod(req, how)
	}
	resp, ok := client.ViaDaemon(req)
	if !ok {
		resp = protocol.Response{Code: protocol.ExitError, Stderr: "spot: mods run in the daemon, and it could not be reached\n"}
	}
	os.Stderr.WriteString(resp.Stderr)
	if how == asBar {
		fmt.Println(forBar(resp))
		return resp.Code
	}
	if resp.Code == 0 && len(resp.Result) > 0 {
		if how == asJSON {
			fmt.Println(string(resp.Result))
		} else {
			os.Stdout.WriteString(asText(resp.Result))
		}
	}
	return resp.Code
}

// watchMod prints until killed, or until nobody reads. It carries on across a
// restart of the daemon.
func watchMod(req protocol.Request, how format) int {
	closed, refused := errors.New("nobody reads"), errors.New("refused")
	pause := time.Second
	last := ""
	for {
		stale, err := client.Watch(req, func(r protocol.Response) error {
			pause = time.Second
			var line string
			if r.Code == protocol.ExitUsage {
				os.Stderr.WriteString(r.Stderr)
				if how == asBar {
					fmt.Println(forBar(r))
				}
				return refused
			}
			switch {
			case how == asBar:
				line = forBar(r) + "\n"
			case r.Code != 0:
				// Once, not at every reconnect.
				if r.Stderr != last {
					os.Stderr.WriteString(r.Stderr)
					last = r.Stderr
				}
				return nil
			case how == asJSON:
				line = string(r.Result) + "\n"
				if len(r.Result) == 0 {
					line = "null\n"
				}
			default:
				line = asText(r.Result)
			}
			if _, err := os.Stdout.WriteString(line); err != nil {
				return closed
			}
			return nil
		})
		switch {
		case errors.Is(err, closed):
			return 0
		case errors.Is(err, refused):
			return protocol.ExitUsage
		case stale:
			continue
		}
		time.Sleep(pause)
		pause = min(2*pause, 10*time.Second)
	}
}

// shows is whether a character may pass as it is: a printable one, or one of the
// private-use characters that icon fonts draw their icons with.
func shows(r rune) bool { return unicode.IsPrint(r) || unicode.Is(unicode.Co, r) }

// field escapes a text so that it stays one field of one line, and cannot steer
// a terminal (S30).
func field(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case !shows(r) && r != '\u200d':
			fmt.Fprintf(&b, `\u{%04x}`, r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// line is JSON as one line of text that cannot steer a terminal and still
// parses: what is not printable becomes a JSON escape.
func line(raw json.RawMessage) string {
	var b strings.Builder
	for _, r := range string(raw) {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			b.WriteByte(' ') // only ever between the parts of JSON
		case !shows(r) && r != '\u200d' && r < 0x10000:
			fmt.Fprintf(&b, `\u%04x`, r)
		case !shows(r) && r != '\u200d':
			// beyond four digits: as the pair JSON writes it
			r -= 0x10000
			fmt.Fprintf(&b, `\u%04x\u%04x`, 0xd800+(r>>10), 0xdc00+(r&0x3ff))
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// asText is a value for reading and for pipes: text as it is, rows as
// label<TAB>key<TAB>state, anything else as JSON.
func asText(raw json.RawMessage) string {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return field(string(raw)) + "\n"
	}
	asIs := line(raw) + "\n"
	switch v := v.(type) {
	case nil:
		return ""
	case string:
		return field(v) + "\n"
	case []any:
		var b strings.Builder
		for _, x := range v {
			row, ok := x.(map[string]any)
			label, isText := row["label"].(string)
			if !ok || !isText {
				return asIs
			}
			text := func(k string) string {
				switch x := row[k].(type) {
				case nil:
					return ""
				case string:
					return field(x)
				}
				return field(fmt.Sprint(row[k]))
			}
			fmt.Fprintf(&b, "%s\t%s\t%s\n", field(label), text("key"), text("state"))
		}
		return b.String()
	}
	return asIs
}

// forBar is the JSON a status bar reads. The bar takes text and tooltip as
// markup, so they are escaped, and a class is limited to a name. Something is
// always shown: a module that vanishes says nothing.
func forBar(r protocol.Response) string {
	markup := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace
	clip := func(s string, n int) string {
		if r := []rune(s); len(r) > n {
			s = string(r[:n]) + "..."
		}
		return markup(strings.Map(func(c rune) rune {
			if c != '\n' && !shows(c) {
				return ' '
			}
			return c
		}, s))
	}
	className := func(s string) string {
		return strings.Map(func(c rune) rune {
			if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-' {
				return c
			}
			return -1
		}, s)
	}
	var text, tooltip, class string
	var v any
	if r.Code == 0 && len(r.Result) > 0 {
		_ = json.Unmarshal(r.Result, &v)
	}
	switch v := v.(type) {
	case string:
		text = v
	case float64, bool:
		text = string(r.Result)
	case map[string]any:
		text, _ = v["text"].(string)
		tooltip, _ = v["tooltip"].(string)
		class, _ = v["class"].(string)
		if text == "" && tooltip == "" {
			text = string(r.Result)
		}
	case []any:
		text = fmt.Sprint(len(v))
		var labels []string
		for _, x := range v {
			if row, ok := x.(map[string]any); ok && len(labels) < 20 {
				l, _ := row["label"].(string)
				labels = append(labels, l)
			}
		}
		tooltip = strings.Join(labels, "\n")
	}
	switch {
	case r.Code != 0:
		text, tooltip, class = "!", strings.TrimPrefix(strings.TrimSpace(r.Stderr), "spot: "), "error"
	case r.Stderr != "": // an old value
		tooltip, class = strings.TrimSpace(tooltip+"\n"+strings.TrimPrefix(strings.TrimSpace(r.Stderr), "spot: ")), "stale"
	}
	b, _ := json.Marshal(map[string]string{"text": clip(text, 200), "tooltip": clip(tooltip, 2000), "class": className(class)})
	return string(b)
}

// modStates asks a daemon that is running how its mods are doing.
func modStates() map[string]modd.Status {
	out := map[string]modd.Status{}
	resp, ok := client.IfRunning(protocol.Request{Op: protocol.OpMods, Version: protocol.Version})
	var list []modd.Status
	if ok && json.Unmarshal(resp.Result, &list) == nil {
		for _, s := range list {
			out[s.Name] = s
		}
	}
	return out
}

func reloadMods(name string) bool {
	_, ok := client.IfRunning(protocol.Request{Op: protocol.OpReload, Version: protocol.Version, Name: name})
	return ok
}

// parseCommand is `spot parse`: standard input through a parser, printed as a
// JSON array.
func parseCommand(args []string) int {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		fmt.Fprintln(os.Stderr, "usage: spot parse <parser>   (reads standard input)")
		return protocol.ExitUsage
	}
	input, err := io.ReadAll(io.LimitReader(os.Stdin, protocol.MaxStdin+1))
	if err != nil {
		fmt.Fprintf(os.Stderr, "spot: reading the input: %v\n", err)
		return protocol.ExitError
	}
	if len(input) > protocol.MaxStdin {
		fmt.Fprintf(os.Stderr, "spot: more than %d MB to parse\n", protocol.MaxStdin>>20)
		return protocol.ExitError
	}
	result, err := client.Mods{}.Parse(args[0], input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "spot: %v\n", err)
		return protocol.ExitError
	}
	fmt.Println(line(result))
	return 0
}
