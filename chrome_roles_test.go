package ochazuke

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestChromeTickerKeepsTextAndGeometry(t *testing.T) {
	t.Parallel()
	jobs := []JobInfo{
		{Verb: "build.run", Label: "failed 界", Status: JobRunning},
		{Verb: "ship.run", Label: "failed", Status: JobFailed, Error: "failed again\nretry"},
	}
	for _, theme := range []string{"dark", "light", "mono"} {
		base := NewStyles(Appearance{Theme: theme})
		for width := range 100 {
			entries := TickerEntries(jobs, width)
			plain := JobsTicker(jobs, width)
			want := (Layout{Styles: base}).TickerRule(plain, width)
			for _, level := range ChromeLevels {
				l := Layout{Styles: base.WithChrome(level, []string{"mode.insert", "mode"})}
				got := l.TickerEntriesRule(entries, width)
				if ansi.Strip(got) != ansi.Strip(want) {
					t.Fatalf("%s %s width %d: %q, want %q", theme, level, width, ansi.Strip(got), ansi.Strip(want))
				}
				if level == ChromeOff && got != want {
					t.Fatalf("%s width %d: off changes ANSI bytes", theme, width)
				}
			}
		}
	}
	s := NewStyles(Appearance{Theme: "dark"}).WithChrome(ChromeSubtle, nil)
	got := (Layout{Styles: s}).TickerEntriesRule(TickerEntries(jobs[1:], 100), 100)
	state := s.Chrome("job.failed", s.Muted).Render("failed")
	if !strings.Contains(got, s.Muted.Render("ship.run failed ")+state+s.Muted.Render(": failed again retry")) {
		t.Fatalf("subtle must colour only the state, not the identical label or error: %q", got)
	}
}

func TestChromeTabsRulesAndErrors(t *testing.T) {
	t.Parallel()
	base := NewStyles(Appearance{Theme: "dark"})
	for _, level := range ChromeLevels {
		s := base.WithChrome(level, []string{"mode.insert", "mode"})
		l := Layout{Width: 34, Height: 8, Styles: s}
		bare := Layout{Width: 34, Height: 8, Styles: base}
		h := Header{Title: "App", Tabs: []string{"Alpha", "Bravo", "Charlie", "Delta"}, Active: 2}
		f := Footer{Text: "Error: cannot open", Error: true}
		got, want := l.Shell(h, Text("body"), f).Text, bare.Shell(h, Text("body"), f).Text
		if ansi.Strip(got) != ansi.Strip(want) {
			t.Fatalf("%s changes the frame's text or geometry", level)
		}
		if level == ChromeOff && got != want {
			t.Fatal("off changes the shell's ANSI bytes")
		}
		if level == ChromeSubtle && s.Chrome("tab.active", s.ActiveTab).GetForeground() != lipgloss.Color("#7CB7E8") {
			t.Fatal("subtle active tab must use the mode hue, not the badge's surface background")
		}
		if level == ChromeRich {
			if !strings.Contains(got, s.Chrome("footer.error", s.Error).Render(f.Text)) {
				t.Fatal("error status does not draw the error role")
			}
			if !strings.Contains(l.tabs(h.Tabs, h.Active, 60), s.Chrome("tab.active", s.ActiveTab).Render("Charlie")) {
				t.Fatal("active tab does not draw its role")
			}
			rule := l.HeaderRule(nil, 20)
			if !strings.Contains(rule, fg("#465D55")) || strings.Contains(rule, "\x1b[2;") {
				t.Fatalf("rule should be border ink without faint: %q", rule)
			}
		}
	}
	accent := lipgloss.Color("#E8833A")
	s := NewStyles(Appearance{Theme: "dark", Accent: accent}).WithChrome(ChromeRich, []string{"mode.insert", "mode"})
	if s.Chrome("tab.active", s.ActiveTab).GetBackground() != accent {
		t.Fatal("app accent must replace mode tint on active tab")
	}
}
