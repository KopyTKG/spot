package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode"

	"gitlab.com/KopyTKG/spot/internal/client"
	"gitlab.com/KopyTKG/spot/internal/protocol"
)

const askUsage = `usage: spot get <mod.value> [--json]          print a value of a mod
       spot call <mod.request> [args...]      run a request of a mod
`

// askMod is `spot get` and `spot call`.
func askMod(verb string, args []string) int {
	asJSON := false
	if verb == protocol.OpGet {
		var rest []string
		for _, a := range args {
			if a == "--json" {
				asJSON = true
				continue
			}
			rest = append(rest, a)
		}
		args = rest
	}
	// Everything after a request's name is its arguments, dashes and all.
	if len(args) == 0 || strings.HasPrefix(args[0], "-") || verb == protocol.OpGet && len(args) != 1 {
		fmt.Fprint(os.Stderr, askUsage)
		return protocol.ExitUsage
	}
	resp, ok := client.ViaDaemon(protocol.Request{Op: verb, Version: protocol.Version, Name: args[0], Args: args[1:]})
	if !ok {
		fmt.Fprintln(os.Stderr, "spot: mods run in the daemon, and it could not be reached")
		return protocol.ExitError
	}
	os.Stderr.WriteString(resp.Stderr)
	if resp.Code == 0 && len(resp.Result) > 0 {
		if asJSON {
			fmt.Println(string(resp.Result))
		} else {
			os.Stdout.WriteString(asText(resp.Result))
		}
	}
	return resp.Code
}

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
		case !unicode.IsPrint(r) && r != '‍':
			fmt.Fprintf(&b, `\u{%04x}`, r)
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
				return field(string(raw)) + "\n"
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
	return field(string(raw)) + "\n"
}
