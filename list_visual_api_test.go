package ochazuke

import (
	"strings"
	"testing"
)

// A program's request (no source, "cli", "mcp", "hook:x") while V is on
// acts on its own target: the mode stays visual, the selection is kept, no
// "done on N rows" notice.
func TestVisualSurvivesProgramRequests(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"", "cli", "mcp", "hook:tick"} {
		t.Run("source="+source, func(t *testing.T) {
			t.Parallel()
			r, m, l, calls, notices := subjectsTable(t)
			motionKeys(t, r, m, "V j j")
			before := strings.Join(ids(l.Marked()), " ")
			mode := r.Mode.Name
			if !l.Visual() || before != "r1 r2 r3" {
				t.Fatalf("setup: visual %v marks %q", l.Visual(), before)
			}
			func() {
				if source != "" {
					defer r.forceSource(source)()
				}
				if _, err := r.Execute(Request{Verb: "zap", Noun: "row", Target: "r2"}); err != nil {
					t.Fatal(err)
				}
			}()
			if strings.Join(*calls, ", ") != "zap r2" {
				t.Fatalf("ran on %v, want once on r2", *calls)
			}
			if !l.Visual() || r.Mode.Name != mode || strings.Join(ids(l.Marked()), " ") != before {
				t.Fatalf("visual %v mode %s marks %v: the request changed the selection", l.Visual(), r.Mode.Name, ids(l.Marked()))
			}
			for _, n := range notices() {
				if strings.Contains(n.Text, "rows") {
					t.Fatalf("selection-wide notice for a program request: %q", n.Text)
				}
			}
		})
	}
}

// A key, a leader fall-through and the palette in visual mode still act on
// every selected row.
func TestVisualKeyAndPaletteStillApplyToTheSelection(t *testing.T) {
	t.Parallel()
	r, m, l, calls, notices := subjectsTable(t)
	motionKeys(t, r, m, "V j j x")
	if strings.Join(*calls, ", ") != "zap r1, zap r2, zap r3" || l.Visual() {
		t.Fatalf("key x: %v visual %v", *calls, l.Visual())
	}
	if got := notices(); len(got) == 0 || !strings.Contains(got[len(got)-1].Text, "done on 3 rows") {
		t.Fatalf("key notice: %+v", got)
	}
	*calls = nil
	motionKeys(t, r, m, "g g V j :")
	if _, err := r.Dispatch(MacroKey{Key: "[zap]", Paste: true}); err != nil {
		t.Fatal(err)
	}
	motionKeys(t, r, m, "enter")
	if strings.Join(*calls, ", ") != "zap r1, zap r2" {
		t.Fatalf("palette: %v", *calls)
	}
	// A confirm step re-issued with the key's source keeps the selection.
	*calls = nil
	motionKeys(t, r, m, "g g V j c")
	motionKeys(t, r, m, "y")
	if strings.Join(*calls, ", ") != "check r1, check r2" {
		t.Fatalf("confirmed key: %v", *calls)
	}
}

func TestPersonSource(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]bool{"": false, "api": false, "cli": false, "mcp": false, "hook:x": false, "palette": true, "key:V": true, "macro:q": true} {
		if got := personSource(source); got != want {
			t.Errorf("personSource(%q) = %v", source, got)
		}
	}
}
