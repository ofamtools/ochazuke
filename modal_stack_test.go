package ochazuke

import (
	"strings"
	"testing"
)

// A visual-line selection with a modal open over it: the modal saves the
// row and visual-line as one return; ending the selection while the modal
// is open repairs that return's mode to normal through the stack's owner
// and leaves the live focus in the modal alone. A write to the
// Payload.Return snapshot through a Modals() pointer is not a second route
// into the frame: closing still restores the saved return.
func TestModalReturnRepairDuringSelection(t *testing.T) {
	r, m, a, _ := visualList(t)
	motionKeys(t, r, m, "V j")
	row := r.Focus
	if r.Mode.Name != "visual-line" || !a.Visual() {
		t.Fatalf("V j: mode %s visual %v", r.Mode.Name, a.Visual())
	}
	if err := r.Push("ask", "app"); err != nil {
		t.Fatal(err)
	}
	if r.Focus != "ask" {
		t.Fatalf("push: focus %q, want the modal body", r.Focus)
	}
	want := ModalReturnState{Focus: row, Mode: ModeState{Name: "visual-line"}}
	if got := r.ModalReturns["ask"]; got.Focus != want.Focus || got.Mode.Name != want.Mode.Name {
		t.Fatalf("saved return %+v, want %+v", got, want)
	}
	if snap := r.Instances["ask"].Payload.Return; snap != row {
		t.Fatalf("Payload.Return snapshot %q, want %q", snap, row)
	}

	r.endSelection(a)
	if a.Visual() {
		t.Fatal("endSelection left the selection on")
	}
	got := r.ModalReturns["ask"]
	if got.Focus != row || got.Mode.Name != "normal" {
		t.Fatalf("repaired return %+v, want focus %q mode normal", got, row)
	}
	if focus, mode := r.ModalReturn("ask"); focus != row || mode.Name != "normal" {
		t.Fatalf("ModalReturn %q %q after repair", focus, mode.Name)
	}
	if r.Focus != "ask" || r.Instances["ask"].Payload.Return != row {
		t.Fatalf("repair moved the live focus %q or the snapshot %q", r.Focus, r.Instances["ask"].Payload.Return)
	}

	// The frames handed out are detached, and so is the snapshot.
	frames := r.ModalFrames()
	if len(frames) != 1 || frames[0].ID != "ask" || !frames[0].Created || frames[0].Return.Focus != row {
		t.Fatalf("frames %+v", frames)
	}
	frames[0].Return.Mode.Name = "insert"
	r.Modals()[0].Payload.Return = "page"
	if got := r.ModalReturns["ask"]; got.Mode.Name != "normal" || got.Focus != row {
		t.Fatalf("a write to a detached frame or the snapshot reached the saved return: %+v", got)
	}
	if err := r.Pop(); err != nil {
		t.Fatal(err)
	}
	if r.Focus != row || r.Mode.Name != "normal" || a.Visual() || len(r.ModalReturns) != 0 {
		t.Fatalf("pop: focus %q mode %s visual %v returns %v", r.Focus, r.Mode.Name, a.Visual(), r.ModalReturns)
	}
}

// A modal pushed over a focus outside its own subtree borrows that focus
// (parents it under the modal while open) and gives it its own parent
// back on close; when that parent was removed meanwhile, app. The saved
// return and the adoption come and go with the frame.
func TestModalBorrowedParentRestoration(t *testing.T) {
	r, _ := focusFixture(t)
	r.Instance("viewer")
	if err := r.Push("viewer", "field"); err != nil {
		t.Fatal(err)
	}
	viewer := r.Instances["viewer"]
	if r.Focus != "field" || r.Instances["field"].Parent != "viewer" || viewer.borrowed != "field" || viewer.lentParent != "panel" {
		t.Fatalf("push: focus %q parent %q borrowed %q lent %q", r.Focus, r.Instances["field"].Parent, viewer.borrowed, viewer.lentParent)
	}
	frames := r.ModalFrames()
	if len(frames) != 1 || frames[0].Created || frames[0].Return.Focus != "field" || frames[0].Return.Mode.Name != "normal" {
		t.Fatalf("frames %+v", frames)
	}
	if err := r.Pop(); err != nil {
		t.Fatal(err)
	}
	if r.Instances["field"].Parent != "panel" || r.Focus != "field" {
		t.Fatalf("pop: parent %q focus %q, want panel and field", r.Instances["field"].Parent, r.Focus)
	}
	if viewer.borrowed != "" || viewer.lentParent != "" || viewer.Payload.Return != "" || len(r.ModalReturns) != 0 || r.Instances["viewer"] == nil {
		t.Fatalf("pop left bookkeeping: borrowed %q lent %q snapshot %q returns %v", viewer.borrowed, viewer.lentParent, viewer.Payload.Return, r.ModalReturns)
	}

	// The lent parent is removed while the modal is open: the field, held
	// by the modal, survives, and goes to app on close.
	if err := r.Push("viewer", "field"); err != nil {
		t.Fatal(err)
	}
	if err := r.RemoveInstance("panel"); err != nil {
		t.Fatal(err)
	}
	if r.Instances["field"] == nil || r.Instances["field"].Parent != "viewer" || r.ModalReturns["viewer"].Focus != "field" {
		t.Fatalf("remove: field %v return %+v", r.Instances["field"], r.ModalReturns["viewer"])
	}
	if err := r.Pop(); err != nil {
		t.Fatal(err)
	}
	if got := r.Instances["field"].Parent; got != "app" {
		t.Fatalf("pop after the lent parent went: parent %q, want app", got)
	}
	if strings.Join(r.modalIDs(), ",") != "" {
		t.Fatalf("modals %v", r.modalIDs())
	}
}
