package collector

import (
	"sync"
	"testing"
	"time"

	"github.com/nimweo/pulse-agent/internal/model"
)

func TestBatchDrainClearsMetricsAndRetainsSystem(t *testing.T) {
	batch := &Batch{}
	batch.SetSystem(model.SystemSample{Time: 100, OS: "linux"})
	batch.AddCore(model.CoreSample{Time: 101})
	batch.AddPoint(model.Point{Time: 102, Metric: "first"})
	batch.AddSample(
		model.CoreSample{Time: 103},
		[]model.Point{{Time: 104, Metric: "second"}},
	)
	batch.AddPoints([]model.Point{
		{Time: 105, Metric: "third"},
		{Time: 106, Metric: "fourth"},
	})

	system, core, points := batch.Drain()
	if system == nil || system.Time != 100 {
		t.Fatalf("system = %#v, want retained snapshot", system)
	}
	if len(core) != 2 || core[0].Time != 101 || core[1].Time != 103 {
		t.Fatalf("core = %#v, want two ordered samples", core)
	}
	if len(points) != 4 || points[0].Metric != "first" || points[3].Metric != "fourth" {
		t.Fatalf("points = %#v, want four ordered points", points)
	}

	retainedSystem, drainedCore, drainedPoints := batch.Drain()
	if retainedSystem == nil || retainedSystem.Time != 100 {
		t.Fatalf("retained system = %#v", retainedSystem)
	}
	if len(drainedCore) != 0 || len(drainedPoints) != 0 {
		t.Fatalf("drained metrics were returned again: core=%d points=%d", len(drainedCore), len(drainedPoints))
	}
}

func TestBatchSupportsConcurrentWriters(t *testing.T) {
	const writers = 50
	batch := &Batch{}
	var workers sync.WaitGroup
	for index := 0; index < writers; index++ {
		workers.Add(1)
		go func(value int) {
			defer workers.Done()
			batch.AddCore(model.CoreSample{Time: int64(value)})
			batch.AddPoint(model.Point{Time: int64(value), Metric: "concurrent"})
		}(index)
	}
	workers.Wait()

	_, core, points := batch.Drain()
	if len(core) != writers || len(points) != writers {
		t.Fatalf("concurrent metrics = %d/%d, want %d/%d", len(core), len(points), writers, writers)
	}
}

func TestDefaultCollectors(t *testing.T) {
	collectors := Default()
	wantNames := []string{"system", "core", "disk", "network", "gpu"}
	wantIntervals := []time.Duration{time.Minute, time.Second, time.Second, time.Second, 5 * time.Second}
	if len(collectors) != len(wantNames) {
		t.Fatalf("len(Default()) = %d, want %d", len(collectors), len(wantNames))
	}
	for index, metricCollector := range collectors {
		if metricCollector.Name() != wantNames[index] {
			t.Errorf("collector %d name = %q, want %q", index, metricCollector.Name(), wantNames[index])
		}
		if metricCollector.Interval() != wantIntervals[index] {
			t.Errorf("collector %s interval = %s, want %s", metricCollector.Name(), metricCollector.Interval(), wantIntervals[index])
		}
	}
}

func TestNewCore(t *testing.T) {
	metricCollector := NewCore(true, 3*time.Second)
	if metricCollector.Name() != "core" {
		t.Errorf("Name() = %q, want core", metricCollector.Name())
	}
	if metricCollector.Interval() != 3*time.Second {
		t.Errorf("Interval() = %s, want 3s", metricCollector.Interval())
	}
	core, ok := metricCollector.(*coreCollector)
	if !ok || !core.perCPU {
		t.Fatal("NewCore() did not preserve per-CPU collection")
	}
}
