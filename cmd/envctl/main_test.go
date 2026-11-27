package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func buildCLI(t *testing.T, dir string) string {
	t.Helper()
	bin := filepath.Join(dir, "envctl")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

// daemonPID finds the daemon listening on socketPath by its command line.
func daemonPID(t *testing.T, socketPath string) string {
	t.Helper()
	cmdlines, _ := filepath.Glob("/proc/[0-9]*/cmdline")
	for _, path := range cmdlines {
		data, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(data), "__daemon\x00--socket\x00"+socketPath+"\x00") {
			return filepath.Base(filepath.Dir(path))
		}
	}
	t.Fatalf("no envctl daemon found for %s", socketPath)
	return ""
}

func TestCLIEndToEnd(t *testing.T) {
	dir := t.TempDir()
	bin := buildCLI(t, dir)

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

	// The daemon holds the key, so it marks itself non-dumpable. That shows as
	// /proc/<pid>/environ being unreadable to this user, which Yama's ptrace
	// restrictions do not cause on their own. Root can always read it.
	if os.Getuid() != 0 {
		pid := daemonPID(t, filepath.Join(sockDir, "a.sock"))
		if _, err := os.Open(filepath.Join("/proc", pid, "environ")); !errors.Is(err, os.ErrPermission) {
			t.Fatalf("expected the daemon to be non-dumpable, got %v", err)
		}
	}

	// --help prints usage and succeeds instead of being parsed as a flag error or a key.
	for _, sub := range []string{"set", "get", "rm", "list", "lock", "status", "check", "run", "config", "unlock"} {
		if out, err := runCLI("", sub, "--help"); err != nil || !strings.Contains(out, "envctl "+sub) {
			t.Fatalf("%s --help: err=%v out=%q", sub, err, out)
		}
	}

	// Piped input without --stdin is stored whole, and an empty value is refused
	// rather than overwriting the secret.
	if out, err := runCLI("line1\nline2\n", "set", "MULTILINE"); err != nil {
		t.Fatalf("piped set failed: %v\n%s", err, out)
	}
	if out, _ := runCLI("", "get", "MULTILINE"); out != "line1\nline2" {
		t.Fatalf("expected both piped lines to be stored, got %q", out)
	}
	if out, err := runCLI("", "set", "MULTILINE"); err == nil {
		t.Fatalf("expected empty value to be rejected, got %q", out)
	}
	if out, _ := runCLI("", "get", "MULTILINE"); out != "line1\nline2" {
		t.Fatalf("rejected empty set changed the stored value to %q", out)
	}
	if out, err := runCLI("", "set", "TOKEN=hunter2-value"); err == nil || strings.Contains(out, "hunter2-value") {
		t.Fatalf("set KEY=VALUE must fail without echoing the value: err=%v out=%q", err, out)
	}

	if out, err := runCLI("", "check", dotenvPath); err == nil {
		t.Fatalf("expected a positional file to be rejected by check, got %q", out)
	}
	typoPath := filepath.Join(dir, "typo.env")
	if err := os.WriteFile(typoPath, []byte("API_KEY=%ENVCTL-OPENROUTER_API_KEY\n"), 0o600); err != nil {
		t.Fatalf("write typo.env: %v", err)
	}
	if out, err := runCLI("", "check", "--env-file", typoPath); err == nil || !strings.Contains(out, "API_KEY") {
		t.Fatalf("expected check to flag the unterminated reference: err=%v out=%q", err, out)
	}

	// run hands the command its own exit status and signals, and resolves
	// references that arrive through the environment.
	runStatus := func(extraEnv string, args ...string) (string, syscall.WaitStatus) {
		cmd := exec.Command(bin, append([]string{"run", "--env-file", dotenvPath, "--"}, args...)...)
		cmd.Env = append(env, extraEnv)
		out, _ := cmd.CombinedOutput()
		return string(out), cmd.ProcessState.Sys().(syscall.WaitStatus)
	}
	if out, ws := runStatus("FROM_ENV=%ENVCTL-OPENROUTER_API_KEY%", "sh", "-c", `printf %s "$FROM_ENV"; exit 3`); ws.ExitStatus() != 3 || out != "sk-or-live-999" {
		t.Fatalf("run: exit=%d out=%q", ws.ExitStatus(), out)
	}
	if _, ws := runStatus("X=1", "sh", "-c", "kill -TERM $$"); !ws.Signaled() || ws.Signal() != syscall.SIGTERM {
		t.Fatalf("expected the command's SIGTERM death to be visible, got %v", ws)
	}
	if out, ws := runStatus("X=1", "envctl-no-such-command"); ws.ExitStatus() != 127 {
		t.Fatalf("expected exit 127 for a missing command, got %d: %q", ws.ExitStatus(), out)
	}

	// passwd retires the data key the daemon holds, so it locks the vault. Only
	// the new password unlocks it, and the secrets survive.
	newPwFile := filepath.Join(dir, "new-pw.txt")
	if err := os.WriteFile(newPwFile, []byte("rotated-pass\n"), 0o600); err != nil {
		t.Fatalf("write new pw file: %v", err)
	}
	if out, err := runCLI("", "passwd", "--old-password-file", pwFile, "--new-password-file", newPwFile); err != nil {
		t.Fatalf("passwd failed: %v\n%s", err, out)
	}
	if out, err := runCLI("", "get", "OPENROUTER_API_KEY"); err == nil {
		t.Fatalf("expected the vault to be locked after passwd, got %q", out)
	}
	if out, err := runCLI("", "unlock", "--password-file", pwFile); err == nil {
		t.Fatalf("expected the old password to be rejected, got %q", out)
	}
	if out, err := runCLI("", "unlock", "--password-file", newPwFile); err != nil {
		t.Fatalf("unlock with the new password failed: %v\n%s", err, out)
	}
	if out, err := runCLI("", "get", "OPENROUTER_API_KEY"); err != nil || out != "sk-or-live-999" {
		t.Fatalf("secret lost after passwd: err=%v out=%q", err, out)
	}

	if out, err := runCLI("", "lock"); err != nil {
		t.Fatalf("lock failed: %v\n%s", err, out)
	}
	if _, err := runCLI("", "get", "OPENROUTER_API_KEY"); err == nil {
		t.Fatalf("expected get after lock to fail")
	}
}

func TestStatusReportsMissingVault(t *testing.T) {
	dir := t.TempDir()
	bin := buildCLI(t, dir)
	env := append(os.Environ(),
		"ENVCTL_CONFIG_DIR="+filepath.Join(dir, "config"),
		"ENVCTL_DATA_DIR="+filepath.Join(dir, "data"),
		"ENVCTL_SOCKET="+filepath.Join(dir, "sock", "a.sock"),
	)
	for _, args := range [][]string{{"status"}, {"list"}, {"get", "ANY"}} {
		cmd := exec.Command(bin, args...)
		cmd.Env = env
		out, _ := cmd.CombinedOutput()
		if strings.Contains(string(out), "locked") || !strings.Contains(string(out), "envctl init") {
			t.Fatalf("%v should point at 'envctl init', not report a locked vault: %q", args, out)
		}
	}
}
