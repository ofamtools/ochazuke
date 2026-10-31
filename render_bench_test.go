package ochazuke

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// renderFixture counts actual page compositions separately from View calls.
func renderFixture(tb testing.TB) (*Model, *int) {
	tb.Helper()
	r := NewRuntime()
	r.Config().Disable()
	m := NewModel(r)
	calls := new(int)
	l, err := r.AddList("rows", []ListItem{{ID: "one", Noun: "app", Label: "One"}, {ID: "two", Noun: "app", Label: "Two"}}, Parent("app"))
	if err != nil {
		tb.Fatal(err)
	}
	_, err = r.AddPage("page", "Page", func(_ *Runtime, w, h int) Block {
		*calls++
		return Text(l.View(w, h))
	})
	if err != nil {
		tb.Fatal(err)
	}
	r.Instances["rows"].Parent = "page"
	r.PageOf("page").Focus = "rows"
	if err := r.OpenPage("page"); err != nil {
		tb.Fatal(err)
	}
	return m, calls
}

func BenchmarkRenderIdle(b *testing.B) {
	m, _ := renderFixture(b)
	m.View()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		m.Update(SettingsPollMsg{})
		m.View()
	}
}

func BenchmarkRenderBusyTerminal(b *testing.B) {
	m, _ := renderFixture(b)
	term, err := m.Runtime.AddTerminal("pane", newFakeStream(), Parent("page"))
	if err != nil {
		b.Fatal(err)
	}
	m.Runtime.PageOf("page").Render = func(_ *Runtime, w, h int) Block { return term.ViewBlock(w, h) }
	now := time.Unix(100, 0)
	m.Now = func() time.Time { return now }
	m.View()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		now = now.Add(time.Millisecond)
		m.Update(Completion{Ticket: m.Runtime.jobs[m.Runtime.jobSeq].ticket(), Value: TerminalFrame{Lines: []string{fmt.Sprint(i)}}})
		// The harness supplies frames itself, without running Next.
		m.Runtime.commands = nil
		m.View()
	}
}

func BenchmarkRenderFrame(b *testing.B) {
	m, _ := renderFixture(b)
	m.View()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		m.View()
	}
}

func BenchmarkRenderStyles(b *testing.B) {
	m, _ := renderFixture(b)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		m.Styles()
	}
}

func BenchmarkRenderHints(b *testing.B) {
	m, _ := renderFixture(b)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		BindingHints(m.Runtime, m.Runtime.Focus)
	}
}

func BenchmarkRenderViewport(b *testing.B) {
	text := strings.Repeat("\x1b[31mStyled 界 graphemes é\x1b[0m and a long line to wrap.\n", 200)
	for _, cached := range []bool{false, true} {
		b.Run(fmt.Sprint(cached), func(b *testing.B) {
			m, _ := renderFixture(b)
			view := m.viewport("bench", text, 80, 24)
			m.viewCache.text("bench", view)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if cached {
					m.viewCache.text("bench", m.viewport("bench", text, 80, 24))
				} else {
					view.SetContent(text)
					view.View()
				}
			}
		})
	}
}

// This deterministic one-second trace does not wait, start processes or
// read user configuration. Every Update is followed by View, as Tea does.
func TestRenderMessageTrace(t *testing.T) {
	for _, kind := range []string{"settings", "terminal", "job"} {
		t.Run(kind, func(t *testing.T) {
			m, renders := renderFixture(t)
			m.redraw.trace = &renderTrace{messages: map[string]int{}}
			now := time.Unix(100, 0)
			m.Now = func() time.Time { return now }
			var term *Terminal
			if kind == "terminal" {
				term, _ = m.Runtime.AddTerminal("pane", newFakeStream(), Parent("page"))
			}
			m.View()
			start := *renders
			for i := range 1000 {
				now = now.Add(time.Millisecond)
				var msg tea.Msg = SettingsPollMsg{}
				switch kind {
				case "terminal":
					msg = Completion{Ticket: m.Runtime.jobs[m.Runtime.jobSeq].ticket(), Value: TerminalFrame{Lines: []string{fmt.Sprint(i)}}}
				case "job":
					ticket, _, _ := m.Runtime.Issue("app", "", "", Ctx{})
					msg = Completion{Ticket: ticket}
				}
				m.Update(msg)
				m.Runtime.commands = nil
				m.View()
			}
			_ = term
			t.Logf("%s: messages/s=1000 View/s=1000 compositions/s=%d message-types=%v", kind, *renders-start, m.redraw.trace.messages)
			if *renders-start > 30 {
				t.Fatal("background message rate exceeded the render cap")
			}
		})
	}
}
