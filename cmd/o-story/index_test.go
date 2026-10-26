package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke/story"
)

var updateIndex = flag.Bool("update-index", false, "rewrite testdata/index.golden")

// The index's areas are tables of storyColumns without area: a header per
// section, steps right-aligned, size then steps dropping as the terminal
// narrows, the gap reason cut with "…", the focused row the one highlight.
func TestIndexIsATablePerArea(t *testing.T) {
	keys := []story.Event{{Key: "j"}, {Key: "k"}, {Key: "enter"}}
	lib := story.Library{Store: story.Store{Dir: t.TempDir()}, Stories: []*story.Story{
		{Name: "j moves down a row", Area: "lists", Width: 40, Height: 6, Script: keys},
		{Name: "a focused empty list stays visible", Area: "lists", Script: keys[:1]},
		{Name: "count spills once at the edge", Area: "motions", Width: 60, Height: 10, Script: keys, Skip: "gap: the count is spent on the first stop instead of the spill"},
	}}
	h, err := newHost(lib)
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	for _, width := range []int{100, 64, 40} {
		h.Update(tea.WindowSizeMsg{Width: width, Height: 16})
		fmt.Fprintf(&out, "-- width %d --\n", width)
		for _, line := range strings.Split(ansi.Strip(h.View().Content), "\n") {
			out.WriteString(strings.TrimRight(line, " ") + "\n")
		}
	}
	got := out.String()
	golden := filepath.Join("testdata", "index.golden")
	if *updateIndex {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("index differs from %s (go test ./cmd/o-story -run TestIndexIsATablePerArea -update-index):\n%s", golden, got)
	}
	if n := strings.Count(h.View().Content, "\x1b[7m"); n != 1 {
		t.Fatalf("%d reversed rows, want the focused one", n)
	}
}
