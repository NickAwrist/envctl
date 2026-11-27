package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/NickAwrist/envctl/internal/daemon"
	"github.com/NickAwrist/envctl/internal/dotenv"
)

const (
	exitCannotExecute   = 126
	exitCommandNotFound = 127
)

func (a *App) cmdRun(args []string) error {
	cmd := a.newCommand("run",
		"envctl run [--env-file <path>] [--override] -- <command> [args...]",
		"Resolve the %ENVCTL-<KEY>% references in a .env file and in the current\n"+
			"environment, then replace envctl with <command>, which receives the resolved\n"+
			"variables. Nothing is written to disk. Variables already set in the\n"+
			"environment win over the .env file unless --override is given.\n"+
			"Exits 127 if <command> is not found and 126 if it cannot be executed.")
	envFile := cmd.String("env-file", ".env", "Read the .env file at `path`")
	override := cmd.Bool("override", false, "Let the .env file win over variables already set in the environment")
	if err := cmd.parse(args); err != nil {
		return err
	}
	cmdArgs := cmd.Args()
	if len(cmdArgs) == 0 {
		return cmd.usageError()
	}

	data, err := os.ReadFile(*envFile)
	if err != nil {
		return fmt.Errorf("read %s: %w", *envFile, err)
	}
	parsed := dotenv.Parse(string(data))
	existing := environMap()

	client, err := newClient()
	if err != nil {
		return err
	}
	resolve := func() (map[string]string, []string, error) {
		return dotenv.Resolve(parsed, existing, *override, client.Get)
	}

	resolved, missing, err := resolve()
	if (errors.Is(err, daemon.ErrDaemonUnavailable) || errors.Is(err, daemon.ErrLocked)) && isTerminal(os.Stdin) {
		if unlockErr := a.inlineUnlockPrompt(); unlockErr != nil {
			return unlockErr
		}
		resolved, missing, err = resolve()
	}
	if err != nil {
		return err
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing secrets in vault: %s", strings.Join(missing, ", "))
	}

	path, err := exec.LookPath(cmdArgs[0])
	if err != nil {
		code := exitCommandNotFound
		if errors.Is(err, fs.ErrPermission) {
			code = exitCannotExecute
		}
		return &ExitCodeError{Code: code, Err: err}
	}

	// Replace this process with the command so signals, the terminal, and the
	// exit status belong to it directly.
	err = syscall.Exec(path, cmdArgs, mergeEnviron(existing, resolved))
	return &ExitCodeError{Code: exitCannotExecute, Err: fmt.Errorf("exec %s: %w", path, err)}
}
