package cli

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/NickAwrist/envctl/internal/config"
	"github.com/NickAwrist/envctl/internal/crypto"
	"github.com/NickAwrist/envctl/internal/output"
	"github.com/NickAwrist/envctl/internal/paths"
	"github.com/NickAwrist/envctl/internal/vault"
)

func (a *App) cmdInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	passwordFile := fs.String("password-file", "", "Read master password from file")
	fromStdin := fs.Bool("stdin", false, "Read master password from stdin")
	if err := fs.Parse(args); err != nil {
		return err
	}

	vaultPath, err := paths.VaultFile()
	if err != nil {
		return err
	}
	configPath, err := paths.ConfigFile()
	if err != nil {
		return err
	}
	if vault.Exists(vaultPath) {
		return fmt.Errorf("vault already exists at %s", vaultPath)
	}

	pw, err := a.readSecretInput("Master password: ", *passwordFile, *fromStdin)
	if err != nil {
		return err
	}
	defer crypto.Zero(pw)
	if len(pw) == 0 {
		return errors.New("password cannot be empty")
	}

	if *passwordFile == "" && !*fromStdin && isTerminal(os.Stdin) {
		confirm, err := a.promptHidden("Confirm master password: ")
		if err != nil {
			return err
		}
		defer crypto.Zero(confirm)
		if !bytes.Equal(pw, confirm) {
			return errors.New("passwords do not match")
		}
	}

	if err := vault.Init(vaultPath, pw); err != nil {
		return err
	}
	if _, err := os.Stat(configPath); errors.Is(err, os.ErrNotExist) {
		if err := config.Save(configPath, config.Default()); err != nil {
			return err
		}
	}

	a.out.Success("Initialized vault")
	a.out.KeyValues([]output.KV{
		{Key: "Vault", Value: vaultPath},
		{Key: "Config", Value: configPath},
	})
	return nil
}
