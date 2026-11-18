package ochazuke

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// timingFixture is a Model whose clock moves 150 ms per reading, so every
// frame reads as slow, with its state directory in a temp dir.
func timingFixture(t *testing.T) (*Model, string) {
	t.Helper()
	r := NewRuntime()
	r.EnsureConfigSettings()
	dir := t.TempDir()
	r.Config().StateDir = dir
	if _, err := r.AddPage("p", "P", func(*Runtime, int, int) Block { return Text("") }); err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	now := time.Unix(1000, 0)
	m.Now = func() time.Time { now = now.Add(150 * time.Millisecond); return now }
	return m, filepath.Join(dir, TimingLogFile)
}

// On, a slow key and the draw after it each write a line naming the message's
// type and that it was a key, and nothing of what was pressed. Off, or under
// the threshold, nothing is written.
func TestTimingLogSetting(t *testing.T) {
	press := tea.KeyPressMsg{Code: 'z', Text: "z"}
	t.Run("off", func(t *testing.T) {
		m, path := timingFixture(t)
		m.Update(press)
		m.View()
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("the log was written with the setting off: %v", err)
		}
	})
	t.Run("on", func(t *testing.T) {
		m, path := timingFixture(t)
		if err := m.Runtime.Settings().Set(TimingLogSetting, true); err != nil {
			t.Fatal(err)
		}
		m.Update(press)
		m.View()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		if len(lines) != 2 {
			t.Fatalf("want an update and a view line:\n%s", data)
		}
		for i, kind := range []string{"update", "view"} {
			var got timingLine
			if err := json.Unmarshal([]byte(lines[i]), &got); err != nil {
				t.Fatal(err)
			}
			if got.Kind != kind || got.Msg != "tea.KeyPressMsg" || !got.Key || got.MS < 150 || got.Time == "" {
				t.Fatalf("line %d: %+v", i, got)
			}
			if strings.Contains(lines[i], `"z"`) {
				t.Fatalf("the key's value reached the log: %s", lines[i])
			}
		}
	})
	t.Run("under the threshold", func(t *testing.T) {
		m, path := timingFixture(t)
		for name, value := range map[string]any{TimingLogSetting: true, TimingThresholdSetting: time.Hour} {
			if err := m.Runtime.Settings().Set(name, value); err != nil {
				t.Fatal(err)
			}
		}
		m.Update(press)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("a fast frame was written: %v", err)
		}
	})
}

// Wrapped by TimingModel, the application's frame is timed once: the Model
// inside it stands down.
func TestTimingModelTimesTheWholeFrameOnce(t *testing.T) {
	m, path := timingFixture(t)
	if err := m.Runtime.Settings().Set(TimingLogSetting, true); err != nil {
		t.Fatal(err)
	}
	app := m.TimingModel(m)
	app, _ = app.Update(tea.KeyPressMsg{Code: 'z', Text: "z"})
	app.View()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(data), "\n"); n != 2 {
		t.Fatalf("want one update and one view line:\n%s", data)
	}
}

// The log turns over at its limit and keeps one older file.
func TestTimingLogTurnsOver(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", TimingLogFile)
	for _, line := range []string{"one\n", "two\n", "three\n"} {
		if err := appendTimingLine(path, []byte(line), 6); err != nil {
			t.Fatal(err)
		}
	}
	for file, want := range map[string]string{path: "three\n", path + ".1": "two\n"} {
		if data, err := os.ReadFile(file); err != nil || string(data) != want {
			t.Fatalf("%s holds %q (%v), want %q", file, data, err, want)
		}
	}
}
