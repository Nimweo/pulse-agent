//go:build linux || darwin

package updater

import (
	"path/filepath"
	"testing"
)

func TestAcquireLockRejectsConcurrentUpdater(t *testing.T) {
	path := filepath.Join(t.TempDir(), "update.lock")
	release, err := AcquireLock(path)
	if err != nil {
		t.Fatalf("AcquireLock() error = %v", err)
	}
	defer release()

	if _, err := AcquireLock(path); err == nil {
		t.Fatal("second AcquireLock() error = nil")
	}
}
