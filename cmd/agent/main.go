package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/nimweo/pulse-agent/internal/collector"
	"github.com/nimweo/pulse-agent/internal/config"
	"github.com/nimweo/pulse-agent/internal/model"
	"github.com/nimweo/pulse-agent/internal/transport"
)

var version = "0.2.0"

func main() {
	configFlag := flag.String("config", "", "path to the configuration file")
	flag.Parse()

	configPath, err := config.ResolvePath(*configFlag)
	if err != nil {
		slog.Error("failed to resolve configuration path", "err", err)
		os.Exit(1)
	}

	created, err := config.Ensure(configPath)
	if err != nil {
		slog.Error("failed to prepare configuration", "path", configPath, "err", err)
		os.Exit(1)
	}
	if created {
		slog.Error(
			"configuration file created; update its settings and set configured to true",
			"path", configPath,
		)
		os.Exit(1)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		if errors.Is(err, config.ErrNotConfigured) {
			slog.Error(
				"agent is not configured; update the configuration file and set configured to true",
				"path", configPath,
			)
		} else {
			slog.Error("failed to load configuration", "path", configPath, "err", err)
		}
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

	client, err := transport.New(
		cfg.Server.BaseURL,
		cfg.Server.APIKey,
		time.Duration(cfg.Server.Timeout)*time.Second,
	)
	if err != nil {
		slog.Error("failed to configure API client", "err", err)
		os.Exit(1)
	}
	if err := client.CheckHealth(ctx); err != nil {
		slog.Error("API health check failed", "base_url", cfg.Server.BaseURL, "err", err)
		os.Exit(1)
	}

	batch := &collector.Batch{}
	collectors := make([]collector.Collector, 0, 4)
	if cfg.Collectors.System.Enabled {
		collectors = append(
			collectors,
			collector.NewSystem(time.Duration(cfg.Collectors.System.Interval)*time.Second),
		)
	}
	collectors = append(collectors,
		collector.NewCore(
			cfg.Collectors.CPU.PerCPU,
			time.Duration(cfg.Intervals.Collect)*time.Second,
		),
	)
	if cfg.Collectors.Disk.Enabled {
		collectors = append(
			collectors,
			collector.NewDisk(time.Duration(cfg.Collectors.Disk.Interval)*time.Second),
		)
	}
	if cfg.Collectors.Network.Enabled {
		collectors = append(
			collectors,
			collector.NewNetwork(time.Duration(cfg.Collectors.Network.Interval)*time.Second),
		)
	}

	var collectorWorkers sync.WaitGroup
	for _, metricCollector := range collectors {
		collectorWorkers.Add(1)
		go runCollector(ctx, &collectorWorkers, metricCollector, batch)
	}

	sendTick := time.NewTicker(time.Duration(cfg.Intervals.Send) * time.Second)
	defer sendTick.Stop()

	slog.Info("agent started")

	for {
		select {
		case <-ctx.Done():
			slog.Info("shutting down")
			collectorWorkers.Wait()
			return

		case <-sendTick.C:
			system, cs, ps := batch.Drain()
			if system == nil && len(cs) == 0 && len(ps) == 0 {
				continue
			}
			payload := model.Payload{
				AgentVersion: version,
				Hostname:     hostname,
				System:       system,
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

func runCollector(
	ctx context.Context,
	workers *sync.WaitGroup,
	metricCollector collector.Collector,
	batch *collector.Batch,
) {
	defer workers.Done()

	ticker := time.NewTicker(metricCollector.Interval())
	defer ticker.Stop()
	collect := func() {
		if err := metricCollector.Collect(ctx, batch); err != nil {
			slog.Error(
				"metric collection failed",
				"collector", metricCollector.Name(),
				"err", err,
			)
		}
	}
	collect()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			collect()
		}
	}
}
