package form

import (
	"reflect"
	"testing"
	"time"

	"ofam.dev/ochazuke/core"
)

func reactiveSpecs() []core.ParamSpec {
	return []core.ParamSpec{
		{Name: "mode", Type: core.ParamString, Enum: []string{"basic", "advanced"}, Default: "basic"},
		{Name: "token", Type: core.ParamString, Required: true, VisibleWhen: `mode == "advanced"`},
		{Name: "retries", Type: core.ParamInt, Default: 1},
		{Name: "backoff", Type: core.ParamString, Required: true, VisibleWhen: `values.retries > 2`},
		{Name: "debug", Type: core.ParamBool},
		{Name: "trace", Type: core.ParamString, VisibleWhen: `debug && values.mode == "advanced"`},
		{Name: "broken", Type: core.ParamString, VisibleWhen: `((( invalid syntax`},
		{Name: "note", Type: core.ParamString},
	}
}

func TestReactiveVisibilityAndValidation(t *testing.T) {
	m := New("Reactive", reactiveSpecs(), nil)

	// Initially: mode="basic", retries=1, debug=false.
	// Visible: mode(0), retries(2), debug(4), broken(6, fail-open), note(7).
	wantVis := []int{0, 2, 4, 6, 7}
	if got := m.VisibleIndices(); !reflect.DeepEqual(got, wantVis) {
		t.Fatalf("VisibleIndices = %v, want %v", got, wantVis)
	}
	if m.IsFieldVisible(1) || m.IsFieldVisible(3) || m.IsFieldVisible(5) {
		t.Fatalf("hidden fields reported visible")
	}
	if !m.IsFieldVisible(6) {
		t.Fatalf("malformed VisibleWhen must fail open to true")
	}

	// Required hidden fields ("token", "backoff") must NOT block validation or appear in TypedValues.
	vals, ok := m.Submit()
	if !ok {
		t.Fatalf("expected valid submit when required fields are hidden, errors=%v", m.Errors)
	}
	if _, has := vals["token"]; has {
		t.Fatalf("hidden field token leaked into TypedValues: %v", vals)
	}

	// Reveal "token" by switching mode to "advanced"; now required "token" fails validation.
	m.SetValue("mode", "advanced")
	if !m.IsFieldVisible(1) {
		t.Fatalf("expected token visible when mode==advanced")
	}
	if m.Validate() || m.Errors["token"] == "" {
		t.Fatalf("expected token validation error, got errors=%v", m.Errors)
	}

	// Populate token, then hide it again: value retained in m.Values, error cleared, excluded from TypedValues.
	m.SetValue("token", "secret-123")
	m.SetValue("retries", "5")
	if !m.IsFieldVisible(3) {
		t.Fatalf("expected backoff visible when retries > 2")
	}
	if m.Validate() || m.Errors["backoff"] == "" {
		t.Fatalf("expected backoff error when visible and empty, errors=%v", m.Errors)
	}

	// Hiding backoff clears stale m.Errors["backoff"] immediately on touch.
	m.SetValue("retries", "1")
	if m.IsFieldVisible(3) {
		t.Fatalf("expected backoff hidden when retries==1")
	}
	if m.Errors["backoff"] != "" {
		t.Fatalf("stale error on hidden field backoff was not cleared: %v", m.Errors)
	}

	// Hiding token preserves m.Values["token"] in memory but omits it from TypedValues.
	m.SetValue("mode", "basic")
	if m.Values["token"] != "secret-123" {
		t.Fatalf("hidden field value not retained: %v", m.Values["token"])
	}
	vals, ok = m.Submit()
	if !ok {
		t.Fatalf("submit failed after hiding required fields: %v", m.Errors)
	}
	if _, has := vals["token"]; has {
		t.Fatalf("hidden field token leaked into Submit: %v", vals)
	}

	// Re-showing token restores its retained value and includes it in Submit.
	m.SetValue("mode", "advanced")
	vals, ok = m.Submit()
	if !ok || vals["token"] != "secret-123" {
		t.Fatalf("re-shown token not submitted: ok=%v vals=%v errors=%v", ok, vals, m.Errors)
	}

	// ShowAllFields bypasses all VisibleWhen predicates and changes View/CacheKey.
	// With mode=="advanced", retries==1, debug==false: mode, token, retries, debug, broken, note (6 visible).
	vFiltered := m.View("f")
	if vFiltered.Facts["visibleCount"] != 6 {
		t.Fatalf("visibleCount = %v, want 6", vFiltered.Facts["visibleCount"])
	}
	m.ShowAllFields = true
	if len(m.VisibleIndices()) != len(m.Specs) {
		t.Fatalf("ShowAllFields should expose all %d fields, got %d", len(m.Specs), len(m.VisibleIndices()))
	}
	vAll := m.View("f")
	if vAll.Facts["visibleCount"] != len(m.Specs) || vAll.CacheKey == vFiltered.CacheKey {
		t.Fatalf("ShowAllFields view mismatch: facts=%v sameKey=%v", vAll.Facts, vAll.CacheKey == vFiltered.CacheKey)
	}
}

func TestReactiveFocusReanchoringAndDrafts(t *testing.T) {
	specs := []core.ParamSpec{
		{Name: "mode", Type: core.ParamString, Enum: []string{"basic", "advanced"}, Default: "basic"},
		{Name: "extra", Type: core.ParamString, VisibleWhen: `mode == "advanced"`},
		{Name: "note", Type: core.ParamString},
	}
	m := New("Anchor", specs, nil)

	// Hidden field cannot be focused; visible field can.
	if m.FocusField("extra") {
		t.Fatal("FocusField should return false for hidden field")
	}
	if !m.FocusField("note") || m.ActiveFieldName() != "note" || m.ActiveIdx != 1 {
		t.Fatalf("FocusField(note): name=%q idx=%d", m.ActiveFieldName(), m.ActiveIdx)
	}

	// Revealing "extra" above "note" re-anchors ActiveIdx from 1 -> 2, keeping focus on "note".
	m.SetValue("mode", "advanced")
	if m.ActiveFieldName() != "note" || m.ActiveIdx != 2 {
		t.Fatalf("expected re-anchor to note at idx 2, got name=%q idx=%d", m.ActiveFieldName(), m.ActiveIdx)
	}

	// Focus "extra" and enter Editing; hiding "extra" clamps ActiveIdx and exits Editing cleanly.
	if !m.FocusField("extra") || m.ActiveIdx != 1 {
		t.Fatalf("FocusField(extra): idx=%d", m.ActiveIdx)
	}
	m.HandleCommand(core.Command{ID: CmdEdit})
	if !m.Editing {
		t.Fatal("expected Editing=true on extra")
	}
	m.SetValue("mode", "basic")
	if m.Editing {
		t.Fatal("expected Editing=false after focused row disappeared")
	}
	if m.ActiveFieldName() != "note" || m.ActiveIdx != 1 {
		t.Fatalf("expected clamp to note at idx 1, got name=%q idx=%d", m.ActiveFieldName(), m.ActiveIdx)
	}

	// Draft cycling re-anchors focus when drafts change visibility.
	m.SetDrafts([]Draft{
		{Values: map[string]any{"mode": "advanced", "extra": "x", "note": "n"}, SavedAt: time.Unix(1, 0)},
	})
	if !m.DraftPrev() {
		t.Fatal("DraftPrev failed")
	}
	if m.ActiveFieldName() != "note" || m.ActiveIdx != 2 {
		t.Fatalf("DraftPrev did not re-anchor note: name=%q idx=%d", m.ActiveFieldName(), m.ActiveIdx)
	}
	if !m.DraftNext() {
		t.Fatal("DraftNext failed")
	}
	if m.ActiveFieldName() != "note" || m.ActiveIdx != 1 {
		t.Fatalf("DraftNext did not re-anchor note: name=%q idx=%d", m.ActiveFieldName(), m.ActiveIdx)
	}
}
