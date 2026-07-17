package collector

import (
	"context"

	"github.com/shirou/gopsutil/v4/mem"
)

func readMem(ctx context.Context) (used, total uint64, err error) {
	m, err := mem.VirtualMemoryWithContext(ctx)

	if err != nil {
		return 0, 0, err
	}

	return m.Used, m.Total, nil
}
