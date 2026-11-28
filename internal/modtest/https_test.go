package modtest

import (
	"net"
	"os"
	"testing"
	"time"
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

// A real HTTPS request from inside the sandbox: certificates, name lookup and
// the port. It needs the network; a test without it is skipped.
func TestHTTPSFromAMod(t *testing.T) {
	const url = "https://archlinux.org/"
	conn, err := net.DialTimeout("tcp", "archlinux.org:443", 5*time.Second)
	if err != nil {
		t.Skip("no network: " + err.Error())
	}
	conn.Close()
	const curl = "[permissions.run]\ncurl='r'\n"
	for name, c := range map[string]struct {
		net  string
		want string // curl's exit status and the answer
	}{
		"port 443 granted":  {"[permissions.net]\n\"443\"='n'\n", "0 200"},
		"another port only": {"[permissions.net]\n\"80\"='n'\n", "7 000"}, // resolves, cannot connect
		"no network at all": {"", "6 000"},                                // cannot even resolve
	} {
		w := newWorld(t)
		l := w.must(curl+c.net, `spot.on("get", function()
  local r = spot.run({ "curl", "-sS", "-o", "/dev/null", "-w", "%{http_code}", "--max-time", "8", "`+url+`" }, { timeout = 10000 })
  return r.status .. " " .. r.stdout
end)`)
		l.expect(name, "get", `"`+c.want+`"`)
	}
}
