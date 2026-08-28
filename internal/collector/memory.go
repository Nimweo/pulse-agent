package collector

import (
	"context"
	"fmt"
	"time"

	"github.com/nimweo/pulse-agent/internal/model"
	"github.com/shirou/gopsutil/v4/mem"
)

const (
	swapTotalBytesMetric   = "swap_total_bytes"
	swapUsedBytesMetric    = "swap_used_bytes"
	swapUsedPercentMetric  = "swap_used_percent"
	swapInBytesRateMetric  = "swap_in_bytes_per_second"
	swapOutBytesRateMetric = "swap_out_bytes_per_second"
)

const systemDevice = "system"

type memoryStats struct {
	used            uint64
	total           uint64
	swapTotal       uint64
	swapUsed        uint64
	swapUsedPercent float64
	swapIn          uint64
	swapOut         uint64
}

func readMem(ctx context.Context) (memoryStats, error) {
	memory, err := mem.VirtualMemoryWithContext(ctx)

	if err != nil {
		return memoryStats{}, fmt.Errorf("read virtual memory: %w", err)
	}

	swap, err := mem.SwapMemoryWithContext(ctx)
	if err != nil {
		return memoryStats{}, fmt.Errorf("read swap memory: %w", err)
	}

	return memoryStats{
		used:            memory.Used,
		total:           memory.Total,
		swapTotal:       swap.Total,
		swapUsed:        swap.Used,
		swapUsedPercent: swap.UsedPercent,
		swapIn:          swap.Sin,
		swapOut:         swap.Sout,
	}, nil
}

func buildSwapPoints(
	timestamp int64,
	current memoryStats,
	previous memoryStats,
	elapsed time.Duration,
) []model.Point {
	points := []model.Point{
		newPoint(timestamp, swapTotalBytesMetric, systemDevice, float64(current.swapTotal)),
		newPoint(timestamp, swapUsedBytesMetric, systemDevice, float64(current.swapUsed)),
		newPoint(timestamp, swapUsedPercentMetric, systemDevice, current.swapUsedPercent),
	}

	if elapsed <= 0 {
		return points
	}

	seconds := elapsed.Seconds()
	points = appendCounterRate(
		points,
		timestamp,
		swapInBytesRateMetric,
		systemDevice,
		current.swapIn,
		previous.swapIn,
		seconds,
	)
	points = appendCounterRate(
		points,
		timestamp,
		swapOutBytesRateMetric,
		systemDevice,
		current.swapOut,
		previous.swapOut,
		seconds,
	)

	return points
}
