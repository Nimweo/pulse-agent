package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nimweo/pulse-agent/internal/collector"
	"github.com/nimweo/pulse-agent/internal/config"
	"github.com/nimweo/pulse-agent/internal/model"
	"github.com/nimweo/pulse-agent/internal/transport"
)

func main() {
	cfg, err := config.Load(config.DefaultPath)
	if err != nil {
		slog.Error("failed to start agent", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	hostname := cfg.Agent.Hostname
	if hostname == "" {
		hostname, err = os.Hostname()
		if err != nil {
			slog.Error("failed to read hostname", "err", err)
			os.Exit(1)
		}
	}

	client := transport.New(
		cfg.Server.URL,
		time.Duration(cfg.Server.Timeout)*time.Second,
	)
	batch := &collector.Batch{}
	core := collector.NewCore(
		cfg.Collectors.CPU.PerCPU,
		time.Duration(cfg.Intervals.Collect)*time.Second,
	)

	collectTick := time.NewTicker(time.Duration(cfg.Intervals.Collect) * time.Second)
	defer collectTick.Stop()
	sendTick := time.NewTicker(time.Duration(cfg.Intervals.Send) * time.Second)
	defer sendTick.Stop()

	slog.Info("agent started")

	for {
		select {
		case <-ctx.Done():
			slog.Info("shutting down")
			return

		case <-collectTick.C:
			if err := core.Collect(ctx, batch); err != nil {
				slog.Error("collect", "err", err)
			}

		case <-sendTick.C:
			cs, ps := batch.Drain()
			if len(cs) == 0 && len(ps) == 0 {
				continue
			}
			payload := model.Payload{
				AgentVersion: cfg.Agent.Version,
				Hostname:     hostname,
				Core:         cs,
				Points:       ps,
			}
			if err := client.Send(ctx, payload); err != nil {
				slog.Error("send", "err", err, "dropped", len(cs)+len(ps))
				continue // Samples are dropped until disk spooling is implemented.
			}
			slog.Info("metrics sent", "core", len(cs), "points", len(ps))
		}
	}
}
