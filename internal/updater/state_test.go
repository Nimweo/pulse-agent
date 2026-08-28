package updater

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestCheckDueAndSaveCheck(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "update.json")
	now := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)

	due, err := CheckDue(path, 6*time.Hour, now)
	if err != nil {
		t.Fatalf("CheckDue() error = %v", err)
	}
	if !due {
		t.Fatal("CheckDue() = false for missing state")
	}
	if err := SaveCheck(path, now); err != nil {
		t.Fatalf("SaveCheck() error = %v", err)
	}

	due, err = CheckDue(path, 6*time.Hour, now.Add(5*time.Hour))
	if err != nil {
		t.Fatalf("CheckDue() error = %v", err)
	}
	if due {
		t.Fatal("CheckDue() = true before interval elapsed")
	}
	due, err = CheckDue(path, 6*time.Hour, now.Add(6*time.Hour))
	if err != nil {
		t.Fatalf("CheckDue() error = %v", err)
	}
	if !due {
		t.Fatal("CheckDue() = false after interval elapsed")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("state mode = %o, want 600", info.Mode().Perm())
	}
}

func TestCheckDueTreatsFutureTimestampAsDue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "update.json")
	now := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	if err := SaveCheck(path, now.Add(time.Hour)); err != nil {
		t.Fatalf("SaveCheck() error = %v", err)
	}

	due, err := CheckDue(path, 24*time.Hour, now)
	if err != nil {
		t.Fatalf("CheckDue() error = %v", err)
	}
	if !due {
		t.Fatal("CheckDue() = false for future state timestamp")
	}
}
