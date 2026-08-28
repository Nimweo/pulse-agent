package collector

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nimweo/pulse-agent/internal/model"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
)

func TestSystemCollectorStoresSnapshot(t *testing.T) {
	metricCollector := &systemCollector{
		interval: time.Minute,
		readHost: func(context.Context) (*host.InfoStat, error) {
			return &host.InfoStat{Hostname: "pulse-host", OS: "linux"}, nil
		},
		readCPUCounts: func(_ context.Context, logical bool) (int, error) {
			if logical {
				return 8, nil
			}
			return 4, nil
		},
		readCPUInfo: func(context.Context) ([]cpu.InfoStat, error) {
			return []cpu.InfoStat{{ModelName: "Example Processor"}}, nil
		},
		readMemory: func(context.Context) (*mem.VirtualMemoryStat, error) {
			return &mem.VirtualMemoryStat{Total: 16_000}, nil
		},
		logicalCPUs: func() int { return 2 },
		now:         func() time.Time { return time.UnixMilli(123) },
	}
	batch := &Batch{}
	if err := metricCollector.Collect(context.Background(), batch); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	system, _, _ := batch.Drain()
	if system == nil {
		t.Fatal("system snapshot was not buffered")
	}
	if system.Time != 123 || system.ComputerName != "pulse-host" || system.ProcessorModel != "Example Processor" {
		t.Errorf("system identity = %#v", system)
	}
	if system.MemoryTotalBytes != 16_000 || system.PhysicalCores != 4 || system.LogicalCPUs != 8 {
		t.Errorf("system resources = %#v", system)
	}
}

func TestSystemCollectorRetainsPartialSnapshotAndJoinsErrors(t *testing.T) {
	physicalErr := errors.New("physical cores unavailable")
	logicalErr := errors.New("logical CPUs unavailable")
	processorErr := errors.New("processor unavailable")
	memoryErr := errors.New("memory unavailable")
	metricCollector := &systemCollector{
		readHost: func(context.Context) (*host.InfoStat, error) {
			return &host.InfoStat{Hostname: "pulse-host"}, nil
		},
		readCPUCounts: func(_ context.Context, logical bool) (int, error) {
			if logical {
				return 0, logicalErr
			}
			return 0, physicalErr
		},
		readCPUInfo: func(context.Context) ([]cpu.InfoStat, error) {
			return nil, processorErr
		},
		readMemory: func(context.Context) (*mem.VirtualMemoryStat, error) {
			return nil, memoryErr
		},
		logicalCPUs: func() int { return 12 },
		now:         func() time.Time { return time.UnixMilli(123) },
	}
	batch := &Batch{}
	err := metricCollector.Collect(context.Background(), batch)
	for _, wanted := range []error{physicalErr, logicalErr, processorErr, memoryErr} {
		if !errors.Is(err, wanted) {
			t.Errorf("Collect() error = %v, want joined %v", err, wanted)
		}
	}
	system, _, _ := batch.Drain()
	if system == nil || system.LogicalCPUs != 12 {
		t.Fatalf("partial system snapshot = %#v, want fallback logical CPU count", system)
	}
	if system.MemoryTotalBytes != 0 || system.ProcessorModel != "" {
		t.Errorf("unavailable values were not left empty: %#v", system)
	}
}

func TestSystemCollectorStopsWhenHostReadFails(t *testing.T) {
	hostErr := errors.New("host unavailable")
	countsCalled := false
	metricCollector := &systemCollector{
		readHost: func(context.Context) (*host.InfoStat, error) {
			return nil, hostErr
		},
		readCPUCounts: func(context.Context, bool) (int, error) {
			countsCalled = true
			return 0, nil
		},
	}
	batch := &Batch{}
	if err := metricCollector.Collect(context.Background(), batch); !errors.Is(err, hostErr) {
		t.Fatalf("Collect() error = %v, want host error", err)
	}
	if countsCalled {
		t.Fatal("CPU counts were read after host discovery failed")
	}
	system, _, _ := batch.Drain()
	if system != nil {
		t.Fatalf("system snapshot = %#v, want nil", system)
	}
}

func TestBuildSystemSample(t *testing.T) {
	info := &host.InfoStat{
		Hostname:             "pulse-host",
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

	sample := buildSystemSample(123, info, "Example Processor", 16_000_000_000, 4, 8)
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
	if sample.ComputerName != "pulse-host" {
		t.Errorf("ComputerName = %q, want pulse-host", sample.ComputerName)
	}
	if sample.ProcessorModel != "Example Processor" {
		t.Errorf("ProcessorModel = %q, want Example Processor", sample.ProcessorModel)
	}
	if sample.MemoryTotalBytes != 16_000_000_000 {
		t.Errorf("MemoryTotalBytes = %d, want 16000000000", sample.MemoryTotalBytes)
	}
}

func TestFirstProcessorModel(t *testing.T) {
	processors := []cpu.InfoStat{
		{ModelName: ""},
		{ModelName: "  Example Processor  "},
	}
	if got := firstProcessorModel(processors); got != "Example Processor" {
		t.Fatalf("firstProcessorModel() = %q, want Example Processor", got)
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
