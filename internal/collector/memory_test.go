package collector

import (
	"testing"
	"time"
)

func TestBuildSwapPointsCalculatesUsageAndRates(t *testing.T) {
	previous := memoryStats{
		swapIn:  100,
		swapOut: 200,
	}
	current := memoryStats{
		swapTotal:       1_000,
		swapUsed:        250,
		swapUsedPercent: 25,
		swapIn:          300,
		swapOut:         600,
	}

	points := buildSwapPoints(123, current, previous, 2*time.Second)
	if len(points) != 5 {
		t.Fatalf("len(points) = %d, want 5", len(points))
	}

	want := map[string]float64{
		swapTotalBytesMetric:   1_000,
		swapUsedBytesMetric:    250,
		swapUsedPercentMetric:  25,
		swapInBytesRateMetric:  100,
		swapOutBytesRateMetric: 200,
	}
	for _, point := range points {
		if point.Time != 123 {
			t.Errorf("point.Time = %d, want 123", point.Time)
		}
		if point.Device != systemDevice {
			t.Errorf("point.Device = %q, want %q", point.Device, systemDevice)
		}
		if point.Value != want[point.Metric] {
			t.Errorf("%s value = %v, want %v", point.Metric, point.Value, want[point.Metric])
		}
	}
}

func TestBuildSwapPointsOmitsRatesWithoutElapsedTime(t *testing.T) {
	current := memoryStats{
		swapTotal: 1_000,
		swapUsed:  250,
	}

	points := buildSwapPoints(123, current, memoryStats{}, 0)
	if len(points) != 3 {
		t.Fatalf("len(points) = %d, want 3", len(points))
	}
}

func TestBuildSwapPointsSkipsResetCounters(t *testing.T) {
	previous := memoryStats{swapIn: 200}
	current := memoryStats{swapIn: 100}

	points := buildSwapPoints(123, current, previous, time.Second)
	for _, point := range points {
		if point.Metric == swapInBytesRateMetric {
			t.Fatal("swap-in rate was emitted after the counter reset")
		}
	}
}
