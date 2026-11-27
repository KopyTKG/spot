package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gitlab.com/KopyTKG/spot/internal/mod"
	"gitlab.com/KopyTKG/spot/internal/modcheck"
	"gitlab.com/KopyTKG/spot/internal/modhost"
	"gitlab.com/KopyTKG/spot/internal/protocol"
)

const modsUsage = `usage: spot mods check <name|folder> [-]

  check   read a mod's code and point at what deserves a look. A parser is then
          held to its contract: loaded, run on its examples and on empty and junk
          input, in a sandbox. With -, it also parses what comes on stdin.
`

// modsCommand is `spot mods ...`. It returns the exit status.
func modsCommand(args []string) int {
	if len(args) < 2 || args[0] != "check" || len(args) > 3 || len(args) == 3 && args[2] != "-" {
		fmt.Fprint(os.Stderr, modsUsage)
		return protocol.ExitUsage
	}
	dir, err := modFolder(args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "spot: %v\n", err)
		return protocol.ExitError
	}
	var live []byte
	// Piped input without "-" is easy to take for tested.
	if st, err := os.Stdin.Stat(); err == nil && len(args) == 2 && st.Mode()&os.ModeNamedPipe != 0 {
		fmt.Fprintln(os.Stderr, "spot: what is piped in is not read; add - to parse it too")
	}
	if len(args) == 3 {
		if live, err = io.ReadAll(io.LimitReader(os.Stdin, 5<<20)); err != nil {
			fmt.Fprintf(os.Stderr, "spot: %v\n", err)
			return protocol.ExitError
		}
	}
	host, err := modhost.HostPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "spot: %v\n", err)
		return protocol.ExitError
	}
	if !modcheck.Check(host, dir, live, os.Stdout) {
		return protocol.ExitError
	}
	return 0
}

// modFolder is the folder meant by arg: a folder that is there, or the name of
// an installed mod.
func modFolder(arg string) (string, error) {
	st, err := os.Stat(arg)
	switch {
	case err == nil && st.IsDir():
		return filepath.Abs(arg)
	case err == nil:
		return "", fmt.Errorf("%s is a file: give the folder of the mod, the one with init.lua in it", arg)
	case strings.ContainsRune(arg, '/') || !mod.ValidName(arg):
		return "", fmt.Errorf("there is no folder %s", arg)
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	installed := filepath.Join(base, "spot", "mods", arg)
	if st, err := os.Stat(installed); err != nil || !st.IsDir() {
		return "", fmt.Errorf("there is no folder %s here, and no mod of that name in %s", arg, filepath.Dir(installed))
	}
	return installed, nil
}
