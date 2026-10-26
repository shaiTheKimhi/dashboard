package main

import (
	"context"
	"errors"
	"io"
	"math"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Identity struct {
	Model        string `json:"model"`
	Hostname     string `json:"hostname"`
	OS           string `json:"os"`
	L4T          string `json:"l4t"`
	Kernel       string `json:"kernel"`
	Architecture string `json:"architecture"`
}
type Disk struct {
	MountPath      string `json:"mount_path"`
	TotalBytes     uint64 `json:"total_bytes"`
	UsedBytes      uint64 `json:"used_bytes"`
	FreeBytes      uint64 `json:"free_bytes"`
	AvailableBytes uint64 `json:"available_bytes"`
}
type Network struct {
	Name             string   `json:"name"`
	Addresses        []string `json:"addresses"`
	RXBytesPerSecond *float64 `json:"rx_bytes_per_second"`
	TXBytesPerSecond *float64 `json:"tx_bytes_per_second"`
}
type Snapshot struct {
	Identity       Identity  `json:"identity"`
	SampledAt      time.Time `json:"sampled_at"`
	Stale          bool      `json:"stale"`
	Warnings       []string  `json:"warnings"`
	UptimeSeconds  *float64  `json:"uptime_seconds"`
	CPUPercent     *float64  `json:"cpu_percent"`
	GPUPercent     *float64  `json:"gpu_percent"`
	CPUTempC       *float64  `json:"cpu_temp_c"`
	GPUTempC       *float64  `json:"gpu_temp_c"`
	RAMTotalBytes  *uint64   `json:"ram_total_bytes"`
	RAMUsedBytes   *uint64   `json:"ram_used_bytes"`
	SwapTotalBytes *uint64   `json:"swap_total_bytes"`
	SwapUsedBytes  *uint64   `json:"swap_used_bytes"`
	Disks          []Disk    `json:"disks"`
	Networks       []Network `json:"networks"`
}
type cpuCount struct{ total, idle uint64 }
type netCount struct{ rx, tx uint64 }
type Collector struct {
	mu                sync.Mutex
	proc, sys, export string
	identity          Identity
	snapshot          Snapshot
	previousCPU       cpuCount
	previousNetwork   map[string]netCount
	previousTime      time.Time
}

func readText(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 65537))
	if len(b) > 65536 {
		return "", errors.New("telemetry file too large")
	}
	return strings.TrimSpace(strings.TrimRight(string(b), "\x00\n")), err
}
func textOrEmpty(path string) string { s, _ := readText(path); return s }
func NewCollector(procRoot, sysRoot, etcRoot, exportRoot string) *Collector {
	hostname, _ := os.Hostname()
	osName := ""
	for _, line := range strings.Split(textOrEmpty(filepath.Join(etcRoot, "os-release")), "\n") {
		if v, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
			osName = strings.Trim(v, "\"'")
		}
	}
	identity := Identity{Model: textOrEmpty(filepath.Join(procRoot, "device-tree/model")), Hostname: hostname, OS: osName, L4T: textOrEmpty(filepath.Join(etcRoot, "nv_tegra_release")), Kernel: textOrEmpty(filepath.Join(procRoot, "sys/kernel/osrelease")), Architecture: runtime.GOARCH}
	return &Collector{proc: procRoot, sys: sysRoot, export: exportRoot, identity: identity, snapshot: Snapshot{Identity: identity, Stale: true, Warnings: []string{}, Disks: []Disk{}, Networks: []Network{}}}
}
func pointer[T any](v T) *T { return &v }
func copyPointer[T any](p *T) *T {
	if p == nil {
		return nil
	}
	return pointer(*p)
}
func cloneSnapshot(s Snapshot) Snapshot {
	s.Warnings = append([]string{}, s.Warnings...)
	s.Disks = append([]Disk{}, s.Disks...)
	s.Networks = append([]Network{}, s.Networks...)
	for i := range s.Networks {
		s.Networks[i].Addresses = append([]string{}, s.Networks[i].Addresses...)
		s.Networks[i].RXBytesPerSecond = copyPointer(s.Networks[i].RXBytesPerSecond)
		s.Networks[i].TXBytesPerSecond = copyPointer(s.Networks[i].TXBytesPerSecond)
	}
	s.UptimeSeconds = copyPointer(s.UptimeSeconds)
	s.CPUPercent = copyPointer(s.CPUPercent)
	s.GPUPercent = copyPointer(s.GPUPercent)
	s.CPUTempC = copyPointer(s.CPUTempC)
	s.GPUTempC = copyPointer(s.GPUTempC)
	s.RAMTotalBytes = copyPointer(s.RAMTotalBytes)
	s.RAMUsedBytes = copyPointer(s.RAMUsedBytes)
	s.SwapTotalBytes = copyPointer(s.SwapTotalBytes)
	s.SwapUsedBytes = copyPointer(s.SwapUsedBytes)
	return s
}
func (c *Collector) Latest() Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := cloneSnapshot(c.snapshot)
	s.Stale = s.SampledAt.IsZero() || time.Since(s.SampledAt) > 6*time.Second
	return s
}
func parseCPU(text string) (cpuCount, error) {
	var result cpuCount
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[0] != "cpu" {
			continue
		}
		for i := 1; i < len(fields) && i <= 8; i++ {
			n, err := strconv.ParseUint(fields[i], 10, 64)
			if err != nil {
				return result, err
			}
			result.total += n
			if i == 4 || i == 5 {
				result.idle += n
			}
		}
		return result, nil
	}
	return result, errors.New("CPU counters unavailable")
}
func memoryInfo(text string) map[string]uint64 {
	out := make(map[string]uint64)
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) != 3 || f[2] != "kB" {
			continue
		}
		n, err := strconv.ParseUint(f[1], 10, 64)
		if err == nil && n <= ^uint64(0)/1024 {
			out[strings.TrimSuffix(f[0], ":")] = n * 1024
		}
	}
	return out
}
func (c *Collector) Sample(now time.Time) (Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cpuText, err := readText(filepath.Join(c.proc, "stat"))
	if err != nil {
		return Snapshot{}, errors.New("CPU counters unavailable")
	}
	cpu, err := parseCPU(cpuText)
	if err != nil {
		return Snapshot{}, err
	}
	s := Snapshot{Identity: c.identity, SampledAt: now, Warnings: []string{}, Disks: []Disk{}, Networks: []Network{}}
	elapsed := now.Sub(c.previousTime).Seconds()
	if !c.previousTime.IsZero() && elapsed > 0 && cpu.total > c.previousCPU.total && cpu.idle >= c.previousCPU.idle {
		total, idle := cpu.total-c.previousCPU.total, cpu.idle-c.previousCPU.idle
		if idle <= total {
			s.CPUPercent = pointer(100 * float64(total-idle) / float64(total))
		}
	}
	if fields := strings.Fields(textOrEmpty(filepath.Join(c.proc, "uptime"))); len(fields) > 0 {
		if n, e := strconv.ParseFloat(fields[0], 64); e == nil && n >= 0 && !math.IsInf(n, 0) {
			s.UptimeSeconds = pointer(n)
		}
	}
	mem := memoryInfo(textOrEmpty(filepath.Join(c.proc, "meminfo")))
	if total, ok := mem["MemTotal"]; ok && total > 0 {
		s.RAMTotalBytes = pointer(total)
		if avail, ok := mem["MemAvailable"]; ok && avail <= total {
			s.RAMUsedBytes = pointer(total - avail)
		}
	}
	if total, ok := mem["SwapTotal"]; ok {
		s.SwapTotalBytes = pointer(total)
		if free, ok := mem["SwapFree"]; ok && free <= total {
			s.SwapUsedBytes = pointer(total - free)
		}
	}
	if n, e := strconv.ParseUint(textOrEmpty(filepath.Join(c.sys, "devices/gpu.0/load")), 10, 16); e == nil && n <= 1000 {
		s.GPUPercent = pointer(float64(n) / 10)
	}
	zones, _ := filepath.Glob(filepath.Join(c.sys, "class/thermal/thermal_zone*"))
	for _, zone := range zones {
		kind := strings.ToLower(textOrEmpty(filepath.Join(zone, "type")))
		n, e := strconv.ParseFloat(textOrEmpty(filepath.Join(zone, "temp")), 64)
		if e != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < -40000 || n > 150000 {
			continue
		}
		if strings.HasPrefix(kind, "cpu") {
			s.CPUTempC = pointer(n / 1000)
		} else if strings.HasPrefix(kind, "gpu") {
			s.GPUTempC = pointer(n / 1000)
		}
	}
	for _, item := range []struct {
		missing bool
		message string
	}{{s.RAMUsedBytes == nil, "RAM usage unavailable"}, {s.SwapUsedBytes == nil, "Swap usage unavailable"}, {s.GPUPercent == nil, "GPU load unavailable"}, {s.CPUTempC == nil, "CPU temperature unavailable"}, {s.GPUTempC == nil, "GPU temperature unavailable"}, {s.UptimeSeconds == nil, "Uptime unavailable"}} {
		if item.missing {
			s.Warnings = append(s.Warnings, item.message)
		}
	}
	for _, path := range []string{"/", c.export} {
		var stat syscall.Statfs_t
		if err := syscall.Statfs(path, &stat); err != nil {
			s.Warnings = append(s.Warnings, "A disk reading is unavailable")
			continue
		}
		b := uint64(stat.Bsize)
		s.Disks = append(s.Disks, Disk{MountPath: path, TotalBytes: stat.Blocks * b, UsedBytes: (stat.Blocks - stat.Bfree) * b, FreeBytes: stat.Bfree * b, AvailableBytes: stat.Bavail * b})
	}
	netText, netErr := readText(filepath.Join(c.proc, "net/dev"))
	current := make(map[string]netCount)
	addrs := map[string][]string{}
	interfaces, _ := net.Interfaces()
	for _, iface := range interfaces {
		list, _ := iface.Addrs()
		for _, a := range list {
			addrs[iface.Name] = append(addrs[iface.Name], a.String())
		}
	}
	for _, line := range strings.Split(netText, "\n") {
		name, rest, ok := strings.Cut(line, ":")
		name = strings.TrimSpace(name)
		fields := strings.Fields(rest)
		if !ok || name == "lo" || len(fields) < 9 {
			continue
		}
		rx, e1 := strconv.ParseUint(fields[0], 10, 64)
		tx, e2 := strconv.ParseUint(fields[8], 10, 64)
		if e1 != nil || e2 != nil {
			continue
		}
		current[name] = netCount{rx, tx}
		n := Network{Name: name, Addresses: append([]string{}, addrs[name]...)}
		if old, ok := c.previousNetwork[name]; ok && elapsed > 0 && !c.previousTime.IsZero() {
			if rx >= old.rx {
				n.RXBytesPerSecond = pointer(float64(rx-old.rx) / elapsed)
			}
			if tx >= old.tx {
				n.TXBytesPerSecond = pointer(float64(tx-old.tx) / elapsed)
			}
		}
		s.Networks = append(s.Networks, n)
	}
	if netErr != nil || len(s.Networks) == 0 {
		s.Warnings = append(s.Warnings, "Network counters unavailable")
	}
	sort.Slice(s.Networks, func(i, j int) bool { return s.Networks[i].Name < s.Networks[j].Name })
	c.previousTime = now
	c.previousCPU = cpu
	c.previousNetwork = current
	c.snapshot = s
	return cloneSnapshot(s), nil
}
func (c *Collector) Run(ctx context.Context, interval time.Duration) {
	if ctx.Err() != nil {
		return
	}
	_, _ = c.Sample(time.Now())
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			_, _ = c.Sample(now)
		}
	}
}
