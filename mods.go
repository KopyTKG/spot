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

const modsUsage = `usage: spot mods                       every mod and whether it is approved
       spot mods allow <name>          read what it asks for, and approve it
       spot mods show <name>           its permissions and all its code
       spot mods diff <name>           what changed since you approved it
       spot mods revoke <name>         take the approval back
       spot mods audit <name>          what it ran, read, wrote and was refused
       spot mods reload [name]         start it anew
       spot mods check <name|folder> [-]
                                       while writing one: read its code and, for
                                       a parser, hold it to its contract (with -,
                                       also parse what comes on stdin)
`

// modsCommand is `spot mods ...`. It returns the exit status.
func modsCommand(args []string) int {
	fail := func(format string, a ...any) int {
		fmt.Fprintf(os.Stderr, "spot: "+format+"\n", a...)
		return protocol.ExitError
	}
	usage := func() int {
		fmt.Fprint(os.Stderr, modsUsage)
		return protocol.ExitUsage
	}
	if len(args) == 0 {
		return listMods()
	}
	verb, args := args[0], args[1:]
	switch verb {
	case "-h", "--help", "help":
		fmt.Print(modsUsage)
		return 0
	case "list":
		if len(args) != 0 {
			return usage()
		}
		return listMods()
	case "allow", "show", "diff", "revoke", "audit":
		if len(args) != 1 {
			return usage()
		}
		s, err := modStore()
		if err != nil {
			return fail("%v", err)
		}
		name := strings.TrimSuffix(args[0], "/")
		if !mod.ValidName(name) {
			return fail("%q is not a mod's name: give the name of a folder in %s, not a path", args[0], s.Mods)
		}
		switch verb {
		case "allow":
			return allowMod(s, name)
		case "show":
			return showMod(s, name)
		case "diff":
			return diffMod(s, name)
		case "audit":
			return auditMod(s, name)
		}
		had, err := s.Revoke(name)
		reloadMods(name) // stops it now
		if err != nil {
			return fail("%v", err)
		}
		if !had {
			return fail("%s was not approved", name)
		}
		fmt.Printf("%s is no longer approved\n", name)
		return 0
	case "reload":
		if len(args) > 1 {
			return usage()
		}
		if !reloadMods(strings.Join(args, "")) {
			fmt.Println("no daemon is running: mods start when they are asked for")
		}
		return 0
	case "check":
	default:
		return usage()
	}
	if len(args) < 1 || len(args) > 2 || len(args) == 2 && args[1] != "-" {
		return usage()
	}
	dir, err := modFolder(args[0])
	if err != nil {
		return fail("%v", err)
	}
	var live []byte
	// Piped input without "-" is easy to take for tested.
	if st, err := os.Stdin.Stat(); err == nil && len(args) == 1 && st.Mode()&os.ModeNamedPipe != 0 {
		fmt.Fprintln(os.Stderr, "spot: what is piped in is not read; add - to parse it too")
	}
	if len(args) == 2 {
		if live, err = io.ReadAll(io.LimitReader(os.Stdin, 5<<20)); err != nil {
			return fail("%v", err)
		}
	}
	host, err := modhost.HostPath()
	if err != nil {
		return fail("%v", err)
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
	store, err := modStore()
	if err != nil {
		return "", err
	}
	installed := filepath.Join(store.Mods, arg)
	if st, err := os.Stat(installed); err != nil || !st.IsDir() {
		return "", fmt.Errorf("there is no folder %s here, and no mod of that name in %s", arg, filepath.Dir(installed))
	}
	return installed, nil
}
