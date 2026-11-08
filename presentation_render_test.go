package ochazuke

import (
	"reflect"
	"strings"
	"testing"
)

// drawnSeen is what a renderer read while the frame drew it: the live
// state (which must be the live interaction, never a borrowed one) and the
// presentation of the surface it draws.
type drawnSeen struct {
	focus, mode       string
	drawnAt           string
	drawnMode         string
	focused, drawnHas bool
	inspected         string
	presented         Presentation
}

// presentationFixture is a page with a list (a1..a3) and a modal body
// "m1" with its own list (m1/x, m1/y), each renderer recording what it read
// while drawn.
func presentationFixture(t *testing.T) (*Model, *Runtime, *[]drawnSeen, *[]drawnSeen) {
	t.Helper()
	r := NewRuntime()
	r.Config().Disable()
	m := NewModel(r)
	m.Appearance.Theme = "mono"
	m.Width, m.Height = 80, 24
	var page, modal []drawnSeen
	record := func(r *Runtime, list, surface string) drawnSeen {
		l := r.List(list)
		return drawnSeen{
			focus: r.Focus, mode: r.Mode.Name,
			drawnAt: r.DrawnFocus(list), drawnMode: r.DrawnBaseMode(list),
			focused: l.Focused(), drawnHas: l.DrawnFocused(),
			inspected: r.Inspect().Mode, presented: r.PresentationAt(surface),
		}
	}
	if _, err := r.AddPage("p", "Page", func(r *Runtime, w, h int) Block {
		page = append(page, record(r, "a", "p"))
		return Text(r.List("a").View(w, 3))
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.AddList("a", []ListItem{{ID: "a1", Label: "a1", Noun: "ochazuke.collection"}, {ID: "a2", Label: "a2", Noun: "ochazuke.collection"}, {ID: "a3", Label: "a3", Noun: "ochazuke.collection"}}, Parent("p")); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("p"); err != nil {
		t.Fatal(err)
	}
	r.Instance("m1")
	if _, err := r.AddList("ml", []ListItem{{ID: "m1/x", Label: "x", Noun: "ochazuke.collection"}, {ID: "m1/y", Label: "y", Noun: "ochazuke.collection"}}, Parent("m1")); err != nil {
		t.Fatal(err)
	}
	m.ModalViews["m1"] = ModalView{Title: "M1", Render: func(r *Runtime, _ *Instance, w, h int) Block {
		modal = append(modal, record(r, "ml", "m1"))
		return Text(r.List("ml").View(w, 2))
	}}
	return m, r, &page, &modal
}

// frameState is everything a frame must leave as it found it.
func frameState(r *Runtime) any {
	return []any{r.Focus, cloneModeState(r.Mode), r.ModalFrames(), r.Modals(), r.BaseFocus(), r.Page}
}

// A frame reads each surface's presentation and never assigns the live
// focus or mode: the page renderer beneath a modal sees the live focus
// (the modal's) on Runtime.Focus and Runtime.Mode, and its own drawn
// focus and mode through DrawnFocus/DrawnFocused/PresentationAt. A covered
// modal draws at the focus the modal above it saved. Nothing changes
// across the frame (MO-10: Model.beneath is gone).
func TestRenderDoesNotBorrowInteraction(t *testing.T) {
	m, r, page, modal := presentationFixture(t)
	if err := r.SetFocus("a2"); err != nil {
		t.Fatal(err)
	}
	if err := r.Push("m1", "m1/y"); err != nil {
		t.Fatal(err)
	}
	before := frameState(r)
	*page, *modal = nil, nil
	m.View()
	if after := frameState(r); !reflect.DeepEqual(before, after) {
		t.Fatalf("a frame changed the interaction:\n before %#v\n after  %#v", before, after)
	}
	if len(*page) == 0 || len(*modal) == 0 {
		t.Fatalf("renderers not run: page %d modal %d", len(*page), len(*modal))
	}
	for _, seen := range *page {
		if seen.focus != "m1/y" || seen.mode != r.Mode.Name || seen.inspected != r.Mode.Name {
			t.Errorf("page renderer saw a borrowed live state: focus %q mode %q inspect %q, want the modal's m1/y %q", seen.focus, seen.mode, seen.inspected, r.Mode.Name)
		}
		if seen.drawnAt != "a2" || !seen.drawnHas || seen.focused {
			t.Errorf("page list drawn at %q (drawn focused %v, live focused %v), want a2, true, false", seen.drawnAt, seen.drawnHas, seen.focused)
		}
		if seen.presented.Live || seen.presented.Focus != "a2" || seen.presented.Surface != "p" {
			t.Errorf("page presentation %+v, want p at a2, not live", seen.presented)
		}
	}
	for _, seen := range *modal {
		if seen.drawnAt != "m1/y" || !seen.drawnHas || !seen.presented.Live {
			t.Errorf("top modal drawn at %q (live view %v), want the live m1/y", seen.drawnAt, seen.presented.Live)
		}
	}

	// A second modal over m1: m1 is now covered and draws at the focus the
	// new modal saved (m1/y), the page still at the outermost return.
	if _, err := r.OpenModal("confirm", Payload{Prompt: "Sure?"}); err != nil {
		t.Fatal(err)
	}
	before = frameState(r)
	*page, *modal = nil, nil
	m.View()
	if after := frameState(r); !reflect.DeepEqual(before, after) {
		t.Fatalf("a nested frame changed the interaction:\n before %#v\n after  %#v", before, after)
	}
	for _, seen := range *modal {
		if seen.focus != r.Focus || seen.drawnAt != "m1/y" || !seen.drawnHas || seen.focused || seen.presented.Live {
			t.Errorf("covered modal saw live %q drawn %q (drawn focused %v, focused %v, live view %v), want the live confirm and drawn m1/y", seen.focus, seen.drawnAt, seen.drawnHas, seen.focused, seen.presented.Live)
		}
	}
	for _, seen := range *page {
		if seen.drawnAt != "a2" || seen.focus != r.Focus {
			t.Errorf("page under two modals: drawn %q live %q, want a2 and %q", seen.drawnAt, seen.focus, r.Focus)
		}
	}
}

// Drawing is repeatable and inspection during a draw is reentrant: three
// frames in a row draw the same text and leave the same state, and the
// inspections a renderer makes mid-frame (Inspect, CurrentMode, FocusID,
// PresentationAt) answer what they answer outside a frame.
func TestRenderRepeatedAndReentrantInspection(t *testing.T) {
	m, r, page, _ := presentationFixture(t)
	if err := r.SetFocus("a3"); err != nil {
		t.Fatal(err)
	}
	if err := r.SetMode("visual-line"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.OpenModal("confirm", Payload{Prompt: "Sure?"}); err != nil {
		t.Fatal(err)
	}
	outside := struct {
		inspect   Inspection
		mode      ModeState
		focus     string
		presented Presentation
	}{r.Inspect(), r.CurrentMode(), r.FocusID(), r.PresentationAt("p")}
	state := frameState(r)
	var frames []string
	for range 3 {
		*page = nil
		m.redraw = modelRedraw{}
		frames = append(frames, m.View().Content)
		if got := frameState(r); !reflect.DeepEqual(state, got) {
			t.Fatalf("frame %d changed the interaction: %#v, want %#v", len(frames), got, state)
		}
		for _, seen := range *page {
			if seen.inspected != outside.inspect.Mode || seen.focus != outside.focus || !reflect.DeepEqual(seen.presented, outside.presented) {
				t.Fatalf("mid-frame inspection differs: mode %q focus %q view %+v, outside %q %q %+v", seen.inspected, seen.focus, seen.presented, outside.inspect.Mode, outside.focus, outside.presented)
			}
			if seen.drawnMode != "visual-line" {
				t.Fatalf("page drawn in %q, want the visual-line the modal opened over", seen.drawnMode)
			}
		}
	}
	if frames[0] != frames[1] || frames[1] != frames[2] {
		t.Fatalf("repeated frames differ:\n%s\n---\n%s\n---\n%s", frames[0], frames[1], frames[2])
	}
	if !reflect.DeepEqual(r.CurrentMode(), outside.mode) || !reflect.DeepEqual(r.Inspect().Mode, outside.inspect.Mode) {
		t.Fatalf("inspection moved: %+v", r.CurrentMode())
	}
	if !strings.Contains(frames[0], "a3") {
		t.Fatalf("page not drawn under the modal:\n%s", frames[0])
	}
}

// A saved return whose focus was removed has no mode: the page beneath
// draws in the mode that focus declares (what closing the modal lands
// in), never in the modal's live mode. PresentationAt and the drawn reads
// agree.
func TestDrawnModeOfARepairedReturnIsTheLanding(t *testing.T) {
	_, r, _, _ := presentationFixture(t)
	if _, err := r.AddList("b", []ListItem{{ID: "b1", Label: "b1", Noun: "ochazuke.collection"}}, Parent("p")); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFocus("b1"); err != nil {
		t.Fatal(err)
	}
	if err := r.Push("m1", "m1/x"); err != nil {
		t.Fatal(err)
	}
	if err := r.SetMode("insert"); err != nil {
		t.Fatal(err)
	}
	if err := r.RemoveInstance("b"); err != nil {
		t.Fatal(err)
	}
	ret := r.ModalFrames()[0].Return
	if ret.Mode.Name != "" {
		t.Skipf("removal kept a mode on the return (%q): nothing to fall back from", ret.Mode.Name)
	}
	want := r.DeclaredMode(ret.Focus)
	if want == r.Mode.Name {
		t.Fatalf("fixture: the landing's mode %q is the live one; nothing tells the rules apart", want)
	}
	if got := r.DrawnBaseMode("a"); got != want {
		t.Fatalf("page drawn in %q, want the landing's %q (live %q)", got, want, r.Mode.Name)
	}
	if view := r.PresentationAt("p"); view.Mode.Name != want || view.Focus != ret.Focus {
		t.Fatalf("PresentationAt %+v disagrees with the drawn read %q at %q", view, want, ret.Focus)
	}
}
