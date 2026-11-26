package config

import (
	"fmt"
	"strings"
	"time"
)

// SourceConfig is one [[sources]] entry.
type SourceConfig struct {
	Name       string `toml:"name"`
	Prefix     string `toml:"prefix"`      // typed first to select this source ("=", ".", "@", ">"); not used by a default source
	Default    bool   `toml:"default"`     // the source for everything typed without another source's prefix (at most one); the app list is not shown
	Exec       string `toml:"exec"`        // run with `sh -c`; the query is in $SPOT_QUERY and as {query}
	Activate   string `toml:"activate"`    // run on Enter; {value} {label} {query} are shell-quoted; empty = print the value
	Icon       string `toml:"icon"`        // icon theme name shown on every row (none by default)
	Delay      *int   `toml:"delay"`       // ms to wait after the last keystroke (default 150)
	MinChars   int    `toml:"min_chars"`   // shortest query that runs (default 1)
	MaxResults int    `toml:"max_results"` // rows kept (default 50)
	Timeout    int    `toml:"timeout"`     // ms before the command is killed (default 5000)
}

func (s *SourceConfig) Debounce() time.Duration {
	if s.Delay == nil {
		return 150 * time.Millisecond
	}
	return time.Duration(*s.Delay) * time.Millisecond
}

func (s *SourceConfig) MinQuery() int {
	if s.MinChars <= 0 {
		return 1
	}
	return s.MinChars
}

func (s *SourceConfig) RowLimit() int {
	if s.MaxResults <= 0 {
		return 50
	}
	return s.MaxResults
}

func (s *SourceConfig) RunTimeout() time.Duration {
	if s.Timeout <= 0 {
		return 5 * time.Second
	}
	return time.Duration(s.Timeout) * time.Millisecond
}

// ValidateSources rejects entries that could never work.
func ValidateSources(src []SourceConfig) error {
	seen := map[string]string{}
	defaults := 0
	for i, s := range src {
		name := s.Name
		if name == "" {
			name = fmt.Sprintf("#%d", i+1)
		}
		if s.Default {
			defaults++
			if defaults > 1 {
				return fmt.Errorf("sources %s: only one source can be the default", name)
			}
			if s.Prefix != "" {
				return fmt.Errorf("sources %s: a default source takes no prefix", name)
			}
			if strings.TrimSpace(s.Exec) == "" {
				return fmt.Errorf("sources %s: exec is required", name)
			}
			continue
		}
		switch {
		case s.Prefix == "":
			return fmt.Errorf("sources %s: prefix is required (or set default = true)", name)
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

// HasDefaultSource reports whether one of the sources is the default.
func HasDefaultSource(sources []SourceConfig) bool {
	for _, s := range sources {
		if s.Default {
			return true
		}
	}
	return false
}
