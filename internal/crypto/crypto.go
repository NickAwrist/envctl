package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/argon2"
)

const (
	KeySize   = 32
	SaltSize  = 16
	NonceSize = 12
)

var ErrAuthFailed = errors.New("authentication failed")

type KDFParams struct {
	Time    uint32
	Memory  uint32 // KiB
	Threads uint8
}

func DefaultKDFParams() KDFParams {
	return KDFParams{
		Time:    3,
		Memory:  64 * 1024,
		Threads: 4,
	}
}

func DeriveKey(password, salt []byte, params KDFParams) []byte {
	return argon2.IDKey(password, salt, params.Time, params.Memory, params.Threads, KeySize)
}

func RandomBytes(n int) ([]byte, error) {
	buf := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return nil, fmt.Errorf("read random bytes: %w", err)
	}
	return buf, nil
}

func Encrypt(key, plaintext, aad []byte) (nonce, ciphertext []byte, err error) {
	if len(key) != KeySize {
		return nil, nil, fmt.Errorf("invalid key length: %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, fmt.Errorf("create gcm: %w", err)
	}
	nonce, err = RandomBytes(NonceSize)
	if err != nil {
		return nil, nil, err
	}
	ciphertext = gcm.Seal(nil, nonce, plaintext, aad)
	return nonce, ciphertext, nil
}

func Decrypt(key, nonce, ciphertext, aad []byte) ([]byte, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("invalid key length: %d", len(key))
	}
	if len(nonce) != NonceSize {
		return nil, fmt.Errorf("invalid nonce length: %d", len(nonce))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create gcm: %w", err)
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, ErrAuthFailed
	}
	return plaintext, nil
}

func Zero(b []byte) {
	clear(b)
}
