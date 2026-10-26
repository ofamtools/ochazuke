package ochazuke

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

var updatePickerTable = flag.Bool("update-picker-table", false, "rewrite testdata/picker-results.golden")

// The picker's results are a table (pickerColumns): a header under the
// count, size right-aligned, modified dropping first and size next as the
// pane narrows, the path cut with "…", one row marked and reversed.
func TestPickerResultsTableGolden(t *testing.T) {
	at := time.Date(2026, time.October, 3, 14, 5, 0, 0, time.Local)
	p := &Picker{Selected: 1, Entries: []PickerEntry{
		{Path: ".", Dir: true},
		{Path: "cmd", Dir: true, Size: 96, Modified: at},
		{Path: "go.mod", Size: 412, Modified: at.Add(-48 * time.Hour)},
		{Path: "docs/a-rather-long-file-name-for-cutting.md", Size: 18_944, Modified: at},
		{Path: "testdata/big.bin", Size: 3 << 20, Modified: at},
	}}
	var out strings.Builder
	for _, width := range []int{60, 34, 20} {
		fmt.Fprintf(&out, "-- width %d --\n", width)
		for _, row := range p.resultRows(width, 7) {
			out.WriteString(strings.TrimRight(ansi.Strip(row), " ") + "\n")
		}
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

// One highlight: only the selected row is reversed, across the width.
func TestPickerResultsHighlightOnlySelected(t *testing.T) {
	p := &Picker{Selected: 0, Entries: []PickerEntry{{Path: "a", Size: 1}, {Path: "b", Size: 2}}}
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

func TestPickerIndexRecordsSizeAndModified(t *testing.T) {
	pickerGitEnvironment(t)
	root := t.TempDir()
	path := filepath.Join(root, "note.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2025, time.March, 1, 9, 30, 0, 0, time.Local)
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
	entries, err := indexPickerEntries(context.Background(), root, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Size != 5 || !entries[0].Modified.Equal(at) {
		t.Fatalf("entries = %#v, want note.txt with size 5 modified %v", entries, at)
	}
}

func TestPickerSize(t *testing.T) {
	for n, want := range map[int64]string{0: "0", 1023: "1023", 1536: "1.5K", 34 << 10: "34K", 2 << 20: "2.0M", 5 << 30: "5.0G"} {
		if got := pickerSize(n); got != want {
			t.Errorf("pickerSize(%d) = %q, want %q", n, got, want)
		}
	}
}
