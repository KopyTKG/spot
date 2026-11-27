package modtest

// Items of the mod host and sandbox checklists (#19, #29) that nothing else
// proved.

import (
	"net"
	"os"
	"strings"
	"testing"
)

func TestAGrantedSocketWorks(t *testing.T) {
	w := newWorld(t)
	os.Mkdir(w.dir+"/svc", 0o700)
	ln, err := net.Listen("unix", w.dir+"/svc/s")
	if err != nil {
		t.Skipf("no socket here: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Write([]byte("hello"))
			c.Close()
		}
	}()
	l := w.must("[permissions.run]\npython3='r'\n[permissions.socket]\n\""+w.dir+"/svc\"='s'\n", `
spot.on("ask", function(a)
  local code = "import socket,sys; s=socket.socket(socket.AF_UNIX); s.connect(sys.argv[1]); print(s.recv(10).decode(), end='')"
  local r = spot.run({ "python3", "-I", "-c", code, a[1] })
  return r.stdout ~= "" and r.stdout or r.stderr
end)`)
	l.expect("the granted socket", "ask", `"hello"`, w.dir+"/svc/s")
}

func TestTheRemainingLimits(t *testing.T) {
	w := newWorld(t)
	w.file("readable/big", strings.Repeat("x", 9<<20))
	l := w.must("[permissions.read]\n\"~/readable\"='r'\n", `
spot.on("big", function() local s, e = spot.read("~/readable/big") return e or #s end)
spot.on("timers", function() for i = 1, 100 do local ok = pcall(function() spot.after(100000, function() end) end) if not ok then return i - 1 end end return "no limit" end)
spot.on("values", function() for i = 1, 200 do local ok = pcall(function() spot.publish("v" .. i, 1) end) if not ok then return i - 1 end end return "no limit" end)
`)
	if got, _ := l.call("big"); !strings.Contains(got, "larger than") {
		t.Errorf("a file of 9 MB: %.80s", got)
	}
	l.expect("timers", "timers", "64")
	l.expect("values", "values", "100")
}
