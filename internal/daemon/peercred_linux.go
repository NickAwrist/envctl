//go:build linux

package daemon

import (
	"fmt"
	"net"
	"os"

	"golang.org/x/sys/unix"
)

func verifyPeerUID(conn *net.UnixConn) error {
	raw, err := conn.SyscallConn()
	if err != nil {
		return fmt.Errorf("get raw socket: %w", err)
	}
	var ucred *unix.Ucred
	var sockErr error
	if err := raw.Control(func(fd uintptr) {
		ucred, sockErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return fmt.Errorf("socket control: %w", err)
	}
	if sockErr != nil {
		return fmt.Errorf("SO_PEERCRED: %w", sockErr)
	}
	if int(ucred.Uid) != os.Getuid() {
		return fmt.Errorf("unauthorized peer uid %d", ucred.Uid)
	}
	return nil
}

func currentSessionPID() int {
	if sid, err := unix.Getsid(0); err == nil && sid > 1 {
		return sid
	}
	return os.Getppid()
}
