package sysstat

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/unixsuperhero/ochazuke"
)

func TestBusyPercent(t *testing.T) {
	for _, c := range []struct {
		prev, cur ticks
		want      float64
	}{
		{ticks{idle: 50, total: 100}, ticks{idle: 75, total: 200}, 75},
		{ticks{idle: 50, total: 100}, ticks{idle: 150, total: 200}, 0},
		{ticks{idle: 50, total: 100}, ticks{idle: 40, total: 200}, 0}, // a counter went backwards
		{ticks{idle: 50, total: 100}, ticks{idle: 50, total: 100}, 0},
	} {
		if got := busyPercent(c.prev, c.cur); got != c.want {
			t.Errorf("busyPercent(%v, %v) = %v, want %v", c.prev, c.cur, got, c.want)
		}
	}
}

func TestBytes(t *testing.T) {
	for n, want := range map[uint64]string{0: "0B", 512: "512B", 8_700_000_000: "8.1G", 128 << 30: "128G", 34359738368: "32G", 1536 << 20: "1.5G"} {
		if got := Bytes(n); got != want {
			t.Errorf("Bytes(%d) = %q, want %q", n, got, want)
		}
	}
}

// The readers answer real figures on the platforms they support.
func TestReadersOnThisMachine(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("no readers on " + runtime.GOOS)
	}
	m, err := ReadMemory()
	if err != nil || m.Total == 0 || m.Used > m.Total {
		t.Fatalf("ReadMemory = %+v, %v", m, err)
	}
	d, err := ReadDisk(t.TempDir())
	if err != nil || d.Total == 0 || d.Free > d.Total {
		t.Fatalf("ReadDisk = %+v, %v", d, err)
	}
	cpu := NewCPU()
	start := time.Now()
	p, err := cpu.Sample(context.Background())
	if err != nil || p < 0 || p > 100 {
		t.Fatalf("Sample = %v, %v", p, err)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("the first sample took %s", took)
	}
	t.Logf("memory %s/%s, disk free %s, cpu %.0f%%", Bytes(m.Used), Bytes(m.Total), Bytes(d.Free), p)
}

// Register adds the providers and three rows, off unless Enabled.
func TestRegisterIsOffByDefault(t *testing.T) {
	for _, on := range []bool{false, true} {
		r := ochazuke.NewRuntime()
		r.Config().Disable()
		if err := Register(r, Options{Enabled: on, DiskPath: t.TempDir()}); err != nil {
			t.Fatal(err)
		}
		r.EnsureSegments()
		got := r.Segments()
		if len(got) != 3 || got[0].ID != IDCPU || got[1].ID != IDRAM || got[2].ID != IDDisk {
			t.Fatalf("segments = %+v", got)
		}
		for _, s := range got {
			if s.Shown != on || s.Error != "" {
				t.Fatalf("Enabled %t: %s shown %t error %q", on, s.ID, s.Shown, s.Error)
			}
		}
	}
}
