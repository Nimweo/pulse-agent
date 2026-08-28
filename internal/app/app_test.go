package app

import (
	"context"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/nimweo/pulse-agent/internal/collector"
	"github.com/nimweo/pulse-agent/internal/model"
)

type senderStub struct {
	payloads []model.Payload
	err      error
}

func (s *senderStub) Send(_ context.Context, payload model.Payload) error {
	s.payloads = append(s.payloads, payload)
	return s.err
}

func TestBuildPayloadCreatesVersionedBatchEnvelope(t *testing.T) {
	system := &model.SystemSample{OS: "linux"}
	core := []model.CoreSample{{Time: 123, CPU: 25}}
	points := []model.Point{{Time: 123, Metric: "disk_used_percent", Value: 50}}

	payload, err := buildPayload("0.4.0", "pulse-host", system, core, points)
	if err != nil {
		t.Fatalf("buildPayload() error = %v", err)
	}
	if payload.SchemaVersion != model.PayloadSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", payload.SchemaVersion, model.PayloadSchemaVersion)
	}
	if payload.AgentVersion != "0.4.0" || payload.Hostname != "pulse-host" {
		t.Errorf("agent identity = %q/%q", payload.AgentVersion, payload.Hostname)
	}
	if payload.SentAt <= 0 {
		t.Errorf("SentAt = %d, want positive timestamp", payload.SentAt)
	}
	if decoded, err := hex.DecodeString(payload.BatchID); err != nil || len(decoded) != 16 {
		t.Errorf("BatchID = %q, want 16 random bytes", payload.BatchID)
	}
	if payload.System != system || len(payload.Core) != 1 || len(payload.Points) != 1 {
		t.Errorf("payload metrics were not preserved")
	}
}

func TestBuildPayloadUsesEmptyMetricArrays(t *testing.T) {
	payload, err := buildPayload("0.4.0", "pulse-host", &model.SystemSample{}, nil, nil)
	if err != nil {
		t.Fatalf("buildPayload() error = %v", err)
	}
	if payload.Core == nil || payload.Points == nil {
		t.Fatalf("empty metrics must be encoded as arrays")
	}
}

func TestSendBatchSendsBufferedMetrics(t *testing.T) {
	batch := &collector.Batch{}
	batch.SetSystem(model.SystemSample{OS: "linux"})
	batch.AddCore(model.CoreSample{Time: 123, CPU: 25})
	batch.AddPoint(model.Point{Time: 123, Metric: "network_receive_bytes_per_second", Value: 10})
	sender := &senderStub{}

	sendBatch(context.Background(), sender, batch, "0.8.0", "pulse-host")

	if len(sender.payloads) != 1 {
		t.Fatalf("Send() calls = %d, want 1", len(sender.payloads))
	}
	payload := sender.payloads[0]
	if payload.AgentVersion != "0.8.0" || payload.Hostname != "pulse-host" {
		t.Errorf("agent identity = %q/%q", payload.AgentVersion, payload.Hostname)
	}
	if payload.System == nil || len(payload.Core) != 1 || len(payload.Points) != 1 {
		t.Errorf("payload does not contain buffered metrics: %+v", payload)
	}
}

func TestSendBatchSkipsEmptyBatch(t *testing.T) {
	sender := &senderStub{}

	sendBatch(context.Background(), sender, &collector.Batch{}, "0.8.0", "pulse-host")

	if len(sender.payloads) != 0 {
		t.Fatalf("Send() calls = %d, want 0", len(sender.payloads))
	}
}

func TestSendBatchDrainsMetricsAfterSendFailure(t *testing.T) {
	batch := &collector.Batch{}
	batch.AddCore(model.CoreSample{Time: 123})
	sender := &senderStub{err: errors.New("send failed")}

	sendBatch(context.Background(), sender, batch, "0.8.0", "pulse-host")
	sendBatch(context.Background(), sender, batch, "0.8.0", "pulse-host")

	if len(sender.payloads) != 1 {
		t.Fatalf("Send() calls = %d, want 1", len(sender.payloads))
	}
}

func TestConfiguredCollectors(t *testing.T) {
	cfg := &model.Config{
		Intervals: model.IntervalsConfig{Collect: 2},
		Collectors: model.CollectorsConfig{
			System:  model.CollectorConfig{Enabled: true, Interval: 60},
			CPU:     model.CPUCollectorConfig{PerCPU: true},
			Disk:    model.CollectorConfig{Enabled: true, Interval: 3},
			Network: model.CollectorConfig{Enabled: true, Interval: 4},
			GPU:     model.CollectorConfig{Enabled: true, Interval: 5},
		},
	}

	collectors := configuredCollectors(cfg)
	wantNames := []string{"system", "core", "disk", "network", "gpu"}
	if len(collectors) != len(wantNames) {
		t.Fatalf("configuredCollectors() length = %d, want %d", len(collectors), len(wantNames))
	}
	for index, want := range wantNames {
		if got := collectors[index].Name(); got != want {
			t.Errorf("collector %d name = %q, want %q", index, got, want)
		}
	}
}

func TestResolveHostnameUsesConfiguredValue(t *testing.T) {
	hostname, err := resolveHostname("configured-host")
	if err != nil {
		t.Fatalf("resolveHostname() error = %v", err)
	}
	if hostname != "configured-host" {
		t.Errorf("resolveHostname() = %q, want configured-host", hostname)
	}
}

func TestRunRequiresConfiguration(t *testing.T) {
	err := Run(context.Background(), nil, "0.8.0")
	if err == nil || err.Error() != "configuration is required" {
		t.Fatalf("Run() error = %v, want configuration is required", err)
	}
}
