package collector

import (
	"context"
	"sync"
	"time"

	"github.com/nimweo/pulse-agent/internal/model"
)

type Batch struct {
	mu     sync.Mutex
	core   []model.CoreSample
	points []model.Point
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

// Drain returns all collected data and clears the buffer before sending.
func (b *Batch) Drain() ([]model.CoreSample, []model.Point) {
	b.mu.Lock()
	defer b.mu.Unlock()

	core, points := b.core, b.points
	b.core, b.points = nil, nil

	return core, points
}

type Collector interface {
	Name() string
	Interval() time.Duration
	Collect(ctx context.Context, out *Batch) error
}

func Default() []Collector {
	return []Collector{
		NewCore(false, time.Second),
		//&diskCollector{},
		//newNetCollector(),
	}
}

func NewCore(perCPU bool, interval time.Duration) Collector {
	return &coreCollector{perCPU: perCPU, interval: interval}
}
