package daemon

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/NickAwrist/envctl/internal/config"
	"github.com/NickAwrist/envctl/internal/protocol"
)

var (
	ErrDaemonUnavailable = errors.New("envctl daemon is not running (vault is locked)")
	ErrLocked            = errors.New("vault is locked")
)

type RemoteError struct {
	Code    string
	Message string
}

func (e *RemoteError) Error() string {
	return e.Message
}

type Client struct {
	SocketPath string
	Timeout    time.Duration
}

func NewClient(socketPath string) *Client {
	return &Client{
		SocketPath: socketPath,
		Timeout:    10 * time.Second,
	}
}

func CurrentSessionPID() int {
	return currentSessionPID()
}

func VerifySocketOwnership(socketPath string) error {
	info, err := os.Lstat(socketPath)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("%s is not a unix socket", socketPath)
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		if int(stat.Uid) != os.Getuid() {
			return fmt.Errorf("socket %s is owned by uid %d, expected %d", socketPath, stat.Uid, os.Getuid())
		}
	}
	return nil
}

func (c *Client) Do(req protocol.Request) (protocol.Response, error) {
	if err := VerifySocketOwnership(c.SocketPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return protocol.Response{}, ErrDaemonUnavailable
		}
		return protocol.Response{}, err
	}

	req.V = protocol.Version
	conn, err := net.DialTimeout("unix", c.SocketPath, c.Timeout)
	if err != nil {
		return protocol.Response{}, ErrDaemonUnavailable
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(c.Timeout))

	if err := protocol.WriteMessage(conn, req); err != nil {
		return protocol.Response{}, fmt.Errorf("send request: %w", err)
	}

	reader := bufio.NewReader(io.LimitReader(conn, protocol.MaxMessageSize+1))
	var resp protocol.Response
	if err := protocol.ReadMessage(reader, &resp); err != nil {
		return protocol.Response{}, fmt.Errorf("read response: %w", err)
	}
	if !resp.OK {
		if resp.ErrorCode == protocol.ErrCodeLocked {
			return resp, ErrLocked
		}
		return resp, &RemoteError{Code: resp.ErrorCode, Message: resp.Error}
	}
	return resp, nil
}

func (c *Client) Status() (*protocol.StatusInfo, error) {
	resp, err := c.Do(protocol.Request{Op: protocol.OpStatus})
	if err != nil {
		return nil, err
	}
	return resp.Status, nil
}

func (c *Client) Unlock(password string, mode config.UnlockMode, ttl time.Duration, sessionPID int) (*protocol.StatusInfo, error) {
	req := protocol.Request{
		Op:         protocol.OpUnlock,
		Password:   password,
		Mode:       mode,
		TTLMs:      ttl.Milliseconds(),
		SessionPID: sessionPID,
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	return resp.Status, nil
}

func (c *Client) Lock() error {
	_, err := c.Do(protocol.Request{Op: protocol.OpLock})
	if errors.Is(err, ErrDaemonUnavailable) {
		return nil
	}
	return err
}

func (c *Client) Get(keys []string) (map[string]string, []string, error) {
	resp, err := c.Do(protocol.Request{Op: protocol.OpGet, Keys: keys})
	if err != nil {
		return nil, nil, err
	}
	return resp.Values, resp.Missing, nil
}

func (c *Client) List() ([]protocol.EntryMeta, error) {
	resp, err := c.Do(protocol.Request{Op: protocol.OpList})
	if err != nil {
		return nil, err
	}
	return resp.Entries, nil
}

func (c *Client) Set(key, value, description string) error {
	_, err := c.Do(protocol.Request{
		Op:          protocol.OpSet,
		Key:         key,
		Value:       value,
		Description: description,
	})
	return err
}

func (c *Client) Delete(key string) error {
	_, err := c.Do(protocol.Request{Op: protocol.OpDelete, Key: key})
	return err
}

// EnsureDaemonRunning spawns the current binary in detached __daemon mode if no
// daemon is accepting connections on socketPath.
func EnsureDaemonRunning(socketPath, vaultPath string) error {
	client := NewClient(socketPath)
	if _, err := client.Status(); err == nil {
		return nil
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate envctl executable: %w", err)
	}

	cmd := exec.Command(exe, "__daemon", "--socket", socketPath, "--vault", vaultPath)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start envctl daemon: %w", err)
	}
	_ = cmd.Process.Release()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := client.Status(); err == nil {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for envctl daemon on %s", socketPath)
}
