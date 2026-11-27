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

  check   hold a parser to its contract: load it, run its examples and empty and
          junk input, in a sandbox. With -, also parse what comes on stdin.
`

// modsCommand is `spot mods ...`. It returns the exit status.
func modsCommand(args []string) int {
	if len(args) < 2 || args[0] != "check" || len(args) > 3 || len(args) == 3 && args[2] != "-" {
		fmt.Fprint(os.Stderr, modsUsage)
		return protocol.ExitUsage
	}
	dir := args[1]
	if !strings.ContainsRune(dir, '/') && mod.ValidName(dir) {
		base, err := os.UserConfigDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "spot: %v\n", err)
			return protocol.ExitError
		}
		dir = filepath.Join(base, "spot", "mods", dir)
	}
	dir, err := filepath.Abs(dir)
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
