//go:build !linux

package main

import "context"

func restartAgentService(_ context.Context, _ string) error {
	return nil
}
