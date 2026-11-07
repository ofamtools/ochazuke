package ochazuke

import (
	"reflect"
	"slices"
	"testing"
)

// interactionState is everything the interaction accessors may not change:
// the live focus, mode, page, modal chain and saved returns, and the
// generation counters caches key on.
func interactionState(r *Runtime) map[string]any {
	state := map[string]any{
		"focus": r.Focus, "mode": cloneModeState(r.Mode), "page": r.Page,
		"modals": slices.Clone(r.modals), "generation": r.modeGeneration, "bindings": r.bindingVersion,
	}
	for _, id := range r.modals {
		back, mode := r.ModalReturn(id)
		state["return:"+id] = back
		state["returnMode:"+id] = cloneModeState(mode)
		state["parent:"+id] = r.Instances[id].Parent
	}
	for _, page := range r.PageList() {
		state["remembered:"+page.ID] = page.Remembered
		state["pageMode:"+page.ID] = cloneModeState(page.mode)
	}
	return state
}

// nestedModalFixture is focusFixture with the other page left in insert
// mode, the focus on the page's panel in normal mode, a modal with an
// input opened over it and put in insert mode, and a second modal opened
// over that input.
func nestedModalFixture(t *testing.T) *Runtime {
	t.Helper()
	r, _ := focusFixture(t)
	if err := r.OpenPage("other"); err != nil {
		t.Fatal(err)
	}
	if err := r.SetMode("insert"); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("page"); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFocus("panel"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.OpenModal("ask", Payload{Prompt: "?"}, WithInput(NewInput("answer", "", false))); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFocus("answer"); err != nil {
		t.Fatal(err)
	}
	if err := r.SetMode("insert"); err != nil {
		t.Fatal(err)
	}
	if err := r.Push("inner", ""); err != nil {
		t.Fatal(err)
	}
	return r
}

// TestInteractionSnapshotsDetached: CurrentMode, FocusID, Context and
// PresentationAt hand out values that share nothing with the runtime;
// changing them, or asking for them, changes no live interaction state.
func TestInteractionSnapshotsDetached(t *testing.T) {
	t.Run("CurrentMode copies the payload's request and shadow", func(t *testing.T) {
		r := NewRuntime()
		stored := &Request{
			Noun: "ochazuke.focus", Verb: "to", Target: "a",
			Args:     Arguments{"id": "a", "nested": map[string]any{"k": []any{"x"}}},
			Subjects: []string{"a", "b"}, Value: map[string]any{"v": 1},
			Actor: &Actor{Kind: "agent", Name: "one", Via: &Actor{Kind: "owner"}},
		}
		shadow := &ModeState{Name: "normal", Payload: Payload{Request: &Request{Noun: "n", Verb: "v", Args: Arguments{"s": "t"}}}}
		forceMode(r, ModeState{Name: "normal", Payload: Payload{Prefix: "g", Request: stored, Shadow: shadow}})
		before := interactionState(r)

		got := r.CurrentMode()
		if !reflect.DeepEqual(got, r.Mode) {
			t.Fatalf("CurrentMode = %+v, want %+v", got, r.Mode)
		}
		if got.Payload.Request == stored || got.Payload.Shadow == shadow || got.Payload.Shadow.Payload.Request == shadow.Payload.Request || got.Payload.Request.Actor == stored.Actor || got.Payload.Request.Actor.Via == stored.Actor.Via {
			t.Fatal("CurrentMode shares a pointer with the runtime")
		}
		got.Name = "insert"
		got.Payload.Prefix = "x"
		got.Payload.Request.Target = "z"
		got.Payload.Request.Args["id"] = "z"
		got.Payload.Request.Args["nested"].(map[string]any)["k"].([]any)[0] = "z"
		got.Payload.Request.Subjects[0] = "z"
		got.Payload.Request.Value.(map[string]any)["v"] = 2
		got.Payload.Request.Actor.Name = "z"
		got.Payload.Request.Actor.Via.Kind = "z"
		got.Payload.Shadow.Name = "z"
		got.Payload.Shadow.Payload.Request.Args["s"] = "z"

		if after := interactionState(r); !reflect.DeepEqual(before, after) {
			t.Fatalf("mutating CurrentMode changed the runtime:\nbefore %+v\nafter  %+v", before, after)
		}
		if stored.Args["id"] != "a" || stored.Args["nested"].(map[string]any)["k"].([]any)[0] != "x" || stored.Subjects[0] != "a" ||
			stored.Value.(map[string]any)["v"] != 1 || stored.Actor.Name != "one" || stored.Actor.Via.Kind != "owner" ||
			shadow.Name != "normal" || shadow.Payload.Request.Args["s"] != "t" {
			t.Fatalf("mutating CurrentMode reached the stored request: %+v %+v", stored, shadow)
		}
	})

	t.Run("FocusID and Context", func(t *testing.T) {
		r, _ := focusFixture(t)
		if r.FocusID() != r.Focus || r.FocusID() != "field" {
			t.Fatalf("FocusID = %q, Focus %q", r.FocusID(), r.Focus)
		}
		before := interactionState(r)
		c := r.Context()
		c.Path[0] = "z"
		c.InputID = "z"
		if after := interactionState(r); !reflect.DeepEqual(before, after) {
			t.Fatalf("mutating Context changed the runtime:\nbefore %+v\nafter  %+v", before, after)
		}
		if again := r.Context(); again.Path[0] != "app" || again.InputID != "field" {
			t.Fatalf("Context after mutating a copy = %+v", again)
		}
	})

	t.Run("PresentationAt", func(t *testing.T) {
		r := nestedModalFixture(t)
		before := interactionState(r)
		for _, id := range []string{"page", "panel", "field", "other", "ask", "answer", "inner", "app", "missing", ""} {
			view := r.PresentationAt(id)
			if after := interactionState(r); !reflect.DeepEqual(before, after) {
				t.Fatalf("PresentationAt(%q) changed the runtime:\nbefore %+v\nafter  %+v", id, before, after)
			}
			view.Focus, view.Surface = "z", "z"
			view.Mode.Name = "z"
			for i := range view.Modals {
				view.Modals[i] = "z"
			}
			for i := range view.Context.Path {
				view.Context.Path[i] = "z"
			}
			if after := interactionState(r); !reflect.DeepEqual(before, after) {
				t.Fatalf("mutating PresentationAt(%q) changed the runtime:\nbefore %+v\nafter  %+v", id, before, after)
			}
		}
	})
}

// TestPresentationAtNestedModals characterizes what each surface is drawn
// in under two open modals: the top modal is live, the modal beneath reads
// the saved return of the modal directly above it (not the outermost
// one's), the current page reads the outermost return, and a page that is
// not current reads what it remembered. Each view agrees with the existing
// readers (ModalFocus, ModalReturn, BaseFocus, PageFocus).
func TestPresentationAtNestedModals(t *testing.T) {
	r := nestedModalFixture(t)
	if r.TopModal() != "inner" || r.Focus != "inner" {
		t.Fatalf("fixture: top %q focus %q", r.TopModal(), r.Focus)
	}
	type want struct {
		surface, focus, mode string
		modals               []string
		live                 bool
	}
	cases := map[string]want{
		"inner":   {"inner", "inner", r.Mode.Name, []string{"ask", "inner"}, true},
		"answer":  {"ask", "answer", "insert", []string{"ask"}, false},
		"ask":     {"ask", "answer", "insert", []string{"ask"}, false},
		"page":    {"page", "panel", "normal", nil, false},
		"field":   {"page", "panel", "normal", nil, false},
		"panel":   {"page", "panel", "normal", nil, false},
		"other":   {"other", "other", "insert", nil, false},
		"app":     {"page", "panel", "normal", nil, false},
		"missing": {},
	}
	for id, w := range cases {
		got := r.PresentationAt(id)
		if got.Surface != w.surface || got.Focus != w.focus || got.Mode.Name != w.mode || !slices.Equal(got.Modals, w.modals) || got.Live != w.live {
			t.Errorf("PresentationAt(%q) = %+v, want %+v", id, got, w)
		}
		if w.focus != "" && !reflect.DeepEqual(got.Context, r.contextOf(w.focus)) {
			t.Errorf("PresentationAt(%q).Context = %+v, want the context of %q", id, got.Context, w.focus)
		}
	}
	if got := r.PresentationAt("ask"); got.Focus != r.ModalFocus("ask") {
		t.Errorf("modal view focus %q, ModalFocus %q", got.Focus, r.ModalFocus("ask"))
	}
	if back, mode := r.ModalReturn("inner"); r.PresentationAt("ask").Focus != back || r.PresentationAt("ask").Mode.Name != mode.Name {
		t.Errorf("modal view disagrees with the return of the modal above it: %q %+v", back, mode)
	}
	if got := r.PresentationAt("page"); got.Focus != r.BaseFocus() || got.Focus != r.PageFocus("page") {
		t.Errorf("page view focus %q, BaseFocus %q, PageFocus %q", got.Focus, r.BaseFocus(), r.PageFocus("page"))
	}
	if got := r.PresentationAt("other"); got.Focus != r.PageFocus("other") {
		t.Errorf("other page view focus %q, PageFocus %q", got.Focus, r.PageFocus("other"))
	}

	// Closing the top modal makes the modal beneath live, in the state
	// its view showed while it was covered.
	covered := r.PresentationAt("ask")
	if err := r.Pop(); err != nil {
		t.Fatal(err)
	}
	if got := r.PresentationAt("ask"); !got.Live || got.Focus != covered.Focus || got.Mode.Name != covered.Mode.Name || r.FocusID() != covered.Focus || r.CurrentMode().Name != covered.Mode.Name {
		t.Fatalf("after Pop: view %+v, live %q %q; covered view %+v", got, r.FocusID(), r.CurrentMode().Name, covered)
	}
	covered = r.PresentationAt("page")
	if err := r.Pop(); err != nil {
		t.Fatal(err)
	}
	if got := r.PresentationAt("field"); !got.Live || got.Surface != "page" || got.Focus != covered.Focus || got.Mode.Name != covered.Mode.Name || len(got.Modals) != 0 {
		t.Fatalf("after closing every modal: %+v; covered view %+v", got, covered)
	}
}

// TestPresentationAtNoPages: a runtime with no pages draws its instances on
// the app surface, live while no modal is open.
func TestPresentationAtNoPages(t *testing.T) {
	r := NewRuntime()
	r.Instance("box")
	if err := r.SetFocus("box"); err != nil {
		t.Fatal(err)
	}
	if got := r.PresentationAt("box"); got.Surface != "app" || got.Focus != "box" || !got.Live || got.Mode.Name != "normal" {
		t.Fatalf("PresentationAt(box) = %+v", got)
	}
	if err := r.Push("m", ""); err != nil {
		t.Fatal(err)
	}
	if got := r.PresentationAt("box"); got.Surface != "app" || got.Focus != "box" || got.Live {
		t.Fatalf("PresentationAt(box) under a modal = %+v", got)
	}
	if got := r.PresentationAt("m"); got.Surface != "m" || got.Focus != "m" || !got.Live {
		t.Fatalf("PresentationAt(m) = %+v", got)
	}
}
