package collector

import (
	"context"
	"time"

	"github.com/nimweo/pulse-agent/internal/model"
)

type coreCollector struct {
	primed bool
}

func (c *coreCollector) Name() string            { return "core" }
func (c *coreCollector) Interval() time.Duration { return time.Second }

func (c *coreCollector) Collect(ctx context.Context, out *Batch) error {
	cpuPct, err := readCPU(ctx)

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

	out.AddCore(model.CoreSample{
		Time:     time.Now().UnixMilli(),
		CPU:      cpuPct,
		MemUsed:  memUsed,
		MemTotal: memTotal,
		Load1:    l1,
		Load5:    l5,
		Load15:   l15,
	})

	return nil
}
