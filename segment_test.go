package ochazuke

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// segmentFixture is a model over one page, a config directory and a clock
// the test moves.
func segmentFixture(t *testing.T, files map[string]string) (*Runtime, *Model, *time.Time) {
	t.Helper()
	r := NewRuntime()
	dir := t.TempDir()
	r.Config().App, r.Config().Dir, r.Config().NoHistoryFile = "seg", dir, true
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.AddPage("main", "Main", func(*Runtime, int, int) Block { return Text("body") }); err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	if err := r.OpenPage("main"); err != nil {
		t.Fatal(err)
	}
	m.Width, m.Height = 80, 12
	now := time.Unix(1000, 0)
	m.Now = func() time.Time { return now }
	return r, m, &now
}

// settle runs cmd's tree off the test goroutine and delivers every
// segment answer it brings, until no refresh is in flight; timers are
// left to sleep.
func settleSegments(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	got := make(chan tea.Msg, 64)
	var start func(tea.Cmd)
	start = func(c tea.Cmd) {
		if c == nil {
			return
		}
		go func() {
			msg := c()
			if batch, ok := msg.(tea.BatchMsg); ok {
				for _, c := range batch {
					start(c)
				}
				return
			}
			if _, ok := msg.(SegmentMsg); ok {
				got <- msg
			}
		}()
	}
	start(cmd)
	deadline := time.After(5 * time.Second)
	for m.SegmentsInFlight() > 0 {
		select {
		case msg := <-got:
			_, next := m.Update(msg)
			start(next)
		case <-deadline:
			t.Fatalf("%d segment refreshes never answered", m.SegmentsInFlight())
		}
	}
}

// frame is the model's frame without escapes.
func frame(m *Model) []string {
	// Explicit fake-clock snapshots do not wait for the background frame timer.
	m.InvalidateRender()
	return strings.Split(ansi.Strip(m.View().Content), "\n")
}

func fixed(text string) SegmentProvider {
	return func(context.Context, Segment) (SegmentValue, error) { return SegmentValue{Text: text}, nil }
}

// A Go provider's segment draws right-aligned in the rule under the
// header; the body keeps every row.
func TestSegmentFromGoProviderDrawsInHeaderRule(t *testing.T) {
	r, m, _ := segmentFixture(t, nil)
	r.SegmentProvider("load", fixed("0.42"))
	if err := r.AddSegment(Segment{ID: "load", Label: "load", Provider: "load"}); err != nil {
		t.Fatal(err)
	}
	before := frame(m)
	settleSegments(t, m, m.Init())
	rows := frame(m)
	t.Logf("frame:\n%s", strings.Join(rows, "\n"))
	if want := "load 0.42 ──"; !strings.HasSuffix(strings.TrimRight(rows[2], " "), want) || !strings.HasPrefix(strings.TrimSpace(rows[2]), "──") {
		t.Fatalf("header rule %q does not end with %q", rows[2], want)
	}
	if len(rows) != len(before) || rows[3] != before[3] {
		t.Fatalf("segments moved the body:\n%s", strings.Join(rows, "\n"))
	}
	if got := m.StatusSegments(); !reflect.DeepEqual(got, []string{"load 0.42"}) {
		t.Fatalf("StatusSegments = %q", got)
	}
}

// A command row in segments.json shows its first line; one that outruns
// its timeout fails with "timed out" and draws its label and "!", and the
// next frame does not wait for it.
func TestSegmentFromCommandRowWithTimeout(t *testing.T) {
	r, m, _ := segmentFixture(t, map[string]string{SegmentsFile: `[
		// a comment: the file is JSONC
		{"id": "greet", "label": "hi", "command": ["sh", "-c", "printf 'there\nsecond\n'"]},
		{"id": "slow", "label": "slow", "command": "sleep 5", "timeout": "200ms", "every": "1s"},
		{"id": "bad", "label": "bad", "provider": "nope"}
	]`})
	start := time.Now()
	settleSegments(t, m, m.Init())
	if took := time.Since(start); took > 3*time.Second {
		t.Fatalf("the timeout did not cut the slow command: %s", took)
	}
	if got := m.StatusSegments(); !reflect.DeepEqual(got, []string{"hi there", "slow !"}) {
		t.Fatalf("StatusSegments = %q", got)
	}
	var slow SegmentState
	for _, s := range r.Segments() {
		if s.ID == "slow" {
			slow = s
		}
	}
	if !strings.Contains(slow.Error, "timed out") || slow.From != SegmentsFile || slow.Source != "command" {
		t.Fatalf("slow = %+v", slow)
	}
	if p := r.segs().problems; len(p) != 1 || !strings.Contains(p[0], `segment bad: no provider "nope"`) {
		t.Fatalf("problems = %q", p)
	}
	if !slices.ContainsFunc(m.Notices, func(n string) bool { return strings.Contains(n, "segment bad") }) {
		t.Fatalf("the bad row is not a notice: %q", m.Notices)
	}
}

// Each segment's setting hides it and stops its refresh; the area's
// setting hides them all; a user row turns a library row off by default.
func TestSegmentDisabledBySetting(t *testing.T) {
	r, m, _ := segmentFixture(t, nil)
	var mu sync.Mutex
	runs := map[string]int{}
	count := func(id string) SegmentProvider {
		return func(context.Context, Segment) (SegmentValue, error) {
			mu.Lock()
			defer mu.Unlock()
			runs[id]++
			return SegmentValue{Text: id}, nil
		}
	}
	r.SegmentProvider("a", count("a"))
	r.SegmentProvider("b", count("b"))
	r.SegmentProvider("c", count("c"))
	if err := r.AddSegment(Segment{ID: "a", Provider: "a"}, Segment{ID: "b", Provider: "b"}, Segment{ID: "c", Provider: "c", Enabled: On(false)}); err != nil {
		t.Fatal(err)
	}
	settleSegments(t, m, m.Init())
	if got := m.StatusSegments(); !reflect.DeepEqual(got, []string{"a", "b"}) || runs["c"] != 0 {
		t.Fatalf("StatusSegments = %q runs %v: c is off by default", got, runs)
	}
	if _, err := r.Execute(Request{Noun: "segment", Verb: "disable", Args: Arguments{"id": "a"}}); err != nil {
		t.Fatal(err)
	}
	if v, _ := r.Settings().Value(SegmentSetting("a")); v != false {
		t.Fatalf("segment.disable did not set %s: %v", SegmentSetting("a"), v)
	}
	if got := m.StatusSegments(); !reflect.DeepEqual(got, []string{"b"}) {
		t.Fatalf("after disable: %q", got)
	}
	if err := r.Settings().Set(SegmentSetting("c"), true); err != nil {
		t.Fatal(err)
	}
	settleSegments(t, m, m.segmentCommand(m.clock()))
	if got := m.StatusSegments(); !reflect.DeepEqual(got, []string{"b", "c"}) || runs["a"] != 1 {
		t.Fatalf("after enabling c: %q runs %v", got, runs)
	}
	if err := r.Settings().Set(SegmentsSetting, false); err != nil {
		t.Fatal(err)
	}
	if got := m.StatusSegments(); len(got) != 0 {
		t.Fatalf("the area is off but draws %q", got)
	}
	if cmd := m.segmentCommand(m.clock().Add(time.Hour)); cmd != nil || m.SegmentsInFlight() != 0 {
		t.Fatal("the area is off but a refresh started")
	}
	rows := frame(m)
	if strings.TrimSpace(rows[2]) != strings.Repeat("─", 76) {
		t.Fatalf("the area is off but the rule is %q", rows[2])
	}
}

// Rows draw by Order, then as added; status.segment-order (segment.move)
// comes first; a narrow rule drops segments from the last.
func TestSegmentOrdering(t *testing.T) {
	r, m, _ := segmentFixture(t, nil)
	for _, id := range []string{"one", "two", "three"} {
		r.SegmentProvider(id, fixed(id))
	}
	if err := r.AddSegment(Segment{ID: "one", Provider: "one", Order: 2}, Segment{ID: "two", Provider: "two", Order: 1}, Segment{ID: "three", Provider: "three", Order: 2}); err != nil {
		t.Fatal(err)
	}
	settleSegments(t, m, m.Init())
	if got := m.StatusSegments(); !reflect.DeepEqual(got, []string{"two", "one", "three"}) {
		t.Fatalf("by Order: %q", got)
	}
	for _, step := range []struct {
		id, to string
		want   []string
	}{
		{"three", "first", []string{"three", "two", "one"}},
		{"three", "down", []string{"two", "three", "one"}},
		{"one", "1", []string{"one", "two", "three"}},
		{"one", "+5", []string{"two", "three", "one"}},
	} {
		if _, err := r.Execute(Request{Noun: "segment", Verb: "move", Args: Arguments{"id": step.id, "to": step.to}}); err != nil {
			t.Fatal(err)
		}
		if got := m.StatusSegments(); !reflect.DeepEqual(got, step.want) {
			t.Fatalf("move %s %s: %q, want %q", step.id, step.to, got, step.want)
		}
	}
	if got := r.Settings().Strings(SegmentOrderSetting); !reflect.DeepEqual(got, []string{"two", "three", "one"}) {
		t.Fatalf("order setting = %q", got)
	}
	l := Layout{Width: 24, Height: 12, Styles: m.Styles()}
	for width, want := range map[int]string{
		26: "───── two · three · one ──",
		20: "───── two · three ──",
		9:  "── two ──",
		8:  "── t… ──",
		7:  "───────",
	} {
		if rule := ansi.Strip(l.HeaderRule(r.SegmentCells(), width)); rule != want {
			t.Errorf("rule at %d: %q, want %q", width, rule, want)
		}
	}
}

// A refresh never blocks a key: while a provider hangs, keys run and
// frames draw with the last text; the answer lands when it comes.
func TestSegmentRefreshDoesNotBlockInput(t *testing.T) {
	r, m, now := segmentFixture(t, nil)
	release := make(chan struct{})
	var calls atomic.Int32
	r.SegmentProvider("hang", func(ctx context.Context, _ Segment) (SegmentValue, error) {
		if calls.Add(1) == 1 {
			return SegmentValue{Text: "first"}, nil
		}
		select {
		case <-release:
			return SegmentValue{Text: "second"}, nil
		case <-ctx.Done():
			return SegmentValue{}, ctx.Err()
		}
	})
	if err := r.AddSegment(Segment{ID: "hang", Provider: "hang", Every: time.Second, Timeout: time.Second}); err != nil {
		t.Fatal(err)
	}
	settleSegments(t, m, m.Init())
	*now = now.Add(2 * time.Second)
	_, cmd := m.Update(SegmentTickMsg{})
	if m.SegmentsInFlight() != 1 {
		t.Fatal("the due refresh did not start")
	}
	done := make(chan tea.Msg, 1)
	go func() {
		for _, c := range leaves(cmd) {
			go func() {
				if msg, ok := c().(SegmentMsg); ok {
					done <- msg
				}
			}()
		}
	}()
	start := time.Now()
	for range 5 {
		m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
		m.View()
	}
	if took := time.Since(start); took > 500*time.Millisecond {
		t.Fatalf("keys waited for the refresh: %s", took)
	}
	if got := m.StatusSegments(); !reflect.DeepEqual(got, []string{"first"}) {
		t.Fatalf("the last text did not stay while refreshing: %q", got)
	}
	close(release)
	select {
	case msg := <-done:
		m.Update(msg)
	case <-time.After(3 * time.Second):
		t.Fatal("the refresh never answered")
	}
	if got := m.StatusSegments(); !reflect.DeepEqual(got, []string{"second"}) {
		t.Fatalf("after the answer: %q", got)
	}
}

func leaves(cmd tea.Cmd) []tea.Cmd {
	if cmd == nil {
		return nil
	}
	// A batch is a command answering BatchMsg; run it only when it is one
	// (a timer would sleep): the batch's own function returns at once.
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	select {
	case msg := <-ch:
		if batch, ok := msg.(tea.BatchMsg); ok {
			var out []tea.Cmd
			for _, c := range batch {
				out = append(out, leaves(c)...)
			}
			return out
		}
		return []tea.Cmd{func() tea.Msg { return msg }}
	case <-time.After(100 * time.Millisecond):
		return nil
	}
}

// A verb source answers on the loop (an interaction verb) or off it (an
// application verb's pending work), without entering the history; its
// answer may carry a tone.
func TestSegmentFromVerbSource(t *testing.T) {
	r, m, _ := segmentFixture(t, nil)
	r.Define("build").
		Verb("state", func(Ctx) Result {
			return Pending(func(context.Context) (any, error) { return `{"text":"red","tone":"state.errored"}`, nil })
		}, "The build state")
	if err := r.AddSegment(Segment{ID: "build", Label: "ci", Verb: "build.state"}); err != nil {
		t.Fatal(err)
	}
	before := len(r.History(1000))
	settleSegments(t, m, m.Init())
	cells := r.SegmentCells()
	if len(cells) != 1 || cells[0].Text != "ci red" || cells[0].Tone != ToneErrored {
		t.Fatalf("cells = %+v", cells)
	}
	if len(r.History(1000)) != before || len(r.Jobs()) != 0 {
		t.Fatalf("a refresh entered the history or the jobs: %d entries, %d jobs", len(r.History(1000))-before, len(r.Jobs()))
	}
}

// A library row turned on by the application keeps the library's fields;
// a user's segments.json row changes one field and keeps the rest;
// segment.list and segment.show answer the merged row.
func TestSegmentRowsLayer(t *testing.T) {
	r, m, _ := segmentFixture(t, map[string]string{SegmentsFile: `[{"id": "cpu", "label": "CPU", "every": "10s"}]`})
	r.SegmentProvider("cpu", fixed("7%"))
	if err := r.AddSegment(Segment{ID: "cpu", Label: "cpu", Provider: "cpu", Every: 2 * time.Second, Enabled: On(false)}); err != nil {
		t.Fatal(err)
	}
	if err := r.AddSegment(Segment{ID: "cpu", Enabled: On(true)}); err != nil {
		t.Fatal(err)
	}
	settleSegments(t, m, m.Init())
	v, err := r.Execute(Request{Noun: "segment", Verb: "show", Args: Arguments{"id": "cpu"}})
	if err != nil {
		t.Fatal(err)
	}
	s := v.(SegmentState)
	if s.Label != "CPU" || s.Every != 10*time.Second || s.Provider != "cpu" || !s.Shown || s.Text != "7%" || s.From != "app+"+SegmentsFile {
		t.Fatalf("show = %+v", s)
	}
	if got := m.StatusSegments(); !reflect.DeepEqual(got, []string{"CPU 7%"}) {
		t.Fatalf("StatusSegments = %q", got)
	}
	names, _ := r.Execute(Request{Noun: "segment", Verb: "names"})
	if !reflect.DeepEqual(names, []string{"cpu"}) {
		t.Fatalf("names = %v", names)
	}
}
