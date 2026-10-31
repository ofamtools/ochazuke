// Package sysstat samples the machine for status segments: CPU busy
// percent, this process's CPU use, memory used and total, and free disk
// space at a path, on macOS and Linux, without cgo. Register adds them to
// a runtime as the segment providers cpu, proc, ram and disk and as
// segment rows, off unless the application turns them on
// (Options.Enabled), each behind its own status.segments.<id> setting;
// status.proc-scale picks the proc segment's scale.
//
// The CPU and busy-percent logic is adapted from tuios
// (internal/app/sysinfo*.go, MIT): a reading is cumulative idle and total
// ticks, and the percent is the busy share of the time between two.
package sysstat

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/unixsuperhero/ochazuke"
)

// ErrUnsupported is a reading on a platform without a reader.
var ErrUnsupported = errors.New("not supported on this platform")

// ticks is one reading of the machine-wide cumulative CPU counters, in
// whatever unit the platform counts; only the difference between two
// readings means anything.
type ticks struct {
	idle, total uint64
}

// busyPercent is the share of the time between two readings the CPUs
// spent busy, 0 to 100; a counter that went backwards yields 0.
func busyPercent(prev, cur ticks) float64 {
	if cur.total <= prev.total || cur.idle < prev.idle {
		return 0
	}
	total, idle := cur.total-prev.total, cur.idle-prev.idle
	if idle >= total {
		return 0
	}
	return 100 * float64(total-idle) / float64(total)
}

// CPU is a CPU sampler: each Sample is the busy percent since the one
// before. The first sample takes a baseline, waits FirstWait and reads
// again, so it answers a real figure.
type CPU struct {
	mu   sync.Mutex
	last ticks
	has  bool
	read func() (ticks, error)
}

// FirstWait is how long the first CPU sample waits between its two
// readings.
const FirstWait = 200 * time.Millisecond

// NewCPU returns a CPU sampler for this platform.
func NewCPU() *CPU { return &CPU{read: readTicks} }

// Sample is the busy percent since the last sample.
func (c *CPU) Sample(ctx context.Context) (float64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.has {
		t, err := c.read()
		if err != nil {
			return 0, err
		}
		c.last, c.has = t, true
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(FirstWait):
		}
	}
	t, err := c.read()
	if err != nil {
		c.has = false
		return 0, err
	}
	p := busyPercent(c.last, t)
	c.last = t
	return p, nil
}

// Memory is the machine's memory: Used is what programs hold (total less
// free, file cache and other reclaimable pages).
type Memory struct {
	Used, Total uint64
}

// ReadMemory reads the machine's memory.
func ReadMemory() (Memory, error) { return readMemory() }

// Disk is the space of the filesystem holding a path: Free is what an
// unprivileged user may still write.
type Disk struct {
	Free, Total uint64
}

// ReadDisk reads the filesystem holding path.
func ReadDisk(path string) (Disk, error) { return readDisk(path) }

// Bytes is n in binary units, one decimal under 10 ("8.1G", "512M", "120G").
func Bytes(n uint64) string {
	const unit = 1024
	units := []string{"B", "K", "M", "G", "T", "P"}
	v := float64(n)
	i := 0
	for v >= unit && i < len(units)-1 {
		v /= unit
		i++
	}
	if v < 10 && i > 0 {
		return fmt.Sprintf("%.1f%s", v, units[i])
	}
	return fmt.Sprintf("%.0f%s", v, units[i])
}

// The tone thresholds: CPU or memory at HotPercent and up, or a disk
// with less than LowDiskPercent free, answer ochazuke.ToneErrored.
const (
	HotPercent     = 90
	LowDiskPercent = 5
)

// Options are Register's: Enabled turns the four segments on by default
// (each still has its setting); DiskPath is the path the disk segment
// reads (default the user's home directory); Every is their refresh
// interval (default 2s for cpu and ram, 30s for disk).
type Options struct {
	Enabled  bool
	DiskPath string
	Every    time.Duration
}

// The provider and segment ids Register adds.
const (
	IDCPU  = "cpu"
	IDProc = "proc"
	IDRAM  = "ram"
	IDDisk = "disk"
)

// Providers registers the cpu, proc, ram and disk providers on r. The
// disk provider reads its segment's Path (default the home directory);
// the proc provider its scale argument (ScaleCore or ScaleMachine).
func Providers(r *ochazuke.Runtime) {
	r.SegmentProvider(IDProc, procProvider(NewProc(), numCPU()))
	cpu := NewCPU()
	r.SegmentProvider(IDCPU, func(ctx context.Context, _ ochazuke.Segment) (ochazuke.SegmentValue, error) {
		p, err := cpu.Sample(ctx)
		if err != nil {
			return ochazuke.SegmentValue{}, err
		}
		return ochazuke.SegmentValue{Text: fmt.Sprintf("%.0f%%", p), Tone: hot(p >= HotPercent)}, nil
	})
	r.SegmentProvider(IDRAM, func(context.Context, ochazuke.Segment) (ochazuke.SegmentValue, error) {
		m, err := ReadMemory()
		if err != nil {
			return ochazuke.SegmentValue{}, err
		}
		hotRAM := m.Total > 0 && float64(m.Used)*100/float64(m.Total) >= HotPercent
		return ochazuke.SegmentValue{Text: Bytes(m.Used) + "/" + Bytes(m.Total), Tone: hot(hotRAM)}, nil
	})
	r.SegmentProvider(IDDisk, func(_ context.Context, s ochazuke.Segment) (ochazuke.SegmentValue, error) {
		path := s.Path
		if path == "" {
			path, _ = os.UserHomeDir()
		}
		d, err := ReadDisk(path)
		if err != nil {
			return ochazuke.SegmentValue{}, err
		}
		low := d.Total > 0 && float64(d.Free)*100/float64(d.Total) < LowDiskPercent
		return ochazuke.SegmentValue{Text: Bytes(d.Free), Tone: hot(low)}, nil
	})
}

func hot(on bool) string {
	if on {
		return ochazuke.ToneErrored
	}
	return ""
}

// Segments are the cpu, proc, ram and disk rows Register adds. The proc
// row is labelled with the program's name ("mtm 118%"), else "proc", and
// draws beside cpu (same order, added after it); its scale argument names
// the status.proc-scale setting.
func Segments(o Options) []ochazuke.Segment {
	every := func(d time.Duration) time.Duration {
		if o.Every > 0 {
			return o.Every
		}
		return d
	}
	on := ochazuke.On(o.Enabled)
	return []ochazuke.Segment{
		{ID: IDCPU, Label: "cpu", Provider: IDCPU, Every: every(2 * time.Second), Order: 100, Enabled: on, Description: "Show the CPU busy percent in the header rule"},
		{ID: IDProc, Label: procLabel(), Provider: IDProc, Args: ochazuke.Arguments{procScaleArg: "{" + ProcScaleSetting + "}"}, Every: every(2 * time.Second), Order: 100, Enabled: on, Description: "Show this program's CPU use in the header rule (status.proc-scale picks the scale)"},
		{ID: IDRAM, Label: "ram", Provider: IDRAM, Every: every(2 * time.Second), Order: 101, Enabled: on, Description: "Show memory used/total in the header rule"},
		{ID: IDDisk, Label: "disk", Provider: IDDisk, Every: every(30 * time.Second), Order: 102, Enabled: on, Path: o.DiskPath, Description: "Show the free disk space of the data volume in the header rule"},
	}
}

// Register adds the providers, the cpu, proc, ram and disk segments and
// the status.proc-scale setting to r.
func Register(r *ochazuke.Runtime, o Options) error {
	Providers(r)
	if !slices.ContainsFunc(r.Settings().Declared(), func(s ochazuke.Setting) bool { return s.Name == ProcScaleSetting }) {
		if err := r.DeclareSetting(procScaleDeclaration()); err != nil {
			return err
		}
	}
	return r.AddSegment(Segments(o)...)
}
