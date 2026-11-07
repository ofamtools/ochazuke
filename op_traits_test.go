package ochazuke

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
)

// opTraitLines lists every framework operation with the traits the
// runtime reads (motion, focus-bound, typing, staying, repeatable, history
// kind) and every unpersisted noun, one sorted line each.
func opTraitLines(r *Runtime, keep func(key string) bool) []string {
	var lines []string
	for k, o := range r.Operations.Entries() {
		if keep(k) {
			lines = append(lines, fmt.Sprintf("%s motion=%v focusBound=%v typing=%v kind=%s repeatable=%v staying=%v", k, o.Motion, o.FocusBound, o.Typing, o.HistoryKind, o.Repeatable, o.Staying))
		}
	}
	for n, noun := range r.Nouns {
		if noun.Unpersisted && keep(n) {
			lines = append(lines, "noun "+n+" unpersisted")
		}
	}
	sort.Strings(lines)
	return lines
}

// TestOperationTraitsKeepClassification proves each built-in operation
// keeps the classification the name tables (motionOps, focusBoundOps,
// typingOps, frameworkHistoryKinds, stayingRequests, repeatableOperations,
// unpersistedNouns) gave it before the traits moved to registration.
// testdata/op_traits.txt was generated from those tables.
// UPDATE_OP_TRAITS=1 rewrites it.
func TestOperationTraitsKeepClassification(t *testing.T) {
	r := NewRuntime()
	r.Config().Disable()
	NewModel(r)
	NewPicker(r)
	got := strings.Join(opTraitLines(r, func(string) bool { return true }), "\n") + "\n"
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
		gl, wl := strings.Split(got, "\n"), strings.Split(string(want), "\n")
		in := func(list []string) map[string]bool {
			m := map[string]bool{}
			for _, l := range list {
				m[l] = true
			}
			return m
		}
		g, w := in(gl), in(wl)
		for _, l := range wl {
			if !g[l] {
				t.Errorf("lost or changed: %s", l)
			}
		}
		for _, l := range gl {
			if !w[l] {
				t.Errorf("new or changed: %s", l)
			}
		}
	}
}
