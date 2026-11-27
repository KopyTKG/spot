package sandbox

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Landlock access bits (linux/landlock.h).
const (
	fsExecute = 1 << iota
	fsWriteFile
	fsReadFile
	fsReadDir
	fsRemoveDir
	fsRemoveFile
	fsMakeChar
	fsMakeDir
	fsMakeReg
	fsMakeSock
	fsMakeFifo
	fsMakeBlock
	fsMakeSym
	fsRefer
	fsTruncate
	fsIoctlDev
	fsResolveUnix

	fsAll      = 1<<17 - 1
	fsOfAFile  = fsExecute | fsWriteFile | fsReadFile | fsTruncate | fsIoctlDev | fsResolveUnix
	fsWriteSet = fsWriteFile | fsRemoveDir | fsRemoveFile | fsMakeDir | fsMakeReg | fsMakeSock |
		fsMakeFifo | fsMakeSym | fsRefer | fsTruncate

	netBindTCP    = 1
	netConnectTCP = 2
	netBindUDP    = 4
	netSendUDP    = 8
	netAll        = 0xf

	scopeAll = 3 // abstract Unix sockets and signals
)

type rulesetAttr struct{ fs, net, scoped uint64 }
type pathAttr struct {
	access uint64
	fd     int32
} // packed in C: the kernel reads the first 12 bytes
type netAttr struct{ access, port uint64 }

// ABI is the Landlock version of this kernel, 0 when it has none.
func ABI() int {
	v, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, 1)
	if errno != 0 {
		return 0
	}
	return int(v)
}

func access(a Access) (bits uint64) {
	if a&Exec != 0 {
		bits |= fsExecute | fsReadFile
	}
	if a&Read != 0 {
		bits |= fsReadFile | fsReadDir
	}
	if a&Write != 0 {
		bits |= fsWriteSet
	}
	if a&Connect != 0 {
		bits |= fsResolveUnix
	}
	return bits
}

func ruleset(p Policy) (int, error) {
	if abi := ABI(); abi < MinABI {
		return -1, fmt.Errorf("mods need Landlock ABI %d or newer, this kernel has %d", MinABI, abi)
	}
	rules, err := p.Rules()
	if err != nil {
		return -1, err
	}
	attr := rulesetAttr{fsAll, netAll, scopeAll}
	r, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, uintptr(unsafe.Pointer(&attr)), unsafe.Sizeof(attr), 0)
	if errno != 0 {
		return -1, fmt.Errorf("landlock: %w", errno)
	}
	fd := int(r)
	for _, rule := range rules {
		if err := addPath(fd, rule); err != nil {
			unix.Close(fd)
			return -1, err
		}
	}
	addPort := func(access, port uint64) error {
		na := netAttr{access, port}
		_, _, errno := unix.Syscall6(unix.SYS_LANDLOCK_ADD_RULE, uintptr(fd), 2, uintptr(unsafe.Pointer(&na)), 0, 0, 0)
		if errno != 0 {
			unix.Close(fd)
			return fmt.Errorf("landlock: port %d: %w", port, errno)
		}
		return nil
	}
	for _, port := range p.Ports {
		if err := addPort(netConnectTCP, uint64(port)); err != nil {
			return -1, err
		}
	}
	if len(p.Ports) > 0 { // name lookups: UDP to port 53, from any port
		if err := addPort(netSendUDP, 53); err != nil {
			return -1, err
		}
		if err := addPort(netBindUDP, 0); err != nil {
			return -1, err
		}
	}
	return fd, nil
}

func addPath(ruleset int, rule Rule) error {
	// Resolve, then open without following anything: a link swapped in between
	// makes this fail instead of granting something else.
	real, err := resolve(rule.Path)
	if err != nil {
		return fmt.Errorf("%s: %w", rule.Path, err)
	}
	f, err := unix.Openat2(unix.AT_FDCWD, real, &unix.OpenHow{Flags: unix.O_PATH | unix.O_CLOEXEC, Resolve: unix.RESOLVE_NO_SYMLINKS})
	if errors.Is(err, unix.ENOENT) && rule.Optional {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%s: %w", rule.Path, err)
	}
	defer unix.Close(f)
	var st unix.Stat_t
	if err := unix.Fstat(f, &st); err != nil {
		return fmt.Errorf("%s: %w", rule.Path, err)
	}
	bits := access(rule.Access)
	if st.Mode&unix.S_IFMT != unix.S_IFDIR {
		bits &= fsOfAFile
	}
	if bits == 0 {
		return nil
	}
	pa := pathAttr{bits, int32(f)}
	if _, _, errno := unix.Syscall6(unix.SYS_LANDLOCK_ADD_RULE, uintptr(ruleset), 1, uintptr(unsafe.Pointer(&pa)), 0, 0, 0); errno != 0 {
		return fmt.Errorf("landlock: %s: %w", rule.Path, errno)
	}
	return nil
}

// Restrict puts the calling thread into the sandbox, for good. Landlock binds
// one thread, and a Go program has many: Restrict locks the goroutine to its
// thread and the caller must execute a program on that same goroutine, which
// then has the restrictions as a whole. Nothing else may be done in between.
func Restrict(p Policy) error {
	runtime.LockOSThread()
	fd, err := ruleset(p)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("no_new_privs: %w", err)
	}
	if _, _, errno := unix.Syscall(unix.SYS_LANDLOCK_RESTRICT_SELF, uintptr(fd), 0, 0); errno != 0 {
		return fmt.Errorf("landlock: %w", errno)
	}
	return installFilter(p)
}

// Verify reports an error unless the process is restricted. It does not depend
// on what was granted: the filter never allows a VM socket, and Landlock never
// allows a signal to a process outside, here the parent.
func Verify() error {
	nnp, err := unix.PrctlRetInt(unix.PR_GET_NO_NEW_PRIVS, 0, 0, 0, 0)
	if err != nil || nnp != 1 {
		return errors.New("no_new_privs is not set")
	}
	if fd, err := unix.Socket(unix.AF_VSOCK, unix.SOCK_STREAM, 0); err == nil {
		unix.Close(fd)
		return errors.New("the filter is missing")
	}
	parent := os.Getppid()
	if parent <= 1 {
		return errors.New("no parent to test against")
	}
	if err := unix.Kill(parent, 0); !errors.Is(err, unix.EPERM) {
		return errors.New("Landlock is missing")
	}
	return nil
}
