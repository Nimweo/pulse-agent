package collector

import (
	"testing"
	"time"

	"github.com/nimweo/pulse-agent/internal/model"
	"github.com/shirou/gopsutil/v4/host"
)

func TestBuildSystemSample(t *testing.T) {
	info := &host.InfoStat{
		Uptime:               3_600,
		BootTime:             1_700_000_000,
		Procs:                42,
		OS:                   "linux",
		Platform:             "ubuntu",
		PlatformFamily:       "debian",
		PlatformVersion:      "24.04",
		KernelVersion:        "6.8.0",
		KernelArch:           "x86_64",
		VirtualizationSystem: "kvm",
		VirtualizationRole:   "guest",
	}

	sample := buildSystemSample(123, info, 4, 8)
	if sample.Time != 123 {
		t.Errorf("Time = %d, want 123", sample.Time)
	}
	if sample.BootTime != 1_700_000_000_000 {
		t.Errorf("BootTime = %d, want milliseconds", sample.BootTime)
	}
	if sample.UptimeSeconds != 3_600 {
		t.Errorf("UptimeSeconds = %d, want 3600", sample.UptimeSeconds)
	}
	if sample.PhysicalCores != 4 || sample.LogicalCPUs != 8 {
		t.Errorf("CPU counts = %d/%d, want 4/8", sample.PhysicalCores, sample.LogicalCPUs)
	}
	if sample.Platform != "ubuntu" || sample.KernelArchitecture != "x86_64" {
		t.Errorf("unexpected system identity: %+v", sample)
	}
}

func TestBatchDrainRetainsSystemSnapshot(t *testing.T) {
	batch := &Batch{}
	batch.SetSystem(model.SystemSample{Time: 123, OS: "linux"})

	first, _, _ := batch.Drain()
	second, _, _ := batch.Drain()
	if first == nil || second == nil {
		t.Fatal("system snapshot was not retained between drains")
	}
	if first.Time != 123 || second.Time != 123 {
		t.Errorf("system snapshot times = %d/%d, want 123/123", first.Time, second.Time)
	}
}

func TestNewSystem(t *testing.T) {
	collector := NewSystem(30 * time.Second)
	if collector.Name() != "system" {
		t.Errorf("Name() = %q, want system", collector.Name())
	}
	if collector.Interval() != 30*time.Second {
		t.Errorf("Interval() = %s, want 30s", collector.Interval())
	}
}
