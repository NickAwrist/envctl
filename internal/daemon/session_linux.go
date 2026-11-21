//go:build linux

package daemon

import (
	"context"
	"errors"

	"golang.org/x/sys/unix"
)

// watchPIDExit blocks until pid exits or ctx is cancelled.
// Returns true if the process exited, false if ctx was cancelled.
func watchPIDExit(ctx context.Context, pid int) bool {
	if pid <= 1 {
		return true
	}
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		// ESRCH means process already exited; fall back to polling on ENOSYS/EINVAL.
		if errors.Is(err, unix.ESRCH) {
			return true
		}
		return pollPIDExit(ctx, pid)
	}
	defer unix.Close(fd)

	fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	for {
		if ctx.Err() != nil {
			return false
		}
		n, err := unix.Poll(fds, 250)
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return true
		}
		if n > 0 && (fds[0].Revents&(unix.POLLIN|unix.POLLHUP|unix.POLLERR)) != 0 {
			return true
		}
	}
}
