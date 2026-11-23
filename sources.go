package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// Dynamic sources: the launcher (a `type = "apps"` item) can hand the search to a
// command. A [[sources]] entry has a prefix; when the query starts with it, the
// prefix is stripped and the command runs on every change (debounced), its output
// replacing the app list:
//
//	= 2+2        calculator       . report      file search
//	@ gtk4       web search       > htop        run a command
//
// The command prints one row per line: "label", "label<TAB>value" or
// "label<TAB>value<TAB>comment". Enter runs the source's `activate` template on
// the selected row. This file is the logic; dialog.go draws and schedules it.

// SourceConfig is one [[sources]] entry.
type SourceConfig struct {
	Name       string `toml:"name"`
	Prefix     string `toml:"prefix"`      // typed first to select this source ("=", ".", "@", ">")
	Exec       string `toml:"exec"`        // run with `sh -c`; the query is in $SPOT_QUERY and as {query}
	Activate   string `toml:"activate"`    // run on Enter; {value} {label} {query} are shell-quoted; empty = print the value
	Icon       string `toml:"icon"`        // icon theme name shown on every row (none by default)
	Delay      *int   `toml:"delay"`       // ms to wait after the last keystroke (default 150)
	MinChars   int    `toml:"min_chars"`   // shortest query that runs (default 1)
	MaxResults int    `toml:"max_results"` // rows kept (default 50)
	Timeout    int    `toml:"timeout"`     // ms before the command is killed (default 5000)
}

func (s *SourceConfig) delay() time.Duration {
	if s.Delay == nil {
		return 150 * time.Millisecond
	}
	return time.Duration(*s.Delay) * time.Millisecond
}

func (s *SourceConfig) minChars() int {
	if s.MinChars <= 0 {
		return 1
	}
	return s.MinChars
}

func (s *SourceConfig) maxResults() int {
	if s.MaxResults <= 0 {
		return 50
	}
	return s.MaxResults
}

func (s *SourceConfig) timeout() time.Duration {
	if s.Timeout <= 0 {
		return 5 * time.Second
	}
	return time.Duration(s.Timeout) * time.Millisecond
}

// sourceRow is one result.
type sourceRow struct {
	label   string // shown
	value   string // what activate gets (default: the label)
	comment string // second line, optional
}

// validateSources rejects entries that could never work.
func validateSources(src []SourceConfig) error {
	seen := map[string]string{}
	for i, s := range src {
		name := s.Name
		if name == "" {
			name = fmt.Sprintf("#%d", i+1)
		}
		switch {
		case s.Prefix == "":
			return fmt.Errorf("sources %s: prefix is required", name)
		case strings.TrimSpace(s.Prefix) != s.Prefix || strings.ContainsAny(s.Prefix, " \t"):
			return fmt.Errorf("sources %s: prefix %q must not contain whitespace", name, s.Prefix)
		case strings.TrimSpace(s.Exec) == "":
			return fmt.Errorf("sources %s: exec is required", name)
		case s.Delay != nil && *s.Delay < 0:
			return fmt.Errorf("sources %s: delay must not be negative", name)
		}
		if other, dup := seen[s.Prefix]; dup {
			return fmt.Errorf("sources %s: prefix %q is already used by %s", name, s.Prefix, other)
		}
		seen[s.Prefix] = name
	}
	return nil
}

// matchSource finds the source whose prefix the query starts with (the longest
// one wins, so "=" and "==" can coexist) and returns it with the rest of the
// query, left-trimmed. It returns nil when the query is plain app search.
func matchSource(sources []SourceConfig, query string) (*SourceConfig, string) {
	q := strings.TrimLeft(query, " ")
	var best *SourceConfig
	for i := range sources {
		p := sources[i].Prefix
		if p != "" && strings.HasPrefix(q, p) && (best == nil || len(p) > len(best.Prefix)) {
			best = &sources[i]
		}
	}
	if best == nil {
		return nil, query
	}
	return best, strings.TrimLeft(q[len(best.Prefix):], " ")
}

// parseSourceRows reads a command's output into rows, keeping at most max.
func parseSourceRows(out string, max int) []sourceRow {
	var rows []sourceRow
	for _, line := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		r := sourceRow{label: parts[0], value: parts[0]}
		if len(parts) > 1 && parts[1] != "" {
			r.value = parts[1]
		}
		if len(parts) > 2 {
			r.comment = parts[2]
		}
		rows = append(rows, r)
		if max > 0 && len(rows) >= max {
			break
		}
	}
	return rows
}

// expandSource fills {value} {label} {query} in an activate / exec template.
// Each is shell-quoted, so a query cannot break out of the command.
func expandSource(tmpl string, row sourceRow, query string) string {
	return strings.NewReplacer(
		"{value}", shellQuote(row.value),
		"{label}", shellQuote(row.label),
		"{query}", shellQuote(query),
	).Replace(tmpl)
}

// runSource runs the source's command for query and returns its rows. The
// command sees the caller's environment plus SPOT_QUERY and SPOT_SOURCE. A
// non-zero exit is not an error (grep with no match exits 1): what it printed
// is the answer. Only a cancelled or timed-out run reports one.
func runSource(ctx context.Context, s *SourceConfig, query string, env []string, dir string) ([]sourceRow, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout())
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", expandSource(s.Exec, sourceRow{}, query))
	cmd.Env = append(append([]string(nil), env...), "SPOT_QUERY="+query, "SPOT_SOURCE="+s.Name)
	cmd.Dir = dir
	// Kill the whole process group on cancel / timeout: killing only the shell
	// leaves its children running with the output pipe open, and Output() would
	// wait for them.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 300 * time.Millisecond
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		if _, isExit := err.(*exec.ExitError); !isExit {
			return nil, err
		}
	}
	return parseSourceRows(string(out), s.maxResults()), nil
}
