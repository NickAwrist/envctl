package cli

import (
	"errors"

	"github.com/NickAwrist/envctl/internal/daemon"
	"github.com/NickAwrist/envctl/internal/paths"
)

func (a *App) cmdList(args []string) error {
	if len(args) > 0 {
		return errors.New("list takes no arguments")
	}
	client := daemon.NewClient(paths.SocketPath())
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
