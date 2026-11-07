package grid

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

// TestOperationTraitsKeepClassification: the grid and selection operations
// keep the classification the framework's name tables gave them
// (testdata/op_traits.txt, generated from those tables). UPDATE_OP_TRAITS=1
// rewrites it.
func TestOperationTraitsKeepClassification(t *testing.T) {
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	Register(r)
	var lines []string
	for k, o := range r.Operations {
		if strings.HasPrefix(k, "ochazuke.grid.") || strings.HasPrefix(k, "ochazuke.selection.") {
			lines = append(lines, fmt.Sprintf("%s motion=%v focusBound=%v typing=%v kind=%s repeatable=%v staying=%v", k, o.Motion, o.FocusBound, o.Typing, o.HistoryKind, o.Repeatable, o.Staying))
		}
	}
	sort.Strings(lines)
	got := strings.Join(lines, "\n") + "\n"
	const path = "testdata/op_traits.txt"
	if os.Getenv("UPDATE_OP_TRAITS") != "" {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("grid operation traits changed:\n%s\nwant:\n%s", got, want)
	}
}
