package paths

import (
	"fmt"
	"os"
	"path/filepath"
)

func ConfigDir() (string, error) {
	if dir := os.Getenv("ENVCTL_CONFIG_DIR"); dir != "" {
		return dir, nil
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "envctl"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".config", "envctl"), nil
}

func DataDir() (string, error) {
	if dir := os.Getenv("ENVCTL_DATA_DIR"); dir != "" {
		return dir, nil
	}
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "envctl"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "envctl"), nil
}

func ConfigFile() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

func VaultFile() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "vault.enc"), nil
}

func SocketPath() string {
	if sock := os.Getenv("ENVCTL_SOCKET"); sock != "" {
		return sock
	}
	if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
		candidate := filepath.Join(runtimeDir, "envctl", "agent.sock")
		if len(candidate) < 96 {
			return candidate
		}
	}
	candidate := filepath.Join(os.TempDir(), fmt.Sprintf("envctl-%d", os.Getuid()), "agent.sock")
	if len(candidate) < 96 {
		return candidate
	}
	return filepath.Join("/tmp", fmt.Sprintf("envctl-%d", os.Getuid()), "agent.sock")
}

func EnsurePrivateDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("chmod directory %s: %w", dir, err)
	}
	return nil
}
