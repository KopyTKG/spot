package protocol

import (
	"encoding/json"
	"net"
	"testing"
)

func TestEnvLookup(t *testing.T) {
	get := EnvLookup([]string{"A=1", "SPOT_TITLE=x=y", "EMPTY=", "NOEQUALS"})
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
		var req Request
		if err := json.NewDecoder(srv).Decode(&req); err != nil {
			return
		}
		json.NewEncoder(srv).Encode(Response{Code: 7, Stdout: req.Stdin, Version: req.Version})
	}()
	resp, err := RoundTrip(cli, Request{Op: OpShow, Version: "v1", Stdin: "x\ny\n", Chain: true})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Code != 7 || resp.Stdout != "x\ny\n" || resp.Version != "v1" {
		t.Fatalf("got %+v", resp)
	}
}
