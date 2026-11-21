package cli

import (
	"errors"

	"github.com/NickAwrist/envctl/internal/config"
	"github.com/NickAwrist/envctl/internal/output"
	"github.com/NickAwrist/envctl/internal/paths"
)

func (a *App) cmdConfig(args []string) error {
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
			return errors.New("usage: envctl config get <key>")
		}
		val, err := cfg.GetField(args[1])
		if err != nil {
			return err
		}
		a.out.Line(val)
		return nil
	case "set":
		if len(args) != 3 {
			return errors.New("usage: envctl config set <key> <value>")
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
		return errors.New("usage: envctl config [get <key> | set <key> <value>]")
	}
}
