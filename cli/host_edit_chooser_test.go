package cli

import (
	"context"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/keytest"
)

// edit {field} on a mounted noun's choice prop opens the chooser once the
// remote get lands; the pick writes through the remote set (a job) and
// closes the chooser, the focus back on the row.
func TestMountedEditFieldChooser(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"change", "confirm"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			r, m, thing := mountedEditFixture(t, mode)
			if _, err := r.Execute(ochazuke.Request{Noun: "ext-parcel", Verb: "edit", Args: ochazuke.Arguments{"field": "status"}}); err != nil {
				t.Fatal(err)
			}
			if err := Settle(r, nil); err != nil {
				t.Fatal(err)
			}
			if r.Chooser(ochazuke.EditChooserKind) == nil || r.RecordEditor("edit-form") != nil {
				t.Fatalf("mounted chooser: modal %q", r.TopModal())
			}
			keytest.Type(m, "done")
			if _, err := r.HandleBinding("enter"); err != nil {
				t.Fatal(err)
			}
			if err := Settle(r, nil); err != nil {
				t.Fatal(err)
			}
			if thing["status"] != "done" || r.TopModal() != "" || r.Focus != "rows/t1" {
				t.Fatalf("mounted pick: %v modal %q focus %q", thing, r.TopModal(), r.Focus)
			}
			// esc writes nothing.
			if _, err := r.Execute(ochazuke.Request{Noun: "ext-parcel", Verb: "edit", Args: ochazuke.Arguments{"field": "active"}}); err != nil {
				t.Fatal(err)
			}
			if err := Settle(r, nil); err != nil {
				t.Fatal(err)
			}
			keytest.Type(m, "true")
			if _, err := r.HandleBinding("esc"); err != nil {
				t.Fatal(err)
			}
			if err := Settle(r, nil); err != nil {
				t.Fatal(err)
			}
			if thing["active"] != false || r.TopModal() != "" || r.Focus != "rows/t1" {
				t.Fatalf("mounted esc: %v modal %q focus %q", thing, r.TopModal(), r.Focus)
			}
		})
	}
}

// On a mounted noun whose get answers later, edit {field}'s chooser opens
// at once, before the get lands: the keys typed meanwhile reach its query,
// never the page's list behind it. The get's answer then highlights the
// value the thing holds (an empty query), and the pick writes.
func TestMountedEditFieldChooserOpensBeforeTheGet(t *testing.T) {
	t.Parallel()
	r, m, thing := mountedEditFixture(t, "change")
	if _, err := r.Execute(ochazuke.Request{Noun: "ext-parcel", Verb: "edit", Args: ochazuke.Arguments{"field": "status"}}); err != nil {
		t.Fatal(err)
	}
	c := r.Chooser(ochazuke.EditChooserKind)
	if c == nil || r.TopModal() != ochazuke.EditChooserKind {
		t.Fatalf("the chooser waits for the get: modal %q", r.TopModal())
	}
	keytest.Type(m, "don")
	if c.Query() != "don" || r.Focus == "rows/t1" {
		t.Fatalf("typed before the get: query %q focus %q", c.Query(), r.Focus)
	}
	if err := Settle(r, nil); err != nil {
		t.Fatal(err)
	}
	if r.Chooser(ochazuke.EditChooserKind) != c || c.Query() != "don" {
		t.Fatalf("the get's answer kept the chooser and query: modal %q query %q", r.TopModal(), c.Query())
	}
	keytest.Type(m, "e")
	if _, err := r.HandleBinding("enter"); err != nil {
		t.Fatal(err)
	}
	if err := Settle(r, nil); err != nil {
		t.Fatal(err)
	}
	if thing["status"] != "done" || r.TopModal() != "" || r.Focus != "rows/t1" {
		t.Fatalf("pick: %v modal %q focus %q", thing, r.TopModal(), r.Focus)
	}
	// Opened again with nothing typed, the get's answer highlights the
	// value it holds.
	if _, err := r.Execute(ochazuke.Request{Noun: "ext-parcel", Verb: "edit", Args: ochazuke.Arguments{"field": "status"}}); err != nil {
		t.Fatal(err)
	}
	if err := Settle(r, nil); err != nil {
		t.Fatal(err)
	}
	if row, ok := r.Chooser(ochazuke.EditChooserKind).Highlighted(); !ok || row.ID != "done" {
		t.Fatalf("highlight after the get: %+v %t", row, ok)
	}
}

// Enter before the get lands, with nothing typed and the highlight not
// moved, is no choice: the chooser says the value is loading and holds
// the enter until the get lands, then takes the value the thing holds,
// so nothing is written (UX R2 N1). A moved highlight is a choice and
// writes at once.
func TestMountedEditChooserEnterBeforeTheGetLands(t *testing.T) {
	t.Parallel()
	var sets int
	r, _, thing := mountedEditFixture(t, "change", func(context.Context) error { sets++; return nil })
	thing["status"] = "done"
	if _, err := r.Execute(ochazuke.Request{Noun: "ext-parcel", Verb: "edit", Args: ochazuke.Arguments{"field": "status"}}); err != nil {
		t.Fatal(err)
	}
	c := r.Chooser(ochazuke.EditChooserKind)
	if c == nil || !strings.Contains(c.Title(), "loading current value") {
		t.Fatalf("the chooser shows the value is loading: %v", c)
	}
	if _, err := r.HandleBinding("enter"); err != nil {
		t.Fatal(err)
	}
	if r.Chooser(ochazuke.EditChooserKind) != c || sets != 0 {
		t.Fatalf("enter before the get is held: modal %q sets %d", r.TopModal(), sets)
	}
	if err := Settle(r, nil); err != nil {
		t.Fatal(err)
	}
	if thing["status"] != "done" || sets != 0 || r.TopModal() != "" || r.Focus != "rows/t1" {
		t.Fatalf("held enter took the loaded value: %v sets %d modal %q focus %q", thing["status"], sets, r.TopModal(), r.Focus)
	}
	// A moved highlight is the user's choice: it writes.
	if _, err := r.Execute(ochazuke.Request{Noun: "ext-parcel", Verb: "edit", Args: ochazuke.Arguments{"field": "status"}}); err != nil {
		t.Fatal(err)
	}
	c = r.Chooser(ochazuke.EditChooserKind)
	if _, err := r.HandleBinding("down"); err != nil || !c.Moved() {
		t.Fatalf("down: %v moved %t", err, c.Moved())
	}
	row, _ := c.Highlighted()
	if _, err := r.HandleBinding("enter"); err != nil {
		t.Fatal(err)
	}
	if err := Settle(r, nil); err != nil {
		t.Fatal(err)
	}
	if thing["status"] != row.ID || sets != 1 || r.TopModal() != "" {
		t.Fatalf("moved pick: %v want %q sets %d modal %q", thing["status"], row.ID, sets, r.TopModal())
	}
}

// An enter held for the get, then a moved highlight, is no enter: the
// move is a new choice and cancels the held enter, as typing does, so the
// get landing writes nothing and the chooser stays open on the moved row
// (UX R3 F1).
func TestMountedEditChooserHeldEnterThenMove(t *testing.T) {
	t.Parallel()
	var sets int
	r, _, thing := mountedEditFixture(t, "change", func(context.Context) error { sets++; return nil })
	if _, err := r.Execute(ochazuke.Request{Noun: "ext-parcel", Verb: "edit", Args: ochazuke.Arguments{"field": "status"}}); err != nil {
		t.Fatal(err)
	}
	c := r.Chooser(ochazuke.EditChooserKind)
	for _, key := range []string{"enter", "down"} {
		if _, err := r.HandleBinding(key); err != nil {
			t.Fatal(err)
		}
	}
	if err := Settle(r, nil); err != nil {
		t.Fatal(err)
	}
	if sets != 0 || thing["status"] != "todo" || r.Chooser(ochazuke.EditChooserKind) != c {
		t.Fatalf("held enter then move wrote: %v sets %d modal %q", thing["status"], sets, r.TopModal())
	}
	row, _ := c.Highlighted()
	if _, err := r.HandleBinding("enter"); err != nil {
		t.Fatal(err)
	}
	if err := Settle(r, nil); err != nil {
		t.Fatal(err)
	}
	if thing["status"] != row.ID || sets != 1 || r.TopModal() != "" {
		t.Fatalf("enter after the move: %v want %q sets %d modal %q", thing["status"], row.ID, sets, r.TopModal())
	}
}

// When the get fails after the chooser picked, the pick's write is the
// result: the get's failure is not reported (UX R2 N2). While the
// chooser is still open, a failed get closes it and is reported.
func TestMountedEditChooserGetFailsAfterPick(t *testing.T) {
	t.Parallel()
	r, _, thing := mountedEditFixture(t, "change")
	thing["get-error"] = "backend down: cannot read parcel"
	if _, err := r.Execute(ochazuke.Request{Noun: "ext-parcel", Verb: "edit", Args: ochazuke.Arguments{"field": "status"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.HandleBinding("down"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.HandleBinding("enter"); err != nil {
		t.Fatal(err)
	}
	if err := Settle(r, nil); err != nil {
		t.Fatalf("the get's failure after the pick is not the result: %v", err)
	}
	if thing["status"] == "todo" || r.TopModal() != "" {
		t.Fatalf("pick written: %v modal %q", thing["status"], r.TopModal())
	}
	// Still open when the get fails: it closes and the failure is shown.
	if _, err := r.Execute(ochazuke.Request{Noun: "ext-parcel", Verb: "edit", Args: ochazuke.Arguments{"field": "status"}}); err != nil {
		t.Fatal(err)
	}
	if err := Settle(r, nil); err == nil || !strings.Contains(err.Error(), "backend down") || r.TopModal() != "" {
		t.Fatalf("open chooser's failed get: %v modal %q", err, r.TopModal())
	}
}
