package ochazuke

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestSurfaceMemoComparesVisibleInputs(t *testing.T) {
	m, _ := renderFixture(t)
	m.redraw.trace = &renderTrace{messages: map[string]int{}}
	now := time.Unix(100, 0)
	m.Now = func() time.Time { return now }
	previous := m.View()
	for range 10 {
		now = now.Add(time.Second)
		m.Update(struct{}{})
		if got := m.View(); !reflect.DeepEqual(previous, got) {
			t.Fatal("unchanged host message changed the frame")
		}
	}
	if m.redraw.trace.compositions != 1 {
		t.Fatalf("compositions=%d, want 1", m.redraw.trace.compositions)
	}
	for _, change := range []func(){
		func() { m.Title = "Changed" },
		func() { m.Width++ },
		func() { m.Height++ },
		func() { m.Info = "message" },
		func() { m.Appearance.Theme = "mono" },
		func() {
			m.Runtime.PageOf("page").Render = func(*Runtime, int, int) Block { return Text("界 host changed content") }
		},
	} {
		before := m.redraw.trace.compositions
		change()
		got := m.View()
		if m.redraw.trace.compositions != before+1 {
			t.Fatal("changed visible input was reused")
		}
		if err := m.Runtime.Settings().Set(RenderMaxFPSSetting, 0); err != nil {
			t.Fatal(err)
		}
		uncached := m.View()
		if !reflect.DeepEqual(got, uncached) {
			t.Fatal("cached/uncached frame differs")
		}
		if err := m.Runtime.Settings().Set(RenderMaxFPSSetting, 30); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSurfaceMemoSnapshotsSlicesAndCursor(t *testing.T) {
	m, _ := renderFixture(t)
	l := m.layout()
	h := Header{Title: "Title", Focus: []string{"one"}, Tabs: []string{"Page"}, Segments: []SegmentCell{{Text: "one"}}, Keys: &KeyEchoCell{Labels: []string{"j"}, Width: 8}}
	f := Footer{Hints: []HintCell{{Key: "j", Text: "down"}}, Jobs: []string{"job done"}}
	b := Block{Text: "body", Cursor: &Pos{1, 1}}
	first := m.shellBlock(l, h, b, f)
	// Reusing and mutating producer buffers must be observable.
	h.Focus[0], h.Tabs[0], h.Segments[0].Text, h.Keys.Labels[0] = "two", "Other", "two", "k"
	f.Hints[0].Text, f.Jobs[0] = "up", "job failed"
	b.Cursor.X++
	got := m.shellBlock(l, h, b, f)
	want := l.Shell(h, b, f)
	if reflect.DeepEqual(first, got) || !reflect.DeepEqual(got, want) {
		t.Fatal("borrowed inputs defeated invalidation")
	}
	got.Cursor.X++
	if again := m.shellBlock(l, h, b, f); !reflect.DeepEqual(again, want) {
		t.Fatal("returned cursor mutated cache")
	}
}

func TestSurfaceMemoModalReuseAndInvalidation(t *testing.T) {
	m, _ := renderFixture(t)
	l := m.layout()
	m.redraw.trace = &renderTrace{messages: map[string]int{}}
	body := Block{Text: "modal 界", Cursor: &Pos{1, 0}}
	call := func() Block {
		return m.modalBlock("modal", l, "background", "title", body, "hint", "", nil, 30, nil, nil)
	}
	first := call()
	got := call()
	if !reflect.DeepEqual(first, got) || m.redraw.trace.modalCompositions != 1 {
		t.Fatal("unchanged modal recomposed")
	}
	body.Text = "different"
	got = call()
	want := composeModalAt("background", "title", body, "hint", nil, m.Width, m.Height, l.Styles, nil)
	if !reflect.DeepEqual(got, want) || m.redraw.trace.modalCompositions != 2 {
		t.Fatal("modal body change missed")
	}
}

func TestHiddenTerminalFramesDoNotCompose(t *testing.T) {
	m, calls := renderFixture(t)
	term, err := m.Runtime.AddTerminal("hidden", newFakeStream(), Parent("page"))
	if err != nil {
		t.Fatal(err)
	}
	m.View()
	before := *calls
	for i := range 100 {
		j := m.Runtime.jobs[m.Runtime.jobSeq]
		m.Update(Completion{Ticket: j.ticket(), Value: TerminalFrame{Lines: []string{fmt.Sprint(i)}}})
		m.Runtime.commands = nil
		m.View()
	}
	if *calls != before || term.frame.Lines[0] != "99" {
		t.Fatal("hidden output either rendered or lost delivery")
	}
	m.Runtime.PageOf("page").Render = func(_ *Runtime, w, h int) Block { return term.ViewBlock(w, h) }
	if frame := m.View().Content; !strings.Contains(frame, "99") {
		t.Fatal("latest hidden frame not shown on reveal")
	}
	// The page's old placements cannot make a now inactive terminal visible.
	_, err = m.Runtime.AddPage("other", "Other", func(*Runtime, int, int) Block { return Text("other") })
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Runtime.OpenPage("other"); err != nil {
		t.Fatal(err)
	}
	m.View()
	if m.Runtime.rendered(term.ID()) {
		t.Fatal("inactive page's placement counted as visible")
	}
}

func TestFittedCellsEquivalent(t *testing.T) {
	for _, line := range []string{"", "ASCII", "界é👩‍💻", "\x1b[31mred 界", "\x1b[31mred\x1b[0m", strings.Repeat("界x", 600), "\x1b]8;;https://example.com\x1b\\link\x1b]8;;\x1b\\"} {
		for width := 0; width < 25; width++ {
			want := ""
			if width > 0 {
				want = ansi.Truncate(line, width, "")
				if strings.ContainsRune(want, '\x1b') {
					want += "\x1b[0m"
				}
				want += strings.Repeat(" ", max(0, width-ansi.StringWidth(want)))
			}
			for range 2 {
				if got := modalCells(line, width); got != want {
					t.Fatalf("width %d: %q != %q", width, got, want)
				}
			}
		}
	}
}

func BenchmarkFittedCells(b *testing.B) {
	line := "\x1b[31m" + strings.Repeat("界 é ", 50) + "\x1b[0m"
	modalCells(line, 180)
	b.ReportAllocs()
	for b.Loop() {
		modalCells(line, 180)
	}
}
