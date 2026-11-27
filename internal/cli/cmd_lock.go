package cli

import (
	"github.com/NickAwrist/envctl/internal/daemon"
	"github.com/NickAwrist/envctl/internal/paths"
)

func (a *App) cmdLock(args []string) error {
	fs := a.newCommand("lock",
		"envctl lock",
		"Lock the vault: the daemon erases the key from memory and exits.")
	if err := fs.parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fs.usageError()
	}
	client := daemon.NewClient(paths.SocketPath())
	if err := client.Lock(); err != nil {
		return err
	}
	a.out.Success("Locked.")
	return nil
}
