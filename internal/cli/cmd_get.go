package cli

import (
	"errors"
	"fmt"

	"github.com/NickAwrist/envctl/internal/daemon"
	"github.com/NickAwrist/envctl/internal/paths"
)

func (a *App) cmdGet(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: envctl get <KEY>")
	}
	key := args[0]
	client := daemon.NewClient(paths.SocketPath())
	values, missing, err := client.Get([]string{key})
	if err != nil {
		return err
	}
	if len(missing) > 0 {
		return fmt.Errorf("secret %q not found", key)
	}
	a.out.Raw(values[key])
	return nil
}
