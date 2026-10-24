package ochazuke

import (
	"encoding/json"
	"slices"
	"testing"

	"ochazuke/internal/keytest"
)

// A capture row is consulted outer-first before the inner-first walk, so an
// inner any row (the terminal's passthrough) does not swallow it. The row
// comes from config with "capture": true.
func TestCaptureRowBeatsInnerAnyRow(t *testing.T) {
	r, _, stream := terminalFixture(t)
	press(t, r, "ctrl+c")
	if r.Quit || len(stream.sent) != 1 {
		t.Fatalf("without a capture row the terminal takes ctrl+c: quit=%v sent=%v", r.Quit, stream.sent)
	}
	var row Binding
	if err := json.Unmarshal([]byte(`{"noun": "app", "keys": "ctrl+c", "capture": true, "request": {"verb": "quit", "noun": "app"}}`), &row); err != nil {
		t.Fatal(err)
	}
	if err := r.Bind(row); err != nil {
		t.Fatal(err)
	}
	press(t, r, "ctrl+c")
	if !r.Quit || len(stream.sent) != 1 {
		t.Fatalf("captured ctrl+c: quit=%v sent=%v", r.Quit, stream.sent)
	}
	press(t, r, "x")
	if len(stream.sent) != 2 {
		t.Fatalf("other keys still reach the terminal: %v", stream.sent)
	}
	listed := false
	for _, b := range r.ListBindings(r.Focus) {
		listed = listed || b.Keys == "ctrl+c" && b.Capture
	}
	if !listed {
		t.Fatal("bindings.list shows the capture row")
	}
}

// Capture runs outer-first: the app's capture row answers before an inner
// level's capture row for the same key; a non-capture app row does not.
func TestCaptureIsOuterFirst(t *testing.T) {
	r := NewRuntime()
	calls := recorder(t, r, "outer", "inner", "plain")
	r.Instance("panel")
	if err := r.SetFocus("panel"); err != nil {
		t.Fatal(err)
	}
	r.Define("panel").Mode("normal").Bind("x", Do("test.inner").Captured())
	r.Define("app").Mode("normal").Bind("x", Do("test.plain"))
	press(t, r, "x")
	r.Define("app").Mode("normal").Bind("x", Do("test.outer").Captured())
	press(t, r, "x")
	if got := *calls; len(got) != 2 || got[0] != "inner" || got[1] != "outer" {
		t.Fatalf("capture order: %v", got)
	}
}

// The model's ctrl+c is a capture row guarded by no-modal: it quits from a
// terminal's passthrough, and inside the palette the palette still closes.
func TestModelCtrlCCapturedOutsideModals(t *testing.T) {
	r, _, stream := terminalFixture(t)
	m := NewModel(r)
	if err := m.Palette.Open(); err != nil {
		t.Fatal(err)
	}
	keytest.Press(m, "ctrl+c")
	if m.Palette.IsOpen() || r.Quit {
		t.Fatalf("palette ctrl+c closes it: open=%v quit=%v", m.Palette.IsOpen(), r.Quit)
	}
	keytest.Press(m, "ctrl+c")
	if !r.Quit || len(stream.sent) != 0 {
		t.Fatalf("terminal focus: quit=%v sent=%v", r.Quit, stream.sent)
	}
}

// The top modal is a resolution boundary: a key inside a modal opened over
// a panel never reaches the panel's (or the page's) rows, while app rows
// still answer.
func TestModalIsResolutionBoundary(t *testing.T) {
	r, _ := focusFixture(t)
	pokes := 0
	r.Register(Operation{Verb: "poke", Noun: "panel", Run: func(*Runtime, Context, Arguments) (any, error) { pokes++; return nil, nil }})
	calls := recorder(t, r, "app")
	r.Define("panel").Mode("normal").Bind("z", Do("panel.poke"))
	r.Define("page").Mode("normal").Bind("y", Do("panel.poke"))
	r.Define("app").Mode("normal").Bind("v", Do("test.app"))
	if err := r.SetMode("normal"); err != nil {
		t.Fatal(err)
	}
	press(t, r, "z")
	if pokes != 1 {
		t.Fatalf("without a modal the panel row answers: %d", pokes)
	}
	if _, err := r.OpenModal("ask", Payload{Prompt: "?"}); err != nil {
		t.Fatal(err)
	}
	if got := r.Ancestry(r.Focus); !slices.Contains(got, "panel") {
		t.Fatalf("the modal stays parented over the panel: %v", got)
	}
	for _, key := range []string{"z", "y"} {
		if _, err := r.Dispatch(MacroKey{Key: key}); err != nil {
			t.Fatal(err)
		}
	}
	if pokes != 1 {
		t.Fatalf("a key inside the modal bubbled beneath it: %d", pokes)
	}
	press(t, r, "v")
	if len(*calls) != 1 {
		t.Fatal("app rows still answer inside a modal")
	}
	for _, b := range r.ListBindings(r.Focus) {
		if b.Noun == "panel" || b.Noun == "page" {
			t.Fatalf("bindings.list shows a row beneath the modal: %+v", b)
		}
	}
}
