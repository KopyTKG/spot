package launcher

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"gitlab.com/KopyTKG/spot/internal/config"
)

func TestMatchSource(t *testing.T) {
	src := []config.SourceConfig{{Name: "calc", Prefix: "="}, {Name: "files", Prefix: "."}, {Name: "web", Prefix: "@"}, {Name: "strict", Prefix: "=="}}
	for _, c := range []struct {
		query, source, rest string
	}{
		{"= 2+2", "calc", "2+2"},
		{"=2+2", "calc", "2+2"},
		{"  = 2+2", "calc", "2+2"},
		{". report pdf", "files", "report pdf"},
		{"@gtk4 layer", "web", "gtk4 layer"},
		{"== x", "strict", "x"}, // the longest prefix wins
		{"=", "calc", ""},
		{"firefox", "", "firefox"},
		{"", "", ""},
		{"a = b", "", "a = b"}, // only a leading prefix counts
	} {
		got, rest := matchSource(src, c.query)
		name := ""
		if got != nil {
			name = got.Name
		}
		if name != c.source || rest != c.rest {
			t.Errorf("matchSource(%q) = (%q, %q), want (%q, %q)", c.query, name, rest, c.source, c.rest)
		}
	}
}

func TestParseSourceRows(t *testing.T) {
	out := "plain\nlabel\tvalue\nwith comment\tv\tsecond line\n\n   \nkeep\ttabs\tc\textra\n"
	rows := parseSourceRows(out, 0)
	want := []sourceRow{
		{"plain", "plain", ""},
		{"label", "value", ""},
		{"with comment", "v", "second line"},
		{"keep", "tabs", "c\textra"},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("rows = %+v", rows)
	}
	if got := parseSourceRows("a\nb\nc\nd\n", 2); len(got) != 2 {
		t.Errorf("max_results: %d", len(got))
	}
	if got := parseSourceRows("empty\t\tc\n", 0); got[0].Value != "empty" {
		t.Errorf("an empty value falls back to the label: %+v", got)
	}
	if got := parseSourceRows("a\r\nb\r\n", 0); len(got) != 2 || got[0].Label != "a" {
		t.Errorf("CRLF: %+v", got)
	}
}

func TestExpandSourceQuotes(t *testing.T) {
	row := sourceRow{Label: "My File", Value: "/tmp/it's here"}
	got := ExpandSource("xdg-open {value} # {label} {query}", row, "a; rm -rf /")
	want := `xdg-open '/tmp/it'\''s here' # 'My File' 'a; rm -rf /'`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestValidateSources(t *testing.T) {
	ok := []config.SourceConfig{{Name: "a", Prefix: "=", Exec: "x"}, {Name: "b", Prefix: ".", Exec: "y"}}
	if err := config.ValidateSources(ok); err != nil {
		t.Errorf("valid sources rejected: %v", err)
	}
	neg := -1
	for name, src := range map[string][]config.SourceConfig{
		"no prefix": {{Name: "a", Exec: "x"}},
		"no exec":   {{Name: "a", Prefix: "="}},
		"space":     {{Name: "a", Prefix: "= ", Exec: "x"}},
		"duplicate": {{Name: "a", Prefix: "=", Exec: "x"}, {Name: "b", Prefix: "=", Exec: "y"}},
		"neg delay": {{Name: "a", Prefix: "=", Exec: "x", Delay: &neg}},
	} {
		if err := config.ValidateSources(src); err == nil {
			t.Errorf("%s should be rejected", name)
		}
	}
}

func TestSourceDefaults(t *testing.T) {
	s := &config.SourceConfig{}
	if s.Debounce() != 150*time.Millisecond || s.MinQuery() != 1 || s.RowLimit() != 50 || s.RunTimeout() != 5*time.Second {
		t.Errorf("defaults: %v %d %d %v", s.Debounce(), s.MinQuery(), s.RowLimit(), s.RunTimeout())
	}
	zero := 0
	s = &config.SourceConfig{Delay: &zero, MinChars: 3, MaxResults: 5, Timeout: 100}
	if s.Debounce() != 0 || s.MinQuery() != 3 || s.RowLimit() != 5 || s.RunTimeout() != 100*time.Millisecond {
		t.Errorf("explicit: %v %d %d %v", s.Debounce(), s.MinQuery(), s.RowLimit(), s.RunTimeout())
	}
}

func TestRunSource(t *testing.T) {
	s := &config.SourceConfig{Name: "echo", Prefix: "=", Exec: `printf '%s\n' "got: $SPOT_QUERY" "src: $SPOT_SOURCE"; printf 'quoted: %s\n' {query}`}
	rows, err := RunSource(context.Background(), s, "it's 2+2", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	var labels []string
	for _, r := range rows {
		labels = append(labels, r.Label)
	}
	want := []string{"got: it's 2+2", "src: echo", "quoted: it's 2+2"}
	if !reflect.DeepEqual(labels, want) {
		t.Errorf("rows = %q, want %q", labels, want)
	}
}

func TestRunSourceNonZeroExitIsAnAnswer(t *testing.T) {
	s := &config.SourceConfig{Exec: `echo partial; exit 1`}
	rows, err := RunSource(context.Background(), s, "x", nil, "")
	if err != nil || len(rows) != 1 || rows[0].Label != "partial" {
		t.Errorf("rows=%v err=%v", rows, err)
	}
}

func TestRunSourceTimeoutAndCancel(t *testing.T) {
	slow := &config.SourceConfig{Exec: `sleep 5; echo late`, Timeout: 100}
	start := time.Now()
	if _, err := RunSource(context.Background(), slow, "x", nil, ""); err == nil {
		t.Error("a command past its timeout should report an error")
	}
	if time.Since(start) > 2*time.Second {
		t.Errorf("the slow command was not killed: %v", time.Since(start))
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	if _, err := RunSource(ctx, &config.SourceConfig{Exec: `sleep 5`}, "x", nil, ""); err == nil {
		t.Error("a cancelled run should report an error")
	}
}

func TestRunSourceCapsRows(t *testing.T) {
	s := &config.SourceConfig{Exec: `seq 1 100`, MaxResults: 7}
	rows, _ := RunSource(context.Background(), s, "x", nil, "")
	if len(rows) != 7 || !strings.HasPrefix(rows[0].Label, "1") {
		t.Errorf("max_results: %d rows", len(rows))
	}
}

func TestDefaultSource(t *testing.T) {
	src := []config.SourceConfig{{Name: "term", Prefix: ">>"}, {Name: "run", Default: true}}
	for _, c := range []struct{ query, source, rest string }{
		{"htop", "run", "htop"},         // no prefix: the default takes everything
		{"  htop -x", "run", "htop -x"}, // leading spaces dropped
		{">> htop", "term", "htop"},     // a real prefix still wins
		{"", "run", ""},                 // empty: still the default, with nothing to run
		{"a >> b", "run", "a >> b"},     // only a leading prefix counts
	} {
		got, rest := matchSource(src, c.query)
		if got == nil || got.Name != c.source || rest != c.rest {
			name := ""
			if got != nil {
				name = got.Name
			}
			t.Errorf("matchSource(%q) = (%q, %q), want (%q, %q)", c.query, name, rest, c.source, c.rest)
		}
	}
	// Without a default source plain text is plain app search.
	if got, rest := matchSource([]config.SourceConfig{{Name: "t", Prefix: ">>"}}, "htop"); got != nil || rest != "htop" {
		t.Errorf("no default: %v %q", got, rest)
	}
	if !config.HasDefaultSource(src) || config.HasDefaultSource(src[:1]) {
		t.Error("hasDefaultSource")
	}
}

func TestValidateDefaultSource(t *testing.T) {
	ok := []config.SourceConfig{{Name: "run", Default: true, Exec: "x"}, {Name: "t", Prefix: ">>", Exec: "y"}}
	if err := config.ValidateSources(ok); err != nil {
		t.Errorf("valid: %v", err)
	}
	for name, bad := range map[string][]config.SourceConfig{
		"two defaults":       {{Name: "a", Default: true, Exec: "x"}, {Name: "b", Default: true, Exec: "y"}},
		"default and prefix": {{Name: "a", Default: true, Prefix: "=", Exec: "x"}},
		"default no exec":    {{Name: "a", Default: true}},
	} {
		if err := config.ValidateSources(bad); err == nil {
			t.Errorf("%s should be rejected", name)
		}
	}
}
