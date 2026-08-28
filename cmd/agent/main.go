package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/nimweo/pulse-agent/internal/app"
	"github.com/nimweo/pulse-agent/internal/config"
)

const version = "0.7.0"

func main() {
	if err := run(); err != nil {
		slog.Error("agent stopped", "err", err)
		os.Exit(1)
	}
}

func run() error {
	configFlag := flag.String("config", "", "path to the configuration file")
	flag.Parse()

	configPath, err := config.ResolvePath(*configFlag)
	if err != nil {
		return fmt.Errorf("resolve configuration path: %w", err)
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
