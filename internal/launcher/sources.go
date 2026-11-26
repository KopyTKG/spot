package launcher

import (
	"context"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"gitlab.com/KopyTKG/spot/internal/config"
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
// the selected row. This file is the logic; package ui draws and schedules it.

// sourceRow is one result.
type sourceRow struct {
	Label   string // shown
	Value   string // what activate gets (default: the label)
	Comment string // second line, optional
}

// matchSource finds the source whose prefix the query starts with (the longest
// one wins, so "=" and "==" can coexist) and returns it with the rest of the
// query, left-trimmed. It returns nil when the query is plain app search.
func matchSource(sources []config.SourceConfig, query string) (*config.SourceConfig, string) {
	q := strings.TrimLeft(query, " ")
	var best *config.SourceConfig
	for i := range sources {
		p := sources[i].Prefix
		if p != "" && strings.HasPrefix(q, p) && (best == nil || len(p) > len(best.Prefix)) {
			best = &sources[i]
		}
	}
	if best == nil {
		for i := range sources { // no prefix typed: the default source takes the whole query
			if sources[i].Default {
				return &sources[i], strings.TrimLeft(query, " ")
			}
		}
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
		r := sourceRow{Label: parts[0], Value: parts[0]}
		if len(parts) > 1 && parts[1] != "" {
			r.Value = parts[1]
		}
		if len(parts) > 2 {
			r.Comment = parts[2]
		}
		rows = append(rows, r)
		if max > 0 && len(rows) >= max {
			break
		}
	}
	return rows
}

// ExpandSource fills {value} {label} {query} in an activate / exec template.
// Each is shell-quoted, so a query cannot break out of the command.
func ExpandSource(tmpl string, row sourceRow, query string) string {
	return strings.NewReplacer(
		"{value}", shellQuote(row.Value),
		"{label}", shellQuote(row.Label),
		"{query}", shellQuote(query),
	).Replace(tmpl)
}

// RunSource runs the source's command for query and returns its rows. The
// command sees the caller's environment plus SPOT_QUERY and SPOT_SOURCE. A
// non-zero exit is not an error (grep with no match exits 1): what it printed
// is the answer. Only a cancelled or timed-out run reports one.
func RunSource(ctx context.Context, s *config.SourceConfig, query string, env []string, dir string) ([]sourceRow, error) {
	ctx, cancel := context.WithTimeout(ctx, s.RunTimeout())
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", ExpandSource(s.Exec, sourceRow{}, query))
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
	return parseSourceRows(string(out), s.RowLimit()), nil
}
