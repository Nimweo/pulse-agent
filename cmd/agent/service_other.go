//go:build !linux && !windows

package main

import "context"

func isWindowsService() (bool, error) { return false, nil }
func runWindowsService() error        { return nil }

func restartAgentService(_ context.Context, _ string) error {
	return nil
}
