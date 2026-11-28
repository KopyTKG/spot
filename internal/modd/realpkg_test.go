//go:build realpkg

package modd

import (
	"os/exec"
	"testing"
)

// S43 with a real package update: a program the mod was granted is replaced by
// pacman, and the next request gets a new host, whose grant is the new file.
//
// Only in a throwaway Arch system: it reinstalls the package "tree" with sudo.
//
//	CGO_ENABLED=0 go test -c -tags realpkg -o modd.test ./internal/modd
//	SPOT_TEST_HOST=... ./modd.test -test.run RealPackage -test.v
func TestRealPackageUpdate(t *testing.T) {
	f := newFixture(t)
	f.write("mods/m/mod.toml", "[permissions.run]\ntree='r'\n")
	f.write("mods/m/init.lua", `local n = 0
spot.value("count", function() n = n + 1 return n end)
spot.value("version", function()
  local r = spot.run({ "tree", "--version" })
  if r.status ~= 0 then error("tree: " .. r.status .. " " .. r.stderr) end
  return r.stdout
end)`)
	f.approve()
	f.enable("")
	before, ok := identify("/usr/bin/tree")
	if !ok {
		t.Fatal("tree is not installed")
	}
	f.get("before", "count", "1")
	f.get("same host", "count", "2")
	if _, _, err := f.Get("m.version"); err != nil {
		t.Fatal("running it before:", err)
	}
	out, err := exec.Command("sudo", "-n", "pacman", "-S", "--noconfirm", "tree").CombinedOutput()
	if err != nil {
		t.Fatalf("pacman: %v\n%s", err, out)
	}
	t.Logf("%s", out)
	after, ok := identify("/usr/bin/tree")
	if !ok || after == before {
		t.Fatalf("pacman did not replace the file (%v, %v): this proves nothing", before, after)
	}
	f.get("a new host", "count", "1")
	if _, _, err := f.Get("m.version"); err != nil {
		t.Fatal("running the new program:", err)
	}
}
