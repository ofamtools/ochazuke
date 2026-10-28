package ochazuke

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// An event card (a task's notification) is indented one level with a
// dashed border while not selected; selected it takes the selected
// border, still indented, and its text wraps at the narrower width.
func TestTimelineEventCardIsIndentedAndDashed(t *testing.T) {
	cards := turns(2)
	cards = append(cards, Card{ID: "e1", Noun: "turn", Header: "⤷ Export format · finished", Meta: "20m43s · 205k tokens", Event: true, Steps: []CardStep{{Header: true}, {ID: "result", Title: "result", Paragraphs: true, Body: TextBody("one\n\ntwo")}}})
	f := newTimelineFixture(t, cards)
	lines := strings.Split(f.frame, "\n")
	var top, row string
	for i, l := range lines {
		if strings.Contains(l, "⤷ Export format") {
			row, top = l, lines[i-1]
		}
	}
	if !strings.HasPrefix(top, "  ╭┄") || !strings.HasPrefix(row, "  ┆ ▸ ⤷ Export format") || ansi.StringWidth(top) != ansi.StringWidth(lines[0]) {
		t.Fatalf("event card not indented and dashed:\n%s", f.frame)
	}
	f.press("j", "j")
	if !strings.Contains(f.frame, "  ┏━") || !strings.Contains(f.frame, "  ┃ ▸ ⤷ Export format") {
		t.Fatalf("selected event card:\n%s", f.frame)
	}
	f.press("enter", "l", "j")
	if text, ok := f.tl.StepText(); !ok || text != "one" {
		t.Fatalf("event card steps: %q %v\n%s", text, ok, f.frame)
	}
	for _, l := range strings.Split(f.frame, "\n") {
		if w := ansi.StringWidth(l); w > 40 {
			t.Fatalf("line %q is %d cells", l, w)
		}
	}
}
