package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIEndToEnd(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "envctl")

	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	sockDir, err := os.MkdirTemp("/tmp", "envctl-cli-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })

	env := append(os.Environ(),
		"ENVCTL_CONFIG_DIR="+filepath.Join(dir, "config"),
		"ENVCTL_DATA_DIR="+filepath.Join(dir, "data"),
		"ENVCTL_SOCKET="+filepath.Join(sockDir, "a.sock"),
	)

	pwFile := filepath.Join(dir, "pw.txt")
	if err := os.WriteFile(pwFile, []byte("super-secret-pass\n"), 0o600); err != nil {
		t.Fatalf("write pw file: %v", err)
	}

	runCLI := func(stdin string, args ...string) (string, error) {
		cmd := exec.Command(bin, args...)
		cmd.Env = env
		if stdin != "" {
			cmd.Stdin = strings.NewReader(stdin)
		}
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	defer func() {
		_, _ = runCLI("", "lock")
	}()

	if out, err := runCLI("", "init", "--password-file", pwFile); err != nil {
		t.Fatalf("init failed: %v\n%s", err, out)
	}

	if out, err := runCLI("", "config", "set", "unlock_mode", "until_lock"); err != nil {
		t.Fatalf("config set failed: %v\n%s", err, out)
	}

	if out, err := runCLI("", "unlock", "--password-file", pwFile); err != nil {
		t.Fatalf("unlock failed: %v\n%s", err, out)
	} else if !strings.Contains(out, "until_lock") {
		t.Fatalf("expected until_lock confirmation, got %q", out)
	}

	if out, err := runCLI("sk-or-live-999\n", "set", "OPENROUTER_API_KEY", "--stdin", "--description", "OpenRouter"); err != nil {
		t.Fatalf("set failed: %v\n%s", err, out)
	}

	if out, err := runCLI("", "get", "OPENROUTER_API_KEY"); err != nil {
		t.Fatalf("get failed: %v\n%s", err, out)
	} else if out != "sk-or-live-999" {
		t.Fatalf("expected raw secret 'sk-or-live-999', got %q", out)
	}

	if out, err := runCLI("", "list"); err != nil {
		t.Fatalf("list failed: %v\n%s", err, out)
	} else if !strings.Contains(out, "OPENROUTER_API_KEY") || strings.Contains(out, "sk-or-live-999") {
		t.Fatalf("list output invalid (must show key, never value): %q", out)
	}

	dotenvPath := filepath.Join(dir, ".env")
	dotenvContent := "API_KEY=%ENVCTL-OPENROUTER_API_KEY%\nAUTH=Bearer %ENVCTL-OPENROUTER_API_KEY%\nPLAIN=hello\n"
	if err := os.WriteFile(dotenvPath, []byte(dotenvContent), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	if out, err := runCLI("", "check", "--env-file", dotenvPath); err != nil {
		t.Fatalf("check failed: %v\n%s", err, out)
	}

	out, err := runCLI("", "run", "--env-file", dotenvPath, "--", "sh", "-c", `printf '%s|%s|%s' "$API_KEY" "$AUTH" "$PLAIN"`)
	if err != nil {
		t.Fatalf("run failed: %v\n%s", err, out)
	}
	if out != "sk-or-live-999|Bearer sk-or-live-999|hello" {
		t.Fatalf("unexpected run output: %q", out)
	}

	if out, err := runCLI("", "lock"); err != nil {
		t.Fatalf("lock failed: %v\n%s", err, out)
	}
	if _, err := runCLI("", "get", "OPENROUTER_API_KEY"); err == nil {
		t.Fatalf("expected get after lock to fail")
	}
}
