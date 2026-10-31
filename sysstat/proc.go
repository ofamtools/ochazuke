package sysstat

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/unixsuperhero/ochazuke"
)

// The proc segment's scales (the status.proc-scale setting): ScaleCore
// counts one core as 100%, as Activity Monitor and top do, so a busy
// process reads over 100% on a machine with several; ScaleMachine is the
// share of the whole machine, 100% every core, as the cpu segment reads.
const (
	ScaleCore    = "core"
	ScaleMachine = "machine"
)

// ProcScaleSetting picks the proc segment's scale.
const ProcScaleSetting = "status.proc-scale"

// procScaleArg is the proc row's argument that carries the scale; its
// value names the setting, so the runtime reads it on the loop at each
// refresh (Segment.Args).
const procScaleArg = "scale"

// Proc is a CPU sampler for this process: each Sample is the user plus
// system time it used since the one before, over the wall time between
// them, in percent of one core (a process busy on two cores reads 200).
// The first sample takes a baseline, waits FirstWait and reads again, as
// CPU's does.
type Proc struct {
	mu    sync.Mutex
	last  time.Duration // CPU time at the last reading
	at    time.Time     // when it was read
	has   bool
	read  func() (time.Duration, error)
	now   func() time.Time
	sleep func(context.Context, time.Duration) error
}

// NewProc returns a process CPU sampler for this platform.
func NewProc() *Proc { return &Proc{read: readProcTime, now: time.Now, sleep: sleepCtx} }

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Sample is the process's CPU percent of one core since the last sample.
func (p *Proc) Sample(ctx context.Context) (float64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.has {
		cpu, err := p.read()
		if err != nil {
			return 0, err
		}
		p.last, p.at, p.has = cpu, p.now(), true
		if err := p.sleep(ctx, FirstWait); err != nil {
			return 0, err
		}
	}
	cpu, err := p.read()
	if err != nil {
		p.has = false
		return 0, err
	}
	at := p.now()
	pct := corePercent(cpu-p.last, at.Sub(p.at))
	p.last, p.at = cpu, at
	return pct, nil
}

// corePercent is cpu time over wall time in percent of one core; no wall
// time or a counter that went backwards yields 0.
func corePercent(cpu, wall time.Duration) float64 {
	if wall <= 0 || cpu <= 0 {
		return 0
	}
	return 100 * float64(cpu) / float64(wall)
}

// procValue is the proc segment's answer for a reading in percent of one
// core: on the machine scale it is divided by the cores; any other scale
// is the core scale. It draws hot as the cpu segment does, when the
// process holds HotPercent of the whole machine.
func procValue(pct float64, scale string, cores int) ochazuke.SegmentValue {
	cores = max(cores, 1)
	machine := pct / float64(cores)
	shown := pct
	if scale == ScaleMachine {
		shown = machine
	}
	return ochazuke.SegmentValue{Text: fmt.Sprintf("%.0f%%", shown), Tone: hot(machine >= HotPercent)}
}

// procLabel is the proc segment's label: the program's name, as Activity
// Monitor and top show the process, else "proc" (a go test binary).
func procLabel() string {
	if len(os.Args) == 0 {
		return IDProc
	}
	name := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	if name == "" || name == "." || strings.HasSuffix(name, ".test") {
		return IDProc
	}
	return name
}

// procProvider answers the proc segment from p: the scale is the row's
// scale argument (the setting's value, read on the loop).
func procProvider(p *Proc, cores int) ochazuke.SegmentProvider {
	return func(ctx context.Context, s ochazuke.Segment) (ochazuke.SegmentValue, error) {
		pct, err := p.Sample(ctx)
		if err != nil {
			return ochazuke.SegmentValue{}, err
		}
		scale, _ := s.Args[procScaleArg].(string)
		return procValue(pct, scale, cores), nil
	}
}

// procScaleDeclaration is the status.proc-scale setting row.
func procScaleDeclaration() ochazuke.Setting {
	return ochazuke.Setting{
		Name: ProcScaleSetting, Section: "status", Type: ochazuke.SettingString,
		Default: ScaleCore, Enum: []string{ScaleCore, ScaleMachine},
		Description: "The proc segment's scale: core counts one core as 100% (as Activity Monitor and top do, so a busy process reads over 100%); machine is the share of every core, as the cpu segment reads",
	}
}

var numCPU = runtime.NumCPU
