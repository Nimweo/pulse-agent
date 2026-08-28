package collector

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/nimweo/pulse-agent/internal/model"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
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
	processorInfo, processorErr := cpu.InfoWithContext(ctx)
	memory, memoryErr := mem.VirtualMemoryWithContext(ctx)
	var memoryTotal uint64
	if memory != nil {
		memoryTotal = memory.Total
	}

	out.SetSystem(buildSystemSample(
		time.Now().UnixMilli(),
		info,
		firstProcessorModel(processorInfo),
		memoryTotal,
		physicalCores,
		logicalCPUs,
	))

	return errors.Join(
		wrapSystemError("read physical CPU count", physicalErr),
		wrapSystemError("read logical CPU count", logicalErr),
		wrapSystemError("read processor information", processorErr),
		wrapSystemError("read total memory", memoryErr),
	)
}

func buildSystemSample(
	timestamp int64,
	info *host.InfoStat,
	processorModel string,
	memoryTotal uint64,
	physicalCores int,
	logicalCPUs int,
) model.SystemSample {
	return model.SystemSample{
		Time:                 timestamp,
		ComputerName:         info.Hostname,
		UptimeSeconds:        info.Uptime,
		BootTime:             int64(info.BootTime) * 1000,
		Processes:            info.Procs,
		OS:                   info.OS,
		Platform:             info.Platform,
		PlatformFamily:       info.PlatformFamily,
		PlatformVersion:      info.PlatformVersion,
		KernelVersion:        info.KernelVersion,
		KernelArchitecture:   info.KernelArch,
		ProcessorModel:       processorModel,
		MemoryTotalBytes:     memoryTotal,
		VirtualizationSystem: info.VirtualizationSystem,
		VirtualizationRole:   info.VirtualizationRole,
		PhysicalCores:        physicalCores,
		LogicalCPUs:          logicalCPUs,
	}
}

func firstProcessorModel(processors []cpu.InfoStat) string {
	for _, processor := range processors {
		if modelName := strings.TrimSpace(processor.ModelName); modelName != "" {
			return modelName
		}
	}
	return ""
}

func wrapSystemError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}
