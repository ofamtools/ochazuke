package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/unixsuperhero/ochazuke/story"
)

// TestExploreWritesGapStories: o-story explore runs one story application
// and writes its report and one skipped gap story per finding, each a
// story that parses and plays.
func TestExploreWritesGapStories(t *testing.T) {
	tool, out := testTool(t)
	dir := t.TempDir()
	if err := tool.Explore([]string{"--depth", "2", "--steps", "60", "--out", dir, "lists/beside-terminal-keeps-row"}); err != nil {
		t.Fatalf("explore: %v\n%s", err, out)
	}
	if !strings.Contains(out.String(), "o-story explore: 1 applications") {
		t.Fatalf("summary missing:\n%s", out)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*", "*.json"))
	if len(files) == 0 {
		t.Fatalf("no gap stories written:\n%s", out)
	}
	lib, err := tool.library("")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		s, err := story.Parse(data, f)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if !strings.HasPrefix(s.Skip, "gap: ") || s.Area != "explore" {
			t.Errorf("%s: skip %q area %q, want a gap story in area explore", f, s.Skip, s.Area)
		}
		if res := lib.Run(s, story.RunOptions{}); res.Err != nil && !res.Skipped {
			t.Errorf("%s does not play: %v", f, res.Err)
		}
	}
}
