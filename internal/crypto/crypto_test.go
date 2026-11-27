package crypto

import (
	"bytes"
	"encoding/hex"
	"errors"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key, err := RandomBytes(KeySize)
	if err != nil {
		t.Fatalf("RandomBytes: %v", err)
	}
	plaintext, aad := []byte("sk-or-super-secret-value-123"), []byte("envctl:v1:payload")

	nonce, ciphertext, err := Encrypt(key, plaintext, aad)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if bytes.Contains(ciphertext, plaintext) {
		t.Fatalf("ciphertext contains the plaintext")
	}
	got, err := Decrypt(key, nonce, ciphertext, aad)
	if err != nil || !bytes.Equal(got, plaintext) {
		t.Fatalf("Decrypt = %q, %v", got, err)
	}

	// A reused nonce under the same key breaks AES-GCM, so every call needs its own.
	nonce2, ciphertext2, err := Encrypt(key, plaintext, aad)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if bytes.Equal(nonce, nonce2) || bytes.Equal(ciphertext, ciphertext2) {
		t.Fatalf("two encryptions of the same plaintext produced the same output")
	}
}

// Decrypt must fail when the key, nonce, ciphertext, or associated data differ
// from what Encrypt used. This is what detects a wrong password and a tampered
// or swapped vault field.
func TestDecryptRejectsAnyMismatch(t *testing.T) {
	key, err := RandomBytes(KeySize)
	if err != nil {
		t.Fatalf("RandomBytes: %v", err)
	}
	aad := []byte("envctl:v1:dek")
	nonce, ciphertext, err := Encrypt(key, []byte("data key"), aad)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	flip := func(b []byte) []byte {
		out := bytes.Clone(b)
		out[0] ^= 1
		return out
	}

	cases := map[string][4][]byte{
		"wrong key":           {flip(key), nonce, ciphertext, aad},
		"wrong nonce":         {key, flip(nonce), ciphertext, aad},
		"tampered ciphertext": {key, nonce, flip(ciphertext), aad},
		"truncated":           {key, nonce, ciphertext[:len(ciphertext)-1], aad},
		"wrong aad":           {key, nonce, ciphertext, []byte("envctl:v1:payload")},
	}
	for name, c := range cases {
		if _, err := Decrypt(c[0], c[1], c[2], c[3]); !errors.Is(err, ErrAuthFailed) {
			t.Errorf("%s: expected ErrAuthFailed, got %v", name, err)
		}
	}
}

// The derived key for a given password, salt, and parameters must never change,
// or existing vaults stop unlocking.
func TestDeriveKeyIsStable(t *testing.T) {
	params := KDFParams{Time: 1, Memory: 8 * 1024, Threads: 1}
	key := DeriveKey([]byte("correct horse"), []byte("0123456789abcdef"), params)
	const want = "eb8cf0e32ea118d4865b51d32836a373be04224269089f805015c3b29cab1445"
	if got := hex.EncodeToString(key); got != want {
		t.Fatalf("DeriveKey = %s, want %s", got, want)
	}

	otherSalt := DeriveKey([]byte("correct horse"), []byte("fedcba9876543210"), params)
	otherPassword := DeriveKey([]byte("correct horsf"), []byte("0123456789abcdef"), params)
	if bytes.Equal(key, otherSalt) || bytes.Equal(key, otherPassword) {
		t.Fatalf("derived key does not depend on both the password and the salt")
	}
}
