package cli

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/NickAwrist/envctl/internal/dotenv"
)

func (a *App) cmdCheck(args []string) error {
	fs := a.newCommand("check",
		"envctl check [--env-file <path>]",
		"Verify that every %ENVCTL-<KEY>% reference in a .env file is well formed and\n"+
			"exists in the vault. Values are never printed.")
	envFile := fs.String("env-file", ".env", "Check the .env file at `path`")
	if err := fs.parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q; pass the file with --env-file", fs.Arg(0))
	}
	data, err := os.ReadFile(*envFile)
	if err != nil {
		return fmt.Errorf("read %s: %w", *envFile, err)
	}

	parsed := dotenv.Parse(string(data))
	neededSet := make(map[string]struct{})
	var malformed []string
	for _, envKey := range parsed.Order {
		v := parsed.Values[envKey]
		if dotenv.HasMalformedPlaceholder(v) {
			malformed = append(malformed, envKey)
		}
		for _, k := range dotenv.ExtractPlaceholders(v) {
			neededSet[k] = struct{}{}
		}
	}
	if len(malformed) > 0 {
		return fmt.Errorf("malformed reference in %s: expected %%ENVCTL-<KEY>%% (uppercase prefix, closing %%)", strings.Join(malformed, ", "))
	}
	if len(neededSet) == 0 {
		a.out.DimInfo("No %%ENVCTL-...%% references found in %s.", *envFile)
		return nil
	}

	keys := make([]string, 0, len(neededSet))
	for k := range neededSet {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	client, err := newClient()
	if err != nil {
		return err
	}
	_, missing, err := client.Get(keys)
	if err != nil {
		return err
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing secrets in vault: %s", strings.Join(missing, ", "))
	}
	a.out.Success("All %d referenced secrets exist in vault.", len(keys))
	return nil
}
