package collector

import (
	"testing"
	"time"

	"github.com/shirou/gopsutil/v4/disk"
)

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
