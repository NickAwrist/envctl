package daemon

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/NickAwrist/envctl/internal/config"
	"github.com/NickAwrist/envctl/internal/crypto"
	"github.com/NickAwrist/envctl/internal/protocol"
	"github.com/NickAwrist/envctl/internal/vault"
)

var testKDF = crypto.KDFParams{Time: 1, Memory: 8 * 1024, Threads: 1}

func startTestServer(t *testing.T) (*Server, *Client, string) {
	t.Helper()
	vaultPath := filepath.Join(t.TempDir(), "data", "vault.enc")

	sockDir, err := os.MkdirTemp("/tmp", "envctl-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })
	sockPath := filepath.Join(sockDir, "a.sock")

	if err := vault.InitWithParams(vaultPath, []byte("test-pass"), testKDF); err != nil {
		t.Fatalf("InitWithParams: %v", err)
	}
	srv := NewServer(sockPath, vaultPath)
	srv.SetInitialIdleTimeout(10 * time.Second)
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	done := make(chan struct{})
	go func() {
		_ = srv.Serve()
		close(done)
	}()
	t.Cleanup(func() {
		srv.Shutdown()
		<-done
	})
	return srv, NewClient(sockPath), sockPath
}

func TestDaemonCRUDAndFixedTTL(t *testing.T) {
	_, client, _ := startTestServer(t)

	// Locked initially.
	if _, _, err := client.Get([]string{"API_KEY"}); !errors.Is(err, ErrLocked) {
		t.Fatalf("expected ErrLocked before unlock, got %v", err)
	}

	// Wrong password rejected.
	if _, err := client.Unlock("wrong", config.UnlockModeTTL, time.Minute, 0); err == nil {
		t.Fatalf("expected wrong password to fail")
	}

	// Unlock with a short fixed TTL (250ms).
	if _, err := client.Unlock("test-pass", config.UnlockModeTTL, 250*time.Millisecond, 0); err != nil {
		t.Fatalf("Unlock: %v", err)
	}

	if err := client.Set("OPENROUTER_API_KEY", "sk-or-abc", "OpenRouter"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	vals, missing, err := client.Get([]string{"OPENROUTER_API_KEY", "DOES_NOT_EXIST"})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if vals["OPENROUTER_API_KEY"] != "sk-or-abc" {
		t.Fatalf("unexpected value: %q", vals["OPENROUTER_API_KEY"])
	}
	if len(missing) != 1 || missing[0] != "DOES_NOT_EXIST" {
		t.Fatalf("unexpected missing: %v", missing)
	}

	entries, err := client.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 || entries[0].Key != "OPENROUTER_API_KEY" || entries[0].Description != "OpenRouter" {
		t.Fatalf("unexpected list entries: %+v", entries)
	}

	// Reading midway must NOT extend the fixed TTL.
	time.Sleep(120 * time.Millisecond)
	if _, _, err := client.Get([]string{"OPENROUTER_API_KEY"}); err != nil {
		t.Fatalf("expected secret still readable before TTL expiry: %v", err)
	}

	// Wait past fixed 250ms TTL: daemon should shut down.
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		_, err := client.Status()
		if errors.Is(err, ErrDaemonUnavailable) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("expected daemon to expire and shut down after fixed TTL")
}

func TestDaemonSessionPIDAutoLocksOnExit(t *testing.T) {
	_, client, _ := startTestServer(t)

	// Start a dummy child process standing in for an SSH login shell.
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	st, err := client.Unlock("test-pass", config.UnlockModeSession, 0, cmd.Process.Pid)
	if err != nil {
		t.Fatalf("Unlock session mode: %v", err)
	}
	if st.Mode != config.UnlockModeSession || st.SessionPID != cmd.Process.Pid {
		t.Fatalf("unexpected status: %+v", st)
	}

	if err := client.Set("K", "V", ""); err != nil {
		t.Fatalf("Set while session alive: %v", err)
	}

	// End the watched session process.
	_ = cmd.Process.Kill()
	_ = cmd.Wait()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_, err := client.Status()
		if errors.Is(err, ErrDaemonUnavailable) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("expected daemon to auto-lock and exit when watched session PID exited")
}

func TestDaemonUntilLockAndDelete(t *testing.T) {
	_, client, _ := startTestServer(t)

	if _, err := client.Unlock("test-pass", config.UnlockModeUntilLock, 0, 0); err != nil {
		t.Fatalf("Unlock until_lock: %v", err)
	}
	if err := client.Set("TEMP", "123", ""); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := client.Delete("TEMP"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	err := client.Delete("TEMP")
	var remoteErr *RemoteError
	if !errors.As(err, &remoteErr) || remoteErr.Code != protocol.ErrCodeNotFound {
		t.Fatalf("expected NOT_FOUND on second delete, got %v", err)
	}
	if err := client.Lock(); err != nil {
		t.Fatalf("Lock: %v", err)
	}

	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		_, err := client.Status()
		if errors.Is(err, ErrDaemonUnavailable) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("expected daemon to stop after Lock()")
}

// A mistyped ENVCTL_SOCKET must never cost the user the file it points at.
func TestStartRefusesToReplaceNonSocketFile(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "envctl-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	precious := filepath.Join(dir, "vault.enc")
	if err := os.WriteFile(precious, []byte("do not delete"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	srv := NewServer(precious, filepath.Join(dir, "unused.enc"))
	if err := srv.Start(); err == nil {
		srv.Shutdown()
		t.Fatalf("expected Start to refuse a socket path that is a regular file")
	}
	if data, err := os.ReadFile(precious); err != nil || string(data) != "do not delete" {
		t.Fatalf("file at socket path was modified: %q, %v", data, err)
	}
}

func TestDaemonRejectsClientExpectingAnotherVault(t *testing.T) {
	srv, client, _ := startTestServer(t)
	if _, err := client.Unlock("test-pass", config.UnlockModeUntilLock, 0, 0); err != nil {
		t.Fatalf("Unlock: %v", err)
	}

	client.VaultPath = filepath.Join(t.TempDir(), "other", "vault.enc")
	err := client.Set("K", "V", "")
	var remoteErr *RemoteError
	if !errors.As(err, &remoteErr) || remoteErr.Code != protocol.ErrCodeInvalidRequest {
		t.Fatalf("expected INVALID_REQUEST for a different vault, got %v", err)
	}

	client.VaultPath = srv.vaultPath
	if err := client.Set("K", "V", ""); err != nil {
		t.Fatalf("Set with the served vault path: %v", err)
	}
}
