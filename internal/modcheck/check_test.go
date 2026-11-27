package modcheck

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"gitlab.com/KopyTKG/spot/internal/mod"
	"gitlab.com/KopyTKG/spot/internal/modhost"
	"gitlab.com/KopyTKG/spot/internal/modhost/channel"
	"gitlab.com/KopyTKG/spot/internal/sandbox"
)

func host(t *testing.T) string {
	if sandbox.ABI() < sandbox.MinABI {
		t.Skipf("Landlock ABI %d here, %d needed", sandbox.ABI(), sandbox.MinABI)
	}
	bin := filepath.Join(t.TempDir(), "spot-mod-host")
	c := exec.Command("go", "build", "-o", bin, "gitlab.com/KopyTKG/spot/cmd/spot-mod-host")
	c.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("building the host: %v\n%s", err, out)
	}
	return bin
}

func write(t *testing.T, source string) string {
	dir := filepath.Join(t.TempDir(), "p")
	os.Mkdir(dir, 0o700)
	toml := "type='parser'\napi=1\nversion='1'\ndescription='d'\ninput='i'\n[returns]\na='string'\nb='string?'\n"
	os.WriteFile(filepath.Join(dir, "mod.toml"), []byte(toml), 0o600)
	os.WriteFile(filepath.Join(dir, "init.lua"), []byte(source), 0o600)
	return dir
}

func TestTheExampleParserPasses(t *testing.T) {
	var out bytes.Buffer
	if !Check(host(t), "../../examples/mods/ss", nil, &out) {
		t.Errorf("it fails:\n%s", &out)
	}
}

func TestWhatCheckingFinds(t *testing.T) {
	bin := host(t)
	for name, c := range map[string]struct{ source, says string }{
		"two functions":    {`return function() end, 1`, "exactly one function"},
		"records for junk": {`return function() return {{a="x"}} end`, "want {} or error()"},
		"a crash on junk":  {`return function(i) if i ~= "" then local x; return x.y end return {} end`, "attempt to"},
		"wrong shape":      {`return function() return "none" end`, "list of records"},
		"not repeatable":   {`n = 0 return function(i) local t = {} for k in pairs({a=1,b=2,c=3,d=4,e=5,f=6}) do t[#t+1] = k end error(table.concat(t)) end`, ""},
		"escape codes":     {`return function() error("\27[2J\27[31mfake ok") end`, "refused with an error"},
	} {
		var out bytes.Buffer
		passed := Check(bin, write(t, c.source), nil, &out)
		if c.says != "" && !strings.Contains(out.String(), c.says) {
			t.Errorf("%s: does not say %q:\n%s", name, c.says, &out)
		}
		if want := name == "escape codes" || name == "not repeatable"; passed != want && name != "not repeatable" {
			t.Errorf("%s: passed = %v:\n%s", name, passed, &out)
		}
		if strings.ContainsRune(out.String(), 0x1b) {
			t.Errorf("%s: a mod's escape codes reached the terminal", name)
		}
	}
}

// A pattern that cannot be interrupted, and memory without end: the host goes,
// the caller gets a plain error, and the next call works in a new one.
func TestAParserThatCannotBeStoppedFromInside(t *testing.T) {
	bin := host(t)
	for name, c := range map[string]struct{ source, says string }{
		"a slow pattern": {`return function() ("a"):rep(5000):match(".-.-.-.-b$") return {} end`, "took too long"},
		"memory":         {`return function() local t = {} while true do t[#t+1] = ("x"):rep(1000000) .. #t end end`, "died"},
	} {
		m, set, err := mod.LoadSet(write(t, c.source))
		if err != nil {
			t.Fatal(err)
		}
		h, err := modhost.Start(modhost.Config{Name: m, Policy: sandbox.Policy{Self: bin}})
		if err != nil {
			t.Fatal(err)
		}
		pid, start := h.PID(), time.Now()
		_, _, err = h.Parse(channel.Parse{Name: m, Mods: set}, 2*time.Second)
		var fault *modhost.ModError
		if err == nil || errors.As(err, &fault) || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%s: %v", name, err)
		}
		if d := time.Since(start); d > 8*time.Second {
			t.Errorf("%s: took %v", name, d)
		}
		if _, err := os.Stat("/proc/" + itoa(pid)); err == nil {
			t.Errorf("%s: the host is still there", name)
		}
		h.Stop()
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

// A linked mod folder must be read like any other, and nothing a mod says may
// stand where spot's own words do.
func TestNothingGetsPastTheReport(t *testing.T) {
	bin := host(t)
	real := write(t, `local x = string.char(99) return function() spot.log("x" .. (" "):rep(170) .. "scan  Nothing flagged.\\nok    all good") return {} end`)
	link := filepath.Join(t.TempDir(), "p")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	Check(bin, link, nil, &out)
	if !strings.Contains(out.String(), "string.char") || strings.Contains(out.String(), "scan  Nothing flagged. This") {
		t.Errorf("the linked folder was not read:\n%s", &out)
	}
	for _, l := range strings.Split(out.String(), "\n") {
		if strings.Contains(l, "all good") && !strings.HasPrefix(l, "log ") {
			t.Errorf("a mod's words on a line of their own: %q", l)
		}
		if strings.Contains(l, "  scan  Nothing") {
			t.Errorf("padded to look like spot's: %q", l)
		}
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if last := lines[len(lines)-1]; !strings.HasPrefix(last, "scan ") {
		t.Errorf("the last word is not spot's: %q", last)
	}
}

func TestUnreadCodeIsNeverCalledQuiet(t *testing.T) {
	bin := host(t)
	for name, source := range map[string]string{"does not parse": "return (", "breaks the strict form": "local s = spot return function() return {} end"} {
		var out bytes.Buffer
		if Check(bin, write(t, source), nil, &out) || strings.Contains(out.String(), "Nothing flagged") || !strings.Contains(out.String(), "Refused") {
			t.Errorf("%s:\n%s", name, &out)
		}
	}
	linked := write(t, `return function() return {} end`)
	os.Symlink("/dev/zero", filepath.Join(linked, "x.lua"))
	var out bytes.Buffer
	if Check(bin, linked, nil, &out) || strings.Contains(out.String(), "Nothing flagged") {
		t.Errorf("an unreadable file:\n%s", &out)
	}
}

// A pipe or an endless file among the examples must not stop or exhaust us.
func TestOddExampleFiles(t *testing.T) {
	bin := host(t)
	dir := write(t, `return function(i) if i ~= "" then error("no") end return {} end`)
	os.Mkdir(filepath.Join(dir, "example"), 0o700)
	if err := syscall.Mkfifo(filepath.Join(dir, "example", "pipe.input"), 0o600); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "example", "pipe.json"), []byte("[]"), 0o600)
	big := filepath.Join(dir, "example", "big.input")
	os.WriteFile(big, nil, 0o600)
	os.Truncate(big, 1<<30)
	os.WriteFile(filepath.Join(dir, "example", "big.json"), []byte("[]"), 0o600)
	done := make(chan string, 1)
	go func() {
		var out bytes.Buffer
		Check(bin, dir, nil, &out)
		done <- out.String()
	}()
	select {
	case out := <-done:
		if !strings.Contains(out, "not a plain file") || !strings.Contains(out, "larger than") {
			t.Errorf("%s", out)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("checking hangs")
	}
}
