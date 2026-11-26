//go:build gui

package main

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// collect runs the garbage collector and the main loop until finalizers are done:
// freeing a widget tree takes a round of each per level.
func collect() {
	for i := 0; i < 30; i++ {
		runtime.GC()
		time.Sleep(2 * time.Millisecond)
		for glib.MainContextDefault().Iteration(false) {
		}
	}
}

func residentKB(t *testing.T) int {
	t.Helper()
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		t.Skip(err)
	}
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "VmRSS:") {
			n, _ := strconv.Atoi(strings.Fields(l)[1])
			return n
		}
	}
	t.Skip("no VmRSS")
	return 0
}

// The daemon builds a dialog per request and lives for days: a closed one must
// leave nothing behind, neither its widgets nor its list rows.
func TestClosedDialogsAreFreed(t *testing.T) {
	const cfg = `
[[items]]
type  = "list"
id    = "c"
stdin = true
[[items]]
type = "tiles"
id   = "t"
[[items.tiles]]
label = "a"
value = "a"
[[items]]
type     = "field"
id       = "f"
required = true
[submit]
stdout = "{c}"
`
	gtk.Init()
	rows := make([]string, 500)
	for i := range rows {
		rows[i] = "row " + strconv.Itoa(i)
	}
	build := func() *coreglib.WeakRef[*gtk.Box] {
		c, err := parseConfig([]byte(cfg), func(string) (string, bool) { return "", false })
		if err != nil {
			t.Fatal(err)
		}
		c.Items[0].Options = rows
		d := newDialog(c, nil, t.TempDir(), &backSpec{label: "Back"})
		ref := coreglib.NewWeakRef(d.root)
		d.release()
		return ref
	}
	round := func(n int) (alive int) {
		var refs []*coreglib.WeakRef[*gtk.Box]
		for i := 0; i < n; i++ {
			refs = append(refs, build())
			if i%10 == 9 {
				collect()
			}
		}
		collect()
		for _, r := range refs {
			if r.Get() != nil {
				alive++
			}
		}
		return alive
	}
	round(100) // warm up: allocator arenas, type registrations, caches
	before := residentKB(t)
	if alive := round(200); alive != 0 {
		t.Errorf("%d of 200 closed dialogs are still alive", alive)
	}
	// Leaking only the rows cost about 800 KB a dialog.
	if grown := residentKB(t) - before; grown > 40*1024 {
		t.Errorf("memory grew %d KB over 200 dialogs", grown)
	}
}
