package cli

import (
	"errors"
	"flag"
	"fmt"

	"github.com/NickAwrist/envctl/internal/crypto"
	"github.com/NickAwrist/envctl/internal/daemon"
	"github.com/NickAwrist/envctl/internal/paths"
	"github.com/NickAwrist/envctl/internal/vault"
)

func (a *App) cmdSet(args []string) error {
	reordered := reorderFlags(args, map[string]bool{
		"description": true,
		"d":           true,
		"value-file":  true,
	})
	fs := flag.NewFlagSet("set", flag.ContinueOnError)
	desc := fs.String("description", "", "Optional description for the secret")
	descShort := fs.String("d", "", "Optional description (short)")
	valueFile := fs.String("value-file", "", "Read secret value from file")
	fromStdin := fs.Bool("stdin", false, "Read secret value from stdin")
	if err := fs.Parse(reordered); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: envctl set <KEY> [--description <text>] [--stdin | --value-file <path>]")
	}

	key := fs.Arg(0)
	if err := vault.ValidateKeyName(key); err != nil {
		return err
	}
	description := *desc
	if description == "" {
		description = *descShort
	}

	client := daemon.NewClient(paths.SocketPath())
	st, err := client.Status()
	if err != nil || st.Locked {
		return errors.New("vault is locked; run 'envctl unlock' first")
	}

	valBytes, err := a.readSecretInput(fmt.Sprintf("Value for %s: ", key), *valueFile, *fromStdin)
	if err != nil {
		return err
	}
	defer crypto.Zero(valBytes)

	if err := client.Set(key, string(valBytes), description); err != nil {
		return err
	}
	a.out.Success("Stored %s", a.out.Bold(key))
	return nil
}
