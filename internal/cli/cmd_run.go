package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/NickAwrist/envctl/internal/daemon"
	"github.com/NickAwrist/envctl/internal/dotenv"
	"github.com/NickAwrist/envctl/internal/paths"
)

func (a *App) cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	envFile := fs.String("env-file", ".env", "Path to .env file")
	override := fs.Bool("override", false, "Override existing process environment variables")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cmdArgs := fs.Args()
	if len(cmdArgs) == 0 {
		return errors.New("usage: envctl run [--env-file .env] [--override] -- <command> [args...]")
	}

	data, err := os.ReadFile(*envFile)
	if err != nil {
		return fmt.Errorf("read %s: %w", *envFile, err)
	}
	parsed := dotenv.Parse(string(data))
	existing := environMap()

	client := daemon.NewClient(paths.SocketPath())
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

	child := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	child.Env = mergeEnviron(existing, resolved)
	child.Stdin = os.Stdin
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr

	if err := child.Start(); err != nil {
		return err
	}

	sigC := make(chan os.Signal, 2)
	signal.Notify(sigC, syscall.SIGINT, syscall.SIGTERM)
	doneC := make(chan struct{})
	go func() {
		for {
			select {
			case sig := <-sigC:
				if child.Process != nil {
					_ = child.Process.Signal(sig)
				}
			case <-doneC:
				return
			}
		}
	}()

	waitErr := child.Wait()
	close(doneC)
	signal.Stop(sigC)
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			return &ExitCodeError{Code: exitErr.ExitCode()}
		}
		return waitErr
	}
	return nil
}
