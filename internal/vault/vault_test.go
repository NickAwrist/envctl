package vault

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NickAwrist/envctl/internal/crypto"
)

var fastKDF = crypto.KDFParams{Time: 1, Memory: 8 * 1024, Threads: 1}

func TestVaultLifecycleAndPasswordRotation(t *testing.T) {
	dir := t.TempDir()
	vaultPath := filepath.Join(dir, "vault.enc")

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
	p := &Payload{
		Secrets: map[string]Entry{
			"OPENROUTER_API_KEY": {
				Value:       "sk-or-super-secret-value-123",
				Description: "Shared OpenRouter key",
				CreatedAt:   now,
				UpdatedAt:   now,
			},
		},
	}
	if err := WritePayload(vaultPath, dek, p); err != nil {
		t.Fatalf("WritePayload: %v", err)
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
