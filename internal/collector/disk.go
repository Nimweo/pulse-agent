package collector

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/nimweo/pulse-agent/internal/model"
	"github.com/shirou/gopsutil/v4/disk"
)

const (
	diskTotalBytesMetric     = "disk_total_bytes"
	diskUsedBytesMetric      = "disk_used_bytes"
	diskFreeBytesMetric      = "disk_free_bytes"
	diskUsedPercentMetric    = "disk_used_percent"
	diskInodesUsedMetric     = "disk_inodes_used_percent"
	diskReadBytesRateMetric  = "disk_read_bytes_per_second"
	diskWriteBytesRateMetric = "disk_write_bytes_per_second"
	diskReadOpsRateMetric    = "disk_read_operations_per_second"
	diskWriteOpsRateMetric   = "disk_write_operations_per_second"
)

type diskCollector struct {
	interval       time.Duration
	lastIO         map[string]disk.IOCountersStat
	lastIOAt       time.Time
	now            func() time.Time
	readUsage      func(context.Context, int64) ([]model.Point, error)
	readIOCounters func(context.Context) (map[string]disk.IOCountersStat, error)
}

func NewDisk(interval time.Duration) Collector {
	return &diskCollector{
		interval:  interval,
		now:       time.Now,
		readUsage: readDiskUsage,
		readIOCounters: func(ctx context.Context) (map[string]disk.IOCountersStat, error) {
			return disk.IOCountersWithContext(ctx)
		},
	}
}

func (c *diskCollector) Name() string            { return "disk" }
func (c *diskCollector) Interval() time.Duration { return c.interval }

func (c *diskCollector) Collect(ctx context.Context, out *Batch) error {
	collectedAt := c.now()
	timestamp := collectedAt.UnixMilli()

	usagePoints, usageErr := c.readUsage(ctx, timestamp)
	ioPoints, ioErr := c.readDiskIO(ctx, timestamp, collectedAt)
	out.AddPoints(append(usagePoints, ioPoints...))

	return errors.Join(usageErr, ioErr)
}

func readDiskUsage(ctx context.Context, timestamp int64) ([]model.Point, error) {
	partitions, err := disk.PartitionsWithContext(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("list disk partitions: %w", err)
	}

	sort.Slice(partitions, func(i, j int) bool {
		return partitions[i].Mountpoint < partitions[j].Mountpoint
	})

	points := make([]model.Point, 0, len(partitions)*5)
	var readErrors []error
	for _, partition := range partitions {
		usage, err := disk.UsageWithContext(ctx, partition.Mountpoint)
		if err != nil {
			readErrors = append(readErrors, fmt.Errorf("read usage for %q: %w", partition.Mountpoint, err))
			continue
		}
		if usage.Total == 0 {
			continue
		}

		device := partition.Mountpoint
		if device == "" {
			device = partition.Device
		}

		points = append(points,
			newPoint(timestamp, diskTotalBytesMetric, device, float64(usage.Total)),
			newPoint(timestamp, diskUsedBytesMetric, device, float64(usage.Used)),
			newPoint(timestamp, diskFreeBytesMetric, device, float64(usage.Free)),
			newPoint(timestamp, diskUsedPercentMetric, device, usage.UsedPercent),
			newPoint(timestamp, diskInodesUsedMetric, device, usage.InodesUsedPercent),
		)
	}

	return points, errors.Join(readErrors...)
}

func (c *diskCollector) readDiskIO(
	ctx context.Context,
	timestamp int64,
	collectedAt time.Time,
) ([]model.Point, error) {
	counters, err := c.readIOCounters(ctx)
	if err != nil {
		return nil, fmt.Errorf("read disk I/O counters: %w", err)
	}

	elapsed := collectedAt.Sub(c.lastIOAt)
	points := buildDiskIOPoints(timestamp, counters, c.lastIO, elapsed)
	c.lastIO = counters
	c.lastIOAt = collectedAt

	return points, nil
}

func buildDiskIOPoints(
	timestamp int64,
	current map[string]disk.IOCountersStat,
	previous map[string]disk.IOCountersStat,
	elapsed time.Duration,
) []model.Point {
	if len(previous) == 0 || elapsed <= 0 {
		return nil
	}

	deviceNames := make([]string, 0, len(current))
	for name := range current {
		deviceNames = append(deviceNames, name)
	}
	sort.Strings(deviceNames)

	points := make([]model.Point, 0, len(deviceNames)*4)
	seconds := elapsed.Seconds()
	for _, name := range deviceNames {
		currentCounters := current[name]
		previousCounters, ok := previous[name]
		if !ok {
			continue
		}

		points = appendCounterRate(
			points, timestamp, diskReadBytesRateMetric, name,
			currentCounters.ReadBytes, previousCounters.ReadBytes, seconds,
		)
		points = appendCounterRate(
			points, timestamp, diskWriteBytesRateMetric, name,
			currentCounters.WriteBytes, previousCounters.WriteBytes, seconds,
		)
		points = appendCounterRate(
			points, timestamp, diskReadOpsRateMetric, name,
			currentCounters.ReadCount, previousCounters.ReadCount, seconds,
		)
		points = appendCounterRate(
			points, timestamp, diskWriteOpsRateMetric, name,
			currentCounters.WriteCount, previousCounters.WriteCount, seconds,
		)
	}

	return points
}
