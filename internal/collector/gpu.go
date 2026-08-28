package collector

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nimweo/pulse-agent/internal/model"
)

const (
	gpuPresentMetric           = "gpu_present"
	gpuUsagePercentMetric      = "gpu_usage_percent"
	gpuMemoryUsedBytesMetric   = "gpu_memory_used_bytes"
	gpuMemoryTotalBytesMetric  = "gpu_memory_total_bytes"
	gpuMemoryUsedPercentMetric = "gpu_memory_used_percent"
	gpuTemperatureMetric       = "gpu_temperature_celsius"
	gpuPowerMetric             = "gpu_power_watts"
)

type gpuReader interface {
	Read(ctx context.Context, timestamp int64) ([]model.Point, error)
}

type gpuCollector struct {
	interval   time.Duration
	reader     gpuReader
	discovered bool
}

func NewGPU(interval time.Duration) Collector {
	return &gpuCollector{interval: interval}
}

func (c *gpuCollector) Name() string            { return "gpu" }
func (c *gpuCollector) Interval() time.Duration { return c.interval }

func (c *gpuCollector) Collect(ctx context.Context, out *Batch) error {
	if !c.discovered {
		c.reader = discoverGPUReader()
		c.discovered = true
	}
	if c.reader == nil {
		return nil
	}

	points, err := c.reader.Read(ctx, time.Now().UnixMilli())
	out.AddPoints(points)
	return err
}

func discoverGPUReader() gpuReader {
	if path, err := exec.LookPath("nvidia-smi"); err == nil {
		return &nvidiaGPUReader{path: path}
	}

	switch runtime.GOOS {
	case "linux":
		return &linuxGPUReader{root: "/sys/class/drm"}
	case "darwin":
		if path, err := exec.LookPath("system_profiler"); err == nil {
			return &darwinGPUReader{path: path}
		}
	case "windows":
		for _, name := range []string{"powershell.exe", "powershell"} {
			if path, err := exec.LookPath(name); err == nil {
				return &windowsGPUReader{path: path}
			}
		}
	}

	return nil
}

type nvidiaGPUReader struct {
	path string
}

func (r *nvidiaGPUReader) Read(ctx context.Context, timestamp int64) ([]model.Point, error) {
	output, err := runGPUCommand(
		ctx,
		r.path,
		"--query-gpu=index,name,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw",
		"--format=csv,noheader,nounits",
	)
	if err != nil && strings.Contains(strings.ToLower(string(output)), "no devices were found") {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read NVIDIA GPU metrics: %w", err)
	}

	return parseNVIDIASMI(output, timestamp)
}

func parseNVIDIASMI(output []byte, timestamp int64) ([]model.Point, error) {
	reader := csv.NewReader(bytes.NewReader(output))
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = -1

	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse nvidia-smi output: %w", err)
	}

	points := make([]model.Point, 0, len(records)*7)
	var parseErrors []error
	for line, record := range records {
		if len(record) == 1 && strings.TrimSpace(record[0]) == "" {
			continue
		}
		if len(record) != 7 {
			parseErrors = append(parseErrors, fmt.Errorf("parse nvidia-smi line %d: expected 7 fields, got %d", line+1, len(record)))
			continue
		}

		index := strings.TrimSpace(record[0])
		name := strings.TrimSpace(record[1])
		device := "gpu" + index
		if name != "" {
			device += ": " + name
		}
		points = append(points, newPoint(timestamp, gpuPresentMetric, device, 1))

		usage, usageOK := optionalFloat(record[2])
		memoryUsedMiB, memoryUsedOK := optionalFloat(record[3])
		memoryTotalMiB, memoryTotalOK := optionalFloat(record[4])
		temperature, temperatureOK := optionalFloat(record[5])
		power, powerOK := optionalFloat(record[6])

		if usageOK {
			points = append(points, newPoint(timestamp, gpuUsagePercentMetric, device, usage))
		}
		if memoryUsedOK {
			points = append(points, newPoint(timestamp, gpuMemoryUsedBytesMetric, device, memoryUsedMiB*1024*1024))
		}
		if memoryTotalOK {
			points = append(points, newPoint(timestamp, gpuMemoryTotalBytesMetric, device, memoryTotalMiB*1024*1024))
		}
		if memoryUsedOK && memoryTotalOK && memoryTotalMiB > 0 {
			points = append(points, newPoint(timestamp, gpuMemoryUsedPercentMetric, device, memoryUsedMiB/memoryTotalMiB*100))
		}
		if temperatureOK {
			points = append(points, newPoint(timestamp, gpuTemperatureMetric, device, temperature))
		}
		if powerOK {
			points = append(points, newPoint(timestamp, gpuPowerMetric, device, power))
		}
	}

	return points, errors.Join(parseErrors...)
}

func optionalFloat(raw string) (float64, bool) {
	value := strings.TrimSpace(raw)
	if value == "" || strings.Contains(strings.ToLower(value), "n/a") || strings.Contains(strings.ToLower(value), "not supported") {
		return 0, false
	}

	parsed, err := strconv.ParseFloat(value, 64)
	return parsed, err == nil
}

type linuxGPUReader struct {
	root string
}

func (r *linuxGPUReader) Read(_ context.Context, timestamp int64) ([]model.Point, error) {
	return readLinuxGPUPoints(r.root, timestamp)
}

func readLinuxGPUPoints(root string, timestamp int64) ([]model.Point, error) {
	devicePaths, err := filepath.Glob(filepath.Join(root, "card*", "device"))
	if err != nil {
		return nil, fmt.Errorf("find Linux GPU devices: %w", err)
	}
	sort.Strings(devicePaths)

	points := make([]model.Point, 0, len(devicePaths)*6)
	var readErrors []error
	for _, devicePath := range devicePaths {
		card := filepath.Base(filepath.Dir(devicePath))
		if _, err := strconv.Atoi(strings.TrimPrefix(card, "card")); err != nil {
			continue
		}

		device := linuxGPUDeviceName(card, devicePath)
		points = append(points, newPoint(timestamp, gpuPresentMetric, device, 1))

		usage, usageOK, err := readNumericFile(filepath.Join(devicePath, "gpu_busy_percent"))
		readErrors = appendOptionalError(readErrors, err)
		memoryUsed, memoryUsedOK, err := readNumericFile(filepath.Join(devicePath, "mem_info_vram_used"))
		readErrors = appendOptionalError(readErrors, err)
		memoryTotal, memoryTotalOK, err := readNumericFile(filepath.Join(devicePath, "mem_info_vram_total"))
		readErrors = appendOptionalError(readErrors, err)
		temperature, temperatureOK, err := readFirstNumericFile(filepath.Join(devicePath, "hwmon", "hwmon*", "temp1_input"))
		readErrors = appendOptionalError(readErrors, err)
		power, powerOK, err := readFirstNumericFile(filepath.Join(devicePath, "hwmon", "hwmon*", "power1_average"))
		readErrors = appendOptionalError(readErrors, err)

		if usageOK {
			points = append(points, newPoint(timestamp, gpuUsagePercentMetric, device, usage))
		}
		if memoryUsedOK {
			points = append(points, newPoint(timestamp, gpuMemoryUsedBytesMetric, device, memoryUsed))
		}
		if memoryTotalOK {
			points = append(points, newPoint(timestamp, gpuMemoryTotalBytesMetric, device, memoryTotal))
		}
		if memoryUsedOK && memoryTotalOK && memoryTotal > 0 {
			points = append(points, newPoint(timestamp, gpuMemoryUsedPercentMetric, device, memoryUsed/memoryTotal*100))
		}
		if temperatureOK {
			points = append(points, newPoint(timestamp, gpuTemperatureMetric, device, temperature/1000))
		}
		if powerOK {
			points = append(points, newPoint(timestamp, gpuPowerMetric, device, power/1_000_000))
		}
	}

	return points, errors.Join(readErrors...)
}

func linuxGPUDeviceName(card string, devicePath string) string {
	if resolved, err := filepath.EvalSymlinks(filepath.Join(devicePath, "driver")); err == nil {
		return card + ": " + filepath.Base(resolved)
	}
	if vendor, err := os.ReadFile(filepath.Join(devicePath, "vendor")); err == nil {
		return card + ": " + strings.TrimSpace(string(vendor))
	}
	return card
}

func readNumericFile(path string) (float64, bool, error) {
	contents, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("read GPU metric %q: %w", path, err)
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(string(contents)), 64)
	if err != nil {
		return 0, false, fmt.Errorf("parse GPU metric %q: %w", path, err)
	}
	return value, true, nil
}

func readFirstNumericFile(pattern string) (float64, bool, error) {
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return 0, false, fmt.Errorf("find GPU metric %q: %w", pattern, err)
	}
	if len(paths) == 0 {
		return 0, false, nil
	}
	sort.Strings(paths)
	return readNumericFile(paths[0])
}

func appendOptionalError(errs []error, err error) []error {
	if err != nil {
		return append(errs, err)
	}
	return errs
}

type staticGPU struct {
	device           string
	memoryTotalBytes float64
}

func buildStaticGPUPoints(devices []staticGPU, timestamp int64) []model.Point {
	points := make([]model.Point, 0, len(devices)*2)
	for _, device := range devices {
		points = append(points, newPoint(timestamp, gpuPresentMetric, device.device, 1))
		if device.memoryTotalBytes > 0 {
			points = append(points, newPoint(timestamp, gpuMemoryTotalBytesMetric, device.device, device.memoryTotalBytes))
		}
	}
	return points
}

type darwinGPUReader struct {
	path    string
	loaded  bool
	devices []staticGPU
}

func (r *darwinGPUReader) Read(ctx context.Context, timestamp int64) ([]model.Point, error) {
	if !r.loaded {
		output, err := runGPUCommand(ctx, r.path, "SPDisplaysDataType", "-json", "-detailLevel", "mini")
		if err != nil {
			return nil, fmt.Errorf("read macOS GPU information: %w", err)
		}
		devices, err := parseDarwinGPUs(output)
		if err != nil {
			return nil, err
		}
		r.devices = devices
		r.loaded = true
	}
	return buildStaticGPUPoints(r.devices, timestamp), nil
}

func parseDarwinGPUs(output []byte) ([]staticGPU, error) {
	var data struct {
		Displays []map[string]any `json:"SPDisplaysDataType"`
	}
	if err := json.Unmarshal(output, &data); err != nil {
		return nil, fmt.Errorf("parse macOS GPU information: %w", err)
	}

	devices := make([]staticGPU, 0, len(data.Displays))
	for index, display := range data.Displays {
		name := firstString(display, "sppci_model", "_name")
		if name == "" {
			name = "Unknown GPU"
		}
		memory, _ := parseByteSize(firstString(display, "spdisplays_vram", "spdisplays_vram_shared"))
		devices = append(devices, staticGPU{
			device:           fmt.Sprintf("gpu%d: %s", index, name),
			memoryTotalBytes: memory,
		})
	}
	return devices, nil
}

func firstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func parseByteSize(raw string) (float64, bool) {
	fields := strings.Fields(strings.ReplaceAll(raw, ",", "."))
	if len(fields) < 2 {
		return 0, false
	}
	value, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, false
	}

	multipliers := map[string]float64{
		"KB": 1024,
		"MB": 1024 * 1024,
		"GB": 1024 * 1024 * 1024,
		"TB": 1024 * 1024 * 1024 * 1024,
	}
	multiplier, ok := multipliers[strings.ToUpper(fields[1])]
	return value * multiplier, ok
}

type windowsGPUReader struct {
	path    string
	loaded  bool
	devices []staticGPU
}

func (r *windowsGPUReader) Read(ctx context.Context, timestamp int64) ([]model.Point, error) {
	if !r.loaded {
		output, err := runGPUCommand(
			ctx,
			r.path,
			"-NoProfile",
			"-NonInteractive",
			"-Command",
			"Get-CimInstance Win32_VideoController | Select-Object Name,AdapterRAM | ConvertTo-Json -Compress",
		)
		if err != nil {
			return nil, fmt.Errorf("read Windows GPU information: %w", err)
		}
		devices, err := parseWindowsGPUs(output)
		if err != nil {
			return nil, err
		}
		r.devices = devices
		r.loaded = true
	}
	return buildStaticGPUPoints(r.devices, timestamp), nil
}

func parseWindowsGPUs(output []byte) ([]staticGPU, error) {
	type windowsGPU struct {
		Name       string `json:"Name"`
		AdapterRAM any    `json:"AdapterRAM"`
	}

	trimmed := bytes.TrimSpace(output)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	if trimmed[0] == '{' {
		trimmed = append(append([]byte{'['}, trimmed...), ']')
	}

	var rawDevices []windowsGPU
	if err := json.Unmarshal(trimmed, &rawDevices); err != nil {
		return nil, fmt.Errorf("parse Windows GPU information: %w", err)
	}

	devices := make([]staticGPU, 0, len(rawDevices))
	for index, rawDevice := range rawDevices {
		name := strings.TrimSpace(rawDevice.Name)
		if name == "" {
			name = "Unknown GPU"
		}
		memory, _ := numericJSONValue(rawDevice.AdapterRAM)
		devices = append(devices, staticGPU{
			device:           fmt.Sprintf("gpu%d: %s", index, name),
			memoryTotalBytes: memory,
		})
	}
	return devices, nil
}

func numericJSONValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, typed > 0
	case string:
		parsed, err := strconv.ParseFloat(typed, 64)
		return parsed, err == nil && parsed > 0
	default:
		return 0, false
	}
}

func runGPUCommand(ctx context.Context, path string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, path, args...)
	output, err := command.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message != "" {
			return output, fmt.Errorf("%w: %s", err, message)
		}
	}
	return output, err
}
