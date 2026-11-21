package cli

import (
	"errors"
	"flag"
	"fmt"
	"time"

	"github.com/NickAwrist/envctl/internal/config"
	"github.com/NickAwrist/envctl/internal/crypto"
	"github.com/NickAwrist/envctl/internal/daemon"
	"github.com/NickAwrist/envctl/internal/paths"
	"github.com/NickAwrist/envctl/internal/vault"
)

func (a *App) cmdUnlock(args []string) error {
	fs := flag.NewFlagSet("unlock", flag.ContinueOnError)
	ttlFlag := fs.String("ttl", "", "Fixed unlock duration (e.g. 30m, 4h)")
	sessionFlag := fs.Bool("session", false, "Unlock until current shell/SSH session exits")
	sessionPIDFlag := fs.Int("session-pid", 0, "Override watched session PID (with --session)")
	untilLockFlag := fs.Bool("until-lock", false, "Unlock until 'envctl lock' or reboot")
	passwordFile := fs.String("password-file", "", "Read master password from file")
	fromStdin := fs.Bool("stdin", false, "Read master password from stdin")
	if err := fs.Parse(args); err != nil {
		return err
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
		return fmt.Errorf("vault not found at %s (run 'envctl init' first)", vaultPath)
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

	status, err := unlockDaemon(paths.SocketPath(), vaultPath, string(pw), mode, ttl, sessionPID)
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
