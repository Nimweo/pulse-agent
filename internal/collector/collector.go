package collector

import (
	"context"
	"sync"
	"time"

	"github.com/nimweo/pulse-agent/internal/model"
)

type Batch struct {
	mu     sync.Mutex
	system *model.SystemSample
	core   []model.CoreSample
	points []model.Point
}

func (b *Batch) SetSystem(system model.SystemSample) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.system = &system
}

func (b *Batch) AddCore(core model.CoreSample) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.core = append(b.core, core)
}

func (b *Batch) AddPoint(point model.Point) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.points = append(b.points, point)
}

func (b *Batch) AddSample(core model.CoreSample, points []model.Point) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.core = append(b.core, core)
	b.points = append(b.points, points...)
}

func (b *Batch) AddPoints(points []model.Point) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.points = append(b.points, points...)
}

func newPoint(timestamp int64, metric string, device string, value float64) model.Point {
	return model.Point{
		Time:   timestamp,
		Metric: metric,
		Device: device,
		Value:  value,
	}
}

func appendCounterRate(
	points []model.Point,
	timestamp int64,
	metric string,
	device string,
	current uint64,
	previous uint64,
	seconds float64,
) []model.Point {
	if current < previous {
		return points
	}

	value := float64(current-previous) / seconds
	return append(points, newPoint(timestamp, metric, device, value))
}

// Drain returns the latest system snapshot and all buffered metrics.
// The system snapshot is retained so it can be included in every payload.
func (b *Batch) Drain() (*model.SystemSample, []model.CoreSample, []model.Point) {
	b.mu.Lock()
	defer b.mu.Unlock()

	system, core, points := b.system, b.core, b.points

	b.core, b.points = nil, nil

	return system, core, points
}

type Collector interface {
	Name() string
	Interval() time.Duration
	Collect(ctx context.Context, out *Batch) error
}

func Default() []Collector {
	return []Collector{
		NewSystem(time.Minute),
		NewCore(false, time.Second),
		NewDisk(time.Second),
		NewNetwork(time.Second),
	}
}

func NewCore(perCPU bool, interval time.Duration) Collector {
	return &coreCollector{perCPU: perCPU, interval: interval}
}
