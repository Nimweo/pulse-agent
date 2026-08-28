package collector

import (
	"testing"
	"time"

	gopsutilnet "github.com/shirou/gopsutil/v4/net"
)

func TestBuildNetworkIOPointsCalculatesRates(t *testing.T) {
	previous := map[string]gopsutilnet.IOCountersStat{
		"en0": {
			BytesRecv:   100,
			BytesSent:   200,
			PacketsRecv: 10,
			PacketsSent: 20,
			Errin:       1,
			Errout:      2,
			Dropin:      3,
			Dropout:     4,
		},
	}
	current := map[string]gopsutilnet.IOCountersStat{
		"en0": {
			BytesRecv:   300,
			BytesSent:   600,
			PacketsRecv: 16,
			PacketsSent: 30,
			Errin:       3,
			Errout:      6,
			Dropin:      7,
			Dropout:     10,
		},
	}

	points := buildNetworkIOPoints(123, current, previous, 2*time.Second)
	if len(points) != 8 {
		t.Fatalf("len(points) = %d, want 8", len(points))
	}

	want := map[string]float64{
		networkReceiveBytesRateMetric:    100,
		networkTransmitBytesRateMetric:   200,
		networkReceivePacketsRateMetric:  3,
		networkTransmitPacketsRateMetric: 5,
		networkReceiveErrorsRateMetric:   1,
		networkTransmitErrorsRateMetric:  2,
		networkReceiveDropsRateMetric:    2,
		networkTransmitDropsRateMetric:   3,
	}
	for _, point := range points {
		if point.Time != 123 {
			t.Errorf("point.Time = %d, want 123", point.Time)
		}
		if point.Device != "en0" {
			t.Errorf("point.Device = %q, want en0", point.Device)
		}
		if point.Value != want[point.Metric] {
			t.Errorf("%s value = %v, want %v", point.Metric, point.Value, want[point.Metric])
		}
	}
}

func TestBuildNetworkIOPointsSkipsFirstSample(t *testing.T) {
	current := map[string]gopsutilnet.IOCountersStat{
		"en0": {BytesRecv: 100},
	}

	points := buildNetworkIOPoints(123, current, nil, time.Second)
	if len(points) != 0 {
		t.Fatalf("len(points) = %d, want 0", len(points))
	}
}

func TestBuildNetworkIOPointsSkipsResetCounters(t *testing.T) {
	previous := map[string]gopsutilnet.IOCountersStat{
		"en0": {BytesRecv: 200},
	}
	current := map[string]gopsutilnet.IOCountersStat{
		"en0": {BytesRecv: 100},
	}

	points := buildNetworkIOPoints(123, current, previous, time.Second)
	for _, point := range points {
		if point.Metric == networkReceiveBytesRateMetric {
			t.Fatal("receive byte rate was emitted after the counter reset")
		}
	}
}

func TestNonLoopbackInterfaceNames(t *testing.T) {
	interfaces := gopsutilnet.InterfaceStatList{
		{Name: "lo0", Flags: []string{"up", "loopback"}},
		{Name: "en0", Flags: []string{"up", "broadcast"}},
	}

	names := nonLoopbackInterfaceNames(interfaces)
	if _, ok := names["lo0"]; ok {
		t.Error("loopback interface was included")
	}
	if _, ok := names["en0"]; !ok {
		t.Error("non-loopback interface was excluded")
	}
}
