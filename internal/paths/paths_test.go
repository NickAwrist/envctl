package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsurePrivateDirNeverChangesExistingDirectories(t *testing.T) {
	shared := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(shared, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	if err := EnsurePrivateDir(shared); err == nil {
		t.Fatalf("expected a directory readable by others to be rejected")
	}
	info, err := os.Stat(shared)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o755 {
		t.Fatalf("existing directory permissions changed to %04o", perm)
	}

	created := filepath.Join(shared, "envctl", "data")
	if err := EnsurePrivateDir(created); err != nil {
		t.Fatalf("EnsurePrivateDir on a missing directory: %v", err)
	}
	if info, err = os.Stat(created); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("expected a new 0700 directory, got %v, %v", info, err)
	}
}
