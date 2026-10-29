package ochazuke

import (
	"errors"
	"strings"
	"testing"
)

// subjectsTable is visualTable with a per-row verb that fails on r3
// (f row.flaky), a many-subject verb that asks a chooser once (t row.tag),
// and a per-row verb that asks a chooser for its row (a row.ask).
func subjectsTable(t *testing.T) (*Runtime, *Model, *List, *[]string, func() []UIMessage) {
	t.Helper()
	r, m, l, calls := visualTable(t)
	r.Define("row").
		Verb("flaky", func(c Ctx) Result {
			if c.Instance.ID == "r3" {
				return Failed(errors.New("r3 refuses"))
			}
			*calls = append(*calls, "flaky "+c.Instance.ID)
			return Done(nil)
		}, "fail on r3").
		Verb("tag", func(c Ctx) Result {
			if color := StringArg(c.Args, "color", ""); color != "" {
				return c.Each(func(one Ctx) Result {
					*calls = append(*calls, "tag "+color+" "+one.Instance.ID)
					return Done(nil)
				})
			}
			subjects, target := c.SubjectIDs(), c.Instance.ID
			_, err := c.Runtime.OpenChooser(ChooserSpec{Kind: "tag", Title: "Tag", Rows: []ChooserRow{{ID: "red", Label: "red"}}, Choose: func(row ChooserRow) Result {
				// The choice hands the subjects on to the verb, given.
				v, err := c.Runtime.Execute(Request{Verb: "tag", Noun: "row", Target: target, Subjects: subjects, Args: Arguments{"color": row.ID}})
				if err != nil {
					return Failed(err)
				}
				return Done(v)
			}})
			if err != nil {
				return Failed(err)
			}
			return Done(nil)
		}, "tag every subject at once").Many().
		Verb("ask", func(c Ctx) Result {
			id := c.Instance.ID
			_, err := c.Runtime.OpenChooser(ChooserSpec{Kind: "ask", Title: "Ask", Rows: []ChooserRow{{ID: "yes", Label: "yes"}}, Choose: func(row ChooserRow) Result {
				*calls = append(*calls, "ask "+id)
				return Done(nil)
			}})
			if err != nil {
				return Failed(err)
			}
			return Done(nil)
		}, "ask about one row").
		Mode("visual-line").Bind("f", Do("row.flaky")).Bind("t", Do("row.tag")).Bind("a", Do("row.ask"))
	op := r.Operations["row.tag"]
	op.Parameters = map[string]Parameter{"color": {Type: "string"}}
	r.Operations["row.tag"] = op
	stop := r.UI().capture()
	return r, m, l, calls, stop
}

// V j j selects three rows; a per-row verb runs once per row, a verb that
// takes many runs once with all three, and visual mode ends either way.
func TestVisualSelectionIsTheSubject(t *testing.T) {
	r, m, l, calls, notices := subjectsTable(t)
	motionKeys(t, r, m, "V j j x")
	if strings.Join(*calls, ", ") != "zap r1, zap r2, zap r3" || l.Visual() {
		t.Fatalf("per-row x: %v visual %v", *calls, l.Visual())
	}
	if got := notices(); len(got) == 0 || !strings.Contains(got[len(got)-1].Text, "done on 3 rows") {
		t.Fatalf("one notice for the rows: %+v", got)
	}
	*calls = nil
	motionKeys(t, r, m, "g g V j j t")
	if r.TopModal() == "" || l.Visual() {
		t.Fatalf("t: one chooser for all the rows, visual ended: modal %q visual %v", r.TopModal(), l.Visual())
	}
	motionKeys(t, r, m, "enter")
	if strings.Join(*calls, ", ") != "tag red r1, tag red r2, tag red r3" || r.TopModal() != "" {
		t.Fatalf("many-subject t: %v modal %q", *calls, r.TopModal())
	}
}

// The selection is the subject from the palette and the API as well as a
// key: a row verb executed without a target while V is on acts on every
// selected row.
func TestVisualSelectionFromTheAPI(t *testing.T) {
	r, m, l, calls, _ := subjectsTable(t)
	motionKeys(t, r, m, "V j j")
	if _, err := r.Execute(Request{Verb: "zap", Noun: "row"}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(*calls, ", ") != "zap r1, zap r2, zap r3" || l.Visual() {
		t.Fatalf("API row.zap: %v visual %v", *calls, l.Visual())
	}
}

// A per-row verb that fails on one row runs on the rest; one error notice
// counts the done and the failed, and the key does not fail.
func TestVisualSelectionPartialFailure(t *testing.T) {
	r, m, l, calls, notices := subjectsTable(t)
	motionKeys(t, r, m, "j V j j f")
	if strings.Join(*calls, ", ") != "flaky r2, flaky r4" || l.Visual() {
		t.Fatalf("f: %v visual %v", *calls, l.Visual())
	}
	got := notices()
	if len(got) == 0 || got[len(got)-1].Kind != "status" || !strings.Contains(got[len(got)-1].Text, "2 done, 1 failed (r3: r3 refuses)") {
		t.Fatalf("partial failure notice: %+v", got)
	}
}

// A per-row verb that asks about its row stops after the first: the rest
// are left as they were and the notice says so, instead of the second
// chooser failing over the first.
func TestVisualSelectionPerRowQuestionStops(t *testing.T) {
	r, m, _, calls, notices := subjectsTable(t)
	motionKeys(t, r, m, "V j j a")
	if r.TopModal() == "" {
		t.Fatal("a: no chooser")
	}
	got := notices()
	if len(got) == 0 || !strings.Contains(got[len(got)-1].Text, "ran on 1, left 2 as they were") {
		t.Fatalf("stop notice: %+v", got)
	}
	motionKeys(t, r, m, "enter")
	if strings.Join(*calls, ", ") != "ask r1" {
		t.Fatalf("a: %v", *calls)
	}
}

// Request.Subjects runs a per-row verb once per subject and a confirm
// names the first and how many more.
func TestRequestSubjects(t *testing.T) {
	r, _, _, calls, _ := subjectsTable(t)
	if _, err := r.Execute(Request{Verb: "zap", Noun: "row", Target: "r5", Subjects: []string{"r5", "r7"}}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(*calls, ", ") != "zap r5, zap r7" {
		t.Fatalf("subjects: %v", *calls)
	}
	v, err := r.Execute(Request{Verb: "check", Noun: "row", Target: "r5", Subjects: []string{"r5", "r6", "r7"}})
	if err != nil {
		t.Fatal(err)
	}
	if p := v.(map[string]any)["prompt"]; p != "Check r5 and 2 more?" {
		t.Fatalf("confirm prompt %q", p)
	}
	*calls = nil
	if _, err := r.Dispatch(MacroKey{Key: "y"}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(*calls, ", ") != "check r5, check r6, check r7" {
		t.Fatalf("confirmed subjects: %v", *calls)
	}
	if _, err := r.Execute(Request{Verb: "zap", Noun: "row", Target: "r5", Subjects: []string{"t"}}); err == nil {
		t.Fatal("a subject of another noun was accepted")
	}
}

// Y in a visual selection copies every selected row, one per line, and
// ends the selection.
func TestVisualYankCopiesEverySelectedRow(t *testing.T) {
	r, m, l, _, _ := subjectsTable(t)
	motionKeys(t, r, m, "V j j Y")
	y, _ := r.ReadYank("")
	if y.Shape != "line" || strings.Count(y.Text, "\n") != 3 || !strings.HasPrefix(y.Text, "r1\t") || !strings.Contains(y.Text, "\nr3\t") || l.Visual() {
		t.Fatalf("V j j Y: %+v visual %v", y, l.Visual())
	}
	motionKeys(t, r, m, "V j y")
	// The cursor stayed on r3 after Y: V j selects r3 and r4; y in
	// visual-line yanks the rows, as Y does.
	if y, _ := r.ReadYank(""); y.Shape != "line" || !strings.HasPrefix(y.Text, "r3\t") || !strings.Contains(y.Text, "\nr4\t") || l.Visual() || r.Mode.Name != "normal" {
		t.Fatalf("V j y: %q visual %v mode %s", y.Text, l.Visual(), r.Mode.Name)
	}
}
