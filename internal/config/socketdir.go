package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

// sharedSocketDir is the folder used for the socket when there is no runtime
// directory: our own, inside the temporary directory everybody can write to.
func sharedSocketDir() string {
	return filepath.Join(os.TempDir(), "spot-"+strconv.Itoa(os.Getuid()))
}

// PrepareSocket makes sure the socket may be used. For the fallback in the
// shared temporary directory it creates the private folder and checks that it
// really is ours: someone else may have made it first. Other paths (the runtime
// directory, SPOT_SOCKET, daemon.toml) are the user's own business; the peer is
// checked on every connection anyway.
func PrepareSocket(sock string) error {
	dir := sharedSocketDir()
	if filepath.Dir(sock) != dir {
		return nil
	}
	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	return checkPrivateDir(dir, os.Getuid())
}

// checkPrivateDir: a real directory (not a link to one), owned by uid, closed to
// everybody else.
func checkPrivateDir(dir string, uid int) error {
	st, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	switch {
	case !st.IsDir():
		return fmt.Errorf("%s is not a directory", dir)
	case !ok || int(sys.Uid) != uid:
		return fmt.Errorf("%s belongs to another user", dir)
	case st.Mode().Perm()&0o077 != 0:
		return fmt.Errorf("%s is open to other users (mode %04o, want 0700)", dir, st.Mode().Perm())
	}
	return nil
}
