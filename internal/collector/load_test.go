package collector

import (
	"context"
	"math"
	"testing"
)

func TestReadLoadReportsFiniteNonNegativeAverages(t *testing.T) {
	l1, l5, l15, err := readLoad(context.Background())
	if err != nil {
		t.Skipf("load averages are unavailable on this platform: %v", err)
	}
	for name, value := range map[string]float64{"1m": l1, "5m": l5, "15m": l15} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			t.Errorf("load average %s = %v, want a finite non-negative value", name, value)
		}
	}
}
