package protocol

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/NickAwrist/envctl/internal/config"
)

const (
	Version        = 1
	MaxMessageSize = 16 * 1024 * 1024
)

const (
	OpStatus = "status"
	OpUnlock = "unlock"
	OpLock   = "lock"
	OpGet    = "get"
	OpList   = "list"
	OpSet    = "set"
	OpDelete = "delete"
)

const (
	ErrCodeLocked          = "LOCKED"
	ErrCodeInvalidPassword = "INVALID_PASSWORD"
	ErrCodeInvalidRequest  = "INVALID_REQUEST"
	ErrCodeVersionMismatch = "VERSION_MISMATCH"
	ErrCodeNotFound        = "NOT_FOUND"
	ErrCodeInternal        = "INTERNAL"
)

type Request struct {
	V           int               `json:"v"`
	Op          string            `json:"op"`
	Password    string            `json:"password,omitempty"`
	Mode        config.UnlockMode `json:"mode,omitempty"`
	TTLMs       int64             `json:"ttl_ms,omitempty"`
	SessionPID  int               `json:"session_pid,omitempty"`
	Keys        []string          `json:"keys,omitempty"`
	Key         string            `json:"key,omitempty"`
	Value       string            `json:"value,omitempty"`
	Description string            `json:"description,omitempty"`
}

type StatusInfo struct {
	Locked     bool              `json:"locked"`
	Mode       config.UnlockMode `json:"mode,omitempty"`
	ExpiresAt  *time.Time        `json:"expires_at,omitempty"`
	SessionPID int               `json:"session_pid,omitempty"`
	VaultPath  string            `json:"vault_path,omitempty"`
}

type EntryMeta struct {
	Key         string    `json:"key"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Response struct {
	V         int               `json:"v"`
	OK        bool              `json:"ok"`
	ErrorCode string            `json:"error_code,omitempty"`
	Error     string            `json:"error,omitempty"`
	Status    *StatusInfo       `json:"status,omitempty"`
	Values    map[string]string `json:"values,omitempty"`
	Missing   []string          `json:"missing,omitempty"`
	Entries   []EntryMeta       `json:"entries,omitempty"`
}

func ReadMessage(r *bufio.Reader, dst any) error {
	line, err := r.ReadBytes('\n')
	if err != nil {
		if err == io.EOF && len(line) > 0 {
			// Allow final line without trailing newline if stream closed cleanly.
		} else {
			return err
		}
	}
	if len(line) > MaxMessageSize {
		return fmt.Errorf("message exceeds %d bytes", MaxMessageSize)
	}
	if err := json.Unmarshal(line, dst); err != nil {
		return fmt.Errorf("decode message: %w", err)
	}
	return nil
}

func WriteMessage(w io.Writer, msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("encode message: %w", err)
	}
	data = append(data, '\n')
	_, err = w.Write(data)
	return err
}
