package ochazuke

import "testing"

// Selection reads the visual selection as one rune range: charwise from
// the anchor to the cursor inclusive, either direction; linewise whole
// lines; nothing outside visual or in visual-block.
func TestInputSelection(t *testing.T) {
	in := NewInput("q", ".items | select(.id)\nnext", false)
	if _, _, ok := in.Selection(); ok {
		t.Fatal("normal mode has no selection")
	}
	in.SetMode("visual")
	in.Anchor, in.Cursor = 9, 14
	if s, e, ok := in.Selection(); !ok || string(in.Text[s:e]) != "select" {
		t.Fatalf("charwise %d %d %v %q", s, e, ok, string(in.Text[s:e]))
	}
	in.Anchor, in.Cursor = 14, 9
	if s, e, _ := in.Selection(); string(in.Text[s:e]) != "select" {
		t.Fatalf("backwards %q", string(in.Text[s:e]))
	}
	in.SetMode("visual-line")
	in.Anchor, in.Cursor = 14, 9
	if s, e, ok := in.Selection(); !ok || string(in.Text[s:e]) != ".items | select(.id)\n" {
		t.Fatalf("linewise %q", string(in.Text[s:e]))
	}
	in.SetMode("visual-block")
	if _, _, ok := in.Selection(); ok {
		t.Fatal("a block is not one range")
	}
}
