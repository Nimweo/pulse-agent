package collector

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCoreCollectorPrimesBeforeEmittingMetrics(t *testing.T) {
	readIndex := 0
	loadCalls := 0
	times := []time.Time{
		time.Unix(100, 0),
		time.Unix(102, 0),
	}
	memories := []memoryStats{
		{used: 200, total: 1_000, swapIn: 100, swapOut: 200},
		{used: 300, total: 1_000, swapTotal: 800, swapUsed: 200, swapUsedPercent: 25, swapIn: 500, swapOut: 1_000},
	}
	metricCollector := &coreCollector{
		perCPU:   true,
		interval: time.Second,
		readCPU: func(_ context.Context, perCPU bool) (float64, []float64, error) {
			if !perCPU {
				t.Fatal("per-CPU flag was not forwarded")
			}
			return 30, []float64{10, 20}, nil
		},
		readMemory: func(context.Context) (memoryStats, error) {
			memory := memories[readIndex]
			return memory, nil
		},
		readLoad: func(context.Context) (float64, float64, float64, error) {
			loadCalls++
			return 1, 2, 3, nil
		},
		now: func() time.Time {
			current := times[readIndex]
			readIndex++
			return current
		},
	}
	batch := &Batch{}

	if err := metricCollector.Collect(context.Background(), batch); err != nil {
		t.Fatalf("first Collect() error = %v", err)
	}
	_, core, points := batch.Drain()
	if len(core) != 0 || len(points) != 0 {
		t.Fatalf("first collection emitted metrics: core=%d points=%d", len(core), len(points))
	}
	if loadCalls != 0 {
		t.Fatalf("load calls after priming = %d, want 0", loadCalls)
	}

	if err := metricCollector.Collect(context.Background(), batch); err != nil {
		t.Fatalf("second Collect() error = %v", err)
	}
	_, core, points = batch.Drain()
	if len(core) != 1 {
		t.Fatalf("len(core) = %d, want 1", len(core))
	}
	if core[0].Time != times[1].UnixMilli() || core[0].CPU != 30 || core[0].MemUsed != 300 || core[0].MemTotal != 1_000 {
		t.Errorf("core sample = %#v", core[0])
	}
	if core[0].Load1 != 1 || core[0].Load5 != 2 || core[0].Load15 != 3 {
		t.Errorf("load averages = %v/%v/%v", core[0].Load1, core[0].Load5, core[0].Load15)
	}
	if len(points) != 7 {
		t.Fatalf("len(points) = %d, want 7", len(points))
	}

	want := map[string]float64{
		"cpu0":                 10,
		"cpu1":                 20,
		swapTotalBytesMetric:   800,
		swapUsedBytesMetric:    200,
		swapUsedPercentMetric:  25,
		swapInBytesRateMetric:  200,
		swapOutBytesRateMetric: 400,
	}
	for _, point := range points {
		key := point.Metric
		if point.Metric == cpuUsageMetric {
			key = point.Device
		}
		if point.Value != want[key] {
			t.Errorf("%s value = %v, want %v", key, point.Value, want[key])
		}
	}
}

func TestCoreCollectorPropagatesReadErrors(t *testing.T) {
	cpuErr := errors.New("CPU unavailable")
	memoryCalled := false
	metricCollector := &coreCollector{
		readCPU: func(context.Context, bool) (float64, []float64, error) {
			return 0, nil, cpuErr
		},
		readMemory: func(context.Context) (memoryStats, error) {
			memoryCalled = true
			return memoryStats{}, nil
		},
	}
	if err := metricCollector.Collect(context.Background(), &Batch{}); !errors.Is(err, cpuErr) {
		t.Fatalf("Collect() error = %v, want CPU error", err)
	}
	if memoryCalled {
		t.Fatal("memory was read after the CPU read failed")
	}

	memoryErr := errors.New("memory unavailable")
	metricCollector = &coreCollector{
		readCPU: func(context.Context, bool) (float64, []float64, error) {
			return 0, nil, nil
		},
		readMemory: func(context.Context) (memoryStats, error) {
			return memoryStats{}, memoryErr
		},
	}
	if err := metricCollector.Collect(context.Background(), &Batch{}); !errors.Is(err, memoryErr) {
		t.Fatalf("Collect() error = %v, want memory error", err)
	}
}

func TestCoreCollectorDoesNotBufferPartialSampleWhenLoadFails(t *testing.T) {
	loadErr := errors.New("load unavailable")
	metricCollector := &coreCollector{
		primed:      true,
		lastMemRead: time.Unix(100, 0),
		readCPU: func(context.Context, bool) (float64, []float64, error) {
			return 50, nil, nil
		},
		readMemory: func(context.Context) (memoryStats, error) {
			return memoryStats{used: 10, total: 100}, nil
		},
		readLoad: func(context.Context) (float64, float64, float64, error) {
			return 0, 0, 0, loadErr
		},
		now: func() time.Time { return time.Unix(101, 0) },
	}
	batch := &Batch{}
	if err := metricCollector.Collect(context.Background(), batch); !errors.Is(err, loadErr) {
		t.Fatalf("Collect() error = %v, want load error", err)
	}
	_, core, points := batch.Drain()
	if len(core) != 0 || len(points) != 0 {
		t.Fatalf("partial sample was buffered: core=%d points=%d", len(core), len(points))
	}
}
