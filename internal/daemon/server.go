package daemon

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/NickAwrist/envctl/internal/config"
	"github.com/NickAwrist/envctl/internal/crypto"
	"github.com/NickAwrist/envctl/internal/paths"
	"github.com/NickAwrist/envctl/internal/protocol"
	"github.com/NickAwrist/envctl/internal/vault"
)

const defaultInitialIdleTimeout = 15 * time.Second

var errSecretNotFound = errors.New("secret not found")

type Server struct {
	socketPath         string
	vaultPath          string
	initialIdleTimeout time.Duration

	mu          sync.Mutex
	dek         []byte
	mode        config.UnlockMode
	expiresAt   *time.Time
	sessionPID  int
	gen         uint64
	cancelWatch context.CancelFunc
	closed      bool

	listener  *net.UnixListener
	shutdownC chan struct{}
	once      sync.Once
}

func NewServer(socketPath, vaultPath string) *Server {
	return &Server{
		socketPath:         socketPath,
		vaultPath:          vaultPath,
		initialIdleTimeout: defaultInitialIdleTimeout,
		shutdownC:          make(chan struct{}),
	}
}

func (s *Server) SetInitialIdleTimeout(d time.Duration) {
	s.initialIdleTimeout = d
}

// checkSocketPath verifies that a daemon may listen on socketPath, creating its
// directory if needed. It never replaces a file that is not a socket.
func checkSocketPath(socketPath string) error {
	if len(socketPath) >= 104 {
		return fmt.Errorf("socket path %q exceeds unix domain socket length limit (%d >= 104)", socketPath, len(socketPath))
	}
	if err := paths.EnsurePrivateDir(filepath.Dir(socketPath)); err != nil {
		return err
	}
	if info, err := os.Lstat(socketPath); err == nil && info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("%s exists and is not a unix socket; refusing to replace it", socketPath)
	}
	return nil
}

func (s *Server) Start() error {
	if err := checkSocketPath(s.socketPath); err != nil {
		return err
	}

	// Refuse to clobber a live daemon; clean up stale socket files.
	if _, err := os.Lstat(s.socketPath); err == nil {
		conn, dialErr := net.DialTimeout("unix", s.socketPath, 200*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			return fmt.Errorf("daemon already listening on %s", s.socketPath)
		}
		if err := os.Remove(s.socketPath); err != nil {
			return fmt.Errorf("remove stale socket %s: %w", s.socketPath, err)
		}
	}

	oldUmask := syscall.Umask(0o077)
	addr := &net.UnixAddr{Name: s.socketPath, Net: "unix"}
	ln, err := net.ListenUnix("unix", addr)
	syscall.Umask(oldUmask)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.socketPath, err)
	}
	if err := os.Chmod(s.socketPath, 0o600); err != nil {
		_ = ln.Close()
		_ = os.Remove(s.socketPath)
		return fmt.Errorf("chmod socket: %w", err)
	}

	s.mu.Lock()
	s.listener = ln
	if s.initialIdleTimeout > 0 {
		gen := s.gen
		ctx, cancel := context.WithCancel(context.Background())
		s.cancelWatch = cancel
		go func() {
			timer := time.NewTimer(s.initialIdleTimeout)
			defer timer.Stop()
			select {
			case <-timer.C:
				s.lockAndShutdown(gen)
			case <-ctx.Done():
			}
		}()
	}
	s.mu.Unlock()
	return nil
}

func (s *Server) Serve() error {
	for {
		conn, err := s.listener.AcceptUnix()
		if err != nil {
			select {
			case <-s.shutdownC:
				return nil
			default:
			}
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			continue
		}
		go s.handleConn(conn)
	}
}

func (s *Server) Shutdown() {
	s.once.Do(func() {
		s.mu.Lock()
		s.closed = true
		if s.cancelWatch != nil {
			s.cancelWatch()
			s.cancelWatch = nil
		}
		if s.dek != nil {
			crypto.Zero(s.dek)
			s.dek = nil
		}
		s.expiresAt = nil
		s.sessionPID = 0
		ln := s.listener
		s.mu.Unlock()

		close(s.shutdownC)
		if ln != nil {
			_ = ln.Close()
			_ = os.Remove(s.socketPath)
		}
	})
}

func (s *Server) lockAndShutdown(expectedGen uint64) {
	s.mu.Lock()
	if s.closed || s.gen != expectedGen {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	s.Shutdown()
}

func (s *Server) handleConn(conn *net.UnixConn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))

	if err := verifyPeerUID(conn); err != nil {
		_ = protocol.WriteMessage(conn, protocol.Response{
			V:         protocol.Version,
			OK:        false,
			ErrorCode: protocol.ErrCodeInvalidRequest,
			Error:     err.Error(),
		})
		return
	}

	reader := bufio.NewReader(io.LimitReader(conn, protocol.MaxMessageSize+1))
	var req protocol.Request
	if err := protocol.ReadMessage(reader, &req); err != nil {
		_ = protocol.WriteMessage(conn, protocol.Response{
			V:         protocol.Version,
			OK:        false,
			ErrorCode: protocol.ErrCodeInvalidRequest,
			Error:     err.Error(),
		})
		return
	}

	resp, shouldShutdown := s.dispatch(req)
	_ = protocol.WriteMessage(conn, resp)
	if shouldShutdown {
		s.Shutdown()
	}
}

func (s *Server) dispatch(req protocol.Request) (protocol.Response, bool) {
	if req.V != protocol.Version {
		return protocol.Response{
			V:         protocol.Version,
			OK:        false,
			ErrorCode: protocol.ErrCodeVersionMismatch,
			Error:     fmt.Sprintf("unsupported protocol version %d (daemon supports %d)", req.V, protocol.Version),
		}, false
	}

	if req.Vault != "" && !s.servesVault(req.Vault) {
		return errResp(protocol.ErrCodeInvalidRequest, fmt.Sprintf(
			"the daemon on %s serves vault %s, not %s; use a separate ENVCTL_SOCKET for each vault",
			s.socketPath, s.vaultPath, req.Vault)), false
	}

	switch req.Op {
	case protocol.OpStatus:
		return s.handleStatus(), false
	case protocol.OpUnlock:
		return s.handleUnlock(req), false
	case protocol.OpLock:
		return protocol.Response{V: protocol.Version, OK: true}, true
	case protocol.OpGet:
		return s.handleGet(req), false
	case protocol.OpList:
		return s.handleList(), false
	case protocol.OpSet:
		return s.handleSet(req), false
	case protocol.OpDelete:
		return s.handleDelete(req), false
	default:
		return protocol.Response{
			V:         protocol.Version,
			OK:        false,
			ErrorCode: protocol.ErrCodeInvalidRequest,
			Error:     fmt.Sprintf("unknown operation %q", req.Op),
		}, false
	}
}

func (s *Server) servesVault(path string) bool {
	if path == s.vaultPath {
		return true
	}
	want, err := os.Stat(path)
	if err != nil {
		return false
	}
	have, err := os.Stat(s.vaultPath)
	return err == nil && os.SameFile(want, have)
}

func (s *Server) handleStatus() protocol.Response {
	s.mu.Lock()
	defer s.mu.Unlock()

	info := &protocol.StatusInfo{
		Locked:    s.dek == nil,
		VaultPath: s.vaultPath,
	}
	if s.dek != nil {
		info.Mode = s.mode
		info.SessionPID = s.sessionPID
		if s.expiresAt != nil {
			t := *s.expiresAt
			info.ExpiresAt = &t
		}
	}
	return protocol.Response{
		V:      protocol.Version,
		OK:     true,
		Status: info,
	}
}

func (s *Server) handleUnlock(req protocol.Request) protocol.Response {
	if req.Password == "" {
		return errResp(protocol.ErrCodeInvalidRequest, "password is required")
	}

	mode := req.Mode
	if mode == "" {
		mode = config.UnlockModeTTL
	}
	switch mode {
	case config.UnlockModeTTL:
		if req.TTLMs <= 0 {
			return errResp(protocol.ErrCodeInvalidRequest, "ttl_ms must be positive for ttl mode")
		}
	case config.UnlockModeSession:
		if req.SessionPID <= 1 {
			return errResp(protocol.ErrCodeInvalidRequest, "session_pid must be > 1 for session mode")
		}
	case config.UnlockModeUntilLock:
	default:
		return errResp(protocol.ErrCodeInvalidRequest, fmt.Sprintf("invalid unlock mode %q", mode))
	}

	pwBytes := []byte(req.Password)
	dek, err := vault.UnwrapDEK(s.vaultPath, pwBytes)
	crypto.Zero(pwBytes)
	if err != nil {
		if errors.Is(err, vault.ErrInvalidPassword) {
			return errResp(protocol.ErrCodeInvalidPassword, "invalid master password")
		}
		return errResp(protocol.ErrCodeInternal, err.Error())
	}

	s.mu.Lock()
	if s.closed {
		crypto.Zero(dek)
		s.mu.Unlock()
		return errResp(protocol.ErrCodeLocked, "daemon is shutting down")
	}
	if s.cancelWatch != nil {
		s.cancelWatch()
		s.cancelWatch = nil
	}
	if s.dek != nil {
		crypto.Zero(s.dek)
	}

	s.gen++
	gen := s.gen
	s.dek = dek
	s.mode = mode
	s.expiresAt = nil
	s.sessionPID = 0

	switch mode {
	case config.UnlockModeTTL:
		ttl := time.Duration(req.TTLMs) * time.Millisecond
		exp := time.Now().UTC().Add(ttl)
		s.expiresAt = &exp
		ctx, cancel := context.WithCancel(context.Background())
		s.cancelWatch = cancel
		go func() {
			timer := time.NewTimer(ttl)
			defer timer.Stop()
			select {
			case <-timer.C:
				s.lockAndShutdown(gen)
			case <-ctx.Done():
			}
		}()
	case config.UnlockModeSession:
		s.sessionPID = req.SessionPID
		ctx, cancel := context.WithCancel(context.Background())
		s.cancelWatch = cancel
		pid := req.SessionPID
		go func() {
			if watchPIDExit(ctx, pid) {
				s.lockAndShutdown(gen)
			}
		}()
	case config.UnlockModeUntilLock:
		// Stays unlocked in RAM until explicit lock or process termination.
	}

	info := &protocol.StatusInfo{
		Locked:     false,
		Mode:       s.mode,
		SessionPID: s.sessionPID,
		VaultPath:  s.vaultPath,
	}
	if s.expiresAt != nil {
		t := *s.expiresAt
		info.ExpiresAt = &t
	}
	s.mu.Unlock()

	return protocol.Response{
		V:      protocol.Version,
		OK:     true,
		Status: info,
	}
}

func (s *Server) handleGet(req protocol.Request) protocol.Response {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.dek == nil {
		return errResp(protocol.ErrCodeLocked, "vault is locked; run 'envctl unlock'")
	}
	if len(req.Keys) == 0 {
		return protocol.Response{V: protocol.Version, OK: true, Values: map[string]string{}}
	}

	payload, err := vault.ReadPayload(s.vaultPath, s.dek)
	if err != nil {
		return errResp(protocol.ErrCodeInternal, err.Error())
	}

	values := make(map[string]string, len(req.Keys))
	var missing []string
	seen := make(map[string]struct{}, len(req.Keys))
	for _, key := range req.Keys {
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		if entry, ok := payload.Secrets[key]; ok {
			values[key] = entry.Value
		} else {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)

	return protocol.Response{
		V:       protocol.Version,
		OK:      true,
		Values:  values,
		Missing: missing,
	}
}

func (s *Server) handleList() protocol.Response {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.dek == nil {
		return errResp(protocol.ErrCodeLocked, "vault is locked; run 'envctl unlock'")
	}

	payload, err := vault.ReadPayload(s.vaultPath, s.dek)
	if err != nil {
		return errResp(protocol.ErrCodeInternal, err.Error())
	}

	entries := make([]protocol.EntryMeta, 0, len(payload.Secrets))
	for key, entry := range payload.Secrets {
		entries = append(entries, protocol.EntryMeta{
			Key:         key,
			Description: entry.Description,
			CreatedAt:   entry.CreatedAt,
			UpdatedAt:   entry.UpdatedAt,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Key < entries[j].Key
	})

	return protocol.Response{
		V:       protocol.Version,
		OK:      true,
		Entries: entries,
	}
}

func (s *Server) handleSet(req protocol.Request) protocol.Response {
	if err := vault.ValidateKeyName(req.Key); err != nil {
		return errResp(protocol.ErrCodeInvalidRequest, err.Error())
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.dek == nil {
		return errResp(protocol.ErrCodeLocked, "vault is locked; run 'envctl unlock'")
	}

	err := vault.UpdatePayload(s.vaultPath, s.dek, func(payload *vault.Payload) error {
		now := time.Now().UTC()
		entry := vault.Entry{
			Value:       req.Value,
			Description: req.Description,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if prev, exists := payload.Secrets[req.Key]; exists {
			entry.CreatedAt = prev.CreatedAt
			if req.Description == "" {
				entry.Description = prev.Description
			}
		}
		payload.Secrets[req.Key] = entry
		return nil
	})
	if err != nil {
		return errResp(protocol.ErrCodeInternal, err.Error())
	}
	return protocol.Response{V: protocol.Version, OK: true}
}

func (s *Server) handleDelete(req protocol.Request) protocol.Response {
	if err := vault.ValidateKeyName(req.Key); err != nil {
		return errResp(protocol.ErrCodeInvalidRequest, err.Error())
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.dek == nil {
		return errResp(protocol.ErrCodeLocked, "vault is locked; run 'envctl unlock'")
	}

	err := vault.UpdatePayload(s.vaultPath, s.dek, func(payload *vault.Payload) error {
		if _, exists := payload.Secrets[req.Key]; !exists {
			return errSecretNotFound
		}
		delete(payload.Secrets, req.Key)
		return nil
	})
	if errors.Is(err, errSecretNotFound) {
		return errResp(protocol.ErrCodeNotFound, fmt.Sprintf("secret %q not found", req.Key))
	}
	if err != nil {
		return errResp(protocol.ErrCodeInternal, err.Error())
	}
	return protocol.Response{V: protocol.Version, OK: true}
}

func errResp(code, msg string) protocol.Response {
	return protocol.Response{
		V:         protocol.Version,
		OK:        false,
		ErrorCode: code,
		Error:     msg,
	}
}

func pollPIDExit(ctx context.Context, pid int) bool {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
}
