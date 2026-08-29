package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/nimweo/pulse-agent/internal/app"
	"github.com/nimweo/pulse-agent/internal/config"
	"github.com/nimweo/pulse-agent/internal/transport"
	"github.com/nimweo/pulse-agent/internal/updater"
)

const (
	version                 = "0.11.0"
	authenticationExitCode  = 78
	linuxSystemConfigPath   = "/etc/nimweo/pulse-agent/config.yaml"
	linuxUpdateStatePath    = "/var/lib/pulse-agent-updater/update-state.json"
	defaultAgentServiceName = "pulse-agent.service"
)

func main() {
	if err := run(); err != nil {
		code := exitCode(err)
		slog.Error("agent stopped", "err", err, "exit_code", code)
		os.Exit(code)
	}
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if errors.Is(err, transport.ErrAuthentication) {
		return authenticationExitCode
	}
	return 1
}

func run() error {
	configFlag := flag.String("config", "", "path to the configuration file")
	manualUpdate := flag.Bool("update", false, "check for and install the latest release")
	automaticUpdate := flag.Bool(
		"automatic-update",
		false,
		"run a scheduled update check using the configuration",
	)
	updateStateFlag := flag.String("update-state", "", "path to the update scheduler state")
	migrateConfig := flag.Bool(
		"migrate-config",
		false,
		"add missing settings to an existing configuration",
	)
	showVersion := flag.Bool("version", false, "show the agent version")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return nil
	}
	selectedModes := 0
	for _, selected := range []bool{*manualUpdate, *automaticUpdate, *migrateConfig} {
		if selected {
			selectedModes++
		}
	}
	if selectedModes > 1 {
		return errors.New("update, automatic-update, and migrate-config modes are mutually exclusive")
	}

	if *manualUpdate || *automaticUpdate {
		return runUpdate(*configFlag, *updateStateFlag, *automaticUpdate)
	}

	configPath, err := config.ResolvePath(*configFlag)
	if err != nil {
		return fmt.Errorf("resolve configuration path: %w", err)
	}
	if *migrateConfig {
		migrated, err := config.Migrate(configPath)
		if err != nil {
			return fmt.Errorf("migrate configuration at %q: %w", configPath, err)
		}
		if migrated {
			slog.Info("configuration migrated", "path", configPath)
		}
		return nil
	}

	created, err := config.Ensure(configPath)
	if err != nil {
		return fmt.Errorf("prepare configuration at %q: %w", configPath, err)
	}
	if created {
		return fmt.Errorf(
			"configuration file created at %q; update its settings and set configured to true",
			configPath,
		)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		if errors.Is(err, config.ErrNotConfigured) {
			return fmt.Errorf(
				"agent is not configured; update %q and set configured to true: %w",
				configPath,
				err,
			)
		}
		return fmt.Errorf("load configuration from %q: %w", configPath, err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return app.Run(ctx, cfg, version)
}

func runUpdate(configFlag string, stateFlag string, automatic bool) error {
	configPath, err := resolveUpdateConfigPath(configFlag, automatic)
	if err != nil {
		return err
	}
	statePath, err := resolveUpdateStatePath(stateFlag)
	if err != nil {
		return err
	}
	releaseLock, err := updater.AcquireLock(statePath + ".lock")
	if err != nil {
		return err
	}
	defer releaseLock()

	checkedAt := time.Now()
	if automatic {
		cfg, err := config.Load(configPath)
		if err != nil {
			return fmt.Errorf("load update configuration from %q: %w", configPath, err)
		}
		if !cfg.Updates.Enabled {
			slog.Info("automatic updates disabled")
			return nil
		}
		interval, err := config.UpdateInterval(cfg.Updates.Interval)
		if err != nil {
			return err
		}
		due, err := updater.CheckDue(statePath, interval, checkedAt)
		if err != nil {
			return err
		}
		if !due {
			slog.Info("update check not due", "interval", cfg.Updates.Interval)
			return nil
		}
	}

	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve current executable: %w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	result, err := updater.NewClient().Apply(ctx, version, executable, configPath)
	if err != nil {
		return fmt.Errorf("update agent: %w", err)
	}
	stateErr := updater.SaveCheck(statePath, checkedAt)
	if !result.Updated {
		if stateErr != nil {
			return stateErr
		}
		slog.Info(
			"agent is up to date",
			"current_version", result.CurrentVersion,
			"latest_version", result.LatestVersion,
		)
		return nil
	}

	slog.Info(
		"agent updated",
		"previous_version", result.CurrentVersion,
		"version", result.LatestVersion,
	)
	if stateErr != nil {
		slog.Warn("failed to save update state", "err", stateErr)
	}
	if err := restartAgentService(ctx, defaultAgentServiceName); err != nil {
		slog.Warn("failed to restart agent service", "err", err)
	}
	return nil
}

func resolveUpdateConfigPath(explicit string, required bool) (string, error) {
	if explicit != "" {
		return config.ResolvePath(explicit)
	}
	if runtime.GOOS == "linux" {
		if _, err := os.Stat(linuxSystemConfigPath); err == nil {
			return linuxSystemConfigPath, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("inspect system configuration: %w", err)
		}
	}

	path, err := config.ResolvePath("")
	if err != nil {
		return "", fmt.Errorf("resolve update configuration path: %w", err)
	}
	if _, err := os.Stat(path); err == nil {
		return path, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("inspect update configuration: %w", err)
	}
	if required {
		return "", errors.New("automatic updates require an existing configuration file")
	}
	return "", nil
}

func resolveUpdateStatePath(explicit string) (string, error) {
	if explicit != "" {
		absolute, err := filepath.Abs(explicit)
		if err != nil {
			return "", fmt.Errorf("resolve update state path: %w", err)
		}
		return absolute, nil
	}
	if runtime.GOOS == "linux" {
		if _, err := os.Stat(filepath.Dir(linuxUpdateStatePath)); err == nil {
			return linuxUpdateStatePath, nil
		}
	}
	cacheDirectory, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve user cache directory: %w", err)
	}
	return filepath.Join(cacheDirectory, "nimweo", "pulse-agent", "update-state.json"), nil
}
