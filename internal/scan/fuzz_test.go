package scan

import "testing"

// Whatever a file holds, reading it must end and not crash.
func FuzzScan(f *testing.F) {
	for _, s := range []string{`spot.run({"ss"})`, `local x = [==[ a ]==] .. "\27"`, `function a.b:c(...) return ... end`, `x = { [1] = -2, y = function() end }`, "for i=1,2 do break end", `local a <const> = 1`, "\x00\xff", `--[[`, `x = 0x`, `return f"x"{y}`} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, src []byte) { Mod(map[string][]byte{"init.lua": src}, nil) })
}
