package collector

import (
	"context"

	"github.com/shirou/gopsutil/v4/load"
)

func readLoad(ctx context.Context) (l1, l5, l15 float64, err error) {
	avg, err := load.AvgWithContext(ctx)

	if err != nil {
		return 0, 0, 0, err
	}

	return avg.Load1, avg.Load5, avg.Load15, nil
}
