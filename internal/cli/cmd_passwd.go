package cli

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/NickAwrist/envctl/internal/crypto"
	"github.com/NickAwrist/envctl/internal/paths"
	"github.com/NickAwrist/envctl/internal/vault"
)

func (a *App) cmdPasswd(args []string) error {
	fs := flag.NewFlagSet("passwd", flag.ContinueOnError)
	oldFile := fs.String("old-password-file", "", "Read current master password from file")
	newFile := fs.String("new-password-file", "", "Read new master password from file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	vaultPath, err := paths.VaultFile()
	if err != nil {
		return err
	}
	if !vault.Exists(vaultPath) {
		return fmt.Errorf("vault not found at %s", vaultPath)
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

	if err := vault.ChangePassword(vaultPath, oldPw, newPw); err != nil {
		return err
	}
	a.out.Success("Master password updated.")
	return nil
}
