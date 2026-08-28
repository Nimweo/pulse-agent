package collector

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/nimweo/pulse-agent/internal/model"
	gopsutilprocess "github.com/shirou/gopsutil/v4/process"
)

func TestNewProcess(t *testing.T) {
	metricCollector := NewProcess(
		5*time.Second,
		3,
		4,
		[]string{" nginx ", "NGINX", "redis-server.exe"},
	)
	if metricCollector.Name() != "process" {
		t.Errorf("Name() = %q, want process", metricCollector.Name())
	}
	if metricCollector.Interval() != 5*time.Second {
		t.Errorf("Interval() = %s, want 5s", metricCollector.Interval())
	}

	processes, ok := metricCollector.(*processCollector)
	if !ok {
		t.Fatal("NewProcess() returned an unexpected collector type")
	}
	if processes.topCPU != 3 || processes.topMemory != 4 {
		t.Fatalf("top limits = %d/%d, want 3/4", processes.topCPU, processes.topMemory)
	}
	if len(processes.monitoredProcesses) != 2 ||
		processes.monitoredProcesses[0] != "nginx" ||
		processes.monitoredProcesses[1] != "redis-server" {
		t.Fatalf("monitored processes = %#v", processes.monitoredProcesses)
	}
}

func TestProcessCollectorEmitsCountsTopConsumersAndMonitoredProcesses(t *testing.T) {
	readIndex := 0
	times := []time.Time{time.Unix(100, 0), time.Unix(102, 0)}
	samples := [][]processSnapshot{
		{
			processTestSnapshot(10, 1, "nginx", gopsutilprocess.Sleep, 1, 100),
			processTestSnapshot(11, 1, "nginx", gopsutilprocess.Running, 2, 200),
			processTestSnapshot(20, 1, "postgres", gopsutilprocess.Running, 5, 500),
			processTestSnapshot(30, 1, "defunct", gopsutilprocess.Zombie, 0, 0),
		},
		{
			processTestSnapshot(10, 1, "nginx", gopsutilprocess.Sleep, 1.4, 150),
			processTestSnapshot(11, 1, "nginx", gopsutilprocess.Running, 2.2, 200),
			processTestSnapshot(20, 1, "postgres", gopsutilprocess.Running, 5.8, 600),
			processTestSnapshot(30, 1, "defunct", gopsutilprocess.Zombie, 0, 0),
		},
	}
	metricCollector := &processCollector{
		interval:           time.Second,
		topCPU:             1,
		topMemory:          1,
		monitoredProcesses: []string{"nginx"},
		now: func() time.Time {
			return times[readIndex]
		},
		readProcesses: func(context.Context) ([]processSnapshot, error) {
			result := samples[readIndex]
			readIndex++
			return result, nil
		},
	}
	batch := &Batch{}

	if err := metricCollector.Collect(context.Background(), batch); err != nil {
		t.Fatalf("first Collect() error = %v", err)
	}
	_, _, firstPoints := batch.Drain()
	assertProcessPoint(t, firstPoints, processCountTotalMetric, "", 4)
	assertProcessPoint(t, firstPoints, processCountRunningMetric, "", 2)
	assertProcessPoint(t, firstPoints, processCountSleepingMetric, "", 1)
	assertProcessPoint(t, firstPoints, processCountZombieMetric, "", 1)
	assertProcessPoint(t, firstPoints, processCountOtherMetric, "", 0)
	assertProcessPoint(t, firstPoints, processTopMemoryRSSBytesMetric, "postgres", 500)
	assertProcessPoint(t, firstPoints, processMonitoredInstancesMetric, "nginx", 2)
	assertProcessPoint(t, firstPoints, processMonitoredMemoryMetric, "nginx", 300)
	assertNoProcessMetric(t, firstPoints, processTopCPUPercentMetric)
	assertNoProcessMetric(t, firstPoints, processMonitoredCPUPercentMetric)

	if err := metricCollector.Collect(context.Background(), batch); err != nil {
		t.Fatalf("second Collect() error = %v", err)
	}
	_, _, secondPoints := batch.Drain()
	assertProcessPoint(t, secondPoints, processTopCPUPercentMetric, "postgres", 40)
	assertProcessPoint(t, secondPoints, processTopMemoryRSSBytesMetric, "postgres", 600)
	assertProcessPoint(t, secondPoints, processMonitoredInstancesMetric, "nginx", 2)
	assertProcessPoint(t, secondPoints, processMonitoredMemoryMetric, "nginx", 350)
	assertProcessPoint(t, secondPoints, processMonitoredCPUPercentMetric, "nginx", 30)
}

func TestProcessCollectorReturnsReaderError(t *testing.T) {
	readErr := errors.New("process list unavailable")
	metricCollector := &processCollector{
		now: func() time.Time { return time.Unix(100, 0) },
		readProcesses: func(context.Context) ([]processSnapshot, error) {
			return nil, readErr
		},
	}

	err := metricCollector.Collect(context.Background(), &Batch{})
	if !errors.Is(err, readErr) {
		t.Fatalf("Collect() error = %v, want reader error", err)
	}
}

func TestBuildProcessPointsReportsMissingMonitoredProcessAsZero(t *testing.T) {
	points, _ := buildProcessPoints(
		123,
		nil,
		map[processIdentity]float64{{pid: 1}: 1},
		time.Second,
		0,
		0,
		[]string{"redis-server"},
	)

	assertProcessPoint(t, points, processCountTotalMetric, "", 0)
	assertProcessPoint(t, points, processMonitoredInstancesMetric, "redis-server", 0)
	assertProcessPoint(t, points, processMonitoredMemoryMetric, "redis-server", 0)
	assertProcessPoint(t, points, processMonitoredCPUPercentMetric, "redis-server", 0)
}

func TestBuildProcessPointsSkipsUnavailableStatusCounts(t *testing.T) {
	snapshots := []processSnapshot{{name: "service"}}
	points, _ := buildProcessPoints(123, snapshots, nil, 0, 0, 0, nil)

	assertProcessPoint(t, points, processCountTotalMetric, "", 1)
	assertNoProcessMetric(t, points, processCountRunningMetric)
	assertNoProcessMetric(t, points, processCountSleepingMetric)
	assertNoProcessMetric(t, points, processCountZombieMetric)
}

func TestParseProcessStatuses(t *testing.T) {
	statuses := parseProcessStatuses("  1 Ss\n  25 R+\n  40 Z\ninvalid line\n")

	want := map[int32]string{
		1:  gopsutilprocess.Sleep,
		25: gopsutilprocess.Running,
		40: gopsutilprocess.Zombie,
	}
	if len(statuses) != len(want) {
		t.Fatalf("statuses = %#v, want %#v", statuses, want)
	}
	for pid, status := range want {
		if statuses[pid] != status {
			t.Errorf("status for PID %d = %q, want %q", pid, statuses[pid], status)
		}
	}
}

func processTestSnapshot(
	pid int32,
	startedAt int64,
	name string,
	status string,
	cpuSeconds float64,
	memoryRSS uint64,
) processSnapshot {
	return processSnapshot{
		identity: processIdentity{
			pid:       pid,
			startedAt: startedAt,
			name:      processNameKey(name),
		},
		name:            name,
		status:          status,
		statusAvailable: true,
		cpuSeconds:      cpuSeconds,
		cpuAvailable:    true,
		memoryRSS:       memoryRSS,
		memoryAvailable: true,
	}
}

func assertProcessPoint(
	t *testing.T,
	points []model.Point,
	metric string,
	device string,
	want float64,
) {
	t.Helper()
	for _, point := range points {
		if point.Metric != metric || point.Device != device {
			continue
		}
		if math.Abs(point.Value-want) > 0.000001 {
			t.Fatalf("%s/%s value = %v, want %v", metric, device, point.Value, want)
		}
		return
	}
	t.Fatalf("missing point %s/%s in %#v", metric, device, points)
}

func assertNoProcessMetric(t *testing.T, points []model.Point, metric string) {
	t.Helper()
	for _, point := range points {
		if point.Metric == metric {
			t.Fatalf("unexpected %s point: %#v", metric, point)
		}
	}
}
