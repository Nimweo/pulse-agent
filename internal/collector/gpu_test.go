package collector

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseNVIDIASMI(t *testing.T) {
	output := []byte("0, NVIDIA RTX 4090, 25, 1024, 24564, 51, 120.5\n")
	points, err := parseNVIDIASMI(output, 123)
	if err != nil {
		t.Fatalf("parseNVIDIASMI() error = %v", err)
	}
	if len(points) != 7 {
		t.Fatalf("len(points) = %d, want 7", len(points))
	}

	want := map[string]float64{
		gpuPresentMetric:           1,
		gpuUsagePercentMetric:      25,
		gpuMemoryUsedBytesMetric:   1024 * 1024 * 1024,
		gpuMemoryTotalBytesMetric:  24564 * 1024 * 1024,
		gpuMemoryUsedPercentMetric: float64(1024) / 24564 * 100,
		gpuTemperatureMetric:       51,
		gpuPowerMetric:             120.5,
	}
	for _, point := range points {
		if point.Time != 123 {
			t.Errorf("point.Time = %d, want 123", point.Time)
		}
		if point.Device != "gpu0: NVIDIA RTX 4090" {
			t.Errorf("point.Device = %q", point.Device)
		}
		if point.Value != want[point.Metric] {
			t.Errorf("%s value = %v, want %v", point.Metric, point.Value, want[point.Metric])
		}
	}
}

func TestParseNVIDIASMIOmitsUnsupportedMetrics(t *testing.T) {
	output := []byte("0, NVIDIA GPU, N/A, [N/A], 8192, [Not Supported], N/A\n")
	points, err := parseNVIDIASMI(output, 123)
	if err != nil {
		t.Fatalf("parseNVIDIASMI() error = %v", err)
	}
	if len(points) != 2 {
		t.Fatalf("len(points) = %d, want present and total memory", len(points))
	}
	if points[0].Metric != gpuPresentMetric || points[1].Metric != gpuMemoryTotalBytesMetric {
		t.Fatalf("metrics = %q/%q", points[0].Metric, points[1].Metric)
	}
}

func TestReadLinuxGPUPoints(t *testing.T) {
	root := t.TempDir()
	devicePath := filepath.Join(root, "card0", "device")
	hwmonPath := filepath.Join(devicePath, "hwmon", "hwmon0")
	if err := os.MkdirAll(hwmonPath, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	writeGPUFixture(t, filepath.Join(devicePath, "vendor"), "0x1002\n")
	writeGPUFixture(t, filepath.Join(devicePath, "gpu_busy_percent"), "40\n")
	writeGPUFixture(t, filepath.Join(devicePath, "mem_info_vram_used"), "100\n")
	writeGPUFixture(t, filepath.Join(devicePath, "mem_info_vram_total"), "400\n")
	writeGPUFixture(t, filepath.Join(hwmonPath, "temp1_input"), "55000\n")
	writeGPUFixture(t, filepath.Join(hwmonPath, "power1_average"), "75000000\n")

	points, err := readLinuxGPUPoints(root, 123)
	if err != nil {
		t.Fatalf("readLinuxGPUPoints() error = %v", err)
	}
	if len(points) != 7 {
		t.Fatalf("len(points) = %d, want 7", len(points))
	}

	want := map[string]float64{
		gpuPresentMetric:           1,
		gpuUsagePercentMetric:      40,
		gpuMemoryUsedBytesMetric:   100,
		gpuMemoryTotalBytesMetric:  400,
		gpuMemoryUsedPercentMetric: 25,
		gpuTemperatureMetric:       55,
		gpuPowerMetric:             75,
	}
	for _, point := range points {
		if point.Device != "card0: 0x1002" {
			t.Errorf("point.Device = %q", point.Device)
		}
		if point.Value != want[point.Metric] {
			t.Errorf("%s value = %v, want %v", point.Metric, point.Value, want[point.Metric])
		}
	}
}

func TestParseDarwinGPUs(t *testing.T) {
	output := []byte(`{"SPDisplaysDataType":[{"sppci_model":"Apple M4 Pro","spdisplays_vram":"16 GB"}]}`)
	devices, err := parseDarwinGPUs(output)
	if err != nil {
		t.Fatalf("parseDarwinGPUs() error = %v", err)
	}
	points := buildStaticGPUPoints(devices, 123)
	if len(points) != 2 {
		t.Fatalf("len(points) = %d, want 2", len(points))
	}
	if points[0].Device != "gpu0: Apple M4 Pro" {
		t.Errorf("device = %q", points[0].Device)
	}
	if points[1].Value != 16*1024*1024*1024 {
		t.Errorf("memory = %v", points[1].Value)
	}
}

func TestParseWindowsGPUsAcceptsSingleDevice(t *testing.T) {
	devices, err := parseWindowsGPUs([]byte(`{"Name":"Example GPU","AdapterRAM":4294967296}`))
	if err != nil {
		t.Fatalf("parseWindowsGPUs() error = %v", err)
	}
	points := buildStaticGPUPoints(devices, 123)
	if len(points) != 2 {
		t.Fatalf("len(points) = %d, want 2", len(points))
	}
	if points[0].Device != "gpu0: Example GPU" || points[1].Value != 4294967296 {
		t.Fatalf("unexpected points: %#v", points)
	}
}

func TestNewGPU(t *testing.T) {
	metricCollector := NewGPU(5 * time.Second)
	if metricCollector.Name() != "gpu" {
		t.Errorf("Name() = %q, want gpu", metricCollector.Name())
	}
	if metricCollector.Interval() != 5*time.Second {
		t.Errorf("Interval() = %s, want 5s", metricCollector.Interval())
	}
}

func writeGPUFixture(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}
