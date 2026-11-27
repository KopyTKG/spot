package sandbox

// Every call the design says is refused, by its x86_64 number, written out here
// and not taken from the filter's own list: an entry missing there must fail.

import (
	"encoding/json"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

var mustBeRefused = map[string]uintptr{
	"chmod": 90, "fchmod": 91, "fchmodat": 268, "fchmodat2": 452,
	"chown": 92, "fchown": 93, "lchown": 94, "fchownat": 260,
	"setxattr": 188, "lsetxattr": 189, "fsetxattr": 190, "removexattr": 197, "lremovexattr": 198, "fremovexattr": 199,
	"setxattrat": 463, "removexattrat": 466,
	"shmget": 29, "shmat": 30, "shmctl": 31, "shmdt": 67, "semget": 64, "semop": 65, "semctl": 66, "semtimedop": 220,
	"msgget": 68, "msgsnd": 69, "msgrcv": 70, "msgctl": 71,
	"mq_open": 240, "mq_unlink": 241, "mq_timedsend": 242, "mq_timedreceive": 243, "mq_notify": 244, "mq_getsetattr": 245,
	"add_key": 248, "request_key": 249, "keyctl": 250,
	"memfd_create": 319, "memfd_secret": 447, "execveat": 322,
	"unshare": 272, "setns": 308, "mount": 165, "umount2": 166, "pivot_root": 155, "chroot": 161,
	"open_tree": 428, "move_mount": 429, "fsopen": 430, "fsconfig": 431, "fsmount": 432, "fspick": 433, "mount_setattr": 442,
	"ptrace": 101, "process_vm_readv": 310, "process_vm_writev": 311, "pidfd_getfd": 438, "kcmp": 312, "open_by_handle_at": 304,
	"io_uring_setup": 425, "io_uring_enter": 426, "io_uring_register": 427,
	"bpf": 321, "userfaultfd": 323, "perf_event_open": 298,
}

// "Not implemented", so that callers fall back to what the filter can read.
var mustLookMissing = map[string]uintptr{"clone3": 435, "pidfd_open": 434}

func init() {
	switch os.Getenv("SPOT_PROBE") {
	case "calls":
		got := map[string]int{}
		for _, set := range []map[string]uintptr{mustBeRefused, mustLookMissing} {
			for name, nr := range set {
				// All zero: harmless should one ever get through.
				_, _, errno := unix.Syscall6(nr, 0, 0, 0, 0, 0, 0)
				got[name] = int(errno)
			}
		}
		// The socket type carries flags that must not fool the filter.
		for name, typ := range map[string]int{"socket: stream with flags": unix.SOCK_STREAM | unix.SOCK_NONBLOCK | unix.SOCK_CLOEXEC, "socket: raw with flags": unix.SOCK_RAW | unix.SOCK_CLOEXEC} {
			fd, err := unix.Socket(unix.AF_INET, typ, 0)
			got[name] = 0
			if err != nil {
				got[name] = int(err.(syscall.Errno))
			} else {
				unix.Close(fd)
			}
		}
		fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW, 9) // audit: never grantable
		got["netlink: audit"] = 0
		if err != nil {
			got["netlink: audit"] = int(err.(syscall.Errno))
		} else {
			unix.Close(fd)
		}
		// UDP: port 53 only.
		for name, port := range map[string]int{"udp to 53": 53, "udp to 5353": 5353} {
			s, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM, 0)
			if err == nil {
				err = unix.Connect(s, &unix.SockaddrInet4{Port: port, Addr: [4]byte{127, 0, 0, 1}})
				unix.Close(s)
			}
			got[name] = 0
			if err != nil {
				got[name] = int(err.(syscall.Errno))
			}
		}
		_, rerr := os.ReadFile("/etc/resolv.conf")
		got["read resolv.conf"] = 0
		if rerr != nil {
			got["read resolv.conf"] = 1
		}
		json.NewEncoder(os.Stdout).Encode(got)
		os.Exit(0)
	case "x32":
		var buf [1]byte
		unix.Syscall(0x40000000+39 /* getpid, the x32 way */, uintptr(unsafe.Pointer(&buf)), 0, 0)
		os.Exit(0)
	}
}

func restrictedHelper(t *testing.T, mode string) *exec.Cmd {
	if ABI() < MinABI {
		t.Skipf("Landlock ABI %d here, %d needed", ABI(), MinABI)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	e, _ := json.Marshal(probeEnv{Policy: Policy{Self: self, Ports: []uint16{1}, Netlink: []string{"diag"}}})
	c := exec.Command(self)
	c.Env = []string{"SPOT_PROBE=restrict", "SPOT_PROBE_NEXT=" + mode, "SPOT_PROBE_ENV=" + string(e)}
	return c
}

func TestEveryRefusedCall(t *testing.T) {
	out, err := restrictedHelper(t, "calls").Output()
	var got map[string]int
	if jerr := json.Unmarshal(out, &got); err != nil || jerr != nil {
		t.Fatalf("%v %v %s", err, jerr, out)
	}
	for name := range mustBeRefused {
		if got[name] != int(unix.EPERM) {
			t.Errorf("%s: errno %d, want EPERM", name, got[name])
		}
	}
	for name := range mustLookMissing {
		if got[name] != int(unix.ENOSYS) {
			t.Errorf("%s: errno %d, want ENOSYS", name, got[name])
		}
	}
	for name, want := range map[string]int{
		"socket: stream with flags": 0, "socket: raw with flags": int(unix.EACCES), "netlink: audit": int(unix.EACCES),
		"udp to 53": 0, "udp to 5353": int(unix.EACCES), "read resolv.conf": 0,
	} {
		if got[name] != want {
			t.Errorf("%s: %d, want %d", name, got[name], want)
		}
	}
}

func TestOtherCallConventionsKill(t *testing.T) {
	err := restrictedHelper(t, "x32").Run()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.Sys().(syscall.WaitStatus).Signal() != syscall.SIGSYS {
		t.Errorf("an x32 call: %v, want SIGSYS", err)
	}
}
