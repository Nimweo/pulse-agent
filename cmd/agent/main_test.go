package main

import (
	"encoding/hex"
	"testing"

	"github.com/nimweo/pulse-agent/internal/model"
)

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
