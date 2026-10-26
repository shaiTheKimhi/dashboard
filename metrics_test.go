package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func put(t *testing.T, root, name, value string) {
	t.Helper()
	p := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
}
func fixtureCollector(t *testing.T) (*Collector, string, string) {
	t.Helper()
	base := t.TempDir()
	proc := filepath.Join(base, "proc")
	sys := filepath.Join(base, "sys")
	etc := filepath.Join(base, "etc")
	put(t, proc, "stat", "cpu 20 0 20 60 0 0 0 0 5 0\n")
	put(t, proc, "uptime", "1234.50 0\n")
	put(t, proc, "meminfo", "MemTotal: 1000 kB\nMemAvailable: 400 kB\nSwapTotal: 100 kB\nSwapFree: 75 kB\n")
	put(t, proc, "net/dev", "Inter-| Receive | Transmit\n face |bytes packets errs drop fifo frame compressed multicast|bytes packets errs drop fifo colls carrier compressed\n eth0: 1000 0 0 0 0 0 0 0 2000 0 0 0 0 0 0 0\n lo: 99 0 0 0 0 0 0 0 99 0 0 0 0 0 0 0\n")
	put(t, proc, "device-tree/model", "NVIDIA Jetson Nano\x00")
	put(t, proc, "sys/kernel/osrelease", "4.9.253-tegra\n")
	put(t, etc, "os-release", "PRETTY_NAME=\"Ubuntu 18.04.6 LTS\"\n")
	put(t, etc, "nv_tegra_release", "# R32 (release), REVISION: 7.1\n")
	put(t, sys, "devices/gpu.0/load", "250\n")
	put(t, sys, "class/thermal/thermal_zone3/type", "CPU-therm\n")
	put(t, sys, "class/thermal/thermal_zone3/temp", "36500\n")
	put(t, sys, "class/thermal/thermal_zone7/type", "GPU-therm\n")
	put(t, sys, "class/thermal/thermal_zone7/temp", "34000\n")
	return NewCollector(proc, sys, etc, base), proc, sys
}
func eqFloat(t *testing.T, got *float64, want float64) {
	t.Helper()
	if got == nil || *got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// Catches unit conversion, named sensor discovery and initial-sample errors.
func TestSampleLinux(t *testing.T) {
	c, _, _ := fixtureCollector(t)
	s, err := c.Sample(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if s.Identity.Model != "NVIDIA Jetson Nano" || s.Identity.OS != "Ubuntu 18.04.6 LTS" {
		t.Fatalf("bad identity: %+v", s.Identity)
	}
	if s.RAMUsedBytes == nil || *s.RAMUsedBytes != 614400 {
		t.Fatalf("bad RAM: %+v", s.RAMUsedBytes)
	}
	if s.SwapUsedBytes == nil || *s.SwapUsedBytes != 25600 {
		t.Fatal("bad swap conversion")
	}
	eqFloat(t, s.GPUPercent, 25)
	eqFloat(t, s.CPUTempC, 36.5)
	eqFloat(t, s.GPUTempC, 34)
	eqFloat(t, s.UptimeSeconds, 1234.5)
	if s.CPUPercent != nil || len(s.Networks) != 1 || s.Networks[0].RXBytesPerSecond != nil {
		t.Fatal("first counters must be unavailable, loopback excluded")
	}
	if len(s.Disks) == 0 || s.Disks[0].TotalBytes == 0 {
		t.Fatal("missing statfs disk readings")
	}
}

// Catches guest double-counting, negative rates and fabricated utilization.
func TestCounterDeltas(t *testing.T) {
	c, proc, _ := fixtureCollector(t)
	now := time.Now()
	if _, err := c.Sample(now); err != nil {
		t.Fatal(err)
	}
	put(t, proc, "stat", "cpu 40 0 40 120 0 0 0 0 10 0\n")
	put(t, proc, "net/dev", "eth0: 3000 0 0 0 0 0 0 0 6000 0 0 0 0 0 0 0\n")
	s, err := c.Sample(now.Add(2 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	eqFloat(t, s.CPUPercent, 40)
	eqFloat(t, s.Networks[0].RXBytesPerSecond, 1000)
	eqFloat(t, s.Networks[0].TXBytesPerSecond, 2000)
	put(t, proc, "net/dev", "eth0: 1 0 0 0 0 0 0 0 1 0 0 0 0 0 0 0\n wlan0: 30 0 0 0 0 0 0 0 50 0 0 0 0 0 0 0\n")
	s, _ = c.Sample(now.Add(4 * time.Second))
	if len(s.Networks) != 2 || s.Networks[0].RXBytesPerSecond != nil || s.Networks[1].RXBytesPerSecond != nil {
		t.Fatal("reset/new counters must be null")
	}
	put(t, proc, "net/dev", "wlan0: 40 0 0 0 0 0 0 0 60 0 0 0 0 0 0 0\n")
	s, _ = c.Sample(now.Add(4 * time.Second))
	if len(s.Networks) != 1 || s.Networks[0].RXBytesPerSecond != nil {
		t.Fatal("zero elapsed/removal handling")
	}
}

// Catches sensor failures poisoning the rest of the system snapshot.
func TestMissingSensors(t *testing.T) {
	c, proc, sys := fixtureCollector(t)
	if err := os.RemoveAll(filepath.Join(sys, "class/thermal")); err != nil {
		t.Fatal(err)
	}
	put(t, sys, "devices/gpu.0/load", "not a number")
	put(t, proc, "meminfo", "MemTotal: 1000 kB\n")
	s, err := c.Sample(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if s.CPUTempC != nil || s.GPUTempC != nil || s.GPUPercent != nil || s.RAMUsedBytes != nil || len(s.Warnings) == 0 {
		t.Fatal("missing metrics must be null with warnings")
	}
	if s.UptimeSeconds == nil {
		t.Fatal("unrelated metric lost")
	}
}

// Catches non-finite optional readings preventing valid JSON for all metrics.
func TestNonFiniteSensors(t *testing.T) {
	c, proc, sys := fixtureCollector(t)
	put(t, proc, "uptime", "+Inf 0")
	put(t, sys, "class/thermal/thermal_zone3/temp", "NaN")
	s, err := c.Sample(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if s.CPUTempC != nil || s.UptimeSeconds != nil {
		t.Fatal("non-finite optional readings must be unavailable")
	}
}

// Catches failed sampling and mutable API snapshots corrupting the cache.
func TestSnapshotFreshness(t *testing.T) {
	c, proc, _ := fixtureCollector(t)
	now := time.Now().Add(-7 * time.Second)
	s, err := c.Sample(now)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Latest().Stale {
		t.Fatal("old snapshot not stale")
	}
	put(t, proc, "stat", "malformed")
	if _, err = c.Sample(time.Now()); err == nil {
		t.Fatal("malformed CPU should fail cycle")
	}
	if !c.Latest().SampledAt.Equal(s.SampledAt) {
		t.Fatal("failure advanced timestamp")
	}
	copy := c.Latest()
	copy.Networks[0].Name = "changed"
	*copy.GPUPercent = 999
	if c.Latest().Networks[0].Name == "changed" || *c.Latest().GPUPercent == 999 {
		t.Fatal("snapshot aliases collector state")
	}
	c2, _, _ := fixtureCollector(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c2.Run(ctx, time.Millisecond)
}
