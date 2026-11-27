package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os"

	"github.com/NickAwrist/envctl/internal/crypto"
	"github.com/NickAwrist/envctl/internal/paths"
	"github.com/NickAwrist/envctl/internal/vault"
)

func (a *App) cmdPasswd(args []string) error {
	fs := a.newCommand("passwd",
		"envctl passwd [--old-password-file <path>] [--new-password-file <path>]",
		"Change the master password and re-encrypt the secrets with a new data key.\n"+
			"The vault is locked afterwards.")
	oldFile := fs.String("old-password-file", "", "Read the current master password from the file at `path`")
	newFile := fs.String("new-password-file", "", "Read the new master password from the file at `path`")
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
	if !vault.Exists(vaultPath) {
		return errNoVault(vaultPath)
	}

	oldPw, err := a.readSecretInput("Current master password: ", *oldFile, false)
	if err != nil {
		return err
	}
	defer crypto.Zero(oldPw)

	newPw, err := a.readSecretInput("New master password: ", *newFile, false)
	if err != nil {
		return err
	}
	defer crypto.Zero(newPw)

	if *newFile == "" && isTerminal(os.Stdin) {
		confirm, err := a.promptHidden("Confirm new master password: ")
		if err != nil {
			return err
		}
		defer crypto.Zero(confirm)
		if !bytes.Equal(newPw, confirm) {
			return errors.New("new passwords do not match")
		}
	}

	client, err := newClient()
	if err != nil {
		return err
	}
	if err := vault.ChangePassword(vaultPath, oldPw, newPw); err != nil {
		return err
	}
	// A running daemon still holds the previous data key, which no longer opens the vault.
	if err := client.Lock(); err != nil {
		return fmt.Errorf("master password updated, but locking the running daemon failed (run 'envctl lock'): %w", err)
	}
	a.out.Success("Master password updated. The vault is locked.")
	return nil
}
