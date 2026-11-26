package daemon

import (
	"testing"

	"github.com/diamondburned/gotk4/pkg/gio/v2"
)

// GApplication rejects an ID with a part that starts with a digit; config files
// named like "01-form.toml" must still produce a valid one.
func TestAppIDValid(t *testing.T) {
	for _, name := range []string{"ssh", "01-form", "9", "my conf.v2", "", "ünï"} {
		id := appID(name, 123)
		if !gio.ApplicationIDIsValid(id) {
			t.Errorf("appID(%q) = %q is not a valid application ID", name, id)
		}
	}
}
