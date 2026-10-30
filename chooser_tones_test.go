package ochazuke

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// chooserLine draws the chooser in the dark theme and returns the line
// holding text.
func chooserLine(t *testing.T, rows []ChooserRow, text string) string {
	t.Helper()
	r := NewRuntime()
	r.Config().Disable()
	m := NewModel(r)
	m.Init()
	m.Appearance.Theme = "dark"
	c, err := r.OpenChooser(ChooserSpec{Kind: "tones", Rows: rows, Choose: func(ChooserRow) Result { return Done(nil) }})
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(c.View(40, 8).Text, "\n") {
		if strings.Contains(ansi.Strip(line), text) {
			return line
		}
	}
	t.Fatalf("no line holds %q", text)
	return ""
}

// A chooser row's Tones colour their runs through the theme: on the
// Label, or on a cell where alignCells puts it, never changing the text
// (an app dims a task's path prefix, "parent › child").
func TestChooserRowTones(t *testing.T) {
	errored := "\x1b[1;" + fg("#E99B8D") + "m"
	line := chooserLine(t, []ChooserRow{
		{ID: "top", Label: "top"},
		{ID: "a", Label: "parent › child", Tones: []ToneSpan{SpanTone(0, 0, 9, ToneErrored)}},
	}, "child")
	if got := ansi.Strip(line); strings.TrimRight(got, " ") != "  parent › child" {
		t.Fatalf("text %q changed", got)
	}
	if !strings.Contains(line, errored+"parent › \x1b[m") || strings.Contains(line, errored+"parent › child") {
		t.Fatalf("label tone: %q", line)
	}
	line = chooserLine(t, []ChooserRow{
		{ID: "top", Cells: []string{"longer name", "1"}},
		{ID: "a", Label: "a", Cells: []string{"a", "busy"}, Tones: []ToneSpan{CellTone(1, ToneErrored)}},
	}, "busy")
	if got := strings.TrimRight(ansi.Strip(line), " "); got != "  a            busy" {
		t.Fatalf("text %q", got)
	}
	if !strings.Contains(line, errored+"busy\x1b[m") {
		t.Fatalf("cell tone: %q", line)
	}
	// The highlighted row keeps its reverse bar under the tone.
	line = chooserLine(t, []ChooserRow{{ID: "a", Label: "parent › child", Tones: []ToneSpan{SpanTone(0, 0, 9, ToneErrored)}}}, "child")
	if !strings.Contains(line, "\x1b[7") || len(ansi.Strip(line)) < 40 {
		t.Fatalf("highlighted toned row: %q", line)
	}
	// Without a tone the row draws as before: no SGR on a plain row.
	if line := chooserLine(t, []ChooserRow{{ID: "top", Label: "top"}, {ID: "b", Label: "plain"}}, "plain"); line != ansi.Strip(line) {
		t.Fatalf("plain row styled: %q", line)
	}
}
