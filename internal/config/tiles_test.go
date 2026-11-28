package config

import (
	"strings"
	"testing"
)

func TestLiveTilesAndMeter(t *testing.T) {
	tile := func(body string) string {
		return "[[items]]\ntype='tiles'\nid='q'\n[[items.tiles]]\nlabel='x'\nvalue='v'\n" + body
	}
	meter := func(body string) string { return "[[items]]\ntype='meter'\nid='m'\n" + body }
	for name, ok := range map[string]string{
		"switching":   tile("state='s'\non_change='c {value}'"),
		"only shows":  tile("state='s'"),
		"classes":     tile("class='warm  big_one'"),
		"a mod":       tile("from='m.v'\ncall='m.set'"),
		"meter":       meter("state='cat x'\nmin=10\nmax=20\nformat='{value}%'"),
		"meter a mod": meter("from='m.v'"),
	} {
		c, err := ParseConfig([]byte(ok), noEnv)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if w := UnknownKeys([]byte(ok)); len(w) != 0 {
			t.Errorf("%s: %v", name, w)
		}
		if uses := strings.Contains(ok, "from="); c.UsesMods() != uses {
			t.Errorf("%s: uses mods %v", name, c.UsesMods())
		}
	}
	for name, c := range map[string]struct{ toml, want string }{
		"change, no state":    {tile("on_change='c'"), "needs state"},
		"call, no from":       {tile("call='m.set'"), "needs from"},
		"state and from":      {tile("state='s'\nfrom='m.v'"), "not both"},
		"mixed":               {tile("state='s'\ncall='m.set'"), "needs from"},
		"mixed the other way": {tile("from='m.v'\non_change='c'"), "needs state"},
		"bad from":            {tile("from='m'"), "<mod>.<value>"},
		"bad class":           {tile("class='a{}'"), "not a CSS class"},
		"meter, nothing":      {meter(""), "needs state or from"},
		"meter that changes":  {meter("state='s'\non_change='c'"), "only shows"},
		"meter backwards":     {meter("state='s'\nmin=5\nmax=5"), "greater"},
		"format elsewhere":    {"[[items]]\ntype='field'\nid='f'\nformat='x'", "only applies to a meter"},
	} {
		if _, err := ParseConfig([]byte(c.toml), noEnv); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", name, err, c.want)
		}
	}
	if CSSValue("a b/č.1") != "a-b---1" {
		t.Errorf("%q", CSSValue("a b/č.1"))
	}
}
