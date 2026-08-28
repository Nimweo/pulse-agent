package collector

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nimweo/pulse-agent/internal/model"
	"github.com/shirou/gopsutil/v4/disk"
)

func TestNewDisk(t *testing.T) {
	metricCollector := NewDisk(3 * time.Second)
	if metricCollector.Name() != "disk" {
		t.Errorf("Name() = %q, want disk", metricCollector.Name())
	}
	if metricCollector.Interval() != 3*time.Second {
		t.Errorf("Interval() = %s, want 3s", metricCollector.Interval())
	}
}

func TestDiskCollectorPrimesIOAndEmitsRates(t *testing.T) {
	readIndex := 0
	times := []time.Time{time.Unix(100, 0), time.Unix(102, 0)}
	counters := []map[string]disk.IOCountersStat{
		{"disk0": {ReadBytes: 100, WriteBytes: 200, ReadCount: 10, WriteCount: 20}},
		{"disk0": {ReadBytes: 300, WriteBytes: 600, ReadCount: 16, WriteCount: 30}},
	}
	metricCollector := &diskCollector{
		interval: time.Second,
		now: func() time.Time {
			return times[readIndex]
		},
		readUsage: func(_ context.Context, timestamp int64) ([]model.Point, error) {
			return []model.Point{newPoint(timestamp, diskUsedPercentMetric, "/", 50)}, nil
		},
		readIOCounters: func(context.Context) (map[string]disk.IOCountersStat, error) {
			result := counters[readIndex]
			readIndex++
			return result, nil
		},
	}
	batch := &Batch{}

	if err := metricCollector.Collect(context.Background(), batch); err != nil {
		t.Fatalf("first Collect() error = %v", err)
	}
	_, _, points := batch.Drain()
	if len(points) != 1 || points[0].Metric != diskUsedPercentMetric {
		t.Fatalf("first points = %#v, want usage only", points)
	}

	if err := metricCollector.Collect(context.Background(), batch); err != nil {
		t.Fatalf("second Collect() error = %v", err)
	}
	_, _, points = batch.Drain()
	if len(points) != 5 {
		t.Fatalf("len(points) = %d, want usage and four I/O rates", len(points))
	}
	wantRates := map[string]float64{
		diskReadBytesRateMetric:  100,
		diskWriteBytesRateMetric: 200,
		diskReadOpsRateMetric:    3,
		diskWriteOpsRateMetric:   5,
	}
	for _, point := range points[1:] {
		if point.Value != wantRates[point.Metric] {
			t.Errorf("%s value = %v, want %v", point.Metric, point.Value, wantRates[point.Metric])
		}
	}
}

func TestDiskCollectorBuffersUsageWhenIOFails(t *testing.T) {
	ioErr := errors.New("I/O unavailable")
	metricCollector := &diskCollector{
		now: func() time.Time { return time.Unix(100, 0) },
		readUsage: func(_ context.Context, timestamp int64) ([]model.Point, error) {
			return []model.Point{newPoint(timestamp, diskUsedPercentMetric, "/", 50)}, nil
		},
		readIOCounters: func(context.Context) (map[string]disk.IOCountersStat, error) {
			return nil, ioErr
		},
	}
	batch := &Batch{}
	if err := metricCollector.Collect(context.Background(), batch); !errors.Is(err, ioErr) {
		t.Fatalf("Collect() error = %v, want I/O error", err)
	}
	_, _, points := batch.Drain()
	if len(points) != 1 || points[0].Metric != diskUsedPercentMetric {
		t.Fatalf("points = %#v, want retained usage point", points)
	}
}

func TestBuildDiskIOPointsCalculatesRates(t *testing.T) {
	previous := map[string]disk.IOCountersStat{
		"disk0": {
			ReadBytes:  100,
			WriteBytes: 200,
			ReadCount:  10,
			WriteCount: 20,
		},
	}
	current := map[string]disk.IOCountersStat{
		"disk0": {
			ReadBytes:  300,
			WriteBytes: 600,
			ReadCount:  16,
			WriteCount: 30,
		},
	}

	points := buildDiskIOPoints(123, current, previous, 2*time.Second)
	if len(points) != 4 {
		t.Fatalf("len(points) = %d, want 4", len(points))
	}

	want := map[string]float64{
		diskReadBytesRateMetric:  100,
		diskWriteBytesRateMetric: 200,
		diskReadOpsRateMetric:    3,
		diskWriteOpsRateMetric:   5,
	}
	for _, point := range points {
		if point.Time != 123 {
			t.Errorf("point.Time = %d, want 123", point.Time)
		}
		if point.Device != "disk0" {
			t.Errorf("point.Device = %q, want disk0", point.Device)
		}
		if point.Value != want[point.Metric] {
			t.Errorf("%s value = %v, want %v", point.Metric, point.Value, want[point.Metric])
		}
	}
}

func TestBuildDiskIOPointsSkipsFirstSample(t *testing.T) {
	current := map[string]disk.IOCountersStat{
		"disk0": {ReadBytes: 100},
	}

	points := buildDiskIOPoints(123, current, nil, time.Second)
	if len(points) != 0 {
		t.Fatalf("len(points) = %d, want 0", len(points))
	}
}

func TestBuildDiskIOPointsSkipsResetCounters(t *testing.T) {
	previous := map[string]disk.IOCountersStat{
		"disk0": {ReadBytes: 200},
	}
	current := map[string]disk.IOCountersStat{
		"disk0": {ReadBytes: 100},
	}

	points := buildDiskIOPoints(123, current, previous, time.Second)
	for _, point := range points {
		if point.Metric == diskReadBytesRateMetric {
			t.Fatal("read byte rate was emitted after the counter reset")
		}
	}
}

func TestBuildDiskIOPointsSortsDevicesAndSkipsNewOnes(t *testing.T) {
	previous := map[string]disk.IOCountersStat{
		"disk1": {ReadBytes: 100},
		"disk2": {ReadBytes: 100},
	}
	current := map[string]disk.IOCountersStat{
		"disk2": {ReadBytes: 200},
		"disk1": {ReadBytes: 200},
		"disk3": {ReadBytes: 200},
	}

	points := buildDiskIOPoints(123, current, previous, time.Second)
	if len(points) != 8 {
		t.Fatalf("len(points) = %d, want rates for two known disks", len(points))
	}
	if points[0].Device != "disk1" || points[4].Device != "disk2" {
		t.Fatalf("device order = %q/%q, want disk1/disk2", points[0].Device, points[4].Device)
	}
}
