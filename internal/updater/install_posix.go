//go:build linux || darwin

package updater

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func selfUpdateSupported(goos string) bool {
	return goos == "linux" || goos == "darwin"
}

func installBinary(
	ctx context.Context,
	target string,
	source string,
	expectedVersion string,
	migrateConfig func() error,
) error {
	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		return fmt.Errorf("resolve executable path %q: %w", target, err)
	}
	targetInfo, err := os.Stat(resolvedTarget)
	if err != nil {
		return fmt.Errorf("inspect current executable: %w", err)
	}
	if !targetInfo.Mode().IsRegular() {
		return errors.New("current executable is not a regular file")
	}

	replacement, err := copyBinaryToDirectory(source, filepath.Dir(resolvedTarget), targetInfo)
	if err != nil {
		return err
	}
	defer os.Remove(replacement)
	if err := validateReleaseBinary(ctx, replacement, expectedVersion); err != nil {
		return err
	}

	backup, err := copyBinaryToDirectory(resolvedTarget, filepath.Dir(resolvedTarget), targetInfo)
	if err != nil {
		return fmt.Errorf("back up current executable: %w", err)
	}
	defer os.Remove(backup)

	if err := os.Rename(replacement, resolvedTarget); err != nil {
		return fmt.Errorf("replace current executable: %w", err)
	}
	if err := migrateConfig(); err != nil {
		rollbackErr := os.Rename(backup, resolvedTarget)
		if rollbackErr != nil {
			return errors.Join(err, fmt.Errorf("restore previous executable: %w", rollbackErr))
		}
		return err
	}
	return nil
}

func validateReleaseBinary(ctx context.Context, path string, expectedVersion string) error {
	validationContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	output, err := exec.CommandContext(validationContext, path, "--version").Output()
	if err != nil {
		return fmt.Errorf("validate release binary: %w", err)
	}
	if version := strings.TrimSpace(string(output)); version != expectedVersion {
		return fmt.Errorf(
			"release binary reports version %q, expected %q",
			version,
			expectedVersion,
		)
	}
	return nil
}

func copyBinaryToDirectory(source string, directory string, targetInfo os.FileInfo) (string, error) {
	input, err := os.Open(source)
	if err != nil {
		return "", fmt.Errorf("open executable source: %w", err)
	}
	defer input.Close()

	output, err := os.CreateTemp(directory, ".pulse-agent-binary-*")
	if err != nil {
		return "", fmt.Errorf("create executable file: %w", err)
	}
	path := output.Name()
	removeOutput := true
	defer func() {
		if removeOutput {
			_ = os.Remove(path)
		}
	}()

	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		return "", fmt.Errorf("copy executable: %w", err)
	}
	if err := output.Chmod(targetInfo.Mode().Perm()); err != nil {
		_ = output.Close()
		return "", fmt.Errorf("set executable permissions: %w", err)
	}
	if err := preserveBinaryOwner(path, targetInfo); err != nil {
		_ = output.Close()
		return "", fmt.Errorf("preserve executable ownership: %w", err)
	}
	if err := output.Sync(); err != nil {
		_ = output.Close()
		return "", fmt.Errorf("sync executable: %w", err)
	}
	if err := output.Close(); err != nil {
		return "", fmt.Errorf("close executable: %w", err)
	}
	removeOutput = false
	return path, nil
}

func preserveBinaryOwner(path string, source os.FileInfo) error {
	sourceStat, sourceOK := source.Sys().(*syscall.Stat_t)
	current, err := os.Stat(path)
	if err != nil {
		return err
	}
	currentStat, currentOK := current.Sys().(*syscall.Stat_t)
	if !sourceOK || !currentOK ||
		(sourceStat.Uid == currentStat.Uid && sourceStat.Gid == currentStat.Gid) {
		return nil
	}
	return os.Chown(path, int(sourceStat.Uid), int(sourceStat.Gid))
}
