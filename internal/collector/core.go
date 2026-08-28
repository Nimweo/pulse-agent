package collector

import (
	"context"
	"strconv"
	"time"

	"github.com/nimweo/pulse-agent/internal/model"
)

type coreCollector struct {
	primed      bool
	perCPU      bool
	interval    time.Duration
	lastMemory  memoryStats
	lastMemRead time.Time
}

func (c *coreCollector) Name() string            { return "core" }
func (c *coreCollector) Interval() time.Duration { return c.interval }

func (c *coreCollector) Collect(ctx context.Context, out *Batch) error {
	cpuPct, logicalCPUPct, err := readCPU(ctx, c.perCPU)

	if err != nil {
		return err
	}

	memory, err := readMem(ctx)
	if err != nil {
		return err
	}
	memoryReadAt := time.Now()

	if !c.primed {
		c.primed = true
		c.lastMemory = memory
		c.lastMemRead = memoryReadAt
		return nil
	}

	l1, l5, l15, err := readLoad(ctx)

	if err != nil {
		return err
	}

	sampleTime := memoryReadAt.UnixMilli()
	coreSample := model.CoreSample{
		Time:     sampleTime,
		CPU:      cpuPct,
		MemUsed:  memory.used,
		MemTotal: memory.total,
		Load1:    l1,
		Load5:    l5,
		Load15:   l15,
	}

	points := make([]model.Point, 0, len(logicalCPUPct)+5)
	for cpuIndex, value := range logicalCPUPct {
		points = append(points, model.Point{
			Time:   sampleTime,
			Metric: cpuUsageMetric,
			Device: "cpu" + strconv.Itoa(cpuIndex),
			Value:  value,
		})
	}
	points = append(
		points,
		buildSwapPoints(
			sampleTime,
			memory,
			c.lastMemory,
			memoryReadAt.Sub(c.lastMemRead),
		)...,
	)
	out.AddSample(coreSample, points)
	c.lastMemory = memory
	c.lastMemRead = memoryReadAt

	return nil
}
