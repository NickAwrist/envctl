//go:build !linux

package daemon

import "context"

func watchPIDExit(ctx context.Context, pid int) bool {
	return pollPIDExit(ctx, pid)
}
