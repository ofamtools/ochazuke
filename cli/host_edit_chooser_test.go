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
