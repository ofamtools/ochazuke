package modal_test

import (
	"slices"
	"testing"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
	"ofam.dev/ochazuke/widgets/modal"
)

type stubModal struct {
	id      string
	mode    string
	scope   string
	focusID string
	closed  []modal.CloseReason
	onClose func(id string, reason modal.CloseReason)
}

func (m *stubModal) ID() string      { return m.id }
func (m *stubModal) Mode() string    { return m.mode }
func (m *stubModal) Scope() string   { return m.scope }
func (m *stubModal) FocusID() string { return m.focusID }
func (m *stubModal) View(w, h int) ui.ViewNode {
	return ui.ViewNode{ID: "modal." + m.id, Kind: ui.KindOverlay}
}
func (m *stubModal) Handle(core.Command) modal.Result {
	return modal.Result{Handled: true}
}
func (m *stubModal) OnClose(reason modal.CloseReason) {
	m.closed = append(m.closed, reason)
	if m.onClose != nil {
		m.onClose(m.id, reason)
	}
}

func frameIDs(s core.State) []string {
	out := make([]string, len(s.Modals))
	for i, f := range s.Modals {
		out[i] = f.ID
	}
	return out
}

func TestStackLifecycleAndStateSync(t *testing.T) {
	st := modal.NewStack()
	if !st.Empty() || st.Len() != 0 || st.Top() != nil || st.IDs() != nil {
		t.Fatalf("new stack should be empty")
	}

	s0 := core.NewState(core.Frame{ID: "home", FocusID: "home.list", Mode: "normal"})
	m1 := &stubModal{id: "one", mode: modal.ModePicker, scope: "one", focusID: "one.list"}
	m2 := &stubModal{id: "two", mode: modal.ModeForm, scope: "two", focusID: "two.form"}

	s1 := st.Push(s0, m1)
	if len(s0.Modals) != 0 {
		t.Fatalf("Push mutated prior state: %+v", s0.Modals)
	}
	s2 := st.Push(s1, m2)
	if len(s1.Modals) != 1 || len(s2.Modals) != 2 {
		t.Fatalf("expected 1:1 COW modal frames, got len(s1)=%d len(s2)=%d", len(s1.Modals), len(s2.Modals))
	}
	if st.Len() != 2 || st.Top() != m2 || st.Get("one") != m1 || !st.Has("two") || st.Has("missing") {
		t.Fatalf("unexpected stack lookup state")
	}
	if !slices.Equal(st.IDs(), []string{"one", "two"}) || !slices.Equal(frameIDs(s2), []string{"one", "two"}) {
		t.Fatalf("stack/state IDs mismatch: stack=%v state=%v", st.IDs(), frameIDs(s2))
	}
	topFrame := s2.ActiveFrame()
	if topFrame.Kind != core.FrameModal || topFrame.ID != "two" || topFrame.Mode != modal.ModeForm || topFrame.FocusID != "two.form" {
		t.Fatalf("unexpected active frame: %+v", topFrame)
	}

	// Duplicate-ID push replaces old modal, calling old.OnClose(CloseCancel) and promoting new one to top.
	m1New := &stubModal{id: "one", mode: "custom", scope: "one", focusID: "one.new"}
	s3 := st.Push(s2, m1New)
	if !slices.Equal(m1.closed, []modal.CloseReason{modal.CloseCancel}) {
		t.Fatalf("replaced modal OnClose = %v, want [cancel]", m1.closed)
	}
	if !slices.Equal(st.IDs(), []string{"two", "one"}) || !slices.Equal(frameIDs(s3), []string{"two", "one"}) {
		t.Fatalf("after replacement: stack=%v state=%v", st.IDs(), frameIDs(s3))
	}
	if st.Top() != m1New || s3.ActiveFrame().Mode != "custom" || s3.ActiveFrame().FocusID != "one.new" {
		t.Fatalf("replaced modal frame mismatch: %+v", s3.ActiveFrame())
	}

	// Close by ID removes non-top modal cleanly.
	s4, closed := st.Close(s3, "two", modal.CloseSubmit)
	if closed != m2 || !slices.Equal(m2.closed, []modal.CloseReason{modal.CloseSubmit}) {
		t.Fatalf("Close returned %v / %v", closed, m2.closed)
	}
	if !slices.Equal(st.IDs(), []string{"one"}) || !slices.Equal(frameIDs(s4), []string{"one"}) {
		t.Fatalf("after Close: stack=%v state=%v", st.IDs(), frameIDs(s4))
	}

	// Pop removes top and empties s.Modals.
	s5, popped := st.Pop(s4, modal.CloseCancel)
	if popped != m1New || !slices.Equal(m1New.closed, []modal.CloseReason{modal.CloseCancel}) {
		t.Fatalf("Pop returned %v / %v", popped, m1New.closed)
	}
	if !st.Empty() || s5.Modals != nil {
		t.Fatalf("expected empty stack and nil Modals, got len=%d modals=%v", st.Len(), s5.Modals)
	}
}

func TestStackDropAllLIFO(t *testing.T) {
	st := modal.NewStack()
	s := core.NewState(core.Frame{ID: "home"})
	var order []string
	record := func(id string, reason modal.CloseReason) {
		if reason != modal.CloseDropAll {
			t.Errorf("DropAll called OnClose(%v) on %s, want CloseDropAll", reason, id)
		}
		if !st.Empty() {
			t.Errorf("stack should already be empty during DropAll OnClose")
		}
		order = append(order, id)
	}
	for _, id := range []string{"a", "b", "c"} {
		s = st.Push(s, &stubModal{id: id, onClose: record})
	}

	after := st.DropAll(s)
	if !st.Empty() || after.Modals != nil {
		t.Fatalf("expected empty stack after DropAll, got len=%d modals=%v", st.Len(), after.Modals)
	}
	if len(s.Modals) != 3 {
		t.Fatalf("DropAll mutated prior state slice")
	}
	if !slices.Equal(order, []string{"c", "b", "a"}) {
		t.Fatalf("DropAll close order = %v, want [c b a]", order)
	}
}

func TestStackApplyResultAndSyncFrame(t *testing.T) {
	st := modal.NewStack()
	s := core.NewState(core.Frame{ID: "home"})
	m1 := &stubModal{id: "m1", mode: modal.ModeForm, focusID: "m1.form"}
	s = st.Push(s, m1)

	// Mode/FocusID sync from Result overrides.
	s = st.ApplyResult(s, modal.Result{Handled: true, Mode: modal.ModeFormEdit, FocusID: "m1.input"})
	if f := s.ActiveFrame(); f.Mode != modal.ModeFormEdit || f.FocusID != "m1.input" {
		t.Fatalf("ApplyResult override mismatch: %+v", f)
	}

	// SyncFrame falls back to top.Mode() / top.FocusID().
	m1.mode = "reverted"
	m1.focusID = "m1.reverted"
	s = st.SyncFrame(s)
	if f := s.ActiveFrame(); f.Mode != "reverted" || f.FocusID != "m1.reverted" {
		t.Fatalf("SyncFrame mismatch: %+v", f)
	}

	// Push only.
	m2 := &stubModal{id: "m2", mode: modal.ModePicker, focusID: "m2.list"}
	s = st.ApplyResult(s, modal.Result{Handled: true, Push: m2})
	if !slices.Equal(st.IDs(), []string{"m1", "m2"}) || !slices.Equal(frameIDs(s), []string{"m1", "m2"}) {
		t.Fatalf("ApplyResult Push mismatch: stack=%v state=%v", st.IDs(), frameIDs(s))
	}

	// Close + Push replaces top atomically.
	m3 := &stubModal{id: "m3", mode: modal.ModeConfirm, focusID: "m3.body"}
	s = st.ApplyResult(s, modal.Result{Handled: true, Close: true, CloseReason: modal.CloseSubmit, Push: m3})
	if !slices.Equal(m2.closed, []modal.CloseReason{modal.CloseSubmit}) {
		t.Fatalf("Close+Push did not close m2 with CloseSubmit: %v", m2.closed)
	}
	if !slices.Equal(st.IDs(), []string{"m1", "m3"}) || !slices.Equal(frameIDs(s), []string{"m1", "m3"}) {
		t.Fatalf("ApplyResult Close+Push mismatch: stack=%v state=%v", st.IDs(), frameIDs(s))
	}

	// Close only pops m3 and restores m1 as active frame.
	s = st.ApplyResult(s, modal.Result{Handled: true, Close: true, CloseReason: modal.CloseCancel})
	if !slices.Equal(m3.closed, []modal.CloseReason{modal.CloseCancel}) {
		t.Fatalf("Close did not close m3 with CloseCancel: %v", m3.closed)
	}
	if !slices.Equal(st.IDs(), []string{"m1"}) || s.ActiveFrame().ID != "m1" {
		t.Fatalf("ApplyResult Close mismatch: stack=%v active=%+v", st.IDs(), s.ActiveFrame())
	}
}
