package main

import (
	"context"
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
				AgentVersion: cfg.Agent.Version,
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
