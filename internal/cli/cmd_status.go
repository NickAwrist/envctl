package cli

import (
	"errors"
	"fmt"
	"time"

	"github.com/NickAwrist/envctl/internal/config"
	"github.com/NickAwrist/envctl/internal/daemon"
	"github.com/NickAwrist/envctl/internal/output"
	"github.com/NickAwrist/envctl/internal/paths"
	"github.com/NickAwrist/envctl/internal/vault"
)

func (a *App) cmdStatus(args []string) error {
	fs := a.newCommand("status",
		"envctl status",
		"Show whether the vault is unlocked, the active unlock mode and expiry, and the\n"+
			"vault and socket paths.")
	if err := fs.parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fs.usageError()
	}
	vaultPath, err := paths.VaultFile()
	if err != nil {
		return err
	}
	configPath, err := paths.ConfigFile()
	if err != nil {
		return err
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	sockPath := paths.SocketPath()
	client := daemon.NewClient(sockPath)
	status, err := client.Status()
	if err != nil {
		if errors.Is(err, daemon.ErrDaemonUnavailable) {
			state := a.out.Red("locked")
			if !vault.Exists(vaultPath) {
				state = a.out.Yellow("no vault (run 'envctl init')")
			}
			a.out.KeyValues([]output.KV{
				{Key: "Status", Value: state},
				{Key: "Default", Value: fmt.Sprintf("%s (ttl: %s)", cfg.UnlockMode, cfg.UnlockTTL)},
				{Key: "Vault", Value: vaultPath},
				{Key: "Socket", Value: sockPath},
			})
			return nil
		}
		return err
	}

	var rows []output.KV
	if status.Locked {
		rows = append(rows, output.KV{Key: "Status", Value: a.out.Red("locked")})
	} else {
		rows = append(rows,
			output.KV{Key: "Status", Value: a.out.Green("unlocked")},
			output.KV{Key: "Mode", Value: a.out.Cyan(string(status.Mode))},
		)
		if status.ExpiresAt != nil {
			rem := time.Until(*status.ExpiresAt).Round(time.Second)
			if rem < 0 {
				rem = 0
			}
			rows = append(rows, output.KV{
				Key:   "Expires",
				Value: fmt.Sprintf("%s (in %s)", status.ExpiresAt.Local().Format(time.RFC3339), rem),
			})
		}
		if status.SessionPID > 0 {
			rows = append(rows, output.KV{Key: "Session", Value: fmt.Sprintf("pid %d", status.SessionPID)})
		}
	}
	rows = append(rows,
		output.KV{Key: "Vault", Value: status.VaultPath},
		output.KV{Key: "Socket", Value: sockPath},
	)
	a.out.KeyValues(rows)
	return nil
}
