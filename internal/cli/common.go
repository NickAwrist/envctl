package cli

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/NickAwrist/envctl/internal/config"
	"github.com/NickAwrist/envctl/internal/crypto"
	"github.com/NickAwrist/envctl/internal/daemon"
	"github.com/NickAwrist/envctl/internal/output"
	"github.com/NickAwrist/envctl/internal/paths"
	"github.com/NickAwrist/envctl/internal/protocol"
	"github.com/NickAwrist/envctl/internal/vault"
)

// command is a subcommand's flag set plus the text shown by --help.
type command struct {
	*flag.FlagSet
	out     *output.Printer
	usage   string
	summary string
}

func (a *App) newCommand(name, usage, summary string) *command {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return &command{FlagSet: fs, out: a.out, usage: usage, summary: summary}
}

// parse parses args. For -h or --help it prints the command's help and returns
// flag.ErrHelp, which Run treats as success.
func (c *command) parse(args []string) error {
	err := c.Parse(args)
	if errors.Is(err, flag.ErrHelp) {
		c.printHelp()
		return err
	}
	if err != nil {
		return fmt.Errorf("%w (see 'envctl %s --help')", err, c.Name())
	}
	return nil
}

func (c *command) usageError() error {
	return fmt.Errorf("usage: %s", c.usage)
}

func (c *command) printHelp() {
	c.out.Section("Usage")
	c.out.Info("  %s", c.usage)
	c.out.Line("")
	c.out.Info("%s", c.summary)

	var flags [][2]string
	width := 0
	c.VisitAll(func(f *flag.Flag) {
		label := "--" + f.Name
		if len(f.Name) == 1 {
			label = "-" + f.Name
		}
		value, usage := flag.UnquoteUsage(f)
		if value != "" {
			label += " <" + value + ">"
		}
		flags = append(flags, [2]string{label, usage})
		width = max(width, len(label))
	})
	if len(flags) > 0 {
		c.out.Line("")
		c.out.Section("Flags")
		for _, f := range flags {
			c.out.Info("  %s%s  %s", c.out.Cyan(f[0]), padSpaces(width-len(f[0])), f[1])
		}
	}
}

// newClient returns a client for this environment's socket that only talks to
// a daemon serving this environment's vault.
func newClient() (*daemon.Client, error) {
	vaultPath, err := paths.VaultFile()
	if err != nil {
		return nil, err
	}
	vaultPath, err = filepath.Abs(vaultPath)
	if err != nil {
		return nil, err
	}
	client := daemon.NewClient(paths.SocketPath())
	client.VaultPath = vaultPath
	return client, nil
}

func errNoVault(vaultPath string) error {
	return fmt.Errorf("%w at %s (run 'envctl init' first)", vault.ErrVaultNotFound, vaultPath)
}

// explainLocked replaces a "vault is locked" error with "vault not found" when
// no vault has been created yet.
func explainLocked(err error) error {
	if !errors.Is(err, daemon.ErrDaemonUnavailable) && !errors.Is(err, daemon.ErrLocked) {
		return err
	}
	vaultPath, pathErr := paths.VaultFile()
	if pathErr != nil || vault.Exists(vaultPath) {
		return err
	}
	return errNoVault(vaultPath)
}

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
	fd := int(os.Stdin.Fd())

	// ReadPassword turns terminal echo off. Turn it back on if the user
	// interrupts the prompt, or their shell is left without echo.
	if state, err := term.GetState(fd); err == nil {
		sigC := make(chan os.Signal, 1)
		signal.Notify(sigC, syscall.SIGINT, syscall.SIGTERM)
		doneC := make(chan struct{})
		defer func() {
			signal.Stop(sigC)
			close(doneC)
		}()
		go func() {
			select {
			case sig := <-sigC:
				_ = term.Restore(fd, state)
				a.out.PromptNewline()
				os.Exit(128 + int(sig.(syscall.Signal)))
			case <-doneC:
			}
		}()
	}

	a.out.Prompt(prompt)
	pw, err := term.ReadPassword(fd)
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

func unlockDaemon(password string, mode config.UnlockMode, ttl time.Duration, sessionPID int) (*protocol.StatusInfo, error) {
	client, err := newClient()
	if err != nil {
		return nil, err
	}
	if err := daemon.EnsureDaemonRunning(client.SocketPath, client.VaultPath); err != nil {
		return nil, err
	}
	return client.Unlock(password, mode, ttl, sessionPID)
}

func (a *App) inlineUnlockPrompt() error {
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

	_, err = unlockDaemon(string(pw), cfg.UnlockMode, ttl, sessionPID)
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
