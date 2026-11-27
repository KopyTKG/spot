package scan

// A mod folder can hold more than the mod: dialogs, scripts, styles. The mod
// runs in a sandbox. A dialog or a script shipped beside it does not: whoever
// opens or runs it gives it everything they can do.

import (
	"bytes"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Outside is a file of a mod folder that runs outside the sandbox when used.
type Outside struct {
	File string
	Runs []string // the commands a dialog would run; nil for a script
	Why  string
}

// commandKeys are the keys of a dialog whose text is run by a shell.
var commandKeys = []string{"exec", "activate", "state", "on_change", "run", "cmd", "command", "terminal"}

var scripts = []string{".sh", ".bash", ".fish", ".zsh", ".py", ".pl", ".rb", ".js"}

func commands(v any, launch bool, out *[]string) {
	switch v := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			s, isText := v[k].(string)
			if isText && s != "" && (launch || slices.Contains(commandKeys, k)) {
				*out = append(*out, k+" = "+Quote(s))
				continue
			}
			commands(v[k], launch || k == "launch", out)
		}
	case []any:
		for _, x := range v {
			commands(x, launch, out)
		}
	}
}

// Beside looks at every file of a mod folder but its code and manifest.
func Beside(files map[string][]byte) []Outside {
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	var out []Outside
	for _, name := range names {
		b := files[name]
		ext := strings.ToLower(filepath.Ext(name))
		switch {
		case name == "mod.toml" || ext == ".lua":
		case ext == ".toml":
			var doc map[string]any
			if err := toml.Unmarshal(b, &doc); err != nil {
				continue // not a dialog spot could open
			}
			var runs []string
			commands(doc, false, &runs)
			if len(runs) > 0 {
				out = append(out, Outside{name, runs, "a dialog that runs commands"})
			}
		case bytes.HasPrefix(b, []byte("#!")) || slices.Contains(scripts, ext) || bytes.HasPrefix(b, []byte("\x7fELF")):
			out = append(out, Outside{File: name, Why: "a program"})
		}
	}
	return out
}
