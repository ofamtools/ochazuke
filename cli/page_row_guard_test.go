package cli

import (
	"testing"

	"ofam.dev/ochazuke"
)

// A key row guarded by a row guard (row:FIELD, row:!FIELD) answers only on
// the mounted rows whose record matches: on a checklist item row (its
// record carries item) the question's key does not run, on the question
// row it does. The guard is data, so a host judges it without the tool.
func TestMountPageRowGuardScopesKeysToMatchingRows(t *testing.T) {
	doc := pageDoc(LayoutStack, false)
	local := func(n string) string { return "ai-" + n }
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	var ran []string
	mark := func(c ochazuke.Ctx) ochazuke.Result {
		ran = append(ran, c.Instance.ID)
		return ochazuke.Done(nil)
	}
	r.Define("ai-skill").Verb("mark", mark).Verb("origin", mark).Mode("normal").
		Bind("m", ochazuke.Do("ai-skill.mark").Guard("row:!item")).
		Bind("o", ochazuke.Do("ai-skill.origin").Guard("row:origin=claude"))
	m := ochazuke.NewModel(r)
	m.Width, m.Height = 90, 30
	m.Appearance.Theme = "mono"
	v, err := MountPage(m, doc, doc.Pages[0], PageConfig{Noun: local})
	if err != nil {
		t.Fatal(err)
	}
	rows := []Row{
		{ID: "s1", Label: "architect", Data: map[string]any{"origin": "claude"}},
		{ID: "s2", Label: "review", Data: map[string]any{"origin": "ai-tools", "item": "x"}},
	}
	if err := v.SetRows("ai/skills", rows); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("ai"); err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		focus, key string
		runs       bool
	}{
		{"ai/skills/s1", "m", true},
		{"ai/skills/s2", "m", false},
		{"ai/skills/s1", "o", true},
		{"ai/skills/s2", "o", false},
	} {
		ran = nil
		if err := r.SetFocus(step.focus); err != nil {
			t.Fatal(err)
		}
		press(m, step.key)
		if got := len(ran) == 1 && ran[0] == step.focus; got != step.runs {
			t.Errorf("%s on %s: ran %v, want run %v", step.key, step.focus, ran, step.runs)
		}
	}
}
