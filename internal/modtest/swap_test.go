package modtest

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"gitlab.com/KopyTKG/spot/internal/modhost"
	"gitlab.com/KopyTKG/spot/internal/modload"
	"gitlab.com/KopyTKG/spot/internal/sandbox"
)

// A script of your own changed after the mod was loaded is not run (S10).
func TestAChangedScriptIsNotRun(t *testing.T) {
	w := newWorld(t)
	script := w.dir + "/bin/mine"
	w.file("bin/mine", "#!/usr/bin/sh\necho approved\n")
	os.Chmod(script, 0o700)
	w.file(".config/spot/mods/m/mod.toml", "[permissions.run]\n\""+script+"\"='mine'\nsh='its interpreter'\n")
	w.file(".config/spot/mods/m/init.lua", `spot.on("run", function(a) return spot.run({ "`+script+`" }).stdout end)`)
	c, l, err := modload.Provider(w.conf+"/spot/mods/m", modload.Options{Host: hostBin, Env: func(k string) string { return w.env[k] },
		Protected: sandbox.Protected{}, EmptyDir: w.dir + "/empty"})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("#!/usr/bin/sh\necho approved\n"))
	l.Approved = map[string]string{script: hex.EncodeToString(sum[:])}
	h, err := modhost.Start(c)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Stop()
	if _, err := h.Load(l); err != nil {
		t.Fatal(err)
	}
	if r, err := h.Call("run", nil); err != nil || !strings.Contains(string(r.Result), "approved") {
		t.Fatalf("as approved: %s %v", r.Result, err)
	}
	// Changed in place, so that the kernel's grant still holds.
	if err := os.WriteFile(script, []byte("#!/usr/bin/sh\necho swapped\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	r, err := h.Call("run", nil)
	if err == nil || !strings.Contains(err.Error(), "has changed since you approved") {
		t.Errorf("changed: %s %v", r.Result, err)
	}
}
