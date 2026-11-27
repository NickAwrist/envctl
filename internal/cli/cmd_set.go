package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/NickAwrist/envctl/internal/crypto"
	"github.com/NickAwrist/envctl/internal/daemon"
	"github.com/NickAwrist/envctl/internal/vault"
)

func (a *App) cmdSet(args []string) error {
	fs := a.newCommand("set",
		"envctl set <KEY> [--description <text>] [--stdin | --value-file <path>]",
		"Store or update a secret. The value is prompted for on a terminal and read\n"+
			"from stdin otherwise; it is never taken from the command line. One trailing\n"+
			"newline is stripped. Empty values are rejected.")
	desc := fs.String("description", "", "Describe the secret with `text` (kept on update when omitted)")
	descShort := fs.String("d", "", "Describe the secret with `text` (short form)")
	valueFile := fs.String("value-file", "", "Read the value from the file at `path`")
	fromStdin := fs.Bool("stdin", false, "Read the value from stdin even on a terminal")
	if err := fs.parse(reorderFlags(args, map[string]bool{
		"description": true,
		"d":           true,
		"value-file":  true,
	})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fs.usageError()
	}

	key := fs.Arg(0)
	if strings.Contains(key, "=") {
		// Do not echo the argument: it most likely contains the secret.
		return errors.New("pass only the key name; the value is read from the prompt, --stdin, or --value-file, never as KEY=VALUE")
	}
	if err := vault.ValidateKeyName(key); err != nil {
		return err
	}
	description := *desc
	if description == "" {
		description = *descShort
	}

	client, err := newClient()
	if err != nil {
		return err
	}
	st, err := client.Status()
	if errors.Is(err, daemon.ErrDaemonUnavailable) || (err == nil && st.Locked) {
		return fmt.Errorf("%w; run 'envctl unlock' first", daemon.ErrLocked)
	}
	if err != nil {
		return err
	}

	// Piped input is the whole value, so a multi-line secret is not cut at its first line.
	wholeStdin := *fromStdin || (*valueFile == "" && !isTerminal(os.Stdin))
	valBytes, err := a.readSecretInput(fmt.Sprintf("Value for %s: ", key), *valueFile, wholeStdin)
	if err != nil {
		return err
	}
	defer crypto.Zero(valBytes)
	if len(valBytes) == 0 {
		return fmt.Errorf("value for %s is empty; nothing was stored", key)
	}

	if err := client.Set(key, string(valBytes), description); err != nil {
		return err
	}
	a.out.Success("Stored %s", a.out.Bold(key))
	return nil
}
