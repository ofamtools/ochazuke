package ochazuke

import (
	"testing"

	"github.com/unixsuperhero/ochazuke/internal/keytest"
)

// Tests for interactions between features that landed on separate
// branches (the v0.2.0 integration).

// F9 (? help hides a row whose guard is false) and the detail's guarded
// J/K rows: help lists J/K exactly while the detail overflows.
func TestHelpListsDetailJKOnlyWhileItOverflows(t *testing.T) {
	m, _, _ := detailModel(t, 100)
	if got := helpKeys(m.Runtime); got["J"] == "" || got["K"] == "" {
		t.Fatalf("overflowing detail: help keys %v; want J and K", got)
	}
	m, _, _ = detailModel(t, 3)
	if got := helpKeys(m.Runtime); got["J"] != "" || got["K"] != "" {
		t.Fatalf("detail that fits: help keys %v; want no J or K", got)
	}
}

// ' (page.pick, feat/many-pages) is an app row, so without a guard it
// answered inside every modal: the settings value prompt (feat/user-config), a
// record editor and its chooser (feat/record-editor), ? help. The picker
// opened, and choosing a page then failed ("close the active modal before
// opening a page"). The row answers only with no modal open.
func TestPagePickKeyDoesNotAnswerInsideAModal(t *testing.T) {
	r, m := pagePickerTest(t, 3)
	r.Config().Dir = t.TempDir()
	if err := r.DeclareSetting(Setting{Name: "editor", Type: SettingString, Default: "vi", Description: "Editor"}); err != nil {
		t.Fatal(err)
	}
	r.Define("app").Mode("normal").Bind("ctrl+o", Do("app.settings.edit"))
	m.Width, m.Height = 100, 24
	m.Init()
	open := map[string]func(){
		"settings prompt": func() { keytest.Press(m, "ctrl+o"); keytest.Press(m, "enter") },
		"help":            func() { keytest.Press(m, "?") },
		"record editor": func() {
			if _, err := r.OpenRecordEditor("rec", RecordSpec{Title: "R", Fields: []RecordField{{Name: "s", Parameter: Parameter{Type: "string", Enum: []string{"a", "b"}}}}, Save: func(RecordWrite) Result { return Done(nil) }}); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, openModal := range open {
		openModal()
		top := r.TopModal()
		if top == "" {
			t.Fatalf("%s: no modal opened", name)
		}
		keytest.Press(m, "'")
		if r.TopModal() != top || r.Chooser(pagePickerID) != nil {
			t.Fatalf("%s: ' opened %q over %q", name, r.TopModal(), top)
		}
		for r.TopModal() != "" {
			if err := r.Pop(); err != nil {
				t.Fatal(err)
			}
		}
	}
	keytest.Press(m, "'")
	if r.Chooser(pagePickerID) == nil {
		t.Fatal("' with no modal open should open the page picker")
	}
}
