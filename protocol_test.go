package main

import (
	"encoding/json"
	"net"
	"os"
	"strings"
	"testing"
)

func TestEnvLookup(t *testing.T) {
	get := envLookup([]string{"A=1", "SPOT_TITLE=x=y", "EMPTY=", "NOEQUALS"})
	for k, want := range map[string]string{"A": "1", "SPOT_TITLE": "x=y", "EMPTY": ""} {
		if v, ok := get(k); !ok || v != want {
			t.Errorf("%s = %q,%v want %q", k, v, ok, want)
		}
	}
	if _, ok := get("NOEQUALS"); ok {
		t.Error("an entry without '=' is not a variable")
	}
	if _, ok := get("MISSING"); ok {
		t.Error("MISSING should not exist")
	}
}

func TestRoundTrip(t *testing.T) {
	srv, cli := net.Pipe()
	defer srv.Close()
	defer cli.Close()
	go func() {
		var req request
		if err := json.NewDecoder(srv).Decode(&req); err != nil {
			return
		}
		json.NewEncoder(srv).Encode(response{Code: 7, Stdout: req.Stdin, Version: req.Version})
	}()
	resp, err := roundTrip(cli, request{Op: opShow, Version: "v1", Stdin: "x\ny\n", Chain: true})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Code != 7 || resp.Stdout != "x\ny\n" || resp.Version != "v1" {
		t.Fatalf("got %+v", resp)
	}
}

func TestBuildJobAppliesCallerEnvAndStdin(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/m.toml"
	toml := "style = \"x.css\"\n[[items]]\ntype = \"list\"\nid = \"c\"\nstdin = true\n[submit]\nstdout = \"{c}\"\n"
	if err := writeFile(path, toml); err != nil {
		t.Fatal(err)
	}
	j, err := buildJob(request{Op: opShow, Config: path, Env: []string{"SPOT_TITLE=From caller"}, Stdin: "a\nb\n", Dir: "/x"})
	if err != nil {
		t.Fatal(err)
	}
	if j.cfg.Title != "From caller" {
		t.Errorf("SPOT_TITLE from the request env was not applied: %q", j.cfg.Title)
	}
	if got := j.cfg.Items[0].Options; len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("stdin options: %v", got)
	}
	if j.css != dir+"/x.css" {
		t.Errorf("css resolved to %q", j.css)
	}
	if j.dir != "/x" {
		t.Errorf("dir %q", j.dir)
	}
}

func TestBuildJobReportsBadConfigWithPath(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/bad.toml"
	writeFile(path, "title = = nope\n")
	_, err := buildJob(request{Config: path})
	if err == nil {
		t.Fatal("want an error")
	}
	if want := path; !contains(err.Error(), want) {
		t.Errorf("error %q should name the file", err)
	}
}

func writeFile(p, s string) error { return os.WriteFile(p, []byte(s), 0o644) }
func contains(s, sub string) bool { return strings.Contains(s, sub) }
