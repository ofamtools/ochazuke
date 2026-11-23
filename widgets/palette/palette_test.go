package palette

import (
	"strings"
	"testing"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
	"ofam.dev/ochazuke/widgets/form"
	"ofam.dev/ochazuke/widgets/input"
	"ofam.dev/ochazuke/widgets/picker"
)

func sampleEntries() []Entry {
	return []Entry{
		{
			Spec: core.CommandSpec{
				ID:          "task.create",
				Description: "Create a new task",
				Params: []core.ParamSpec{
					{Name: "title", Type: core.ParamString, Required: true, Description: "Task title"},
					{Name: "priority", Type: core.ParamString, Enum: []string{"low", "med", "high"}, Default: "low", Description: "Priority level"},
					{Name: "urgent", Type: core.ParamBool, Description: "Mark urgent"},
					{Name: "project", Type: core.ParamRef, RefKinds: []string{"project"}, SourceQuery: "projects", Description: "Target project"},
					{Name: "cwd", Type: core.ParamPath, Description: "Working directory"},
				},
			},
			Item: picker.Item{ID: "task.create", Title: "task.create", Subtitle: "Create a new task"},
		},
		{
			Spec: core.CommandSpec{
				ID:          "app.refresh",
				Description: "Refresh current view",
			},
			Item:     picker.Item{ID: "app.refresh", Title: "app.refresh", Subtitle: "Refresh current view"},
			Defaults: map[string]any{"source": "palette"},
		},
		{
			Spec: core.CommandSpec{
				ID:          "log.tail",
				Description: "Tail recent logs",
				Params: []core.ParamSpec{
					{Name: "lines", Type: core.ParamInt, Default: 50, Description: "Number of lines"},
					{Name: "level", Type: core.ParamString, Enum: []string{"info", "warn", "error"}, Default: "info", Description: "Log level"},
				},
			},
			Item:     picker.Item{ID: "log.tail", Title: "log.tail", Subtitle: "Tail recent logs"},
			Defaults: map[string]any{"lines": 25, "scope": "global"},
		},
	}
}

func TestAlwaysVisibleArgsPreviewSplitView(t *testing.T) {
	m := New("Commands", sampleEntries())
	m.Placeholder = "Type a command..."

	// First command has params: ActiveForm must be populated immediately while unfocused.
	if m.ActiveEntry == nil || m.ActiveEntry.Spec.ID != "task.create" {
		t.Fatalf("ActiveEntry = %+v, want task.create", m.ActiveEntry)
	}
	if m.ActiveForm == nil {
		t.Fatal("ActiveForm is nil for parameterized command")
	}
	if m.FocusForm {
		t.Fatal("FocusForm should start false")
	}
	if m.EditingForm() {
		t.Fatal("EditingForm should start false")
	}
	if got := m.FocusID("pal"); got != "pal.picker" {
		t.Fatalf("FocusID = %q, want pal.picker", got)
	}

	v := m.View("pal")
	if v.Kind != ui.KindSplit {
		t.Fatalf("View kind = %q, want %q", v.Kind, ui.KindSplit)
	}
	if v.Ratio != DefaultSplitRatio {
		t.Fatalf("View ratio = %v, want %v", v.Ratio, DefaultSplitRatio)
	}
	if len(v.Children) != 2 {
		t.Fatalf("split children = %d, want 2", len(v.Children))
	}
	if v.Children[0].ID != "pal.picker" || v.Children[0].Kind != ui.KindPicker {
		t.Fatalf("left child = %+v", v.Children[0])
	}
	right := v.Children[1]
	if right.Kind != ui.KindPanel || !right.Border {
		t.Fatalf("right panel = %+v", right)
	}
	if right.Title != "Arguments · task.create" {
		t.Fatalf("right title = %q", right.Title)
	}
	if len(right.Badges) != 1 || right.Badges[0].Text != "tab to edit" {
		t.Fatalf("right badges = %+v, want tab to edit", right.Badges)
	}
	if len(right.Children) != 1 || right.Children[0].ID != "pal.form" || right.Children[0].Kind != ui.KindForm {
		t.Fatalf("right form child = %+v", right.Children)
	}

	if v.Facts["hasArgs"] != true || v.Facts["focusForm"] != false || v.Facts["editingForm"] != false || v.Facts["activeCommand"] != "task.create" {
		t.Fatalf("facts = %+v", v.Facts)
	}

	frame, _ := ui.Render(v, ui.DefaultDarkTheme(), m.FocusID("pal"), 120, 24)
	for _, want := range []string{"task.create", "Arguments", "tab to edit", "title", "priority"} {
		if !strings.Contains(frame.ANSI, want) {
			t.Errorf("rendered frame missing %q", want)
		}
	}
}

func TestZeroParamHidesArgsAndCachesFormValues(t *testing.T) {
	m := New("Commands", sampleEntries())

	// Focus form and type a value on task.create.
	m.HandleCommand(core.Command{ID: CmdFocusForm})
	m.HandleCommand(core.Command{ID: form.CmdEdit})
	m.HandleCommand(core.Command{ID: form.CmdInsert, Args: map[string]any{"text": "draft task"}})
	m.HandleCommand(core.Command{ID: form.CmdStop}) // exit edit mode
	m.HandleCommand(core.Command{ID: form.CmdStop}) // return to picker list

	if m.FocusForm {
		t.Fatal("expected focus back in picker list after second form.stop")
	}

	// Move down to 0-param command app.refresh.
	m.HandleCommand(core.Command{ID: "motion.down"})
	if m.ActiveEntry == nil || m.ActiveEntry.Spec.ID != "app.refresh" {
		t.Fatalf("ActiveEntry = %+v, want app.refresh", m.ActiveEntry)
	}
	if m.ActiveForm != nil {
		t.Fatal("ActiveForm should be nil for 0-param command")
	}
	v := m.View("pal")
	if v.Kind != ui.KindPicker {
		t.Fatalf("0-param View kind = %q, want %q", v.Kind, ui.KindPicker)
	}
	if v.ID != "pal.picker" {
		t.Fatalf("0-param View ID = %q, want pal.picker", v.ID)
	}
	if v.Facts["hasArgs"] != false || v.Facts["focusForm"] != false || v.Facts["activeCommand"] != "app.refresh" {
		t.Fatalf("0-param facts = %+v", v.Facts)
	}
	// Attempting to focus form on 0-param command is a no-op.
	if m.FocusToForm() || m.FocusForm {
		t.Fatal("FocusToForm should fail on 0-param command")
	}

	// Confirm 0-param command submits cleanly with entry defaults.
	res, ok := m.HandleCommand(core.Command{ID: picker.CmdConfirm})
	if !ok || !res.Submitted || res.Entry.Spec.ID != "app.refresh" || res.Args["source"] != "palette" {
		t.Fatalf("0-param confirm = %+v, ok=%v", res, ok)
	}

	// Move back up to task.create: cached form values must be preserved.
	m.HandleCommand(core.Command{ID: "motion.up"})
	if m.ActiveForm == nil {
		t.Fatal("ActiveForm should be restored")
	}
	if got := m.ActiveForm.Values["title"]; got != "draft task" {
		t.Fatalf("cached title = %v, want %q", got, "draft task")
	}
}

func TestSmartConfirmFocusesFormWhenRequiredMissing(t *testing.T) {
	m := New("Commands", sampleEntries())

	// Press enter on task.create with required "title" empty.
	res, ok := m.HandleCommand(core.Command{ID: picker.CmdConfirm})
	if !ok {
		t.Fatal("picker.confirm should be handled")
	}
	if res.Submitted {
		t.Fatal("should not submit when required param is missing")
	}
	if !m.FocusForm {
		t.Fatal("smart confirm must shift FocusForm to true")
	}
	if m.ActiveForm.Errors["title"] == "" {
		t.Fatalf("expected validation error on title, got %+v", m.ActiveForm.Errors)
	}
	if m.ActiveForm.ActiveIdx != 0 {
		t.Fatalf("ActiveIdx = %d, want 0 (first invalid field)", m.ActiveForm.ActiveIdx)
	}
	if got := m.FocusID("pal"); got != "pal.form" {
		t.Fatalf("FocusID = %q, want pal.form", got)
	}
	v := m.View("pal")
	if v.Facts["focusForm"] != true {
		t.Fatalf("facts focusForm = %v, want true", v.Facts["focusForm"])
	}
	right := v.Children[1]
	if len(right.Badges) != 1 || right.Badges[0].Text != "editing args · enter runs" {
		t.Fatalf("focused right badges = %+v", right.Badges)
	}
}

func TestFormEditingReadlineEnumPickAndSubmit(t *testing.T) {
	m := New("Commands", sampleEntries())

	// Switch focus to form via palette.focus.form.
	if _, ok := m.HandleCommand(core.Command{ID: CmdFocusForm}); !ok || !m.FocusForm {
		t.Fatal("palette.focus.form failed")
	}

	// Edit title using Readline commands.
	m.HandleCommand(core.Command{ID: form.CmdEdit})
	if !m.EditingForm() {
		t.Fatal("EditingForm should be true while editing text field")
	}
	if v := m.View("pal"); v.Facts["editingForm"] != true {
		t.Fatalf("facts editingForm = %v, want true", v.Facts["editingForm"])
	}
	m.HandleCommand(core.Command{ID: input.CmdInsert, Args: map[string]any{"text": "bad ship feature"}})
	m.HandleCommand(core.Command{ID: input.CmdLineStart})
	m.HandleCommand(core.Command{ID: input.CmdKillWordForward}) // removes "bad"
	m.HandleCommand(core.Command{ID: input.CmdDeleteChar})      // removes leading space
	m.HandleCommand(core.Command{ID: form.CmdStop})
	if m.EditingForm() {
		t.Fatal("EditingForm should be false after form.stop")
	}
	if !m.FocusForm {
		t.Fatal("FocusForm should remain true after leaving edit mode")
	}

	// Move down to priority (enum) and cycle with h/l (motion.right / motion.left).
	m.HandleCommand(core.Command{ID: "motion.down"})
	m.HandleCommand(core.Command{ID: "motion.right"}) // low -> med
	m.HandleCommand(core.Command{ID: "motion.right"}) // med -> high
	m.HandleCommand(core.Command{ID: "motion.left"})  // high -> med
	if got := m.ActiveForm.Values["priority"]; got != "med" {
		t.Fatalf("priority = %v, want med", got)
	}

	// Move down to urgent (bool) and toggle.
	m.HandleCommand(core.Command{ID: "motion.down"})
	m.HandleCommand(core.Command{ID: form.CmdToggle})
	if got := m.ActiveForm.Values["urgent"]; got != true {
		t.Fatalf("urgent = %v, want true", got)
	}

	// Move down to project (ref) and request ref picker.
	m.HandleCommand(core.Command{ID: "motion.down"})
	res, ok := m.HandleCommand(core.Command{ID: form.CmdPick})
	if !ok || res.NeedPick == nil || res.PickRef == nil {
		t.Fatalf("expected ref pick request, got %+v ok=%v", res, ok)
	}
	if res.NeedPick.Field != "project" || res.NeedPick.Kind != form.KindRef || res.NeedPick.SourceQuery != "projects" {
		t.Fatalf("NeedPick = %+v", res.NeedPick)
	}
	if !m.SetPickedValue("project", core.Ref{Kind: "project", ID: "42"}) {
		t.Fatal("SetPickedValue(project) returned false")
	}

	// Move down to cwd (path) and request path picker.
	m.HandleCommand(core.Command{ID: "motion.down"})
	res, ok = m.HandleCommand(core.Command{ID: form.CmdPick})
	if !ok || res.NeedPick == nil || res.PickPath == nil {
		t.Fatalf("expected path pick request, got %+v ok=%v", res, ok)
	}
	if res.NeedPick.Field != "cwd" || res.NeedPick.Kind != form.KindPath {
		t.Fatalf("NeedPick = %+v", res.NeedPick)
	}
	if !m.SetPickedValue("cwd", "/tmp/work") {
		t.Fatal("SetPickedValue(cwd) returned false")
	}

	// Submit form and verify typed Args.
	res, ok = m.HandleCommand(core.Command{ID: form.CmdSubmit})
	if !ok || !res.Submitted {
		t.Fatalf("submit failed: res=%+v errors=%+v", res, m.ActiveForm.Errors)
	}
	if res.Entry.Spec.ID != "task.create" {
		t.Fatalf("Entry.Spec.ID = %q", res.Entry.Spec.ID)
	}
	if res.Args["title"] != "ship feature" {
		t.Fatalf("title = %q, want %q", res.Args["title"], "ship feature")
	}
	if res.Args["priority"] != "med" {
		t.Fatalf("priority = %v, want med", res.Args["priority"])
	}
	if res.Args["urgent"] != true {
		t.Fatalf("urgent = %v, want true", res.Args["urgent"])
	}
	if res.Args["project"] != (core.Ref{Kind: "project", ID: "42"}) {
		t.Fatalf("project = %#v, want core.Ref{Kind:project, ID:42}", res.Args["project"])
	}
	if res.Args["cwd"] != "/tmp/work" {
		t.Fatalf("cwd = %v, want /tmp/work", res.Args["cwd"])
	}
}

func TestConfirmOptionalArgsWithDefaultsFromPicker(t *testing.T) {
	m := New("Commands", sampleEntries())

	// Filter to log.tail via picker query.
	m.HandleCommand(core.Command{ID: input.CmdInsert, Args: map[string]any{"text": "log.tail"}})
	if m.ActiveEntry == nil || m.ActiveEntry.Spec.ID != "log.tail" {
		t.Fatalf("ActiveEntry = %+v, want log.tail", m.ActiveEntry)
	}
	if m.ActiveForm == nil {
		t.Fatal("ActiveForm should be populated for log.tail")
	}

	// Confirm directly from picker without focusing form.
	res, ok := m.HandleCommand(core.Command{ID: picker.CmdConfirm})
	if !ok || !res.Submitted {
		t.Fatalf("expected immediate submit, got %+v ok=%v", res, ok)
	}
	if res.Entry.Spec.ID != "log.tail" {
		t.Fatalf("Entry.Spec.ID = %q", res.Entry.Spec.ID)
	}
	// Entry.Defaults overrides ParamSpec.Default for lines (25 vs 50); level uses ParamSpec.Default ("info");
	// extra undeclared default "scope" is preserved.
	if res.Args["lines"] != 25 {
		t.Fatalf("lines = %#v, want 25", res.Args["lines"])
	}
	if res.Args["level"] != "info" {
		t.Fatalf("level = %#v, want info", res.Args["level"])
	}
	if res.Args["scope"] != "global" {
		t.Fatalf("scope = %#v, want global", res.Args["scope"])
	}
}

func TestToggleFocusAndCommandSpecs(t *testing.T) {
	m := New("Commands", sampleEntries())
	if _, ok := m.HandleCommand(core.Command{ID: CmdToggleFocus}); !ok || !m.FocusForm {
		t.Fatal("first toggle should focus form")
	}
	if _, ok := m.HandleCommand(core.Command{ID: CmdToggleFocus}); !ok || m.FocusForm {
		t.Fatal("second toggle should return to list")
	}
	if _, ok := m.HandleCommand(core.Command{ID: CmdFocusForm}); !ok || !m.FocusForm {
		t.Fatal("focus.form should focus form")
	}
	if _, ok := m.HandleCommand(core.Command{ID: CmdFocusList}); !ok || m.FocusForm {
		t.Fatal("focus.list should return to list")
	}
	if specs := CommandSpecs(); len(specs) != 3 {
		t.Fatalf("CommandSpecs len = %d, want 3", len(specs))
	}
}
