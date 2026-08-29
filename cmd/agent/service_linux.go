//go:build linux

package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

func isWindowsService() (bool, error) { return false, nil }
func runWindowsService() error        { return nil }

func restartAgentService(ctx context.Context, name string) error {
	path, err := exec.LookPath("systemctl")
	if err != nil {
		return nil
	}
	output, err := exec.CommandContext(ctx, path, "try-restart", name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl try-restart: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
