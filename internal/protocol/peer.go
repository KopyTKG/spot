package protocol

import (
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
)

// ErrForeignPeer: the other end of the socket belongs to another user.
var ErrForeignPeer = errors.New("the other end of the socket belongs to another user")

// CheckPeer verifies that conn, a Unix socket connection, is with a process of
// our own user. Both sides check before saying anything: a client sends its
// environment and acts on the answer, and a daemon runs what a request asks for.
// File permissions are not enough on their own, since a socket path in a shared
// directory may have been taken by someone else first.
func CheckPeer(conn net.Conn) error {
	return checkPeer(conn, uint32(os.Getuid()))
}

func checkPeer(conn net.Conn, want uint32) error {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return fmt.Errorf("not a Unix socket: %T", conn)
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return err
	}
	var cred *syscall.Ucred
	var cerr error
	if err := raw.Control(func(fd uintptr) {
		cred, cerr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return err
	}
	if cerr != nil {
		return cerr
	}
	if cred.Uid != want {
		return fmt.Errorf("%w (uid %d)", ErrForeignPeer, cred.Uid)
	}
	return nil
}
