// Package agent samples host CPU, memory, disk, and configured TCP service reachability.
// Milestone 0 is Linux-only; this is monitoring, not a game-server launcher.
package agent

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/oncoursemail/metallic-mc/internal/control"
)

type cpuStats struct{ total, idle uint64 }

func parseCPU(data string) (cpuStats, error) {
	lines := strings.Split(data, "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[0] != "cpu" {
			continue
		}
		var nums []uint64
		for _, x := range fields[1:] {
			n, err := strconv.ParseUint(x, 10, 64)
			if err != nil {
				return cpuStats{}, err
			}
			nums = append(nums, n)
		}
		var total uint64
		for _, n := range nums {
			total += n
		}
		idle := nums[3]
		if len(nums) > 4 {
			idle += nums[4]
		}
		return cpuStats{total: total, idle: idle}, nil
	}
	return cpuStats{}, errors.New("/proc/stat: missing cpu line")
}
func cpuPercent(ctx context.Context) (float64, error) {
	a, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, err
	}
	previous, err := parseCPU(string(a))
	if err != nil {
		return 0, err
	}
	timer := time.NewTimer(250 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-timer.C:
	}
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, err
	}
	current, err := parseCPU(string(b))
	if err != nil {
		return 0, err
	}
	dt := current.total - previous.total
	if dt == 0 || current.idle < previous.idle {
		return 0, errors.New("no CPU time elapsed")
	}
	busy := float64(dt-(current.idle-previous.idle)) / float64(dt) * 100
	if busy < 0 {
		busy = 0
	}
	if busy > 100 {
		busy = 100
	}
	return busy, nil
}
func memory() (used, total uint64, err error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	values := map[string]uint64{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 {
			if fields[0] == "MemTotal:" || fields[0] == "MemAvailable:" {
				v, e := strconv.ParseUint(fields[1], 10, 64)
				if e == nil {
					values[fields[0]] = v * 1024
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, err
	}
	total = values["MemTotal:"]
	available, ok := values["MemAvailable:"]
	if total == 0 || !ok || available > total {
		return 0, 0, errors.New("invalid /proc/meminfo")
	}
	return total - available, total, nil
}
func disk(path string) (used, total uint64, err error) {
	var s syscall.Statfs_t
	if err := syscall.Statfs(path, &s); err != nil {
		return 0, 0, err
	}
	total = s.Blocks * uint64(s.Bsize)
	available := s.Bavail * uint64(s.Bsize)
	if available > total {
		return 0, 0, errors.New("invalid disk statistics")
	}
	return total - available, total, nil
}
func loadAndUptime() (float64, uint64, error) {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, 0, err
	}
	parts := strings.Fields(string(b))
	if len(parts) < 1 {
		return 0, 0, errors.New("invalid load average")
	}
	load, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0, 0, err
	}
	b, err = os.ReadFile("/proc/uptime")
	if err != nil {
		return 0, 0, err
	}
	parts = strings.Fields(string(b))
	if len(parts) < 1 {
		return 0, 0, errors.New("invalid uptime")
	}
	up, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0, 0, err
	}
	return load, uint64(up), nil
}

// ParseServices expects name@host:port entries. Entries are never executed as commands.
func ParseServices(value string) ([]control.Instance, error) {
	if strings.TrimSpace(value) == "" {
		return []control.Instance{}, nil
	}
	parts := strings.Split(value, ",")
	if len(parts) > 64 {
		return nil, fmt.Errorf("at most 64 service probes allowed")
	}
	list := make([]control.Instance, 0, len(parts))
	for _, part := range parts {
		pieces := strings.SplitN(strings.TrimSpace(part), "@", 2)
		if len(pieces) != 2 {
			return nil, fmt.Errorf("service must have name@host:port: %q", part)
		}
		name, addr := strings.TrimSpace(pieces[0]), strings.TrimSpace(pieces[1])
		if len(name) == 0 || len(name) > 64 || len(addr) > 128 {
			return nil, fmt.Errorf("invalid service name or address")
		}
		_, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("invalid address %q: %w", addr, err)
		}
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return nil, fmt.Errorf("invalid service port")
		}
		list = append(list, control.Instance{Name: name, Kind: "tcp", Address: addr})
	}
	return list, nil
}
func ProbeServices(ctx context.Context, services []control.Instance) []control.Instance {
	result := make([]control.Instance, len(services))
	copy(result, services)
	for i := range result {
		d := net.Dialer{Timeout: 500 * time.Millisecond}
		c, err := d.DialContext(ctx, "tcp", result[i].Address)
		if err == nil {
			result[i].Healthy = true
			_ = c.Close()
		}
	}
	return result
}
func Sample(ctx context.Context, nodeID, hostname, diskPath string, services []control.Instance) (control.Heartbeat, error) {
	cpu, err := cpuPercent(ctx)
	if err != nil {
		return control.Heartbeat{}, fmt.Errorf("CPU sample: %w", err)
	}
	memUsed, memTotal, err := memory()
	if err != nil {
		return control.Heartbeat{}, fmt.Errorf("memory sample: %w", err)
	}
	diskUsed, diskTotal, err := disk(diskPath)
	if err != nil {
		return control.Heartbeat{}, fmt.Errorf("disk sample: %w", err)
	}
	load, uptime, err := loadAndUptime()
	if err != nil {
		return control.Heartbeat{}, err
	}
	return control.Heartbeat{NodeID: nodeID, Hostname: hostname, CPUPercent: cpu, MemoryUsed: memUsed, MemoryTotal: memTotal, DiskUsed: diskUsed, DiskTotal: diskTotal, Load1: load, UptimeSeconds: uptime, Instances: ProbeServices(ctx, services)}, nil
}
