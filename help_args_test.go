package ochazuke

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// A binding's arguments are one per line, by name, with no JSON: a nested
// object as dotted names, an array of plain values on one line, an array
// of objects by index, a plain string without quotes.
func TestHelpArgLines(t *testing.T) {
	args := Arguments{
		"mode":    "insert",
		"step":    1,
		"request": map[string]any{"verb": "list.top", "args": map[string]any{"wrap": true}},
		"keys":    []string{"g", "g"},
		"steps":   []any{map[string]any{"key": "a"}},
		"empty":   "",
		"none":    nil,
		"obj":     map[string]any{},
		"multi":   "a\nb",
	}
	var got []string
	for _, l := range helpArgLines(args) {
		got = append(got, l.key+"="+l.value)
	}
	want := []string{"empty=\"\"", "keys=g, g", "mode=insert", "multi=a b", "none=null", "obj={}", "request.args.wrap=true", "request.verb=list.top", "step=1", "steps.0.key=a"}
	if !slices.Equal(got, want) {
		t.Fatalf("lines\n%q\nwant\n%q", got, want)
	}
}

// The cell aligns the values in one column and tones each name and value.
func TestHelpArgsCell(t *testing.T) {
	text, tones := helpArgsCell(Arguments{"to": "top", "step": 1}, 2)
	if text != "step 1\nto   top" {
		t.Fatalf("cell %q", text)
	}
	want := []ToneSpan{
		SpanTone(2, 0, 4, ToneHelpArgKey), SpanTone(2, 5, 6, ToneHelpArgValue),
		SpanTone(2, 7, 9, ToneHelpArgKey), SpanTone(2, 12, 15, ToneHelpArgValue),
	}
	if !slices.Equal(tones, want) {
		t.Fatalf("tones %+v", tones)
	}
	if text, tones := helpArgsCell(nil, 2); text != "" || tones != nil {
		t.Fatalf("no arguments: %q %v", text, tones)
	}
	if got := helpArgsLabel(Arguments{"to": "top", "step": 1}); got != "step 1  to top" {
		t.Fatalf("label %q", got)
	}
}

// Every built-in theme draws the argument tones: the name bold, the value
// plain.
func TestHelpArgTonesBuiltIn(t *testing.T) {
	if !defaultTones[ToneHelpArgKey].Bold || defaultTones[ToneHelpArgValue].Bold {
		t.Fatalf("%+v %+v", defaultTones[ToneHelpArgKey], defaultTones[ToneHelpArgValue])
	}
	if _, ok := defaultTones[ToneHelpArgValue]; !ok {
		t.Fatal("help.arg.value is not a built-in tone")
	}
}

// A Lines column breaks its cell at the newlines and cuts each line; a
// newline in any other column draws as a space.
func TestTableLinesColumn(t *testing.T) {
	cols := []Column{{Title: "a", Width: 6, Lines: true}, {Title: "b", Width: 5}}
	lines := tableRowLines(cols, []string{"one\nlonger line", "x\ny"}, []int{0, 1}, []int{6, 5})
	if want := []string{"one     x y  ", "longe…       "}; !slices.Equal(lines, want) {
		t.Fatalf("%q\nwant %q", lines, want)
	}
	if got := strings.TrimRight(tableLine(cols, []string{"p\nq", "r"}, []int{0, 1}, []int{6, 5}), " "); got != "p q     r" {
		t.Fatalf("flat line %q", got)
	}
}

// A span keeps its place across a cell's newlines.
func TestLinesColumnTones(t *testing.T) {
	cols := []Column{{Title: "k", Width: 3}, {Title: "args", Width: 10, Lines: true}}
	text, tones := helpArgsCell(Arguments{"to": "top", "step": 1}, 1)
	_, l := toneFixture(t, "dark", cols, []ListItem{
		{ID: "z", Label: "top", Noun: "row", Cells: []string{"z", ""}},
		{ID: "a", Label: "a", Noun: "row", Cells: []string{"g", text}, Tones: tones},
	})
	lines := strings.Split(l.View(20, 6), "\n")
	if !strings.Contains(lines[2], "\x1b[1mstep\x1b[m") || !strings.Contains(lines[3], "\x1b[1mto\x1b[m") {
		t.Fatalf("names not bold on their lines:\n%q\n%q", lines[2], lines[3])
	}
	if got := ansi.Strip(lines[3]); !strings.Contains(got, "to   top") {
		t.Fatalf("second line %q", got)
	}
}
