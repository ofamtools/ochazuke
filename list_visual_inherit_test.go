package ochazuke

import (
	"slices"
	"strings"
	"testing"
)

// A row noun binding d d in normal (mtm's task delete, pane kill) reads it
// in visual-line too (visual-line extends normal), but the inherited
// sequence does not shadow the list's visual-line d: V j d acts at once,
// as in Vim, and the hints leave out the shadowed d d. sequence-inherited
// keep lets the inherited d d wait, as before.
func TestVisualLineInheritedSequenceDoesNotShadow(t *testing.T) {
	r, m, a, _ := motionPage(t)
	calls := &[]string{}
	r.Define("row").
		Verb("delete", func(c Ctx) Result {
			return c.Each(func(one Ctx) Result {
				*calls = append(*calls, "delete "+one.Instance.ID)
				a.SetItems(slices.DeleteFunc(a.Items(), func(it ListItem) bool { return it.ID == one.Instance.ID }))
				return Done(nil)
			})
		}, "delete rows").Many().
		Mode("normal").Bind("d d", Do("row.delete"))
	r.EnsureConfigSettings()
	if _, own := r.Noun("row").Bindings["visual-line"]; own {
		t.Fatal("the row noun has a visual-line table: d d would not be inherited")
	}
	// Normal mode keeps its own d d.
	motionKeys(t, r, m, "d")
	if len(*calls) != 0 || r.Mode.Payload.Prefix != "d" {
		t.Fatalf("normal d: %v, prefix %q", *calls, r.Mode.Payload.Prefix)
	}
	motionKeys(t, r, m, "d")
	if strings.Join(*calls, ", ") != "delete a1" {
		t.Fatalf("normal d d: %v", *calls)
	}
	*calls = nil
	motionKeys(t, r, m, "V j")
	if hints := BindingHints(r, r.Focus); strings.Contains(hints, "d d row.delete") {
		t.Fatalf("visual-line hints show the shadowed d d: %s", hints)
	}
	motionKeys(t, r, m, "d")
	if strings.Join(*calls, ", ") != "delete a2, delete a3" || r.Mode.Name != "normal" || a.Visual() {
		t.Fatalf("V j d: %v, mode %q visual %v", *calls, r.Mode.Name, a.Visual())
	}

	*calls = nil
	if err := r.Settings().Set(SequenceInheritedSetting, SequenceKeep); err != nil {
		t.Fatal(err)
	}
	motionKeys(t, r, m, "V j d")
	if len(*calls) != 0 || r.Mode.Payload.Prefix != "d" {
		t.Fatalf("keep: V j d ran %v, prefix %q", *calls, r.Mode.Payload.Prefix)
	}
}
