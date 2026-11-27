package cli

import (
	"fmt"
)

func (a *App) cmdGet(args []string) error {
	fs := a.newCommand("get",
		"envctl get <KEY>",
		"Print a single secret value: raw when piped, with a trailing newline on a terminal.")
	if err := fs.parse(reorderFlags(args, nil)); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fs.usageError()
	}
	key := fs.Arg(0)
	client, err := newClient()
	if err != nil {
		return err
	}
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
