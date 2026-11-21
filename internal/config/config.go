package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/NickAwrist/envctl/internal/paths"
)

type UnlockMode string

const (
	UnlockModeTTL       UnlockMode = "ttl"
	UnlockModeSession   UnlockMode = "session"
	UnlockModeUntilLock UnlockMode = "until_lock"
)

type Config struct {
	UnlockMode UnlockMode `json:"unlock_mode"`
	UnlockTTL  string     `json:"unlock_ttl"`
}

func Default() Config {
	return Config{
		UnlockMode: UnlockModeTTL,
		UnlockTTL:  "30m",
	}
}

func (c Config) Validate() error {
	switch c.UnlockMode {
	case UnlockModeTTL, UnlockModeSession, UnlockModeUntilLock:
	default:
		return fmt.Errorf("invalid unlock_mode %q (expected ttl, session, or until_lock)", c.UnlockMode)
	}
	d, err := time.ParseDuration(c.UnlockTTL)
	if err != nil || d <= 0 {
		return fmt.Errorf("invalid unlock_ttl %q (expected positive duration like 30m or 4h)", c.UnlockTTL)
	}
	return nil
}

func (c Config) TTLDuration() (time.Duration, error) {
	d, err := time.ParseDuration(c.UnlockTTL)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("invalid unlock_ttl %q", c.UnlockTTL)
	}
	return d, nil
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Default(), nil
		}
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	cfg := Default()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func Save(path string, cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := paths.EnsurePrivateDir(dir); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, ".config.json.tmp-*")
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	cleanup = false
	return nil
}

func (c *Config) SetField(key, value string) error {
	next := *c
	switch key {
	case "unlock_mode":
		next.UnlockMode = UnlockMode(value)
	case "unlock_ttl":
		next.UnlockTTL = value
	default:
		return fmt.Errorf("unknown config key %q (supported: unlock_mode, unlock_ttl)", key)
	}
	if err := next.Validate(); err != nil {
		return err
	}
	*c = next
	return nil
}

func (c Config) GetField(key string) (string, error) {
	switch key {
	case "unlock_mode":
		return string(c.UnlockMode), nil
	case "unlock_ttl":
		return c.UnlockTTL, nil
	default:
		return "", fmt.Errorf("unknown config key %q (supported: unlock_mode, unlock_ttl)", key)
	}
}
