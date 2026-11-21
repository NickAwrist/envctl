package cli

import (
	"errors"

	"github.com/NickAwrist/envctl/internal/daemon"
	"github.com/NickAwrist/envctl/internal/paths"
)

func (a *App) cmdDelete(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: envctl rm <KEY>")
	}
	key := args[0]
	client := daemon.NewClient(paths.SocketPath())
	if err := client.Delete(key); err != nil {
		return err
	}
	a.out.Success("Deleted %s", a.out.Bold(key))
	return nil
}
