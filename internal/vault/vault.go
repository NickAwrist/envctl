package vault

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
	"time"

	"github.com/NickAwrist/envctl/internal/crypto"
	"github.com/NickAwrist/envctl/internal/paths"
)

const (
	CurrentVersion = 1
	kdfAlgArgon2id = "argon2id"
)

var (
	aadDEK     = []byte("envctl:v1:dek")
	aadPayload = []byte("envctl:v1:payload")
	keyNameRe  = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_./-]{0,127}$`)

	ErrVaultExists     = errors.New("vault already exists")
	ErrVaultNotFound   = errors.New("vault not found")
	ErrInvalidPassword = errors.New("invalid password")
	ErrInvalidKeyName  = errors.New("invalid secret key name")
)

type KDFMetadata struct {
	Algorithm string `json:"alg"`
	Salt      string `json:"salt"`
	Time      uint32 `json:"time"`
	Memory    uint32 `json:"memory"`
	Threads   uint8  `json:"threads"`
}

type CipherBlob struct {
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

type Envelope struct {
	Version    int         `json:"version"`
	KDF        KDFMetadata `json:"kdf"`
	WrappedDEK CipherBlob  `json:"wrapped_dek"`
	Payload    CipherBlob  `json:"payload"`
}

type Entry struct {
	Value       string    `json:"value"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Payload struct {
	Secrets map[string]Entry `json:"secrets"`
}

func ValidateKeyName(key string) error {
	if !keyNameRe.MatchString(key) {
		return fmt.Errorf("%w %q: use 1-128 characters from [A-Za-z0-9_./-]", ErrInvalidKeyName, key)
	}
	return nil
}

func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func Init(path string, password []byte) error {
	return InitWithParams(path, password, crypto.DefaultKDFParams())
}

func InitWithParams(path string, password []byte, params crypto.KDFParams) error {
	if len(password) == 0 {
		return errors.New("password cannot be empty")
	}
	if Exists(path) {
		return fmt.Errorf("%w at %s", ErrVaultExists, path)
	}

	dek, err := crypto.RandomBytes(crypto.KeySize)
	if err != nil {
		return err
	}
	defer crypto.Zero(dek)

	kdf, wrappedDEK, err := wrapDEK(dek, password, params)
	if err != nil {
		return err
	}
	payload, err := sealPayload(dek, &Payload{})
	if err != nil {
		return err
	}
	return writeEnvelopeAtomic(path, Envelope{
		Version:    CurrentVersion,
		KDF:        kdf,
		WrappedDEK: wrappedDEK,
		Payload:    payload,
	})
}

// wrapDEK encrypts dek under a key derived from password and a fresh salt.
func wrapDEK(dek, password []byte, params crypto.KDFParams) (KDFMetadata, CipherBlob, error) {
	salt, err := crypto.RandomBytes(crypto.SaltSize)
	if err != nil {
		return KDFMetadata{}, CipherBlob{}, err
	}
	kek := crypto.DeriveKey(password, salt, params)
	defer crypto.Zero(kek)

	wrapped, err := seal(kek, dek, aadDEK)
	if err != nil {
		return KDFMetadata{}, CipherBlob{}, err
	}
	return KDFMetadata{
		Algorithm: kdfAlgArgon2id,
		Salt:      base64.StdEncoding.EncodeToString(salt),
		Time:      params.Time,
		Memory:    params.Memory,
		Threads:   params.Threads,
	}, wrapped, nil
}

func seal(key, plaintext, aad []byte) (CipherBlob, error) {
	nonce, ciphertext, err := crypto.Encrypt(key, plaintext, aad)
	if err != nil {
		return CipherBlob{}, err
	}
	return CipherBlob{
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
	}, nil
}

func (b CipherBlob) open(key, aad []byte) ([]byte, error) {
	nonce, err := base64.StdEncoding.DecodeString(b.Nonce)
	if err != nil {
		return nil, fmt.Errorf("decode nonce: %w", err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(b.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decode ciphertext: %w", err)
	}
	return crypto.Decrypt(key, nonce, ciphertext, aad)
}

func UnwrapDEK(path string, password []byte) ([]byte, error) {
	env, err := readEnvelope(path)
	if err != nil {
		return nil, err
	}
	if env.KDF.Algorithm != kdfAlgArgon2id {
		return nil, fmt.Errorf("unsupported KDF algorithm %q", env.KDF.Algorithm)
	}
	salt, err := base64.StdEncoding.DecodeString(env.KDF.Salt)
	if err != nil {
		return nil, fmt.Errorf("decode salt: %w", err)
	}

	kek := crypto.DeriveKey(password, salt, crypto.KDFParams{
		Time:    env.KDF.Time,
		Memory:  env.KDF.Memory,
		Threads: env.KDF.Threads,
	})
	defer crypto.Zero(kek)

	dek, err := env.WrappedDEK.open(kek, aadDEK)
	if errors.Is(err, crypto.ErrAuthFailed) {
		return nil, ErrInvalidPassword
	}
	if err != nil {
		return nil, fmt.Errorf("unwrap DEK: %w", err)
	}
	return dek, nil
}

func ReadPayload(path string, dek []byte) (*Payload, error) {
	env, err := readEnvelope(path)
	if err != nil {
		return nil, err
	}
	plaintext, err := env.Payload.open(dek, aadPayload)
	if err != nil {
		return nil, fmt.Errorf("decrypt vault payload: %w", err)
	}
	defer crypto.Zero(plaintext)

	var payload Payload
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		return nil, fmt.Errorf("parse vault payload: %w", err)
	}
	if payload.Secrets == nil {
		payload.Secrets = map[string]Entry{}
	}
	return &payload, nil
}

// UpdatePayload applies mutate to the decrypted payload and writes the result
// back, holding the vault lock so no other process can change the file between
// the read and the write. If mutate returns an error nothing is written.
func UpdatePayload(path string, dek []byte, mutate func(*Payload) error) error {
	return withLock(path, func() error {
		payload, err := ReadPayload(path, dek)
		if err != nil {
			return err
		}
		if err := mutate(payload); err != nil {
			return err
		}
		return writePayload(path, dek, payload)
	})
}

func writePayload(path string, dek []byte, payload *Payload) error {
	env, err := readEnvelope(path)
	if err != nil {
		return err
	}
	env.Payload, err = sealPayload(dek, payload)
	if err != nil {
		return err
	}
	return writeEnvelopeAtomic(path, env)
}

func sealPayload(dek []byte, payload *Payload) (CipherBlob, error) {
	if payload.Secrets == nil {
		payload.Secrets = map[string]Entry{}
	}
	plaintext, err := json.Marshal(payload)
	if err != nil {
		return CipherBlob{}, fmt.Errorf("marshal vault payload: %w", err)
	}
	defer crypto.Zero(plaintext)
	return seal(dek, plaintext, aadPayload)
}

func ChangePassword(path string, oldPassword, newPassword []byte) error {
	return ChangePasswordWithParams(path, oldPassword, newPassword, crypto.DefaultKDFParams())
}

// ChangePasswordWithParams re-encrypts the vault under a new data key wrapped
// by newPassword. The old password and the old data key stop working, so a DEK
// held by a running daemon no longer opens the vault.
func ChangePasswordWithParams(path string, oldPassword, newPassword []byte, params crypto.KDFParams) error {
	if len(newPassword) == 0 {
		return errors.New("new password cannot be empty")
	}
	oldDEK, err := UnwrapDEK(path, oldPassword)
	if err != nil {
		return err
	}
	defer crypto.Zero(oldDEK)

	newDEK, err := crypto.RandomBytes(crypto.KeySize)
	if err != nil {
		return err
	}
	defer crypto.Zero(newDEK)
	kdf, wrappedDEK, err := wrapDEK(newDEK, newPassword, params)
	if err != nil {
		return err
	}

	// Key derivation is slow, so only the re-encryption happens under the lock.
	// It reads the payload as it is now, which keeps any secret written while
	// the keys were being derived.
	return withLock(path, func() error {
		env, err := readEnvelope(path)
		if err != nil {
			return err
		}
		plaintext, err := env.Payload.open(oldDEK, aadPayload)
		if err != nil {
			return fmt.Errorf("decrypt vault payload: %w", err)
		}
		defer crypto.Zero(plaintext)

		env.Payload, err = seal(newDEK, plaintext, aadPayload)
		if err != nil {
			return err
		}
		env.KDF = kdf
		env.WrappedDEK = wrappedDEK
		return writeEnvelopeAtomic(path, env)
	})
}

// withLock runs fn while holding an exclusive lock on the vault, serializing
// read-modify-write cycles across processes (the daemon and 'envctl passwd').
// The lock lives on a sibling file because the vault itself is replaced by
// rename on every write.
func withLock(path string, fn func() error) error {
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open vault lock: %w", err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("lock vault: %w", err)
	}
	return fn()
}

func readEnvelope(path string) (Envelope, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Envelope{}, fmt.Errorf("%w at %s (run 'envctl init' first)", ErrVaultNotFound, path)
		}
		return Envelope{}, fmt.Errorf("read vault: %w", err)
	}
	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return Envelope{}, fmt.Errorf("parse vault envelope: %w", err)
	}
	if env.Version != CurrentVersion {
		return Envelope{}, fmt.Errorf("unsupported vault version %d", env.Version)
	}
	return env, nil
}

func writeEnvelopeAtomic(path string, env Envelope) error {
	dir := filepath.Dir(path)
	if err := paths.EnsurePrivateDir(dir); err != nil {
		return err
	}
	data, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal vault envelope: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, ".vault.enc.tmp-*")
	if err != nil {
		return fmt.Errorf("create temp vault: %w", err)
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
		return fmt.Errorf("replace vault file: %w", err)
	}
	cleanup = false
	return nil
}
