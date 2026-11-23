package form

import (
	"strings"
	"testing"
	"time"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
	"ofam.dev/ochazuke/widgets/input"
)

func specs() []core.ParamSpec {
	return []core.ParamSpec{
		{Name: "name", Type: core.ParamString, Required: true},
		{Name: "count", Type: core.ParamInt, Default: 3},
		{Name: "ratio", Type: core.ParamFloat},
		{Name: "on", Type: core.ParamBool},
		{Name: "level", Type: core.ParamString, Enum: []string{"low", "mid", "high"}, Default: "low"},
		{Name: "dir", Type: core.ParamPath},
		{Name: "task", Type: core.ParamRef, RefKinds: []string{"task"}, SourceQuery: "tasks"},
		{Name: "tags", Type: core.ParamList},
		{Name: "every", Type: core.ParamDuration},
		{Name: "extra", Type: core.ParamObject},
	}
}

func TestKindInference(t *testing.T) {
	want := []FieldKind{KindText, KindNumber, KindNumber, KindBool, KindEnum, KindPath, KindRef, KindList, KindDuration, KindJSON}
	for i, s := range specs() {
		if got := KindFor(s); got != want[i] {
			t.Errorf("%s: %s, want %s", s.Name, got, want[i])
		}
	}
}

func TestDefaultsAndToggleCycle(t *testing.T) {
	m := New("T", specs(), map[string]any{"name": "x"})
	if m.Values["count"] != "3" || m.Values["level"] != "low" || m.Values["name"] != "x" {
		t.Fatalf("values %v", m.Values)
	}
	m.ActiveIdx = 3
	m.ToggleBool()
	if m.Values["on"] != true || !m.Dirty {
		t.Fatal("toggle")
	}
	m.ActiveIdx = 4
	m.CycleEnum(1)
	m.CycleEnum(1)
	m.CycleEnum(1)
	if m.Values["level"] != "low" {
		t.Fatalf("cycle wrap: %v", m.Values["level"])
	}
	m.CycleEnum(-1)
	if m.Values["level"] != "high" {
		t.Fatalf("cycle back: %v", m.Values["level"])
	}
}

func TestPickers(t *testing.T) {
	m := New("T", specs(), nil)
	m.ActiveIdx = 5
	r, ok := m.HandleCommand(core.Command{ID: CmdPick})
	if !ok || r.PickPath == nil || r.PickPath.Field != "dir" {
		t.Fatalf("%+v", r)
	}
	m.ActiveIdx = 6
	r, _ = m.HandleCommand(core.Command{ID: CmdPick})
	if r.PickRef == nil || r.PickRef.SourceQuery != "tasks" || r.PickRef.RefKinds[0] != "task" {
		t.Fatalf("%+v", r)
	}
	m.SetValue("task", core.Ref{Kind: "task", ID: "7"})
	if m.Values["task"] != "task:7" {
		t.Fatal(m.Values["task"])
	}
}

func TestValidation(t *testing.T) {
	m := New("T", specs(), nil)
	m.SetValue("count", "abc")
	m.SetValue("ratio", "x")
	m.SetValue("every", "soon")
	m.SetValue("extra", "{bad")
	if m.Validate() {
		t.Fatal("expected invalid")
	}
	for _, f := range []string{"name", "count", "ratio", "every", "extra"} {
		if m.Errors[f] == "" {
			t.Errorf("no error for %s", f)
		}
	}
	r, _ := m.HandleCommand(core.Command{ID: CmdSubmit})
	if r.Submitted || len(r.ErrorFields) != 5 {
		t.Fatalf("%+v", r)
	}
	m.SetValue("level", "bogus")
	m.Validate()
	if m.Errors["level"] == "" {
		t.Fatal("enum error")
	}
}

func TestSubmitTyped(t *testing.T) {
	m := New("T", specs(), nil)
	m.SetValue("name", "job")
	m.SetValue("count", "42")
	m.SetValue("ratio", "1.5")
	m.SetValue("every", "90s")
	m.SetValue("tags", "a, b ,,c")
	m.SetValue("extra", `{"k":1}`)
	m.SetValue("task", "9")
	m.ActiveIdx = 3
	m.ToggleBool()
	m.ActiveIdx = 0
	m.Editing = true
	m.InsertRune('!')
	m.Backspace()
	m.Backspace()
	m.InsertRune('b')
	vals, ok := m.Submit()
	if !ok {
		t.Fatalf("errors %v", m.Errors)
	}
	if vals["name"] != "job" {
		t.Fatalf("name %v", vals["name"])
	}
	if vals["count"] != 42 || vals["ratio"] != 1.5 || vals["on"] != true || vals["every"] != 90*time.Second {
		t.Fatalf("%v", vals)
	}
	if tags := vals["tags"].([]string); strings.Join(tags, "|") != "a|b|c" {
		t.Fatal(tags)
	}
	if vals["task"] != (core.Ref{Kind: "task", ID: "9"}) {
		t.Fatal(vals["task"])
	}
	if _, ok := vals["dir"]; ok {
		t.Fatal("empty optional should be omitted")
	}
	r, _ := m.HandleCommand(core.Command{ID: CmdSubmit})
	if !r.Submitted || r.Values["count"] != 42 {
		t.Fatalf("%+v", r)
	}
}

func TestNavigationAndRender(t *testing.T) {
	m := New("Edit", specs(), nil)
	m.HandleCommand(core.Command{ID: "motion.bottom"})
	if m.ActiveIdx != 9 {
		t.Fatal(m.ActiveIdx)
	}
	m.HandleCommand(core.Command{ID: "motion.top"})
	m.HandleCommand(core.Command{ID: "motion.up"})
	m.HandleCommand(core.Command{ID: "motion.down"})
	if m.ActiveIdx != 1 {
		t.Fatal(m.ActiveIdx)
	}
	if r, ok := m.HandleCommand(core.Command{ID: CmdStop}); !ok || !r.Cancelled {
		t.Fatal("stop should cancel")
	}
	a := m.View("f")
	b := m.View("f")
	if a.CacheKey != b.CacheKey || a.Kind != ui.KindForm || !a.Focusable {
		t.Fatal("view")
	}
	m.Down()
	if m.View("f").CacheKey == a.CacheKey {
		t.Fatal("cache key should change")
	}
	m.Validate()
	frame, _ := ui.Render(m.View("f"), ui.DefaultDarkTheme(), "f", 60, 14)
	for _, want := range []string{"name", "count"} {
		if !strings.Contains(frame.ANSI, want) {
			t.Errorf("frame lacks %q", want)
		}
	}
}

func TestFormReadlineAndCursor(t *testing.T) {
	m := New("Edit", specs(), map[string]any{"name": "hello world"})
	m.HandleCommand(core.Command{ID: CmdEdit})
	if m.Cursor() != 11 {
		t.Fatalf("cursor on edit = %d, want 11", m.Cursor())
	}
	fEnd, _ := ui.Render(m.View("f"), ui.DefaultDarkTheme(), "f", 60, 14)
	m.HandleCommand(core.Command{ID: input.CmdLineStart})
	if m.Cursor() != 0 {
		t.Fatalf("cursor after line.start = %d", m.Cursor())
	}
	fStart, _ := ui.Render(m.View("f"), ui.DefaultDarkTheme(), "f", 60, 14)
	if fStart.Cursor.X >= fEnd.Cursor.X {
		t.Fatalf("rendered cursor did not move left: start=%+v end=%+v", fStart.Cursor, fEnd.Cursor)
	}
	// Insert at start, kill word forward, yank at end.
	m.HandleCommand(core.Command{ID: CmdInsert, Args: map[string]any{"text": "yo "}})
	if m.Values["name"] != "yo hello world" {
		t.Fatalf("after mid-line insert = %q", m.Values["name"])
	}
	m.HandleCommand(core.Command{ID: input.CmdKillWordForward})
	if m.Values["name"] != "yo  world" {
		t.Fatalf("after kill.word.forward = %q", m.Values["name"])
	}
	m.HandleCommand(core.Command{ID: input.CmdLineEnd})
	m.HandleCommand(core.Command{ID: input.CmdYankKill})
	if m.Values["name"] != "yo  worldhello" {
		t.Fatalf("after yank.kill = %q", m.Values["name"])
	}
}
