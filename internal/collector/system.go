package collector

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"time"

	"github.com/nimweo/pulse-agent/internal/model"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/host"
)

type systemCollector struct {
	interval time.Duration
}

func NewSystem(interval time.Duration) Collector {
	return &systemCollector{interval: interval}
}

func (c *systemCollector) Name() string            { return "system" }
func (c *systemCollector) Interval() time.Duration { return c.interval }

func (c *systemCollector) Collect(ctx context.Context, out *Batch) error {
	info, err := host.InfoWithContext(ctx)
	if err != nil {
		return fmt.Errorf("read system information: %w", err)
	}

	physicalCores, physicalErr := cpu.CountsWithContext(ctx, false)
	logicalCPUs, logicalErr := cpu.CountsWithContext(ctx, true)
	if logicalCPUs <= 0 {
		logicalCPUs = runtime.NumCPU()
	}

	out.SetSystem(buildSystemSample(time.Now().UnixMilli(), info, physicalCores, logicalCPUs))

	return errors.Join(
		wrapSystemError("read physical CPU count", physicalErr),
		wrapSystemError("read logical CPU count", logicalErr),
	)
}

func buildSystemSample(
	timestamp int64,
	info *host.InfoStat,
	physicalCores int,
	logicalCPUs int,
) model.SystemSample {
	return model.SystemSample{
		Time:                 timestamp,
		UptimeSeconds:        info.Uptime,
		BootTime:             int64(info.BootTime) * 1000,
		Processes:            info.Procs,
		OS:                   info.OS,
		Platform:             info.Platform,
		PlatformFamily:       info.PlatformFamily,
		PlatformVersion:      info.PlatformVersion,
		KernelVersion:        info.KernelVersion,
		KernelArchitecture:   info.KernelArch,
		VirtualizationSystem: info.VirtualizationSystem,
		VirtualizationRole:   info.VirtualizationRole,
		PhysicalCores:        physicalCores,
		LogicalCPUs:          logicalCPUs,
	}
}

func wrapSystemError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}
