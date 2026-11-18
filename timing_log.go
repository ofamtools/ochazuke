package ochazuke

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"
)

// The timing log is an opt-in record of slow frames, for finding out where a
// real session lags: with debug.timing-log on, each Update or View that took
// debug.timing-threshold or longer adds one JSON line to TimingLogFile in the
// state directory. A line holds when it happened, what ran, how long it took,
// the message's Go type and whether that message was a key; never a key's
// value, pane text or any other content.
const (
	TimingLogSetting       = "debug.timing-log"
	TimingThresholdSetting = "debug.timing-threshold"
	TimingLogFile          = "timing.log"
	// timingLogMax is where the log turns over: the full file becomes
	// TimingLogFile.1, replacing the one before, so the two stay under
	// twice this size.
	timingLogMax = 1 << 20
)

// timingLog is the Model's side of the log. on mirrors the setting, so a
// frame with the log off reads one bool; outer says TimingModel times the
// whole application, and the Model's own timing stands down.
type timingLog struct {
	on, outer bool
	msg       string // the Go type of the last message, what a View draws after
	key       bool   // the last message was a key
}

// timingLine is one line of the log.
type timingLine struct {
	Time string  `json:"time"`
	Kind string  `json:"kind"` // update or view
	MS   float64 `json:"ms"`
	Msg  string  `json:"msg"`
	Key  bool    `json:"key"`
}

// watchTimingLog keeps timing.on with the setting once it changes; Init reads
// the value the program started with.
func (m *Model) watchTimingLog() {
	m.Runtime.Settings().Observe(TimingLogSetting, func(string, any) { m.readTimingLog() })
}

func (m *Model) readTimingLog() { m.timing.on = m.Runtime.Settings().Bool(TimingLogSetting) }

// timingOff is what timeUpdate and timeView answer with the log off: a frame
// then pays for one bool and an empty call.
func timingOff() {}

// timeUpdate starts timing the Model's Update; the answer ends it.
func (m *Model) timeUpdate(msg tea.Msg) func() {
	if !m.timing.on || m.timing.outer {
		return timingOff
	}
	return m.startUpdate(msg)
}

// timeView starts timing the Model's View; the answer ends it.
func (m *Model) timeView() func() {
	if !m.timing.on || m.timing.outer {
		return timingOff
	}
	return m.startView()
}

func (m *Model) startUpdate(msg tea.Msg) func() {
	start := m.clock()
	return func() {
		m.timing.msg = fmt.Sprintf("%T", msg)
		_, m.timing.key = msg.(tea.KeyMsg)
		m.noteTiming("update", m.clock().Sub(start))
	}
}

func (m *Model) startView() func() {
	start := m.clock()
	return func() { m.noteTiming("view", m.clock().Sub(start)) }
}

// noteTiming writes the line of a frame at or over the threshold. The log is
// a diagnostic: a write that fails is dropped, never shown.
func (m *Model) noteTiming(kind string, took time.Duration) {
	if took < m.Runtime.Settings().Duration(TimingThresholdSetting) {
		return
	}
	dir := m.Runtime.Config().StateDirectory()
	if dir == "" {
		return
	}
	line, err := json.Marshal(timingLine{m.clock().Format(time.RFC3339Nano), kind, float64(took.Microseconds()) / 1000, m.timing.msg, m.timing.key})
	if err != nil {
		return
	}
	_ = appendTimingLine(filepath.Join(dir, TimingLogFile), append(line, '\n'), timingLogMax)
}

// appendTimingLine adds line to the file at path, turning the file over first
// when the line would take it past limit.
func appendTimingLine(path string, line []byte, limit int64) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if info, err := os.Stat(path); err == nil && info.Size()+int64(len(line)) > limit {
		if err := os.Rename(path, path+".1"); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(line)
	return err
}

// TimingModel wraps app, the application's own model around m, so the timing
// log covers the application's whole Update and View and not only the Model's
// share of them. Run the program on what it returns.
func (m *Model) TimingModel(app tea.Model) tea.Model {
	m.timing.outer = true
	return timedModel{app, m}
}

type timedModel struct {
	app tea.Model
	m   *Model
}

func (t timedModel) Init() tea.Cmd { return t.app.Init() }

func (t timedModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if t.m.timing.on {
		defer t.m.startUpdate(msg)()
	}
	var cmd tea.Cmd
	t.app, cmd = t.app.Update(msg)
	return t, cmd
}

func (t timedModel) View() tea.View {
	if t.m.timing.on {
		defer t.m.startView()()
	}
	return t.app.View()
}
