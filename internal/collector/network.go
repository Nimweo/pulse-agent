package collector

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/nimweo/pulse-agent/internal/model"
	gopsutilnet "github.com/shirou/gopsutil/v4/net"
)

const (
	networkReceiveBytesRateMetric    = "network_receive_bytes_per_second"
	networkTransmitBytesRateMetric   = "network_transmit_bytes_per_second"
	networkReceivePacketsRateMetric  = "network_receive_packets_per_second"
	networkTransmitPacketsRateMetric = "network_transmit_packets_per_second"
	networkReceiveErrorsRateMetric   = "network_receive_errors_per_second"
	networkTransmitErrorsRateMetric  = "network_transmit_errors_per_second"
	networkReceiveDropsRateMetric    = "network_receive_dropped_packets_per_second"
	networkTransmitDropsRateMetric   = "network_transmit_dropped_packets_per_second"
)

type networkCollector struct {
	interval       time.Duration
	lastIO         map[string]gopsutilnet.IOCountersStat
	lastIOAt       time.Time
	now            func() time.Time
	readInterfaces func(context.Context) (gopsutilnet.InterfaceStatList, error)
	readIOCounters func(context.Context, bool) ([]gopsutilnet.IOCountersStat, error)
}

func NewNetwork(interval time.Duration) Collector {
	return &networkCollector{
		interval:       interval,
		now:            time.Now,
		readInterfaces: gopsutilnet.InterfacesWithContext,
		readIOCounters: gopsutilnet.IOCountersWithContext,
	}
}

func (c *networkCollector) Name() string            { return "network" }
func (c *networkCollector) Interval() time.Duration { return c.interval }

func (c *networkCollector) Collect(ctx context.Context, out *Batch) error {
	collectedAt := c.now()
	timestamp := collectedAt.UnixMilli()

	interfaces, err := c.readInterfaces(ctx)
	if err != nil {
		return fmt.Errorf("list network interfaces: %w", err)
	}
	nonLoopback := nonLoopbackInterfaceNames(interfaces)

	counters, err := c.readIOCounters(ctx, true)
	if err != nil {
		return fmt.Errorf("read network I/O counters: %w", err)
	}
	current := filterNetworkCounters(counters, nonLoopback)

	elapsed := collectedAt.Sub(c.lastIOAt)
	points := buildNetworkIOPoints(timestamp, current, c.lastIO, elapsed)
	c.lastIO = current
	c.lastIOAt = collectedAt
	out.AddPoints(points)

	return nil
}

func nonLoopbackInterfaceNames(interfaces gopsutilnet.InterfaceStatList) map[string]struct{} {
	names := make(map[string]struct{}, len(interfaces))
	for _, networkInterface := range interfaces {
		if hasNetworkFlag(networkInterface.Flags, "loopback") {
			continue
		}
		names[networkInterface.Name] = struct{}{}
	}

	return names
}

func hasNetworkFlag(flags []string, wanted string) bool {
	for _, flag := range flags {
		if flag == wanted {
			return true
		}
	}
	return false
}

func filterNetworkCounters(
	counters []gopsutilnet.IOCountersStat,
	allowed map[string]struct{},
) map[string]gopsutilnet.IOCountersStat {
	filtered := make(map[string]gopsutilnet.IOCountersStat, len(counters))
	for _, counter := range counters {
		if _, ok := allowed[counter.Name]; !ok {
			continue
		}
		filtered[counter.Name] = counter
	}

	return filtered
}

func buildNetworkIOPoints(
	timestamp int64,
	current map[string]gopsutilnet.IOCountersStat,
	previous map[string]gopsutilnet.IOCountersStat,
	elapsed time.Duration,
) []model.Point {
	if len(previous) == 0 || elapsed <= 0 {
		return nil
	}

	interfaceNames := make([]string, 0, len(current))
	for name := range current {
		interfaceNames = append(interfaceNames, name)
	}
	sort.Strings(interfaceNames)

	points := make([]model.Point, 0, len(interfaceNames)*8)
	seconds := elapsed.Seconds()
	for _, name := range interfaceNames {
		currentCounters := current[name]
		previousCounters, ok := previous[name]
		if !ok {
			continue
		}

		points = appendCounterRate(
			points, timestamp, networkReceiveBytesRateMetric, name,
			currentCounters.BytesRecv, previousCounters.BytesRecv, seconds,
		)
		points = appendCounterRate(
			points, timestamp, networkTransmitBytesRateMetric, name,
			currentCounters.BytesSent, previousCounters.BytesSent, seconds,
		)
		points = appendCounterRate(
			points, timestamp, networkReceivePacketsRateMetric, name,
			currentCounters.PacketsRecv, previousCounters.PacketsRecv, seconds,
		)
		points = appendCounterRate(
			points, timestamp, networkTransmitPacketsRateMetric, name,
			currentCounters.PacketsSent, previousCounters.PacketsSent, seconds,
		)
		points = appendCounterRate(
			points, timestamp, networkReceiveErrorsRateMetric, name,
			currentCounters.Errin, previousCounters.Errin, seconds,
		)
		points = appendCounterRate(
			points, timestamp, networkTransmitErrorsRateMetric, name,
			currentCounters.Errout, previousCounters.Errout, seconds,
		)
		points = appendCounterRate(
			points, timestamp, networkReceiveDropsRateMetric, name,
			currentCounters.Dropin, previousCounters.Dropin, seconds,
		)
		points = appendCounterRate(
			points, timestamp, networkTransmitDropsRateMetric, name,
			currentCounters.Dropout, previousCounters.Dropout, seconds,
		)
	}

	return points
}
