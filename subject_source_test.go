package ochazuke

import (
	"reflect"
	"testing"
)

// A stored noun's app-owned edit keeps the standard subject parameter, but
// declares that a related row can supply its key through the existing From.
func subjectSourceFixture(t *testing.T) (*Runtime, *Palette, *[]string) {
	t.Helper()
	r, s := editFixture(t)
	s.things = append(s.things, Thing{"id": "t2", "title": "Other"})
	ran := new([]string)
	r.Define("parcel").Verb("edit", func(c Ctx) Result {
		key, err := r.subjectKey(r.Noun("parcel"), c)
		if err != nil {
			return Failed(err)
		}
		*ran = append(*ran, key)
		return Done(nil)
	}).Positional("id").Params(map[string]Parameter{
		"id": {Type: "string", Noun: "parcel", Focus: true, From: "row:parcel"},
	})
	r.Define("item").Mode("normal").Bind("e", Do("parcel.edit"))
	r.Instance("item-row", Kind("item")).Data = Thing{"parcel": "t1"}
	if err := r.SetFocus("item-row"); err != nil {
		t.Fatal(err)
	}
	return r, NewPalette(r), ran
}

func TestSubjectSourcePaletteRunsOverriddenStandardEdit(t *testing.T) {
	r, _, ran := subjectSourceFixture(t)
	paletteRequest(t, r, "open", "palette", nil)
	paletteLine(t, r, "parcel edit")
	if !reflect.DeepEqual(*ran, []string{"t1"}) || r.TopModal() != "" || r.Focus != "item-row" {
		t.Fatalf("ran %v, modal %q, focus %q", *ran, r.TopModal(), r.Focus)
	}
}

func TestSubjectSourceKeyAndArgsForm(t *testing.T) {
	r, _, ran := subjectSourceFixture(t)
	formKeys(t, r, "e")
	if !reflect.DeepEqual(*ran, []string{"t1"}) || r.TopModal() != "" {
		t.Fatalf("key: ran %v, modal %q", *ran, r.TopModal())
	}
	ed, err := r.OpenArgsForm(Request{Noun: "parcel", Verb: "edit"}, func(Request) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if ed.Values()["id"] != "t1" {
		t.Fatalf("form values %v", ed.Values())
	}
}

func TestSubjectSourceMissingStillAsks(t *testing.T) {
	for _, data := range []Thing{nil, {}, {"parcel": ""}, {"parcel": "   "}} {
		r, _, ran := subjectSourceFixture(t)
		r.Instances["item-row"].Data = data
		paletteRequest(t, r, "open", "palette", nil)
		paletteLine(t, r, "parcel edit")
		if r.TopModal() != ArgsFormID || r.Focus != ArgsFormID+"/id" || len(*ran) != 0 {
			t.Fatalf("data %v: ran %v, modal %q, focus %q", data, *ran, r.TopModal(), r.Focus)
		}
	}
}

func TestSubjectSourceExplicitArgumentAndTargetWin(t *testing.T) {
	t.Run("palette argument", func(t *testing.T) {
		r, _, ran := subjectSourceFixture(t)
		paletteRequest(t, r, "open", "palette", nil)
		paletteLine(t, r, "parcel edit t2")
		if !reflect.DeepEqual(*ran, []string{"t2"}) {
			t.Fatalf("ran %v", *ran)
		}
	})
	t.Run("target", func(t *testing.T) {
		r, _, ran := subjectSourceFixture(t)
		r.Instance("chosen", Kind("parcel")).Data = Thing{"id": "t2", "parcel": "t1"}
		if _, err := r.Execute(Request{Noun: "parcel", Verb: "edit", Target: "chosen"}); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(*ran, []string{"t2"}) {
			t.Fatalf("ran %v", *ran)
		}
	})
}

func TestSubjectSourceProgramDoesNotBorrowFocus(t *testing.T) {
	r, _, ran := subjectSourceFixture(t)
	if _, err := r.Execute(Request{Noun: "parcel", Verb: "edit"}); needed(err) == nil || len(*ran) != 0 {
		t.Fatalf("targetless program: err %v, ran %v", err, *ran)
	}
	if _, err := r.Execute(Request{Noun: "parcel", Verb: "edit", Target: "item-row"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*ran, []string{"t1"}) {
		t.Fatalf("targeted program ran %v", *ran)
	}
}

func TestSubjectSourcePrecedesFocusAssociation(t *testing.T) {
	r, _, ran := subjectSourceFixture(t)
	r.Define("item").Props(PropSpec{Name: "other", Association: true, Parameter: Parameter{Noun: "parcel"}})
	r.Instances["item-row"].Data = Thing{"parcel": "t1", "other": "t2"}
	r.Instance("other-row", Kind("parcel")).Data = Thing{"id": "t2"}
	formKeys(t, r, "e")
	if !reflect.DeepEqual(*ran, []string{"t1"}) || r.Focus != "item-row" {
		t.Fatalf("ran %v, focus %q", *ran, r.Focus)
	}
}

func TestSubjectSourceRequiresDeclaration(t *testing.T) {
	r, _, ran := subjectSourceFixture(t)
	op := r.Operations.At(Key("parcel.edit"))
	param := op.Parameters["id"]
	param.From = ""
	op.Parameters["id"] = param
	r.Operations.Set(op)
	paletteRequest(t, r, "open", "palette", nil)
	paletteLine(t, r, "parcel edit")
	if r.TopModal() != ArgsFormID || r.Focus != ArgsFormID+"/id" || len(*ran) != 0 {
		t.Fatalf("ran %v, modal %q, focus %q", *ran, r.TopModal(), r.Focus)
	}
}

func TestSubjectSourceStandardFieldForm(t *testing.T) {
	r, _, ran := subjectSourceFixture(t)
	paletteRequest(t, r, "open", "palette", nil)
	paletteLine(t, r, "parcel edit --field title")
	ed := r.RecordEditor(editFormID)
	if ed == nil || ed.Values()["title"] != "Original" || len(*ran) != 0 {
		t.Fatalf("field form %v, app edit calls %v", ed, *ran)
	}
	if err := ed.Leave(); err != nil {
		t.Fatal(err)
	}
	if r.Focus != "item-row" {
		t.Fatalf("returned to %q", r.Focus)
	}
}
