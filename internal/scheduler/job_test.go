package scheduler

import (
	"os"
	"strings"
	"testing"

	"gitlab.com/KopyTKG/spot/internal/protocol"
)

func TestBuildJobAppliesCallerEnvAndStdin(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/m.toml"
	toml := "style = \"x.css\"\n[[items]]\ntype = \"list\"\nid = \"c\"\nstdin = true\n[submit]\nstdout = \"{c}\"\n"
	if err := writeFile(path, toml); err != nil {
		t.Fatal(err)
	}
	j, err := BuildJob(protocol.Request{Op: protocol.OpShow, Config: path, Env: []string{"SPOT_TITLE=From caller"}, Stdin: "a\nb\n", Dir: "/x"})
	if err != nil {
		t.Fatal(err)
	}
	if j.Cfg.Title != "From caller" {
		t.Errorf("SPOT_TITLE from the request env was not applied: %q", j.Cfg.Title)
	}
	if got := j.Cfg.Items[0].Options; len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("stdin options: %v", got)
	}
	if j.CSS != dir+"/x.css" {
		t.Errorf("css resolved to %q", j.CSS)
	}
	if j.Dir != "/x" {
		t.Errorf("dir %q", j.Dir)
	}
}

func TestBuildJobReportsBadConfigWithPath(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/bad.toml"
	writeFile(path, "title = = nope\n")
	_, err := BuildJob(protocol.Request{Config: path})
	if err == nil {
		t.Fatal("want an error")
	}
	if want := path; !contains(err.Error(), want) {
		t.Errorf("error %q should name the file", err)
	}
}

func writeFile(p, s string) error { return os.WriteFile(p, []byte(s), 0o644) }

func contains(s, sub string) bool { return strings.Contains(s, sub) }
