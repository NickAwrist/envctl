package cli

import (
	"errors"
	"flag"
	"fmt"

	"github.com/NickAwrist/envctl/internal/output"
)

const Version = "0.1.0"

// ExitCodeError makes envctl exit with a specific code after reporting Err.
type ExitCodeError struct {
	Code int
	Err  error
}

func (e *ExitCodeError) Error() string {
	return e.Err.Error()
}

func (e *ExitCodeError) Unwrap() error {
	return e.Err
}

// App wires together CLI subcommands and centralized output formatting.
type App struct {
	out *output.Printer
}

func NewApp(out *output.Printer) *App {
	return &App{out: out}
}

// Run executes the CLI with the provided arguments and returns the process exit code.
func Run(args []string) int {
	app := NewApp(output.New())
	if err := app.Execute(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		app.out.Error("%v", explainLocked(err))
		var exitErr *ExitCodeError
		if errors.As(err, &exitErr) {
			return exitErr.Code
		}
		return 1
	}
	return 0
}

func (a *App) Execute(args []string) error {
	if len(args) == 0 {
		a.printUsage()
		return nil
	}

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "init":
		return a.cmdInit(rest)
	case "unlock":
		return a.cmdUnlock(rest)
	case "lock":
		return a.cmdLock(rest)
	case "status":
		return a.cmdStatus(rest)
	case "set":
		return a.cmdSet(rest)
	case "get":
		return a.cmdGet(rest)
	case "list", "ls":
		return a.cmdList(rest)
	case "rm", "delete":
		return a.cmdDelete(rest)
	case "check":
		return a.cmdCheck(rest)
	case "run":
		return a.cmdRun(rest)
	case "config":
		return a.cmdConfig(rest)
	case "passwd":
		return a.cmdPasswd(rest)
	case "__daemon":
		return a.cmdDaemon(rest)
	case "version", "--version", "-v":
		a.out.Info("envctl %s", a.out.Dim(Version))
		return nil
	case "help", "--help", "-h":
		if cmd == "help" && len(rest) > 0 {
			return a.Execute([]string{rest[0], "--help"})
		}
		a.printUsage()
		return nil
	default:
		a.printUsage()
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func (a *App) printUsage() {
	a.out.Info("%s %s", a.out.Bold("envctl"), a.out.Dim("encrypted local environment variable manager"))
	a.out.Line("")
	a.out.Section("Usage")
	a.out.Info("  envctl <command> [flags]")
	a.out.Line("")
	a.out.Section("Commands")
	commands := [][2]string{
		{"init", "Initialize a new encrypted vault and default config"},
		{"unlock", "Unlock the vault (--ttl <dur> | --session | --until-lock)"},
		{"lock", "Lock the vault and stop the daemon"},
		{"status", "Show daemon lock status and configuration"},
		{"set", "Store or update a secret"},
		{"get", "Print a single secret value"},
		{"list", "List stored secret keys (never prints values)"},
		{"rm", "Delete a secret"},
		{"check", "Verify all %ENVCTL-...% references in a .env file exist"},
		{"run", "Resolve a .env file in memory and run a command"},
		{"config", "View or update settings (unlock_mode, unlock_ttl)"},
		{"passwd", "Change the master password"},
		{"version", "Print version"},
	}
	for _, c := range commands {
		a.out.Info("  %s%s  %s", a.out.Cyan(c[0]), padSpaces(9-len(c[0])), c[1])
	}
	a.out.Line("")
	a.out.Info("Run 'envctl <command> --help' for a command's arguments and flags.")
}

func padSpaces(n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, n)
	for i := range b {
		b[i] = ' '
	}
	return string(b)
}
