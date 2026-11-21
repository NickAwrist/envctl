package cli

import (
	"flag"
	"os"
	"os/signal"
	"syscall"

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

	srv := daemon.NewServer(*sockPath, *vaultPath)
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
