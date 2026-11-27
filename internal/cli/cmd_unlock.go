package cli

import (
	"errors"
	"fmt"
	"time"

	"github.com/NickAwrist/envctl/internal/config"
	"github.com/NickAwrist/envctl/internal/crypto"
	"github.com/NickAwrist/envctl/internal/daemon"
	"github.com/NickAwrist/envctl/internal/paths"
	"github.com/NickAwrist/envctl/internal/vault"
)

func (a *App) cmdUnlock(args []string) error {
	fs := a.newCommand("unlock",
		"envctl unlock [--ttl <duration> | --session | --until-lock] [--password-file <path> | --stdin]",
		"Unlock the vault and start the background daemon. Without a mode flag it uses\n"+
			"unlock_mode and unlock_ttl from the config. The master password is prompted for\n"+
			"on a terminal; otherwise pass --password-file or --stdin.")
	ttlFlag := fs.String("ttl", "", "Stay unlocked for a fixed `duration` (e.g. 30m, 4h)")
	sessionFlag := fs.Bool("session", false, "Stay unlocked until the current shell or SSH session exits")
	sessionPIDFlag := fs.Int("session-pid", 0, "With --session, watch this `pid` instead of the session leader")
	untilLockFlag := fs.Bool("until-lock", false, "Stay unlocked until 'envctl lock' or reboot")
	passwordFile := fs.String("password-file", "", "Read the master password from the file at `path`")
	fromStdin := fs.Bool("stdin", false, "Read the master password from stdin")
	if err := fs.parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fs.usageError()
	}

	modeFlags := 0
	if *ttlFlag != "" {
		modeFlags++
	}
	if *sessionFlag {
		modeFlags++
	}
	if *untilLockFlag {
		modeFlags++
	}
	if modeFlags > 1 {
		return errors.New("choose only one of --ttl, --session, or --until-lock")
	}

	vaultPath, err := paths.VaultFile()
	if err != nil {
		return err
	}
	if !vault.Exists(vaultPath) {
		return errNoVault(vaultPath)
	}
	configPath, err := paths.ConfigFile()
	if err != nil {
		return err
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	mode := cfg.UnlockMode
	var ttl time.Duration
	var sessionPID int

	switch {
	case *ttlFlag != "":
		mode = config.UnlockModeTTL
		ttl, err = time.ParseDuration(*ttlFlag)
		if err != nil || ttl <= 0 {
			return fmt.Errorf("invalid --ttl %q", *ttlFlag)
		}
	case *sessionFlag:
		mode = config.UnlockModeSession
		sessionPID = *sessionPIDFlag
		if sessionPID == 0 {
			sessionPID = daemon.CurrentSessionPID()
		}
	case *untilLockFlag:
		mode = config.UnlockModeUntilLock
	default:
		switch mode {
		case config.UnlockModeTTL:
			ttl, err = cfg.TTLDuration()
			if err != nil {
				return err
			}
		case config.UnlockModeSession:
			sessionPID = daemon.CurrentSessionPID()
		case config.UnlockModeUntilLock:
		}
	}

	pw, err := a.readSecretInput("Master password: ", *passwordFile, *fromStdin)
	if err != nil {
		return err
	}
	defer crypto.Zero(pw)

	status, err := unlockDaemon(string(pw), mode, ttl, sessionPID)
	if err != nil {
		return err
	}

	switch status.Mode {
	case config.UnlockModeTTL:
		if status.ExpiresAt != nil {
			rem := time.Until(*status.ExpiresAt).Round(time.Second)
			a.out.Success("Unlocked (mode: %s, expires in %s at %s)",
				a.out.Cyan(string(status.Mode)),
				rem,
				status.ExpiresAt.Local().Format(time.Kitchen),
			)
		} else {
			a.out.Success("Unlocked (mode: %s)", a.out.Cyan(string(status.Mode)))
		}
	case config.UnlockModeSession:
		a.out.Success("Unlocked (mode: %s, watching pid %d)", a.out.Cyan(string(status.Mode)), status.SessionPID)
	case config.UnlockModeUntilLock:
		a.out.Success("Unlocked (mode: %s)", a.out.Cyan(string(status.Mode)))
	}
	return nil
}
