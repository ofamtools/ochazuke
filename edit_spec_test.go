package ochazuke

import (
	"slices"
	"strings"
	"testing"
)

func TestEditSpecIsTheModalForm(t *testing.T) {
	t.Parallel()
	r, _ := editFixture(t)
	spec, err := r.EditSpec("parcel", "t1")
	if err != nil {
		t.Fatal(err)
	}
	mustRun(t, r, Request{Noun: "parcel", Verb: "edit"})
	modal := r.RecordEditor(editFormID)
	if modal == nil {
		t.Fatal("edit did not open the form")
	}
	names := func(fs []RecordField) []string {
		var out []string
		for _, f := range fs {
			out = append(out, f.Name)
			if f.ReadOnly != slices.Contains([]string{"stamp", "created", "updated"}, f.Name) {
				t.Fatalf("field %s read-only %v", f.Name, f.ReadOnly)
			}
		}
		return out
	}
	if got, want := names(spec.Fields), names(modal.spec.Fields); !slices.Equal(got, want) {
		t.Fatalf("placed fields %v, modal %v", got, want)
	}
	if spec.Title != modal.spec.Title || spec.WriteKey != modal.spec.WriteKey || spec.Watch != modal.spec.Watch {
		t.Fatalf("placed %q/%q/%q, modal %q/%q/%q", spec.Title, spec.WriteKey, spec.Watch, modal.spec.Title, modal.spec.WriteKey, modal.spec.Watch)
	}
	rec, _ := spec.Load()
	mrec, _ := modal.spec.Load()
	if len(rec) != len(mrec) || rec["title"] != "Original" || rec["stamp"] != "fixed" {
		t.Fatalf("placed record %v, modal %v", rec, mrec)
	}
}

func TestEditSpecPlacedFormWritesAndReloads(t *testing.T) {
	t.Parallel()
	r, s := editFixture(t)
	spec, err := r.EditSpec("parcel", "t1")
	if err != nil {
		t.Fatal(err)
	}
	e, err := r.AddRecordEditor("placed", spec)
	if err != nil {
		t.Fatal(err)
	}
	f := e.field("title")
	if err := e.set(f, "Placed"); err != nil {
		t.Fatal(err)
	}
	if err := e.seal(f); err != nil {
		t.Fatal(err)
	}
	if got := s.find("t1")["title"]; got != "Placed" {
		t.Fatalf("store title %v", got)
	}
	mustRun(t, r, Request{Noun: "parcel", Verb: "set", Args: Arguments{"id": "t1", "props": map[string]any{"title": "Outside"}}})
	if got := e.Values()["title"]; got != "Outside" {
		t.Fatalf("placed form after an outside write: %q", got)
	}
}

func TestEditSpecReadOnlyPropsMutedAndSetting(t *testing.T) {
	t.Parallel()
	r, _ := editFixture(t)
	spec, err := r.EditSpec("parcel", "t1")
	if err != nil {
		t.Fatal(err)
	}
	e, err := r.AddRecordEditor("placed", spec)
	if err != nil {
		t.Fatal(err)
	}
	if got := e.shownFields(); got != 3 {
		t.Fatalf("focus stops %d, want title, status and active", got)
	}
	if err := e.Focus(); err != nil {
		t.Fatal(err)
	}
	for range 5 {
		if err := e.move(1, false); err != nil {
			t.Fatal(err)
		}
		if f := e.fieldOn(r.Focus); f == nil || f.ReadOnly {
			t.Fatalf("focus on %v", r.Focus)
		}
	}
	text := e.Block(40, 20).Text
	if !strings.Contains(text, "stamp") || !strings.Contains(text, "fixed") {
		t.Fatalf("read-only prop not drawn:\n%s", text)
	}
	if _, ok := e.Values()["stamp"]; ok {
		t.Fatal("a read-only field is not a value of the form")
	}
	if _, err := r.Settings().SetSession(EditShowReadOnlySetting, false); err != nil {
		t.Fatal(err)
	}
	off, err := r.EditSpec("parcel", "t1")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range off.Fields {
		if f.ReadOnly || f.Name == "stamp" {
			t.Fatalf("forms.show-read-only off still has %s", f.Name)
		}
	}
}

func TestEditSpecRefusesWhatCannotBeLoaded(t *testing.T) {
	t.Parallel()
	r, _ := editFixture(t)
	if _, err := r.EditSpec("nothing", "x"); err == nil {
		t.Fatal("an unknown noun has no form")
	}
}
