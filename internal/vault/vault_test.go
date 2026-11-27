package vault

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NickAwrist/envctl/internal/crypto"
)

var fastKDF = crypto.KDFParams{Time: 1, Memory: 8 * 1024, Threads: 1}

func TestVaultLifecycleAndPasswordRotation(t *testing.T) {
	vaultPath := filepath.Join(t.TempDir(), "data", "vault.enc")

	pw1 := []byte("initial-master-password")
	if err := InitWithParams(vaultPath, pw1, fastKDF); err != nil {
		t.Fatalf("InitWithParams: %v", err)
	}

	// Verify file permissions are 0600.
	info, err := os.Stat(vaultPath)
	if err != nil {
		t.Fatalf("stat vault: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("expected 0600 permissions, got %o", perm)
	}

	// Wrong password must fail with ErrInvalidPassword.
	if _, err := UnwrapDEK(vaultPath, []byte("wrong-password")); !errors.Is(err, ErrInvalidPassword) {
		t.Fatalf("expected ErrInvalidPassword, got %v", err)
	}

	// Right password unwraps DEK and allows writing + reading secrets.
	dek, err := UnwrapDEK(vaultPath, pw1)
	if err != nil {
		t.Fatalf("UnwrapDEK: %v", err)
	}
	defer crypto.Zero(dek)

	now := time.Now().UTC()
	err = UpdatePayload(vaultPath, dek, func(p *Payload) error {
		p.Secrets["OPENROUTER_API_KEY"] = Entry{
			Value:       "sk-or-super-secret-value-123",
			Description: "Shared OpenRouter key",
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		return nil
	})
	if err != nil {
		t.Fatalf("UpdatePayload: %v", err)
	}

	// On-disk file must never contain plaintext secret key or value.
	raw, err := os.ReadFile(vaultPath)
	if err != nil {
		t.Fatalf("read raw vault: %v", err)
	}
	if strings.Contains(string(raw), "sk-or-super-secret-value-123") || strings.Contains(string(raw), "OPENROUTER_API_KEY") {
		t.Fatalf("vault file contains plaintext secret data")
	}

	got, err := ReadPayload(vaultPath, dek)
	if err != nil {
		t.Fatalf("ReadPayload: %v", err)
	}
	if got.Secrets["OPENROUTER_API_KEY"].Value != "sk-or-super-secret-value-123" {
		t.Fatalf("unexpected secret value: %q", got.Secrets["OPENROUTER_API_KEY"].Value)
	}

	// Rotate password: old password fails, new password unwraps same DEK and reads secret.
	pw2 := []byte("rotated-master-password")
	if err := ChangePasswordWithParams(vaultPath, pw1, pw2, fastKDF); err != nil {
		t.Fatalf("ChangePasswordWithParams: %v", err)
	}
	if _, err := UnwrapDEK(vaultPath, pw1); !errors.Is(err, ErrInvalidPassword) {
		t.Fatalf("expected old password to fail, got %v", err)
	}
	dek2, err := UnwrapDEK(vaultPath, pw2)
	if err != nil {
		t.Fatalf("UnwrapDEK with new password: %v", err)
	}
	defer crypto.Zero(dek2)

	got2, err := ReadPayload(vaultPath, dek2)
	if err != nil {
		t.Fatalf("ReadPayload after password change: %v", err)
	}
	if got2.Secrets["OPENROUTER_API_KEY"].Value != "sk-or-super-secret-value-123" {
		t.Fatalf("secret corrupted after password rotation")
	}
}

// A password change racing secret writes must lose neither: every stored
// secret survives and only the newest password unlocks the vault.
func TestPasswordChangeDoesNotRaceSecretWrites(t *testing.T) {
	vaultPath := filepath.Join(t.TempDir(), "data", "vault.enc")
	if err := InitWithParams(vaultPath, []byte("pw-0"), fastKDF); err != nil {
		t.Fatalf("InitWithParams: %v", err)
	}
	dek, err := UnwrapDEK(vaultPath, []byte("pw-0"))
	if err != nil {
		t.Fatalf("UnwrapDEK: %v", err)
	}
	defer crypto.Zero(dek)

	const writes, rotations = 200, 20
	var wg sync.WaitGroup
	wg.Add(2)
	errC := make(chan error, 2)
	go func() {
		defer wg.Done()
		for i := range writes {
			err := UpdatePayload(vaultPath, dek, func(p *Payload) error {
				p.Secrets[fmt.Sprintf("KEY_%d", i)] = Entry{Value: "v"}
				return nil
			})
			if err != nil {
				errC <- fmt.Errorf("UpdatePayload %d: %w", i, err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := range rotations {
			oldPw, newPw := fmt.Sprintf("pw-%d", i), fmt.Sprintf("pw-%d", i+1)
			if err := ChangePasswordWithParams(vaultPath, []byte(oldPw), []byte(newPw), fastKDF); err != nil {
				errC <- fmt.Errorf("ChangePassword %d: %w", i, err)
				return
			}
		}
	}()
	wg.Wait()
	close(errC)
	for err := range errC {
		t.Fatal(err)
	}

	if _, err := UnwrapDEK(vaultPath, []byte("pw-0")); !errors.Is(err, ErrInvalidPassword) {
		t.Fatalf("expected the original password to stop working, got %v", err)
	}
	if _, err := UnwrapDEK(vaultPath, fmt.Appendf(nil, "pw-%d", rotations)); err != nil {
		t.Fatalf("expected the newest password to work: %v", err)
	}
	payload, err := ReadPayload(vaultPath, dek)
	if err != nil {
		t.Fatalf("ReadPayload: %v", err)
	}
	if len(payload.Secrets) != writes {
		t.Fatalf("expected %d secrets after concurrent password changes, got %d", writes, len(payload.Secrets))
	}
}

func TestValidateKeyName(t *testing.T) {
	valid := []string{"API_KEY", "openrouter/api_key", "stripe.test-key_1"}
	for _, k := range valid {
		if err := ValidateKeyName(k); err != nil {
			t.Errorf("expected %q to be valid: %v", k, err)
		}
	}
	invalid := []string{"", "BAD KEY", "NO%PERCENT", "BAD=SIGN"}
	for _, k := range invalid {
		if err := ValidateKeyName(k); err == nil {
			t.Errorf("expected %q to be invalid", k)
		}
	}
}
