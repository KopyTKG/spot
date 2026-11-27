package provider

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/sys/unix"

	"gitlab.com/KopyTKG/spot/internal/modperm"
)

// The kernel already confines the host. These checks add what it cannot: no
// hard links, no pipes or devices, nothing of a process, and errors that say
// what was refused.

const maxFile = 8 << 20

var errRefused = errors.New("no permission")

func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
}

var ofProcess = regexp.MustCompile(`^/proc/(self|thread-self|[0-9]+)(/|$)`)

// real is where asked leads, if the mod may have it: under one of dirs both as
// asked and after every link.
func (r *Runtime) real(asked string, dirs []string) (string, error) {
	p, err := modperm.Expand(asked, r.grants.Vars)
	if err != nil {
		return "", err
	}
	// The named exceptions lead into the host's own process folder.
	own := p == "/proc/mounts" || within(p, "/proc/net")
	if within(p, "/dev") || ofProcess.MatchString(p) {
		return "", errRefused
	}
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		// Reaching outside the grants is refused and recorded, also for what
		// is not there; "not found" is only said of what the mod may read.
		if !slices.ContainsFunc(dirs, func(d string) bool { return within(p, d) }) {
			return "", errRefused
		}
		return "", err
	}
	if within(real, "/dev") || ofProcess.MatchString(real) && !own {
		return "", errRefused
	}
	for _, d := range dirs {
		rd, err := filepath.EvalSymlinks(d)
		if err != nil {
			continue
		}
		if within(real, rd) {
			return real, nil
		}
		// Entries of a /sys class folder are links to devices elsewhere.
		if within(rd, "/sys") && within(p, d) && within(real, "/sys") {
			return real, nil
		}
	}
	return "", errRefused
}

// open opens a resolved path without following anything, so that a link swapped
// in since the check makes it fail.
func open(real string, flags int) (*os.File, error) {
	fd, err := unix.Openat2(unix.AT_FDCWD, real, &unix.OpenHow{Flags: uint64(flags | unix.O_CLOEXEC), Resolve: unix.RESOLVE_NO_SYMLINKS})
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), real), nil
}

// linkedElsewhere reports a file that has another name somewhere: it may be a
// file outside the grants. Files of the system that we cannot write are fine.
func linkedElsewhere(st *unix.Stat_t) bool {
	return st.Nlink > 1 && (st.Uid != 0 || st.Mode&0o022 != 0 || os.Getuid() == 0)
}

func (r *Runtime) readFile(asked string) ([]byte, error) {
	real, err := r.real(asked, r.grants.Read)
	if err != nil {
		return nil, err
	}
	f, err := open(real, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOCTTY)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var st unix.Stat_t
	if err := unix.Fstat(int(f.Fd()), &st); err != nil {
		return nil, err
	}
	switch {
	case st.Mode&unix.S_IFMT != unix.S_IFREG:
		return nil, errors.New("not a plain file")
	case linkedElsewhere(&st):
		return nil, errors.New("the file has more than one name")
	}
	// Files under /proc say they are empty: the limit is on what is read.
	b, err := io.ReadAll(io.LimitReader(f, maxFile+1))
	if err == nil && len(b) > maxFile {
		err = fmt.Errorf("larger than %d bytes", maxFile)
	}
	return b, err
}

// writeFile puts a new file in place of the old one, never truncating first.
func (r *Runtime) writeFile(asked string, data []byte) error {
	p, err := modperm.Expand(asked, r.grants.Vars)
	if err != nil {
		return err
	}
	dir, name := filepath.Split(p)
	real, err := r.real(dir, r.grants.Write)
	if err != nil {
		return err
	}
	if within(real, "/proc") || within(real, "/sys") || name == "" || name == "." || name == ".." {
		return errRefused
	}
	if len(data) > maxFile {
		return fmt.Errorf("larger than %d bytes", maxFile)
	}
	d, err := open(real, unix.O_PATH|unix.O_DIRECTORY)
	if err != nil {
		return err
	}
	defer d.Close()
	dfd := int(d.Fd())
	mode := uint32(0o600)
	var st unix.Stat_t
	switch err := unix.Fstatat(dfd, name, &st, unix.AT_SYMLINK_NOFOLLOW); {
	case errors.Is(err, unix.ENOENT):
	case err != nil:
		return err
	case st.Mode&unix.S_IFMT != unix.S_IFREG:
		return errors.New("what is there is not a plain file")
	case st.Nlink > 1:
		return errors.New("the file has more than one name")
	default:
		mode = st.Mode & 0o777
	}
	tmp := fmt.Sprintf(".%s.spot-%d", name, r.next())
	fd, err := unix.Openat(dfd, tmp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, mode)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), tmp)
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = unix.Renameat(dfd, tmp, dfd, name)
	}
	if err != nil {
		_ = unix.Unlinkat(dfd, tmp, 0)
	}
	return err
}

func (r *Runtime) listDir(asked string) ([]string, error) {
	real, err := r.real(asked, r.grants.Read)
	if err != nil {
		return nil, err
	}
	f, err := open(real, unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries, err := f.ReadDir(10_001)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(entries) > 10_000 {
		return nil, errors.New("more than 10000 entries")
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	return names, nil
}

// kindOf is "file", "dir" or "" for nothing there. The kernel lets anyone ask,
// so the permission is checked here.
func (r *Runtime) kindOf(asked string) (string, error) {
	real, err := r.real(asked, r.grants.Read)
	if errors.Is(err, fs.ErrNotExist) {
		p, perr := modperm.Expand(asked, r.grants.Vars)
		if perr != nil {
			return "", perr
		}
		if _, perr = r.real(filepath.Dir(p), r.grants.Read); perr != nil {
			return "", errRefused
		}
		return "", nil
	}
	if err != nil {
		return "", err
	}
	st, err := os.Lstat(real)
	if err != nil {
		return "", nil
	}
	if st.IsDir() {
		return "dir", nil
	}
	return "file", nil
}
