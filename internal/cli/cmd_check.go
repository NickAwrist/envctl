package cli

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/NickAwrist/envctl/internal/daemon"
	"github.com/NickAwrist/envctl/internal/dotenv"
	"github.com/NickAwrist/envctl/internal/paths"
)

func (a *App) cmdCheck(args []string) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	envFile := fs.String("env-file", ".env", "Path to .env file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	data, err := os.ReadFile(*envFile)
	if err != nil {
		return fmt.Errorf("read %s: %w", *envFile, err)
	}

	parsed := dotenv.Parse(string(data))
	neededSet := make(map[string]struct{})
	for _, v := range parsed.Values {
		for _, k := range dotenv.ExtractPlaceholders(v) {
			neededSet[k] = struct{}{}
		}
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

	client := daemon.NewClient(paths.SocketPath())
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
