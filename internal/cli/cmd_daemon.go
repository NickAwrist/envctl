package cli

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/NickAwrist/envctl/internal/daemon"
	"github.com/NickAwrist/envctl/internal/paths"
)

func (a *App) cmdDaemon(args []string) error {
	fs := flag.NewFlagSet("__daemon", flag.ContinueOnError)
	sockPath := fs.String("socket", paths.SocketPath(), "Unix socket path")
	vaultPath := fs.String("vault", "", "Vault file path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *vaultPath == "" {
		vp, err := paths.VaultFile()
		if err != nil {
			return err
		}
		*vaultPath = vp
	}

	// The daemon leaves its starting directory below, so resolve relative paths first.
	socketAbs, err := filepath.Abs(*sockPath)
	if err != nil {
		return err
	}
	vaultAbs, err := filepath.Abs(*vaultPath)
	if err != nil {
		return err
	}

	// Keep the key out of core dumps, and stop other processes of this user from
	// reading the daemon's memory through /proc or ptrace.
	if err := unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0); err != nil {
		return fmt.Errorf("mark daemon non-dumpable: %w", err)
	}
	// Do not keep the directory 'envctl unlock' was run from busy.
	if err := os.Chdir("/"); err != nil {
		return err
	}

	srv := daemon.NewServer(socketAbs, vaultAbs)
	if err := srv.Start(); err != nil {
		return err
	}

	sigC := make(chan os.Signal, 2)
	signal.Notify(sigC, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigC
		srv.Shutdown()
	}()

	return srv.Serve()
}
