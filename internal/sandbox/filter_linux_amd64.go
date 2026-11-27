package sandbox

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

// A tiny assembler for the classic BPF a seccomp filter is written in: jumps
// name labels instead of counting instructions.
type asm struct {
	code   []unix.SockFilter
	labels map[string]int
	jumps  []struct {
		at     int
		yes    string
		ifTrue bool
	}
}

func (a *asm) op(code uint16, k uint32) {
	a.code = append(a.code, unix.SockFilter{Code: code, K: k})
}
func (a *asm) load(offset uint32) { a.op(unix.BPF_LD|unix.BPF_W|unix.BPF_ABS, offset) }
func (a *asm) and(k uint32)       { a.op(unix.BPF_ALU|unix.BPF_AND|unix.BPF_K, k) }
func (a *asm) ret(v uint32)       { a.op(unix.BPF_RET|unix.BPF_K, v) }
func (a *asm) label(name string)  { a.labels[name] = len(a.code) }

// jump goes to label when the test holds (or, with ifTrue false, when it does
// not) and falls through otherwise.
func (a *asm) jump(test uint16, k uint32, label string, ifTrue bool) {
	a.jumps = append(a.jumps, struct {
		at     int
		yes    string
		ifTrue bool
	}{len(a.code), label, ifTrue})
	a.op(unix.BPF_JMP|test|unix.BPF_K, k)
}
func (a *asm) when(k uint32, label string)   { a.jump(unix.BPF_JEQ, k, label, true) }
func (a *asm) unless(k uint32, label string) { a.jump(unix.BPF_JEQ, k, label, false) }

func (a *asm) assemble() ([]unix.SockFilter, error) {
	for _, j := range a.jumps {
		to, ok := a.labels[j.yes]
		d := to - j.at - 1
		if !ok || d < 0 || d > 255 {
			return nil, fmt.Errorf("filter: cannot jump to %q", j.yes)
		}
		if j.ifTrue {
			a.code[j.at].Jt = uint8(d)
		} else {
			a.code[j.at].Jf = uint8(d)
		}
	}
	return a.code, nil
}

const (
	retAllow  = 0x7fff0000
	retKill   = 0x80000000
	retErrno  = 0x00050000
	atSysNo   = 0
	atArch    = 4
	atArg0    = 16
	atArg1    = 24
	atArg2    = 32
	archX8664 = 0xC000003E
	x32Bit    = 0x40000000
)

// denied are calls Landlock does not cover and no mod needs.
var denied = []uint32{
	// modes, owners, extended attributes
	unix.SYS_CHMOD, unix.SYS_FCHMOD, unix.SYS_FCHMODAT, unix.SYS_FCHMODAT2,
	unix.SYS_CHOWN, unix.SYS_FCHOWN, unix.SYS_LCHOWN, unix.SYS_FCHOWNAT,
	unix.SYS_SETXATTR, unix.SYS_LSETXATTR, unix.SYS_FSETXATTR,
	unix.SYS_REMOVEXATTR, unix.SYS_LREMOVEXATTR, unix.SYS_FREMOVEXATTR,
	463, 466, // setxattrat, removexattrat
	// shared memory and queues, SysV and POSIX
	unix.SYS_SHMGET, unix.SYS_SHMAT, unix.SYS_SHMCTL, unix.SYS_SHMDT,
	unix.SYS_SEMGET, unix.SYS_SEMOP, unix.SYS_SEMCTL, unix.SYS_SEMTIMEDOP,
	unix.SYS_MSGGET, unix.SYS_MSGSND, unix.SYS_MSGRCV, unix.SYS_MSGCTL,
	unix.SYS_MQ_OPEN, unix.SYS_MQ_UNLINK, unix.SYS_MQ_TIMEDSEND, unix.SYS_MQ_TIMEDRECEIVE,
	unix.SYS_MQ_NOTIFY, unix.SYS_MQ_GETSETATTR,
	// keyrings
	unix.SYS_ADD_KEY, unix.SYS_REQUEST_KEY, unix.SYS_KEYCTL,
	// programs from memory
	unix.SYS_MEMFD_CREATE, unix.SYS_MEMFD_SECRET, unix.SYS_EXECVEAT,
	// namespaces and mounts
	unix.SYS_UNSHARE, unix.SYS_SETNS, unix.SYS_MOUNT, unix.SYS_UMOUNT2, unix.SYS_PIVOT_ROOT, unix.SYS_CHROOT,
	unix.SYS_OPEN_TREE, unix.SYS_MOVE_MOUNT, unix.SYS_FSOPEN, unix.SYS_FSCONFIG, unix.SYS_FSMOUNT,
	unix.SYS_FSPICK, unix.SYS_MOUNT_SETATTR,
	// other processes, and ways past paths
	unix.SYS_PTRACE, unix.SYS_PROCESS_VM_READV, unix.SYS_PROCESS_VM_WRITEV, unix.SYS_PIDFD_GETFD,
	unix.SYS_KCMP, unix.SYS_OPEN_BY_HANDLE_AT,
	// large kernel surface a mod has no use for
	unix.SYS_IO_URING_SETUP, unix.SYS_IO_URING_ENTER, unix.SYS_IO_URING_REGISTER,
	unix.SYS_BPF, unix.SYS_USERFAULTFD, unix.SYS_PERF_EVENT_OPEN,
}

func filter(p Policy) ([]unix.SockFilter, error) {
	a := &asm{labels: map[string]int{}}
	net := len(p.Ports) > 0
	errno := func(e unix.Errno) uint32 { return retErrno | uint32(e) }

	a.load(atArch)
	a.when(archX8664, "native")
	a.ret(retKill)
	a.label("native")
	a.load(atSysNo)
	a.jump(unix.BPF_JGE, x32Bit, "x32", true)

	// Each test is followed by its own answer, so no jump grows long.
	answer := func(nr uint32, with uint32) {
		a.code = append(a.code,
			unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: nr, Jf: 1},
			unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: with})
	}
	for _, nr := range denied {
		answer(nr, errno(unix.EPERM))
	}
	// Landlock rules bind, not listen on a socket that was never bound.
	if net {
		answer(unix.SYS_LISTEN, errno(unix.EPERM))
	}
	// "Not implemented" makes callers fall back to what can be checked.
	answer(unix.SYS_CLONE3, errno(unix.ENOSYS))
	answer(unix.SYS_PIDFD_OPEN, errno(unix.ENOSYS))

	a.when(unix.SYS_CLONE, "clone")
	a.when(unix.SYS_SOCKET, "socket")
	a.when(unix.SYS_SOCKETPAIR, "socket")
	a.ret(retAllow)

	a.label("x32")
	a.ret(retKill)

	a.label("clone")
	a.load(atArg0)
	a.jump(unix.BPF_JSET, unix.CLONE_NEWUSER, "newuser", true)
	a.ret(retAllow)
	a.label("newuser")
	a.ret(errno(unix.EPERM))

	// Sockets by list; everything else is refused.
	a.label("socket")
	a.load(atArg0)
	a.when(unix.AF_UNIX, "allow") // ruled by Landlock
	if len(p.Netlink) > 0 {
		a.when(unix.AF_NETLINK, "netlink")
	}
	if net {
		a.when(unix.AF_INET, "inet")
		a.when(unix.AF_INET6, "inet")
	}
	a.ret(errno(unix.EACCES))
	if len(p.Netlink) > 0 {
		a.label("netlink")
		a.load(atArg2)
		for _, name := range p.Netlink {
			a.when(Netlink[name], "allow")
		}
		a.ret(errno(unix.EACCES))
	}
	if net {
		a.label("inet")
		a.load(atArg1)
		a.and(0xf) // without SOCK_NONBLOCK and SOCK_CLOEXEC
		a.when(unix.SOCK_STREAM, "stream")
		a.unless(unix.SOCK_DGRAM, "noinet")
		a.load(atArg2)
		a.when(0, "allow") // plain UDP
		a.label("noinet")
		a.ret(errno(unix.EACCES))
		a.label("stream")
		a.load(atArg2)
		a.when(0, "allow")
		a.when(unix.IPPROTO_TCP, "allow") // not Multipath TCP
		a.ret(errno(unix.EACCES))
	}
	a.label("allow")
	a.ret(retAllow)
	return a.assemble()
}

func installFilter(p Policy) error {
	code, err := filter(p)
	if err != nil {
		return err
	}
	prog := unix.SockFprog{Len: uint16(len(code)), Filter: &code[0]}
	// On all threads (TSYNC), besides the exec that follows.
	r, _, errno := unix.Syscall(unix.SYS_SECCOMP, 1 /* SET_MODE_FILTER */, 1 /* TSYNC */, uintptr(unsafe.Pointer(&prog)))
	if errno != 0 {
		return fmt.Errorf("seccomp: %w", errno)
	}
	if r != 0 {
		return fmt.Errorf("seccomp: thread %d could not take the filter", r)
	}
	return nil
}
