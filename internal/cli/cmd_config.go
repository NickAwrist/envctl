package cli

import (
	"github.com/NickAwrist/envctl/internal/config"
	"github.com/NickAwrist/envctl/internal/output"
	"github.com/NickAwrist/envctl/internal/paths"
)

func (a *App) cmdConfig(args []string) error {
	fs := a.newCommand("config",
		"envctl config [get <key> | set <key> <value>]",
		"View or change settings. Without arguments it prints every setting.\n"+
			"Keys: unlock_mode (ttl, session, until_lock) and unlock_ttl (a duration such as 30m or 4h).")
	if err := fs.parse(reorderFlags(args, nil)); err != nil {
		return err
	}
	args = fs.Args()

	configPath, err := paths.ConfigFile()
	if err != nil {
		return err
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		a.out.KeyValues([]output.KV{
			{Key: "unlock_mode", Value: a.out.Cyan(string(cfg.UnlockMode))},
			{Key: "unlock_ttl", Value: cfg.UnlockTTL},
		})
		return nil
	}
	switch args[0] {
	case "get":
		if len(args) != 2 {
			return fs.usageError()
		}
		val, err := cfg.GetField(args[1])
		if err != nil {
			return err
		}
		a.out.Line(val)
		return nil
	case "set":
		if len(args) != 3 {
			return fs.usageError()
		}
		if err := cfg.SetField(args[1], args[2]); err != nil {
			return err
		}
		if err := config.Save(configPath, cfg); err != nil {
			return err
		}
		a.out.Success("Updated %s = %s", a.out.Bold(args[1]), a.out.Cyan(args[2]))
		return nil
	default:
		return fs.usageError()
	}
}
