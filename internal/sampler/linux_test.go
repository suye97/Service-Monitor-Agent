package sampler

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	cpu      int
	commands []string
}

func (f *fakeRunner) Run(_ context.Context, command string) (string, error) {
	f.commands = append(f.commands, command)
	switch command {
	case "head -n 1 /proc/stat":
		f.cpu++
		if f.cpu == 1 {
			return "cpu 100 0 100 700 100 0 0 0\n", nil
		}
		return "cpu 150 0 150 750 100 0 0 0\n", nil
	case "cat /proc/meminfo":
		return "MemTotal: 1000 kB\nMemAvailable: 250 kB\n", nil
	case "df -Pk -l":
		return "Filesystem 1024-blocks Used Available Capacity Mounted on\n/dev/sda1 100 82 18 82% /\ntmpfs 20 0 20 0% /run\n/dev/sdb1 100 90 10 90% /data\n", nil
	}
	return "", errors.New("unexpected command")
}
func TestCollect(t *testing.T) {
	f := &fakeRunner{}
	m, err := CollectWithInterval(context.Background(), f, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if m.CPUPercent != float64(100)*100/150 || m.MemoryPercent != 75 || m.DiskPercent != 90 || len(m.Disks) != 2 || m.Disks[1].Mount != "/data" {
		t.Fatalf("metrics: %+v", m)
	}
	if len(f.commands) != 4 {
		t.Fatalf("commands: %v", f.commands)
	}
}
func TestCancellationAndInvalidCPU(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CollectWithInterval(ctx, &fakeRunner{}, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	if _, err := cpuUsage("cpu 10 0 0 0", "cpu 9 0 0 0"); err == nil {
		t.Fatal("counter regression accepted")
	}
	if _, err := parseDisks(strings.Repeat("invalid\n", 2)); err == nil {
		t.Fatal("bad df accepted")
	}
}
