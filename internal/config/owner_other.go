//go:build !linux && !darwin

package config

import "os"

func preserveOwner(_ string, _ os.FileInfo) error {
	return nil
}
