package cli

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/NickAwrist/envctl/internal/config"
	"github.com/NickAwrist/envctl/internal/crypto"
	"github.com/NickAwrist/envctl/internal/daemon"
	"github.com/NickAwrist/envctl/internal/paths"
	"github.com/NickAwrist/envctl/internal/protocol"
)

func isTerminal(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

func (a *App) readSecretInput(prompt, filePath string, fromStdin bool) ([]byte, error) {
	if filePath != "" && fromStdin {
		return nil, errors.New("cannot use both --password-file/--value-file and --stdin")
	}
	if filePath != "" {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", filePath, err)
		}
		return trimSingleTrailingNewline(data), nil
	}
	if fromStdin {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("read stdin: %w", err)
		}
		return trimSingleTrailingNewline(data), nil
	}
	if !isTerminal(os.Stdin) {
		reader := bufio.NewReader(os.Stdin)
		line, err := reader.ReadBytes('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("read stdin: %w", err)
		}
		return trimSingleTrailingNewline(line), nil
	}
	return a.promptHidden(prompt)
}

func (a *App) promptHidden(prompt string) ([]byte, error) {
	a.out.Prompt(prompt)
	pw, err := term.ReadPassword(int(os.Stdin.Fd()))
	a.out.PromptNewline()
	if err != nil {
		return nil, fmt.Errorf("read input: %w", err)
	}
	return pw, nil
}

func trimSingleTrailingNewline(b []byte) []byte {
	b = bytes.TrimSuffix(b, []byte("\n"))
	b = bytes.TrimSuffix(b, []byte("\r"))
	return b
}

// reorderFlags lets flags appear after positional arguments while respecting "--".
func reorderFlags(args []string, valueFlags map[string]bool) []string {
	var flags []string
	var pos []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			pos = append(pos, args[i:]...)
			break
		}
		if strings.HasPrefix(arg, "-") && len(arg) > 1 {
			flags = append(flags, arg)
			if !strings.Contains(arg, "=") {
				name := strings.TrimLeft(arg, "-")
				if valueFlags[name] && i+1 < len(args) {
					i++
					flags = append(flags, args[i])
				}
			}
			continue
		}
		pos = append(pos, arg)
	}
	return append(flags, pos...)
}

func unlockDaemon(socketPath, vaultPath, password string, mode config.UnlockMode, ttl time.Duration, sessionPID int) (*protocol.StatusInfo, error) {
	if err := daemon.EnsureDaemonRunning(socketPath, vaultPath); err != nil {
		return nil, err
	}
	client := daemon.NewClient(socketPath)
	return client.Unlock(password, mode, ttl, sessionPID)
}

func (a *App) inlineUnlockPrompt() error {
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

	var ttl time.Duration
	var sessionPID int
	switch cfg.UnlockMode {
	case config.UnlockModeTTL:
		ttl, err = cfg.TTLDuration()
		if err != nil {
			return err
		}
	case config.UnlockModeSession:
		sessionPID = daemon.CurrentSessionPID()
	}

	a.out.Warn("envctl vault is locked.")
	pw, err := a.promptHidden("Master password: ")
	if err != nil {
		return err
	}
	defer crypto.Zero(pw)

	_, err = unlockDaemon(paths.SocketPath(), vaultPath, string(pw), cfg.UnlockMode, ttl, sessionPID)
	return err
}

func environMap() map[string]string {
	raw := os.Environ()
	m := make(map[string]string, len(raw))
	for _, kv := range raw {
		if k, v, ok := strings.Cut(kv, "="); ok {
			m[k] = v
		}
	}
	return m
}

func mergeEnviron(base, updates map[string]string) []string {
	merged := make(map[string]string, len(base)+len(updates))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range updates {
		merged[k] = v
	}
	out := make([]string, 0, len(merged))
	for k, v := range merged {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}
