package updater

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type checkState struct {
	LastCheck time.Time `json:"last_check"`
}

func CheckDue(path string, interval time.Duration, now time.Time) (bool, error) {
	if interval <= 0 {
		return false, errors.New("update interval must be greater than zero")
	}

	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("open update state %q: %w", path, err)
	}
	defer file.Close()

	var state checkState
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return false, fmt.Errorf("decode update state %q: %w", path, err)
	}
	if state.LastCheck.IsZero() || state.LastCheck.After(now) {
		return true, nil
	}
	return now.Sub(state.LastCheck) >= interval, nil
}

func SaveCheck(path string, checkedAt time.Time) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create update state directory: %w", err)
	}

	temporary, err := os.CreateTemp(filepath.Dir(path), ".update-state-*")
	if err != nil {
		return fmt.Errorf("create temporary update state: %w", err)
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()

	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set update state permissions: %w", err)
	}
	if err := json.NewEncoder(temporary).Encode(checkState{LastCheck: checkedAt.UTC()}); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write update state: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync update state: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close update state: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace update state %q: %w", path, err)
	}
	removeTemporary = false
	return nil
}
