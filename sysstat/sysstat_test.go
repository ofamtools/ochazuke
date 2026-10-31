package sysstat

import (
	"context"
	"runtime"
	"testing"
	"time"

	"ofam.dev/ochazuke"
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

// Register adds the providers and four rows, off unless Enabled.
func TestRegisterIsOffByDefault(t *testing.T) {
	for _, on := range []bool{false, true} {
		r := ochazuke.NewRuntime()
		r.Config().Disable()
		if err := Register(r, Options{Enabled: on, DiskPath: t.TempDir()}); err != nil {
			t.Fatal(err)
		}
		r.EnsureSegments()
		got := r.Segments()
		if len(got) != 4 || got[0].ID != IDCPU || got[1].ID != IDProc || got[2].ID != IDRAM || got[3].ID != IDDisk {
			t.Fatalf("segments = %+v", got)
		}
		for _, s := range got {
			if s.Shown != on || s.Error != "" {
				t.Fatalf("Enabled %t: %s shown %t error %q", on, s.ID, s.Shown, s.Error)
			}
		}
	}
}

// fakeProc is a Proc over a CPU counter and a clock the test moves.
type fakeProc struct {
	cpu  time.Duration
	wall time.Time
}

func (f *fakeProc) proc() *Proc {
	return &Proc{
		read: func() (time.Duration, error) { return f.cpu, nil },
		now:  func() time.Time { return f.wall },
		sleep: func(context.Context, time.Duration) error {
			// The first sample's wait: 200ms pass, 300ms of CPU.
			f.wall = f.wall.Add(FirstWait)
			f.cpu += 300 * time.Millisecond
			return nil
		},
	}
}

func TestProcSample(t *testing.T) {
	f := &fakeProc{wall: time.Unix(1000, 0)}
	p := f.proc()
	if got, err := p.Sample(context.Background()); err != nil || got != 150 {
		t.Fatalf("first Sample = %v, %v; want 150 (300ms of CPU in 200ms)", got, err)
	}
	f.wall, f.cpu = f.wall.Add(2*time.Second), f.cpu+time.Second
	if got, _ := p.Sample(context.Background()); got != 50 {
		t.Fatalf("Sample = %v, want 50", got)
	}
	f.wall = f.wall.Add(time.Second)
	if got, _ := p.Sample(context.Background()); got != 0 {
		t.Fatalf("idle Sample = %v, want 0", got)
	}
	if got, _ := p.Sample(context.Background()); got != 0 {
		t.Fatalf("Sample with no wall time = %v, want 0", got)
	}
}

func TestProcSampleCanceledFirstWait(t *testing.T) {
	p := &Proc{
		read:  func() (time.Duration, error) { return 0, nil },
		now:   time.Now,
		sleep: sleepCtx,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Sample(ctx); err == nil {
		t.Fatal("a canceled first sample answered")
	}
}

func TestProcValueScales(t *testing.T) {
	for _, c := range []struct {
		pct   float64
		scale string
		cores int
		text  string
		hot   bool
	}{
		{118, ScaleCore, 8, "118%", false},
		{118, "", 8, "118%", false}, // an unresolved or empty scale is core
		{118, ScaleMachine, 8, "15%", false},
		{720, ScaleCore, 8, "720%", true},
		{720, ScaleMachine, 8, "90%", true},
		{95, ScaleCore, 1, "95%", true},
		{95, ScaleMachine, 0, "95%", true}, // no core count reads as one
	} {
		v := procValue(c.pct, c.scale, c.cores)
		if v.Text != c.text || (v.Tone == ochazuke.ToneErrored) != c.hot {
			t.Errorf("procValue(%v, %q, %d) = %+v, want %q hot %t", c.pct, c.scale, c.cores, v, c.text, c.hot)
		}
	}
}

func TestCorePercent(t *testing.T) {
	if got := corePercent(time.Second, 2*time.Second); got != 50 {
		t.Fatalf("corePercent = %v", got)
	}
	if got := corePercent(-time.Second, time.Second); got != 0 {
		t.Fatalf("a counter that went backwards = %v", got)
	}
}

// The proc row sits beside cpu, names the scale setting, and the setting
// is declared once with core as its default; it follows Enabled and its
// own status.segments.proc setting like the others.
func TestRegisterProc(t *testing.T) {
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	if err := Register(r, Options{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := Register(r, Options{Enabled: true}); err != nil {
		t.Fatalf("a second Register: %v", err)
	}
	if got := r.Settings().String(ProcScaleSetting); got != ScaleCore {
		t.Fatalf("%s = %q, want core", ProcScaleSetting, got)
	}
	if err := r.Settings().Set(ProcScaleSetting, "socket"); err == nil {
		t.Fatal("the scale took a value outside its enum")
	}
	r.EnsureSegments()
	segs := r.Segments()
	if len(segs) < 2 || segs[0].ID != IDCPU || segs[1].ID != IDProc {
		t.Fatalf("segments = %+v, want proc after cpu", segs)
	}
	var row ochazuke.Segment
	for _, s := range Segments(Options{}) {
		if s.ID == IDProc {
			row = s
		}
	}
	if row.Args[procScaleArg] != "{"+ProcScaleSetting+"}" || row.Label != IDProc {
		t.Fatalf("proc row = %+v (a test binary is labelled proc)", row)
	}
	if err := r.Settings().Set(ochazuke.SegmentSetting(IDProc), false); err != nil {
		t.Fatal(err)
	}
	for _, s := range r.Segments() {
		if s.ID == IDProc && s.Shown {
			t.Fatal("status.segments.proc off still shows proc")
		}
	}
}

// The process reader answers a real, growing figure.
func TestReadProcTimeOnThisMachine(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("no reader on " + runtime.GOOS)
	}
	a, err := readProcTime()
	if err != nil {
		t.Fatal(err)
	}
	for end := time.Now().Add(20 * time.Millisecond); time.Now().Before(end); {
	}
	b, err := readProcTime()
	if err != nil || b < a || b == 0 {
		t.Fatalf("readProcTime = %v then %v, %v", a, b, err)
	}
}

// The provider reads the scale from the row's resolved argument.
func TestProcProviderReadsScaleArg(t *testing.T) {
	for scale, want := range map[string]string{ScaleCore: "150%", ScaleMachine: "38%"} {
		f := &fakeProc{wall: time.Unix(1000, 0)}
		v, err := procProvider(f.proc(), 4)(context.Background(), ochazuke.Segment{ID: IDProc, Args: ochazuke.Arguments{procScaleArg: scale}})
		if err != nil || v.Text != want {
			t.Errorf("scale %s: %+v, %v; want %s", scale, v, err, want)
		}
	}
}
