package collector

import (
	"context"

	"github.com/shirou/gopsutil/v4/cpu"
)

const cpuUsageMetric = "cpu_usage_percent"

func readCPU(ctx context.Context, perCPU bool) (float64, []float64, error) {
	total, err := cpu.PercentWithContext(ctx, 0, false)

	if err != nil {
		return 0, nil, err
	}

	var totalPercent float64
	if len(total) > 0 {
		totalPercent = total[0]
	}

	if !perCPU {
		return totalPercent, nil, nil
	}

	logicalCPUs, err := cpu.PercentWithContext(ctx, 0, true)
	if err != nil {
		return 0, nil, err
	}

	return totalPercent, logicalCPUs, nil
}
