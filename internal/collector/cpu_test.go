package collector

import (
	"context"
	"testing"
)

func TestReadCPUReportsTotalUsage(t *testing.T) {
	ctx := context.Background()
	_, _, _ = readCPU(ctx, false)

	total, perCPU, err := readCPU(ctx, false)
	if err != nil {
		t.Fatalf("readCPU(false) error = %v", err)
	}
	if perCPU != nil {
		t.Fatalf("readCPU(false) per-CPU values = %#v, want nil", perCPU)
	}
	if total < 0 || total > 100 {
		t.Fatalf("readCPU(false) total = %v, want a percentage from 0 to 100", total)
	}
}

func TestReadCPUReportsLogicalCPUUsage(t *testing.T) {
	ctx := context.Background()
	_, _, _ = readCPU(ctx, true)

	total, perCPU, err := readCPU(ctx, true)
	if err != nil {
		t.Fatalf("readCPU(true) error = %v", err)
	}
	if len(perCPU) == 0 {
		t.Fatal("readCPU(true) returned no logical CPUs")
	}
	if total < 0 || total > 100 {
		t.Fatalf("readCPU(true) total = %v, want a percentage from 0 to 100", total)
	}
	for index, value := range perCPU {
		if value < 0 || value > 100 {
			t.Errorf("readCPU(true) logical CPU %d = %v, want a percentage from 0 to 100", index, value)
		}
	}
}
