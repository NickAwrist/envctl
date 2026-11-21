//go:build !linux

package daemon

import (
	"net"
	"os"
)

func verifyPeerUID(_ *net.UnixConn) error {
	// Non-Linux platforms rely on 0700 directory + 0600 socket permissions.
	return nil
}

func currentSessionPID() int {
	return os.Getppid()
}
