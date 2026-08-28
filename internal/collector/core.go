package collector

import (
	"context"
	"strconv"
	"time"

	"github.com/nimweo/pulse-agent/internal/model"
)

type coreCollector struct {
	primed   bool
	perCPU   bool
	interval time.Duration
}

func (c *coreCollector) Name() string            { return "core" }
func (c *coreCollector) Interval() time.Duration { return c.interval }

func (c *coreCollector) Collect(ctx context.Context, out *Batch) error {
	cpuPct, logicalCPUPct, err := readCPU(ctx, c.perCPU)
	if err != nil {
		return err
	}

	if !c.primed {
		c.primed = true
		return nil
	}

	memUsed, memTotal, err := readMem(ctx)
	if err != nil {
		return err
	}

	l1, l5, l15, err := readLoad(ctx)
	if err != nil {
		return err
	}

	sampleTime := time.Now().UnixMilli()
	coreSample := model.CoreSample{
		Time:     sampleTime,
		CPU:      cpuPct,
		MemUsed:  memUsed,
		MemTotal: memTotal,
		Load1:    l1,
		Load5:    l5,
		Load15:   l15,
	}

	points := make([]model.Point, 0, len(logicalCPUPct))
	for cpuIndex, value := range logicalCPUPct {
		points = append(points, model.Point{
			Time:   sampleTime,
			Metric: cpuUsageMetric,
			Device: "cpu" + strconv.Itoa(cpuIndex),
			Value:  value,
		})
	}
	out.AddSample(coreSample, points)

	return nil
}
