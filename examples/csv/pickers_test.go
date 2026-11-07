package main

import (
	"slices"
	"testing"

	"ofam.dev/ochazuke"
)

// picker.open pickers lists pages, path and a derived picker for every
// noun with a list verb; picking one opens it.
func TestPickerPickerListsEveryPicker(t *testing.T) {
	a := open(t)
	if _, err := a.r.Execute(ochazuke.Request{Noun: ochazuke.PickerNoun, Verb: "open", Args: ochazuke.Arguments{"name": ochazuke.PickersPickerName}}); err != nil {
		t.Fatal(err)
	}
	c := a.r.Chooser("picker-pickers")
	if c == nil {
		t.Fatalf("top modal %q", a.r.TopModal())
	}
	var ids []string
	for _, row := range c.Matches() {
		ids = append(ids, row.ID)
	}
	want := []string{ochazuke.PagesPickerName, ochazuke.PathPickerName}
	for _, d := range a.r.PickerDefs() {
		if d.Derived {
			want = append(want, d.Name)
		}
	}
	if len(want) < 10 || !slices.Contains(want, "transformations") {
		t.Fatalf("derived pickers %v", want)
	}
	for _, name := range want {
		if !slices.Contains(ids, name) {
			t.Fatalf("pickers picker %v lacks %q", ids, name)
		}
	}
	c.SetQuery("transformations")
	press(t, a, "enter")
	picked := a.r.Chooser("picker-transformations")
	if picked == nil {
		t.Fatalf("enter opened %q", a.r.TopModal())
	}
	if len(picked.Matches()) != len(TransformationNames()) {
		t.Fatalf("transformations rows %d", len(picked.Matches()))
	}
}
