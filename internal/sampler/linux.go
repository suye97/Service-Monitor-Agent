// Package sampler collects Linux CPU, memory and root filesystem usage over SSH.
package sampler

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Runner is implemented by sshclient.Client; only fixed, constant commands run.
type Runner interface {
	Run(context.Context, string) (string, error)
}

type Disk struct {
	Mount   string  `json:"mount"`
	Percent float64 `json:"percent"`
}
type Metrics struct {
	CPUPercent    float64   `json:"cpu_percent"`
	MemoryPercent float64   `json:"memory_percent"`
	DiskPercent   float64   `json:"disk_percent"` // highest local disk usage
	Disks         []Disk    `json:"disks"`
	CollectedAt   time.Time `json:"collected_at"`
}

// Collect uses two /proc/stat snapshots separated by one second. A single
// snapshot cannot describe CPU utilization. Cancellation interrupts the wait.
func Collect(ctx context.Context, runner Runner) (Metrics, error) {
	return CollectWithInterval(ctx, runner, time.Second)
}

// CollectWithInterval allows a configurable CPU measurement window (useful in tests).
func CollectWithInterval(ctx context.Context, runner Runner, interval time.Duration) (Metrics, error) {
	var result Metrics
	if runner == nil || interval <= 0 {
		return result, errors.New("runner and positive CPU interval required")
	}
	first, err := runner.Run(ctx, "head -n 1 /proc/stat")
	if err != nil {
		return result, fmt.Errorf("first CPU sample: %w", err)
	}
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return result, ctx.Err()
	case <-timer.C:
	}
	second, err := runner.Run(ctx, "head -n 1 /proc/stat")
	if err != nil {
		return result, fmt.Errorf("second CPU sample: %w", err)
	}
	result.CPUPercent, err = cpuUsage(first, second)
	if err != nil {
		return result, err
	}
	memory, err := runner.Run(ctx, "cat /proc/meminfo")
	if err != nil {
		return result, fmt.Errorf("memory sample: %w", err)
	}
	result.MemoryPercent, err = memoryUsage(memory)
	if err != nil {
		return result, err
	}
	disk, err := runner.Run(ctx, "df -Pk -l")
	if err != nil {
		return result, fmt.Errorf("disk sample: %w", err)
	}
	result.Disks, err = parseDisks(disk)
	if err != nil {
		return result, err
	}
	for _, d := range result.Disks {
		if d.Percent > result.DiskPercent {
			result.DiskPercent = d.Percent
		}
	}
	result.CollectedAt = time.Now().UTC()
	return result, nil
}

func cpuCounters(s string) (uint64, uint64, error) {
	fields := strings.Fields(strings.SplitN(s, "\n", 2)[0])
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0, 0, errors.New("invalid /proc/stat CPU line")
	}
	var total, idle uint64
	for i, f := range fields[1:] {
		n, err := strconv.ParseUint(f, 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid CPU counter: %w", err)
		}
		if total+n < total {
			return 0, 0, errors.New("CPU counter overflow")
		}
		total += n
		if i == 3 || i == 4 {
			idle += n
		}
	}
	return total, idle, nil
}
func cpuUsage(a, b string) (float64, error) {
	totalA, idleA, err := cpuCounters(a)
	if err != nil {
		return 0, err
	}
	totalB, idleB, err := cpuCounters(b)
	if err != nil {
		return 0, err
	}
	if totalB <= totalA || idleB < idleA || idleB-idleA > totalB-totalA {
		return 0, errors.New("invalid CPU counter delta")
	}
	return 100 * float64((totalB-totalA)-(idleB-idleA)) / float64(totalB-totalA), nil
}
func memoryUsage(s string) (float64, error) {
	var total, available uint64
	foundTotal, foundAvailable := false, false
	for _, line := range strings.Split(s, "\n") {
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		switch parts[0] {
		case "MemTotal:":
			total, _ = strconv.ParseUint(parts[1], 10, 64)
			foundTotal = true
		case "MemAvailable:":
			available, _ = strconv.ParseUint(parts[1], 10, 64)
			foundAvailable = true
		}
	}
	if !foundTotal || !foundAvailable || total == 0 || available > total {
		return 0, errors.New("invalid /proc/meminfo totals")
	}
	return 100 * float64(total-available) / float64(total), nil
}
func parseDisks(s string) ([]Disk, error) {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) < 2 {
		return nil, errors.New("invalid df output")
	}
	var disks []Disk
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}
		// Include local block devices and root, avoiding tmpfs and proc mounts.
		mount := strings.Join(fields[5:], " ")
		if !strings.HasPrefix(fields[0], "/dev/") && mount != "/" {
			continue
		}
		pct := strings.TrimSuffix(fields[4], "%")
		n, err := strconv.ParseFloat(pct, 64)
		if err != nil || n < 0 || n > 100 {
			return nil, errors.New("invalid disk use percent")
		}
		disks = append(disks, Disk{Mount: mount, Percent: n})
	}
	if len(disks) == 0 {
		return nil, errors.New("no local disk mounts in df output")
	}
	return disks, nil
}
