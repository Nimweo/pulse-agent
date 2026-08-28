package collector

import (
	"context"
	"errors"
	"testing"
	"time"

	gopsutilnet "github.com/shirou/gopsutil/v4/net"
)

func TestNewNetwork(t *testing.T) {
	metricCollector := NewNetwork(3 * time.Second)
	if metricCollector.Name() != "network" {
		t.Errorf("Name() = %q, want network", metricCollector.Name())
	}
	if metricCollector.Interval() != 3*time.Second {
		t.Errorf("Interval() = %s, want 3s", metricCollector.Interval())
	}
}

func TestNetworkCollectorFiltersInterfacesAndEmitsRates(t *testing.T) {
	readIndex := 0
	times := []time.Time{time.Unix(100, 0), time.Unix(102, 0)}
	counters := [][]gopsutilnet.IOCountersStat{
		{
			{Name: "lo0", BytesRecv: 1_000},
			{Name: "en0", BytesRecv: 100, BytesSent: 200},
		},
		{
			{Name: "lo0", BytesRecv: 2_000},
			{Name: "en0", BytesRecv: 300, BytesSent: 600},
		},
	}
	metricCollector := &networkCollector{
		interval: time.Second,
		now: func() time.Time {
			return times[readIndex]
		},
		readInterfaces: func(context.Context) (gopsutilnet.InterfaceStatList, error) {
			return gopsutilnet.InterfaceStatList{
				{Name: "lo0", Flags: []string{"up", "loopback"}},
				{Name: "en0", Flags: []string{"up"}},
			}, nil
		},
		readIOCounters: func(_ context.Context, perInterface bool) ([]gopsutilnet.IOCountersStat, error) {
			if !perInterface {
				t.Fatal("per-interface counters were not requested")
			}
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
	if len(points) != 0 {
		t.Fatalf("first collection emitted %d points, want 0", len(points))
	}

	if err := metricCollector.Collect(context.Background(), batch); err != nil {
		t.Fatalf("second Collect() error = %v", err)
	}
	_, _, points = batch.Drain()
	if len(points) != 8 {
		t.Fatalf("len(points) = %d, want 8", len(points))
	}
	for _, point := range points {
		if point.Device != "en0" {
			t.Errorf("point.Device = %q, loopback should be excluded", point.Device)
		}
	}
}

func TestNetworkCollectorPropagatesReadErrors(t *testing.T) {
	interfaceErr := errors.New("interfaces unavailable")
	countersCalled := false
	metricCollector := &networkCollector{
		now: func() time.Time { return time.Unix(100, 0) },
		readInterfaces: func(context.Context) (gopsutilnet.InterfaceStatList, error) {
			return nil, interfaceErr
		},
		readIOCounters: func(context.Context, bool) ([]gopsutilnet.IOCountersStat, error) {
			countersCalled = true
			return nil, nil
		},
	}
	if err := metricCollector.Collect(context.Background(), &Batch{}); !errors.Is(err, interfaceErr) {
		t.Fatalf("Collect() error = %v, want interface error", err)
	}
	if countersCalled {
		t.Fatal("counters were read after interface discovery failed")
	}

	counterErr := errors.New("counters unavailable")
	metricCollector = &networkCollector{
		now: func() time.Time { return time.Unix(100, 0) },
		readInterfaces: func(context.Context) (gopsutilnet.InterfaceStatList, error) {
			return gopsutilnet.InterfaceStatList{}, nil
		},
		readIOCounters: func(context.Context, bool) ([]gopsutilnet.IOCountersStat, error) {
			return nil, counterErr
		},
	}
	if err := metricCollector.Collect(context.Background(), &Batch{}); !errors.Is(err, counterErr) {
		t.Fatalf("Collect() error = %v, want counter error", err)
	}
}

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

func TestFilterNetworkCounters(t *testing.T) {
	counters := []gopsutilnet.IOCountersStat{
		{Name: "lo0", BytesRecv: 100},
		{Name: "en0", BytesRecv: 200},
	}
	filtered := filterNetworkCounters(counters, map[string]struct{}{"en0": {}})
	if len(filtered) != 1 || filtered["en0"].BytesRecv != 200 {
		t.Fatalf("filtered counters = %#v", filtered)
	}
}
