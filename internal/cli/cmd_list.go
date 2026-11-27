package cli

func (a *App) cmdList(args []string) error {
	fs := a.newCommand("list",
		"envctl list",
		"List stored secret keys with their last update and description. Values are never printed.")
	if err := fs.parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fs.usageError()
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	entries, err := client.List()
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		a.out.DimInfo("No secrets stored.")
		return nil
	}

	rows := make([][]string, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, []string{
			a.out.Cyan(e.Key),
			a.out.Dim(e.UpdatedAt.Local().Format("2006-01-02 15:04")),
			e.Description,
		})
	}
	a.out.Table([]string{"KEY", "UPDATED", "DESCRIPTION"}, rows)
	return nil
}
