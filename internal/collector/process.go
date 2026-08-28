package collector

import (
	"context"
	"fmt"
	"math"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nimweo/pulse-agent/internal/model"
	gopsutilprocess "github.com/shirou/gopsutil/v4/process"
)

const (
	processCountTotalMetric          = "process_count_total"
	processCountRunningMetric        = "process_count_running"
	processCountSleepingMetric       = "process_count_sleeping"
	processCountZombieMetric         = "process_count_zombie"
	processCountOtherMetric          = "process_count_other"
	processTopCPUPercentMetric       = "process_top_cpu_percent"
	processTopMemoryRSSBytesMetric   = "process_top_memory_rss_bytes"
	processMonitoredInstancesMetric  = "process_monitored_instances"
	processMonitoredCPUPercentMetric = "process_monitored_cpu_percent"
	processMonitoredMemoryMetric     = "process_monitored_memory_rss_bytes"
)

type processIdentity struct {
	pid       int32
	startedAt int64
	name      string
}

type processSnapshot struct {
	identity        processIdentity
	name            string
	status          string
	statusAvailable bool
	cpuSeconds      float64
	cpuAvailable    bool
	memoryRSS       uint64
	memoryAvailable bool
}

type processGroup struct {
	name            string
	instances       int
	cpuPercent      float64
	cpuAvailable    bool
	memoryRSS       uint64
	memoryAvailable bool
}

type processCollector struct {
	interval           time.Duration
	topCPU             int
	topMemory          int
	monitoredProcesses []string
	previousCPU        map[processIdentity]float64
	lastCollectedAt    time.Time
	now                func() time.Time
	readProcesses      func(context.Context) ([]processSnapshot, error)
}

func NewProcess(
	interval time.Duration,
	topCPU int,
	topMemory int,
	monitoredProcesses []string,
) Collector {
	return &processCollector{
		interval:           interval,
		topCPU:             topCPU,
		topMemory:          topMemory,
		monitoredProcesses: normalizeProcessNames(monitoredProcesses),
		now:                time.Now,
		readProcesses:      readProcessSnapshots,
	}
}

func (c *processCollector) Name() string            { return "process" }
func (c *processCollector) Interval() time.Duration { return c.interval }

func (c *processCollector) Collect(ctx context.Context, out *Batch) error {
	collectedAt := c.now()
	snapshots, err := c.readProcesses(ctx)
	if err != nil {
		return err
	}

	points, currentCPU := buildProcessPoints(
		collectedAt.UnixMilli(),
		snapshots,
		c.previousCPU,
		collectedAt.Sub(c.lastCollectedAt),
		c.topCPU,
		c.topMemory,
		c.monitoredProcesses,
	)
	c.previousCPU = currentCPU
	c.lastCollectedAt = collectedAt
	out.AddPoints(points)

	return nil
}

func readProcessSnapshots(ctx context.Context) ([]processSnapshot, error) {
	processes, err := gopsutilprocess.ProcessesWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("list processes: %w", err)
	}
	bulkStatuses, useBulkStatuses := readBulkProcessStatuses(ctx)

	snapshots := make([]processSnapshot, 0, len(processes))
	for _, systemProcess := range processes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		snapshot := processSnapshot{}
		snapshot.identity.pid = systemProcess.Pid

		if useBulkStatuses {
			snapshot.status, snapshot.statusAvailable = bulkStatuses[systemProcess.Pid]
		} else if runtime.GOOS != "windows" {
			statuses, statusErr := systemProcess.StatusWithContext(ctx)
			if statusErr == nil && len(statuses) > 0 && statuses[0] != "" {
				snapshot.status = statuses[0]
				snapshot.statusAvailable = true
			}
		}

		name, nameErr := systemProcess.NameWithContext(ctx)
		if nameErr == nil {
			snapshot.name = displayProcessName(name)
			snapshot.identity.name = processNameKey(snapshot.name)
		}

		if snapshot.name != "" {
			cpuTimes, cpuErr := systemProcess.TimesWithContext(ctx)
			if cpuErr == nil {
				snapshot.cpuSeconds = cpuTimes.User + cpuTimes.System
				snapshot.cpuAvailable = true
			}

			startedAt, startErr := systemProcess.CreateTimeWithContext(ctx)
			if startErr == nil {
				snapshot.identity.startedAt = startedAt
			}

			memory, memoryErr := systemProcess.MemoryInfoWithContext(ctx)
			if memoryErr == nil {
				snapshot.memoryRSS = memory.RSS
				snapshot.memoryAvailable = true
			}
		}

		snapshots = append(snapshots, snapshot)
	}

	return snapshots, nil
}

func readBulkProcessStatuses(ctx context.Context) (map[int32]string, bool) {
	if runtime.GOOS != "darwin" {
		return nil, false
	}

	output, err := exec.CommandContext(ctx, "ps", "-axo", "pid=,state=").Output()
	if err != nil {
		return nil, true
	}
	return parseProcessStatuses(string(output)), true
}

func parseProcessStatuses(output string) map[int32]string {
	statuses := make(map[int32]string)
	for line := range strings.Lines(output) {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.ParseInt(fields[0], 10, 32)
		if err != nil || fields[1] == "" {
			continue
		}
		statuses[int32(pid)] = processStatusFromCode(fields[1][0])
	}
	return statuses
}

func processStatusFromCode(code byte) string {
	switch code {
	case 'R':
		return gopsutilprocess.Running
	case 'S':
		return gopsutilprocess.Sleep
	case 'Z':
		return gopsutilprocess.Zombie
	case 'D', 'U':
		return gopsutilprocess.Blocked
	case 'I':
		return gopsutilprocess.Idle
	case 'T', 't':
		return gopsutilprocess.Stop
	case 'W':
		return gopsutilprocess.Wait
	case 'L':
		return gopsutilprocess.Lock
	default:
		return gopsutilprocess.UnknownState
	}
}

func buildProcessPoints(
	timestamp int64,
	snapshots []processSnapshot,
	previousCPU map[processIdentity]float64,
	elapsed time.Duration,
	topCPU int,
	topMemory int,
	monitoredProcesses []string,
) ([]model.Point, map[processIdentity]float64) {
	points := buildProcessCountPoints(timestamp, snapshots)
	groups := make(map[string]*processGroup)
	currentCPU := make(map[processIdentity]float64, len(snapshots))
	seconds := elapsed.Seconds()

	for _, snapshot := range snapshots {
		if snapshot.cpuAvailable {
			currentCPU[snapshot.identity] = snapshot.cpuSeconds
		}
		if snapshot.name == "" {
			continue
		}

		key := processNameKey(snapshot.name)
		group, ok := groups[key]
		if !ok {
			group = &processGroup{name: snapshot.name}
			groups[key] = group
		}
		group.instances++

		if snapshot.memoryAvailable {
			group.memoryRSS += snapshot.memoryRSS
			group.memoryAvailable = true
		}
		if snapshot.cpuAvailable && seconds > 0 {
			previous, ok := previousCPU[snapshot.identity]
			if ok && snapshot.cpuSeconds >= previous {
				usage := (snapshot.cpuSeconds - previous) / seconds * 100
				if !math.IsNaN(usage) && !math.IsInf(usage, 0) {
					group.cpuPercent += usage
					group.cpuAvailable = true
				}
			}
		}
	}

	points = append(points, buildTopProcessPoints(timestamp, groups, topCPU, topMemory)...)
	points = append(
		points,
		buildMonitoredProcessPoints(
			timestamp,
			groups,
			monitoredProcesses,
			len(previousCPU) > 0 && seconds > 0,
		)...,
	)

	return points, currentCPU
}

func buildProcessCountPoints(timestamp int64, snapshots []processSnapshot) []model.Point {
	points := []model.Point{
		newPoint(timestamp, processCountTotalMetric, "", float64(len(snapshots))),
	}

	var running, sleeping, zombie, other, observed int
	for _, snapshot := range snapshots {
		if !snapshot.statusAvailable {
			continue
		}
		observed++
		switch snapshot.status {
		case gopsutilprocess.Running:
			running++
		case gopsutilprocess.Sleep:
			sleeping++
		case gopsutilprocess.Zombie:
			zombie++
		default:
			other++
		}
	}
	if observed == 0 {
		return points
	}

	return append(points,
		newPoint(timestamp, processCountRunningMetric, "", float64(running)),
		newPoint(timestamp, processCountSleepingMetric, "", float64(sleeping)),
		newPoint(timestamp, processCountZombieMetric, "", float64(zombie)),
		newPoint(timestamp, processCountOtherMetric, "", float64(other)),
	)
}

func buildTopProcessPoints(
	timestamp int64,
	groups map[string]*processGroup,
	topCPU int,
	topMemory int,
) []model.Point {
	capacity := 0
	if topCPU > 0 {
		capacity += min(topCPU, len(groups))
	}
	if topMemory > 0 {
		capacity += min(topMemory, len(groups))
	}
	points := make([]model.Point, 0, capacity)

	cpuGroups := selectProcessGroups(groups, topCPU, func(group *processGroup) (float64, bool) {
		return group.cpuPercent, group.cpuAvailable
	})
	for _, group := range cpuGroups {
		points = append(points, newPoint(
			timestamp,
			processTopCPUPercentMetric,
			group.name,
			group.cpuPercent,
		))
	}

	memoryGroups := selectProcessGroups(groups, topMemory, func(group *processGroup) (float64, bool) {
		return float64(group.memoryRSS), group.memoryAvailable
	})
	for _, group := range memoryGroups {
		points = append(points, newPoint(
			timestamp,
			processTopMemoryRSSBytesMetric,
			group.name,
			float64(group.memoryRSS),
		))
	}

	return points
}

func selectProcessGroups(
	groups map[string]*processGroup,
	limit int,
	value func(*processGroup) (float64, bool),
) []*processGroup {
	if limit <= 0 {
		return nil
	}

	selected := make([]*processGroup, 0, len(groups))
	for _, group := range groups {
		if _, available := value(group); available {
			selected = append(selected, group)
		}
	}
	sort.Slice(selected, func(i, j int) bool {
		left, _ := value(selected[i])
		right, _ := value(selected[j])
		if left == right {
			return selected[i].name < selected[j].name
		}
		return left > right
	})
	if len(selected) > limit {
		selected = selected[:limit]
	}

	return selected
}

func buildMonitoredProcessPoints(
	timestamp int64,
	groups map[string]*processGroup,
	monitoredProcesses []string,
	cpuWindowReady bool,
) []model.Point {
	points := make([]model.Point, 0, len(monitoredProcesses)*3)
	for _, name := range monitoredProcesses {
		group := groups[processNameKey(name)]
		instances := 0
		var cpuPercent float64
		var memoryRSS uint64
		if group != nil {
			instances = group.instances
			cpuPercent = group.cpuPercent
			memoryRSS = group.memoryRSS
		}

		points = append(points,
			newPoint(timestamp, processMonitoredInstancesMetric, name, float64(instances)),
			newPoint(timestamp, processMonitoredMemoryMetric, name, float64(memoryRSS)),
		)
		if cpuWindowReady {
			points = append(points, newPoint(
				timestamp,
				processMonitoredCPUPercentMetric,
				name,
				cpuPercent,
			))
		}
	}

	return points
}

func normalizeProcessNames(names []string) []string {
	normalized := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		displayName := displayProcessName(name)
		key := processNameKey(displayName)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, displayName)
	}
	return normalized
}

func displayProcessName(name string) string {
	name = strings.TrimSpace(name)
	if len(name) > 4 && strings.EqualFold(name[len(name)-4:], ".exe") {
		return name[:len(name)-4]
	}
	return name
}

func processNameKey(name string) string {
	return strings.ToLower(displayProcessName(name))
}
