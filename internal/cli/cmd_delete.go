package cli

func (a *App) cmdDelete(args []string) error {
	fs := a.newCommand("rm",
		"envctl rm <KEY>",
		"Delete a secret from the vault.")
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
	if err := client.Delete(key); err != nil {
		return err
	}
	a.out.Success("Deleted %s", a.out.Bold(key))
	return nil
}
