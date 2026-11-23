package form

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
)

func TestDraftStoreRingAndJSON(t *testing.T) {
	ds := NewDraftStore()
	if ds.MaxPerKey != DefaultMaxDraftsPerKey {
		t.Fatalf("MaxPerKey = %d, want %d", ds.MaxPerKey, DefaultMaxDraftsPerKey)
	}
	ds.MaxPerKey = 3

	t0 := time.Date(2026, 11, 23, 1, 0, 0, 0, time.UTC)
	if ds.Save("", map[string]any{"name": "a"}, "lbl", t0) {
		t.Fatal("empty key should not save")
	}
	if ds.Save("k", map[string]any{}, "lbl", t0) {
		t.Fatal("empty values should not save")
	}

	vals := map[string]any{"name": "first", "tags": []string{"a", "b"}}
	if !ds.Save("k", vals, "v1", t0) {
		t.Fatal("save v1 failed")
	}
	// Mutating caller map/slice must not mutate stored draft.
	vals["name"] = "mutated"
	vals["tags"].([]string)[0] = "z"

	got := ds.List("k")
	if len(got) != 1 || got[0].Values["name"] != "first" || got[0].Values["tags"].([]string)[0] != "a" {
		t.Fatalf("caller mutation leaked: %+v", got)
	}

	// Dedup against newest draft updates SavedAt and Label in place.
	t1 := t0.Add(time.Minute)
	if !ds.Save("k", map[string]any{"name": "first", "tags": []string{"a", "b"}}, "v1-updated", t1) {
		t.Fatal("dedup save failed")
	}
	got = ds.List("k")
	if len(got) != 1 || got[0].Label != "v1-updated" || !got[0].SavedAt.Equal(t1) {
		t.Fatalf("dedup failed: %+v", got)
	}

	// Prepend newest and enforce MaxPerKey = 3.
	ds.Save("k", map[string]any{"name": "second"}, "v2", t0.Add(2*time.Minute))
	ds.Save("k", map[string]any{"name": "third"}, "v3", t0.Add(3*time.Minute))
	ds.Save("k", map[string]any{"name": "fourth"}, "v4", t0.Add(4*time.Minute))
	got = ds.List("k")
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0].Values["name"] != "fourth" || got[1].Values["name"] != "third" || got[2].Values["name"] != "second" {
		t.Fatalf("order/trim wrong: %+v", got)
	}

	// Clone isolation.
	cp := ds.Clone()
	cp.Remove("k", 0)
	if len(ds.List("k")) != 3 || len(cp.List("k")) != 2 {
		t.Fatalf("clone not isolated: orig=%d cp=%d", len(ds.List("k")), len(cp.List("k")))
	}

	// JSON round-trip.
	raw, err := json.Marshal(ds)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var rt DraftStore
	if err := json.Unmarshal(raw, &rt); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rt.MaxPerKey != 3 || len(rt.List("k")) != 3 || rt.List("k")[0].Label != "v4" {
		t.Fatalf("round-trip mismatch: %+v", rt)
	}

	// Remove edge cases and cleanup.
	if ds.Remove("k", -1) || ds.Remove("k", 3) || ds.Remove("missing", 0) {
		t.Fatal("invalid Remove returned true")
	}
	if !ds.Remove("k", 1) {
		t.Fatal("Remove(1) failed")
	}
	got = ds.List("k")
	if len(got) != 2 || got[0].Values["name"] != "fourth" || got[1].Values["name"] != "second" {
		t.Fatalf("after Remove(1): %+v", got)
	}
	ds.Remove("k", 0)
	ds.Remove("k", 0)
	if len(ds.List("k")) != 0 {
		t.Fatalf("expected empty after removing all: %+v", ds.List("k"))
	}
}

func TestFormDraftCyclingAndDiscard(t *testing.T) {
	m := New("New Task", []core.ParamSpec{
		{Name: "title", Type: core.ParamString, Required: true},
		{Name: "count", Type: core.ParamInt, Default: 1},
		{Name: "on", Type: core.ParamBool},
	}, map[string]any{"title": "live title"})

	if m.ActiveDraftIndex() != -1 || m.DraftCount() != 0 {
		t.Fatalf("initial draft state: idx=%d count=%d", m.ActiveDraftIndex(), m.DraftCount())
	}
	if m.DraftPrev() || m.DraftNext() {
		t.Fatal("cycling with no drafts should return false")
	}

	now := time.Now().UTC()
	m.SetDrafts([]Draft{
		{Values: map[string]any{"title": "draft-newest", "count": "10", "on": true}, SavedAt: now, Label: "newest"},
		{Values: map[string]any{"title": "draft-older", "count": "2"}, SavedAt: now.Add(-time.Hour), Label: "older"},
	})
	if m.ActiveDraftIndex() != -1 || m.DraftCount() != 2 {
		t.Fatalf("after SetDrafts: idx=%d count=%d", m.ActiveDraftIndex(), m.DraftCount())
	}

	// Edit live value (dirty=false initially, then make a live edit).
	liveClean := m.Dirty
	if liveClean {
		t.Fatal("expected clean initially")
	}
	m.SetValue("count", "7")
	if !m.Dirty {
		t.Fatal("expected dirty after SetValue")
	}

	// Cycle -1 -> 0 -> 1 -> (blocked) -> 0 -> -1 via commands.
	m.HandleCommand(core.Command{ID: CmdDraftPrev})
	if m.ActiveDraftIndex() != 0 || m.Values["title"] != "draft-newest" || m.Values["count"] != "10" || m.Values["on"] != true {
		t.Fatalf("step 0: idx=%d vals=%v", m.ActiveDraftIndex(), m.Values)
	}
	v0 := m.View("f")
	if v0.Facts["draftCount"] != 2 || v0.Facts["draftIndex"] != 0 || v0.Facts["dirty"] != true {
		t.Fatalf("facts at draft 0: %+v", v0.Facts)
	}

	m.HandleCommand(core.Command{ID: CmdDraftPrev})
	if m.ActiveDraftIndex() != 1 || m.Values["title"] != "draft-older" || m.Values["count"] != "2" || m.Values["on"] != false {
		t.Fatalf("step 1: idx=%d vals=%v", m.ActiveDraftIndex(), m.Values)
	}
	if m.View("f").CacheKey == v0.CacheKey {
		t.Fatal("CacheKey should change when DraftIdx changes")
	}
	if m.DraftPrev() {
		t.Fatal("DraftPrev past oldest should return false")
	}

	// Step forward 1 -> 0 -> -1 restores live working edits.
	m.HandleCommand(core.Command{ID: CmdDraftNext})
	if m.ActiveDraftIndex() != 0 || m.Values["title"] != "draft-newest" {
		t.Fatalf("back to 0: idx=%d vals=%v", m.ActiveDraftIndex(), m.Values)
	}
	m.HandleCommand(core.Command{ID: CmdDraftNext})
	if m.ActiveDraftIndex() != -1 || m.Values["title"] != "live title" || m.Values["count"] != "7" || !m.Dirty {
		t.Fatalf("restored live: idx=%d dirty=%v vals=%v", m.ActiveDraftIndex(), m.Dirty, m.Values)
	}
	if m.DraftNext() {
		t.Fatal("DraftNext from live should return false")
	}

	// Editing a field while viewing a draft preserves edited values AND DraftIdx.
	m.DraftPrev() // idx = 0
	m.SetValue("title", "edited-draft-0")
	if m.ActiveDraftIndex() != 0 || m.Values["title"] != "edited-draft-0" {
		t.Fatalf("editing draft should keep DraftIdx=0: idx=%d vals=%v", m.ActiveDraftIndex(), m.Values)
	}

	// Discard draft 0 -> signals host removal of idx 0 and loads remaining draft.
	res, ok := m.HandleCommand(core.Command{ID: CmdDraftDiscard})
	if !ok || !res.HasDiscardedDraft || res.DiscardedDraftIdx != 0 {
		t.Fatalf("discard 0 result: %+v ok=%v", res, ok)
	}
	if m.DraftCount() != 1 || m.ActiveDraftIndex() != 0 || m.Values["title"] != "draft-older" {
		t.Fatalf("after discard 0: count=%d idx=%d vals=%v", m.DraftCount(), m.ActiveDraftIndex(), m.Values)
	}

	// Discard last remaining draft -> returns to live state (-1).
	res, ok = m.HandleCommand(core.Command{ID: CmdDraftDiscard})
	if !ok || !res.HasDiscardedDraft || res.DiscardedDraftIdx != 0 {
		t.Fatalf("discard last result: %+v ok=%v", res, ok)
	}
	if m.DraftCount() != 0 || m.ActiveDraftIndex() != -1 || m.Values["title"] != "live title" || m.Values["count"] != "7" {
		t.Fatalf("after discard last: count=%d idx=%d vals=%v", m.DraftCount(), m.ActiveDraftIndex(), m.Values)
	}

	// Discard on live state is a no-op.
	res, ok = m.HandleCommand(core.Command{ID: CmdDraftDiscard})
	if !ok || res.HasDiscardedDraft {
		t.Fatalf("discard on live should not flag removal: %+v", res)
	}
}

func TestFormSubmitRowAndSubmitWhileEditing(t *testing.T) {
	m := New("Create", []core.ParamSpec{
		{Name: "name", Type: core.ParamString, Required: true},
		{Name: "flag", Type: core.ParamBool},
	}, nil)
	m.SubmitLabel = "Create"
	m.SubmitRow = true

	if m.OnSubmitRow() {
		t.Fatal("should start on field 0, not submit row")
	}
	vField := m.View("f")
	if vField.Props["submitRow"] != true || vField.Facts["onSubmit"] != false {
		t.Fatalf("view props/facts on field: props=%+v facts=%+v", vField.Props, vField.Facts)
	}

	// Navigate down to Submit row.
	m.Down() // idx 1 (flag)
	m.Down() // idx 2 (submit row)
	if !m.OnSubmitRow() || m.ActiveIdx != 2 {
		t.Fatalf("expected OnSubmitRow at idx 2, got idx=%d on=%v", m.ActiveIdx, m.OnSubmitRow())
	}
	m.Down() // clamps at 2
	if m.ActiveIdx != 2 {
		t.Fatalf("Down should clamp at 2, got %d", m.ActiveIdx)
	}
	vSub := m.View("f")
	if vSub.Facts["onSubmit"] != true || vSub.CacheKey == vField.CacheKey {
		t.Fatalf("view on submit row: facts=%+v sameKey=%v", vSub.Facts, vSub.CacheKey == vField.CacheKey)
	}

	// Pressing CmdEdit on submit row submits (here fails validation because name is required).
	res, ok := m.HandleCommand(core.Command{ID: CmdEdit})
	if !ok || res.Submitted || len(res.ErrorFields) != 1 || res.ErrorFields[0] != "name" {
		t.Fatalf("CmdEdit on submit row (invalid): %+v", res)
	}

	// Render highlighted submit row via ui.Render.
	frame, _ := ui.Render(m.View("f"), ui.DefaultDarkTheme(), "f", 50, 10)
	if !strings.Contains(frame.ANSI, "[ ⏎ Create ]") {
		t.Fatalf("rendered frame missing highlighted submit button:\n%s", frame.ANSI)
	}

	// Move up to field 0, start editing, type, and submit via CmdSubmit while still Editing.
	m.Top()
	m.HandleCommand(core.Command{ID: CmdEdit})
	if !m.Editing {
		t.Fatal("expected Editing=true")
	}
	m.HandleCommand(core.Command{ID: CmdInsert, Args: map[string]any{"text": "alpha"}})
	// Simulate an unsynced in-progress line edit too.
	m.line.Insert("!")
	res, ok = m.HandleCommand(core.Command{ID: CmdSubmit})
	if !ok || !res.Submitted || res.Values["name"] != "alpha!" {
		t.Fatalf("CmdSubmit while editing: %+v", res)
	}
	if m.Editing {
		t.Fatal("CmdSubmit should clear Editing")
	}

	// Navigate via Bottom() and submit via CmdToggle (space on submit row).
	m.Bottom()
	if !m.OnSubmitRow() {
		t.Fatalf("Bottom() should land on submit row, got idx=%d", m.ActiveIdx)
	}
	res, ok = m.HandleCommand(core.Command{ID: CmdToggle})
	if !ok || !res.Submitted || res.Values["name"] != "alpha!" {
		t.Fatalf("CmdToggle on submit row: %+v", res)
	}
}
