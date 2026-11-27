package modtest

import (
	"os"
	"testing"
)

// With net, programs can check who they are talking to: the certificates are in
// the base grant; without net they are not.
func TestCertificatesComeWithNet(t *testing.T) {
	const bundle = "/etc/ssl/certs/ca-certificates.crt"
	if _, err := os.Stat(bundle); err != nil {
		t.Skip("no certificate bundle at " + bundle)
	}
	for toml, want := range map[string]string{
		"[permissions.run]\nhead='r'\n[permissions.net]\n\"443\"='n'\n": "0",
		"[permissions.run]\nhead='r'\n":                                 "1",
	} {
		w := newWorld(t)
		l := w.must(toml, `spot.on("read", function() return spot.run({ "head", "-c", "100", "`+bundle+`" }).status end)`)
		l.expect(toml, "read", want)
	}
}
