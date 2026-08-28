package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/nimweo/pulse-agent/internal/collector"
	"github.com/nimweo/pulse-agent/internal/model"
	"github.com/nimweo/pulse-agent/internal/transport"
)

type metricSender interface {
	Send(ctx context.Context, payload model.Payload) error
}

// Run checks the API connection and runs the agent until the context is canceled.
func Run(ctx context.Context, cfg *model.Config, version string) error {
	if cfg == nil {
		return fmt.Errorf("configuration is required")
	}

	hostname, err := resolveHostname(cfg.Agent.Hostname)
	if err != nil {
		return err
	}

	client, err := transport.New(cfg.Server, cfg.Transport)
	if err != nil {
		return fmt.Errorf("configure API client: %w", err)
	}
	if err := client.CheckHealth(ctx); err != nil {
		return fmt.Errorf("API health check for %q: %w", cfg.Server.BaseURL, err)
	}

	return run(ctx, cfg, version, hostname, client, configuredCollectors(cfg))
}

func run(
	ctx context.Context,
	cfg *model.Config,
	version string,
	hostname string,
	sender metricSender,
	collectors []collector.Collector,
) error {
	batch := &collector.Batch{}

	var collectorWorkers sync.WaitGroup
	for _, metricCollector := range collectors {
		collectorWorkers.Add(1)
		go runCollector(ctx, &collectorWorkers, metricCollector, batch)
	}

	sendTicker := time.NewTicker(time.Duration(cfg.Intervals.Send) * time.Second)
	defer sendTicker.Stop()

	slog.Info("agent started", "version", version, "hostname", hostname)

	for {
		select {
		case <-ctx.Done():
			slog.Info("shutting down")
			collectorWorkers.Wait()
			return nil

		case <-sendTicker.C:
			sendBatch(ctx, sender, batch, version, hostname)
		}
	}
}

func configuredCollectors(cfg *model.Config) []collector.Collector {
	collectors := make([]collector.Collector, 0, 5)
	if cfg.Collectors.System.Enabled {
		collectors = append(
			collectors,
			collector.NewSystem(time.Duration(cfg.Collectors.System.Interval)*time.Second),
		)
	}
	collectors = append(
		collectors,
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
	if cfg.Collectors.GPU.Enabled {
		collectors = append(
			collectors,
			collector.NewGPU(time.Duration(cfg.Collectors.GPU.Interval)*time.Second),
		)
	}

	return collectors
}

func resolveHostname(configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}

	hostname, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("read hostname: %w", err)
	}
	return hostname, nil
}

func sendBatch(
	ctx context.Context,
	sender metricSender,
	batch *collector.Batch,
	version string,
	hostname string,
) {
	system, core, points := batch.Drain()
	if system == nil && len(core) == 0 && len(points) == 0 {
		return
	}

	payload, err := buildPayload(version, hostname, system, core, points)
	if err != nil {
		slog.Error("failed to create metric payload", "err", err)
		return
	}
	if err := sender.Send(ctx, payload); err != nil {
		slog.Error(
			"failed to send metrics",
			"batch_id", payload.BatchID,
			"err", err,
			"dropped", len(core)+len(points),
		)
		return // Samples are dropped until disk spooling is implemented.
	}

	slog.Info(
		"metrics sent",
		"batch_id", payload.BatchID,
		"core", len(core),
		"points", len(points),
	)
}

func buildPayload(
	agentVersion string,
	hostname string,
	system *model.SystemSample,
	core []model.CoreSample,
	points []model.Point,
) (model.Payload, error) {
	batchID := make([]byte, 16)
	if _, err := rand.Read(batchID); err != nil {
		return model.Payload{}, err
	}
	if core == nil {
		core = []model.CoreSample{}
	}
	if points == nil {
		points = []model.Point{}
	}

	return model.Payload{
		SchemaVersion: model.PayloadSchemaVersion,
		BatchID:       hex.EncodeToString(batchID),
		SentAt:        time.Now().UnixMilli(),
		AgentVersion:  agentVersion,
		Hostname:      hostname,
		System:        system,
		Core:          core,
		Points:        points,
	}, nil
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
