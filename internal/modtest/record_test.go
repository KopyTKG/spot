package modtest

import (
	"os"
	"testing"
)

// The approval record decides what runs. No mod may be granted writing it, and
// no write gets there by dots, a symlink or a hard link.
func TestTheApprovalRecordCannotBeWritten(t *testing.T) {
	w := newWorld(t)
	w.file("state/spot/approvals/m.json", "{}")
	record := w.dir + "/state/spot/approvals/m.json"
	for name, grant := range map[string]string{"its folder": "~/state/spot/approvals", "above it": "~/state", "spot's state": "~/state/spot"} {
		if _, err := w.start("[permissions.write]\n\""+grant+"\"='w'\n", "return 1"); err == nil {
			t.Errorf("a grant on %s was accepted", name)
		}
	}
	l := w.must("[permissions.write]\n\"~/writable\"='w'\n", `spot.on("write", function(a) local ok, e = spot.write(a[1], "forged") return ok or e end)`)
	os.Symlink(record, w.dir+"/writable/sym")
	os.Symlink(w.dir+"/state/spot/approvals", w.dir+"/writable/dir")
	os.Link(record, w.dir+"/writable/hard")
	for _, p := range []string{"~/writable/../state/spot/approvals/m.json", "~/writable/sym", "~/writable/dir/m.json", "~/writable/hard", record} {
		l.call("write", p)
		if b, _ := os.ReadFile(record); string(b) != "{}" {
			t.Errorf("written through %s", p)
		}
	}
}
