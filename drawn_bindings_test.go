package ochazuke

import (
	"reflect"
	"slices"
	"testing"
)

// drawnBindingsFixture is the nested-modal fixture (page in normal mode,
// "ask" in insert mode, "inner" over it) with one app row per mode, so a
// surface's bindings tell which mode they were read in.
func drawnBindingsFixture(t *testing.T) *Runtime {
	t.Helper()
	r := nestedModalFixture(t)
	app := r.Define("app")
	app.Mode("normal").Bind("Z", Do("ochazuke.binding.help"))
	app.Mode("insert").Bind("ctrl+y", Do("ochazuke.binding.help"))
	return r
}

func hasKey(rows []Binding, keys string) bool {
	return slices.ContainsFunc(rows, func(b Binding) bool { return b.Keys == keys })
}

// A surface's bindings are read at its drawn focus and base mode, bounded
// by the modals above it: the page beneath two modals gets the page's
// normal-mode rows, the modal beneath another the insert-mode rows the
// modal above it saved, and the top modal the live ones. Closing the
// modals lands in exactly the listings read while they were covered.
func TestListBindingsAtDrawnSurface(t *testing.T) {
	r := drawnBindingsFixture(t)
	page, ask, inner := r.ListBindingsAt("page"), r.ListBindingsAt("ask"), r.ListBindingsAt("inner")
	if !hasKey(page, "Z") || hasKey(page, "ctrl+y") {
		t.Errorf("the page beneath the modals reads its own mode: %v", page)
	}
	if !hasKey(ask, "ctrl+y") || hasKey(ask, "Z") {
		t.Errorf("the covered modal reads the mode saved above it: %v", ask)
	}
	live := r.ListBindings(r.Focus)
	if !reflect.DeepEqual(inner, live) || !reflect.DeepEqual(r.ListBindingsAt("missing"), live) {
		t.Errorf("the top modal and an unknown id read live:\n%v\n%v", inner, live)
	}
	if hasKey(live, "ctrl+y") || reflect.DeepEqual(live, ask) {
		t.Errorf("live ListBindings reads the live mode (%s), not the covered modal's: %v", r.Mode.Name, live)
	}
	for _, id := range []string{"panel", "field"} {
		if !reflect.DeepEqual(r.ListBindingsAt(id), page) {
			t.Errorf("%s draws with the page: %v", id, r.ListBindingsAt(id))
		}
	}
	if !reflect.DeepEqual(r.ListBindingsAt("answer"), ask) {
		t.Errorf("a component in the covered modal draws with it")
	}

	if err := r.Pop(); err != nil {
		t.Fatal(err)
	}
	if now := r.ListBindings(r.Focus); !reflect.DeepEqual(now, ask) {
		t.Errorf("after closing inner, live rows differ from the covered reading:\n%v\n%v", now, ask)
	}
	if err := r.Pop(); err != nil {
		t.Fatal(err)
	}
	if now := r.ListBindings(r.Focus); !reflect.DeepEqual(now, page) {
		t.Errorf("after closing every modal, live rows differ from the covered reading:\n%v\n%v", now, page)
	}
}

// The covered surfaces' hint lines read the same rows: the page's hints
// show its own key, the covered modal's the insert-mode key, and the top
// modal's equal HintCells at the live focus.
func TestHintCellsAtDrawnSurface(t *testing.T) {
	r := drawnBindingsFixture(t)
	hasCell := func(cells []HintCell, key string) bool {
		return slices.ContainsFunc(cells, func(c HintCell) bool { return c.Key == key })
	}
	if cells := HintCellsAt(r, "page"); !hasCell(cells, "Z") || hasCell(cells, "ctrl+y") {
		t.Errorf("page hints %v", cells)
	}
	if !reflect.DeepEqual(HintCellsAt(r, "inner"), HintCells(r, r.Focus)) {
		t.Errorf("the top modal's hints are the live ones")
	}
	covered := HintCellsAt(r, "ask")
	if err := r.Pop(); err != nil {
		t.Fatal(err)
	}
	if now := HintCells(r, r.Focus); !reflect.DeepEqual(now, covered) {
		t.Errorf("hints of the covered modal differ from its live hints once uncovered:\n%v\n%v", covered, now)
	}
}

// Asking changes nothing the live frame relies on: the interaction, the
// saved returns and the binding and hint caches stay as they were.
func TestListBindingsAtDoesNotMutate(t *testing.T) {
	r := drawnBindingsFixture(t)
	r.ListBindings(r.Focus)
	HintCells(r, r.Focus)
	before, state := snapshotInteraction(r, "note"), interactionState(r)
	for _, id := range []string{"page", "panel", "ask", "answer", "inner", "other", "app", "missing", ""} {
		r.ListBindingsAt(id)
		HintCellsAt(r, id)
		assertUntouched(t, "ListBindingsAt("+id+")", before, snapshotInteraction(r, "note"))
		if after := interactionState(r); !reflect.DeepEqual(state, after) {
			t.Fatalf("%q changed the runtime:\nbefore %+v\nafter  %+v", id, state, after)
		}
	}
}
