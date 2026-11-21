package cli

import (
	"errors"

	"github.com/NickAwrist/envctl/internal/daemon"
	"github.com/NickAwrist/envctl/internal/paths"
)

func (a *App) cmdLock(args []string) error {
	if len(args) > 0 {
		return errors.New("lock takes no arguments")
	}
	client := daemon.NewClient(paths.SocketPath())
	if err := client.Lock(); err != nil {
		return err
	}
	a.out.Success("Locked.")
	return nil
}
