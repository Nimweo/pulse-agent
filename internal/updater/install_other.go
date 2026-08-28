//go:build !linux && !darwin

package updater

import (
	"context"
	"fmt"
)

func selfUpdateSupported(_ string) bool {
	return false
}

func installBinary(_ context.Context, _ string, _ string, _ string, _ func() error) error {
	return fmt.Errorf("automatic binary replacement is not supported on this platform")
}
