package modtest

// What a careless mod, or a busy daemon, does to a host.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const careless = "[permissions.run]\necho='r'\nsh='r'\nsleep='r'\ncat='r'\n"

func TestManyCallersAtOnce(t *testing.T) {
	w := newWorld(t)
	l := w.must(careless, `spot.on("x", function() return spot.pipe({ "echo", "solo" }, { "cat" }).stdout end)`)
	var wg sync.WaitGroup
	for range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l.expect("one of many", "x", `"solo\n"`)
		}()
	}
	wg.Wait()
}

func copiesOf(args string) (n int) {
	dirs, _ := os.ReadDir("/proc")
	for _, d := range dirs {
		if b, _ := os.ReadFile("/proc/" + d.Name() + "/cmdline"); string(b) == args {
			n++
		}
	}
	return n
}

// Nothing of the daemon's is left to stop it: the host has to.
func TestProgramsGoWhenTheDaemonDies(t *testing.T) {
	w := newWorld(t)
	l := w.must(careless, `spot.on("nap", function() spot.run({ "sleep", "27.5" }, { timeout = 60000 }) end)`)
	const args = "sleep\x0027.5\x00"
	go l.call("nap")
	for range 200 {
		if copiesOf(args) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if copiesOf(args) != 1 {
		t.Fatal("the program did not start, so this tests nothing")
	}
	l.h.Abandon()
	time.Sleep(700 * time.Millisecond)
	if n := copiesOf(args); n != 0 {
		t.Errorf("%d left running", n)
	}
}

func events(cgroup, key string) int {
	b, _ := os.ReadFile(filepath.Join(cgroup, "pids.events"))
	for _, l := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(l, key+" "); ok {
			n, _ := strconv.Atoi(v)
			return n
		}
	}
	return 0
}

func TestForkBombAndMemoryBomb(t *testing.T) {
	w := newWorld(t)
	l := w.must(careless, `
spot.on("fine", function() return 1 end)
spot.on("forks", function() spot.run({ "sh", "-c", "i=0; while [ $i -lt 400 ]; do sleep 1 & i=$((i+1)); done 2>/dev/null; wait" }, { timeout = 4000 }) end)
spot.on("ulimit", function() return spot.run({ "sh", "-c", "ulimit -d" }).stdout end)
spot.on("memory", function() local t = {} while true do t[#t+1] = ("x"):rep(1000000) .. #t end end)
`)
	l.expect("a program's own memory limit", "ulimit", `"unlimited\n"`)
	if cg := l.h.Cgroup(); cg != "" {
		l.call("forks")
		if events(cg, "max") == 0 {
			t.Error("the task limit was never hit")
		}
		l.expect("after the forks", "fine", `1`)
	}
	l.expect("memory without end", "memory", "!host is gone")
}

func TestFloodsAreCut(t *testing.T) {
	w := newWorld(t)
	l := w.must("", `
spot.on("flood", function()
  for i = 1, 100000 do spot.log("x") end
  for i = 1, 3000 do pcall(function() spot.read("/etc/hostname") end) end
end)`)
	r, err := l.h.Call("flood", nil)
	if err != nil || len(r.Log) != 200 || len(r.Events) != 501 || r.Events[500].Kind != "dropped" {
		t.Errorf("%v: %d log lines, %d events", err, len(r.Log), len(r.Events))
	}
}
