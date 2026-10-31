package main

import (
	"os"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
	story "github.com/unixsuperhero/monogatari"
)

// Check the story's actual terminal cells, including the padding. Golden
// frames trim trailing spaces, so row-count expectations alone cannot prove
// that the cursor reaches both pane edges.
func TestMarkdownItemCursorThreeThenSixFullWidthLines(t *testing.T) {
	data, err := os.ReadFile("stories/markdown/cursor-item-wrapped.json")
	if err != nil {
		t.Fatal(err)
	}
	s, err := story.Parse(data, "markdown/cursor-item-wrapped")
	if err != nil {
		t.Fatal(err)
	}
	ss, err := (story.Library{Apps: hostApps}).Build(s, story.Options{Mono: true})
	if err != nil {
		t.Fatal(err)
	}
	driver := story.NewDriver(ss, story.Options{Mono: true})
	defer driver.Close()
	check := func(want int) {
		frame := driver.S.Frame()
		w, h := s.Size()
		screen := vt.NewEmulator(w, h)
		defer screen.Close()
		if _, err := screen.Write([]byte(strings.ReplaceAll(frame, "\n", "\r\n"))); err != nil {
			t.Fatal(err)
		}
		highlighted := 0
		for y := 0; y < h; y++ {
			hasReverse := false
			for x := 0; x < w; x++ {
				if c := screen.CellAt(x, y); c != nil && c.Style.Attrs&uv.AttrReverse != 0 {
					hasReverse = true
				}
			}
			if !hasReverse {
				continue
			}
			highlighted++
			// Full chrome reserves two cells on each side of the page body.
			for x := 2; x < w-2; x++ {
				c := screen.CellAt(x, y)
				// A wide glyph owns its continuation cell; vt stores the
				// attributes on the leading cell alone.
				if prev := screen.CellAt(x-1, y); prev != nil && prev.Width == 2 && prev.Style.Attrs&uv.AttrReverse != 0 {
					continue
				}
				if c == nil || c.Style.Attrs&uv.AttrReverse == 0 {
					t.Fatalf("item has an unhighlighted cell at (%d,%d):\n%s", x, y, story.Golden(frame))
				}
			}
		}
		if highlighted != want {
			t.Fatalf("highlighted %d lines, want %d:\n%s", highlighted, want, story.Golden(frame))
		}
	}
	check(3)
	if err := driver.Step(story.Event{Key: "j"}); err != nil {
		t.Fatal(err)
	}
	check(6)
}
