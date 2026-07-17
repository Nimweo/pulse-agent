package collector

import (
	"context"

	"github.com/shirou/gopsutil/v4/cpu"
)

func readCPU(ctx context.Context) (float64, error) {
	pct, err := cpu.PercentWithContext(ctx, 0, false)

	if err != nil {
		return 0, err
	}

	if len(pct) == 0 {
		return 0, nil
	}

	return pct[0], nil
}
