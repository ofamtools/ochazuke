package cli

import (
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
