package ochazuke

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

var updatePickerTable = flag.Bool("update-picker-table", false, "rewrite testdata/picker-results.golden")

// pickerTableFixture is a picker over /r with five results; stat fills
// the cache as if the stat job had landed.
func pickerTableFixture(stat bool) *Picker {
	at := time.Date(2026, time.October, 3, 14, 5, 0, 0, time.Local)
	p := &Picker{Directory: "/r", Selected: 1, Entries: []PickerEntry{
		{Path: ".", Dir: true},
		{Path: "cmd", Dir: true},
		{Path: "go.mod"},
		{Path: "docs/a-rather-long-file-name-for-cutting.md"},
		{Path: "testdata/big.bin"},
	}}
	if stat {
		p.stats = newPickerStats()
		for path, s := range map[string]pickerStat{
			"cmd":    {size: 96, modified: at, ok: true},
			"go.mod": {size: 412, modified: at.Add(-48 * time.Hour), ok: true},
			"docs/a-rather-long-file-name-for-cutting.md": {size: 18_944, modified: at, ok: true},
			"testdata/big.bin": {size: 3 << 20, modified: at, ok: true},
		} {
			p.stats.put(p.statKey(PickerEntry{Path: path}), s)
		}
	}
	return p
}

func pickerTableText(p *Picker, width int) string {
	var out strings.Builder
	for _, row := range p.resultRows(width, 7) {
		out.WriteString(strings.TrimRight(ansi.Strip(row), " ") + "\n")
	}
	return out.String()
}

// The picker's results are a table (pickerColumns): a header under the
// count, size right-aligned, modified dropping first and size next as the
// pane narrows, the path cut with "…", one row marked and reversed.
func TestPickerResultsTableGolden(t *testing.T) {
	var out strings.Builder
	for _, width := range []int{60, 34, 20} {
		fmt.Fprintf(&out, "-- width %d --\n", width)
		out.WriteString(pickerTableText(pickerTableFixture(true), width))
	}
	got := []byte(out.String())
	golden := filepath.Join("testdata", "picker-results.golden")
	if *updatePickerTable {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("picker results differ from %s (go test -run TestPickerResultsTableGolden -update-picker-table):\n%s", golden, got)
	}
}

// Before the stats land the size and modified cells are blank and the
// layout is the same: the header and every path sit where they will.
func TestPickerResultsBlankUntilStatted(t *testing.T) {
	blank := strings.Split(pickerTableText(pickerTableFixture(false), 60), "\n")
	full := strings.Split(pickerTableText(pickerTableFixture(true), 60), "\n")
	if blank[1] != full[1] {
		t.Fatalf("header moved: %q vs %q", blank[1], full[1])
	}
	for i := 2; i < len(blank)-1; i++ {
		if !strings.HasPrefix(full[i], blank[i]) || len(blank[i]) > 2+36 {
			t.Fatalf("row %d before stats %q, after %q", i, blank[i], full[i])
		}
	}
}

// One highlight: only the selected row is reversed, across the width.
func TestPickerResultsHighlightOnlySelected(t *testing.T) {
	p := &Picker{Selected: 0, Entries: []PickerEntry{{Path: "a"}, {Path: "b"}}}
	rows := p.resultRows(40, 4)
	reversed := 0
	for _, row := range rows {
		if strings.Contains(row, "\x1b[7m") {
			reversed++
			if ansi.StringWidth(row) != 40 {
				t.Fatalf("highlight is %d cells, want 40: %q", ansi.StringWidth(row), row)
			}
		}
	}
	if reversed != 1 {
		t.Fatalf("%d highlighted rows, want 1: %q", reversed, rows)
	}
}

// statCounter is an injected stat function that records its paths.
type statCounter struct {
	mu    sync.Mutex
	paths []string
}

func (c *statCounter) stat(path string) (fs.FileInfo, error) {
	c.mu.Lock()
	c.paths = append(c.paths, path)
	c.mu.Unlock()
	return os.Lstat(path)
}

func (c *statCounter) take() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.paths
	c.paths = nil
	slices.Sort(out)
	return out
}

// Indexing and searching stat nothing; a frame stats exactly the rows it
// draws, once each, and moving the window stats only the rows it adds.
func TestPickerStatsOnlyDrawnRows(t *testing.T) {
	root := t.TempDir()
	for i := range 30 {
		pickerFile(t, root, fmt.Sprintf("f%02d", i), strings.Repeat("x", i))
	}
	r, p, _ := pickerFixture(t, "", "path", "", 0)
	counter := &statCounter{}
	p.statFunc = counter.stat
	pickerDo(t, r, "open", "picker", Arguments{"path": root + "/"})
	pickerDrain(t, p)
	if len(p.Entries) != 31 {
		t.Fatalf("%d results, want 31", len(p.Entries))
	}
	if got := counter.take(); len(got) != 0 {
		t.Fatalf("indexing statted %d paths: %q", len(got), got)
	}
	drawn := func() []string {
		start, end := p.window(p.drawnRows)
		var keys []string
		for _, e := range p.Entries[start:end] {
			keys = append(keys, p.statKey(e))
		}
		slices.Sort(keys)
		return keys
	}
	p.View(40, 12)
	pickerDrain(t, p)
	first := drawn()
	if got := counter.take(); len(first) == 0 || !slices.Equal(got, first) {
		t.Fatalf("statted %q, want the drawn rows %q", got, first)
	}
	if !regexp.MustCompile(`f03 +3\b`).MatchString(ansi.Strip(p.View(40, 12))) {
		t.Fatalf("stats did not land:\n%s", ansi.Strip(p.View(40, 12)))
	}
	p.View(40, 12)
	pickerDrain(t, p)
	if got := counter.take(); len(got) != 0 {
		t.Fatalf("a redraw statted %q again", got)
	}
	pickerDo(t, r, "move", "picker", Arguments{"delta": len(first) + 2})
	pickerDrain(t, p)
	var added []string
	for _, key := range drawn() {
		if !slices.Contains(first, key) {
			added = append(added, key)
		}
	}
	if got := counter.take(); len(added) == 0 || !slices.Equal(got, added) {
		t.Fatalf("after the move statted %q, want the new rows %q", got, added)
	}
}

func TestPickerStatsLRUIsBounded(t *testing.T) {
	s := newPickerStats()
	for i := range pickerStatLimit + 10 {
		s.put(fmt.Sprint(i), pickerStat{ok: true})
	}
	if s.len() != pickerStatLimit {
		t.Fatalf("%d cached, want %d", s.len(), pickerStatLimit)
	}
	if _, ok := s.get("0"); ok {
		t.Fatal("the oldest entry was kept")
	}
	if _, ok := s.get(fmt.Sprint(pickerStatLimit + 9)); !ok {
		t.Fatal("the newest entry was dropped")
	}
}

func TestPickerSize(t *testing.T) {
	for n, want := range map[int64]string{0: "0", 1023: "1023", 1536: "1.5K", 34 << 10: "34K", 2 << 20: "2.0M", 5 << 30: "5.0G"} {
		if got := pickerSize(n); got != want {
			t.Errorf("pickerSize(%d) = %q, want %q", n, got, want)
		}
	}
}
