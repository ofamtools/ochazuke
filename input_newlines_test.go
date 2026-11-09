package ochazuke

import (
	"testing"
)

const nlPasted = "one\r\ntwo\nthree"
const nlWant = "one\ntwo\nthree"

func newlineInput(t *testing.T, grow bool) (*Runtime, *Input) {
	t.Helper()
	r := NewRuntime()
	r.Config().Disable()
	if err := r.Settings().Set(InputNewlinesGrowSetting, grow); err != nil {
		t.Fatal(err)
	}
	in := NewInput("one-line", "", false)
	if err := r.AddInput(in); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFocus(in.ID); err != nil {
		t.Fatal(err)
	}
	return r, in
}

func TestInputNewlinesGrowOnReplaceSetAndPut(t *testing.T) {
	t.Parallel()
	t.Run("replace", func(t *testing.T) {
		t.Parallel()
		_, in := newlineInput(t, true)
		if err := in.Replace(0, 0, nlPasted); err != nil {
			t.Fatal(err)
		}
		if !in.Multiline || string(in.Text) != nlWant {
			t.Fatalf("multiline %v text %q", in.Multiline, in.Text)
		}
	})
	t.Run("set", func(t *testing.T) {
		t.Parallel()
		_, in := newlineInput(t, true)
		in.SetText(nlPasted)
		if !in.Multiline || string(in.Text) != nlWant {
			t.Fatalf("multiline %v text %q", in.Multiline, in.Text)
		}
	})
	for _, shape := range []string{"char", "line"} {
		t.Run("put "+shape, func(t *testing.T) {
			t.Parallel()
			r, in := newlineInput(t, true)
			if err := r.storeYank(Yank{Text: "one\ntwo\nthree\n", Shape: shape}, "", false); err != nil {
				t.Fatal(err)
			}
			if _, err := r.Execute(Request{Noun: "input", Verb: "paste"}); err != nil {
				t.Fatal(err)
			}
			if !in.Multiline || len(in.Text) == 0 {
				t.Fatalf("multiline %v text %q", in.Multiline, in.Text)
			}
		})
	}
	t.Run("enter without newlines stays single-line", func(t *testing.T) {
		t.Parallel()
		_, in := newlineInput(t, true)
		if err := in.Replace(0, 0, "plain"); err != nil || in.Multiline {
			t.Fatalf("multiline %v err %v", in.Multiline, err)
		}
	})
}

func TestInputNewlinesBecomeSpacesWhenGrowIsOff(t *testing.T) {
	t.Parallel()
	_, in := newlineInput(t, false)
	if err := in.Replace(0, 0, nlPasted); err != nil {
		t.Fatal(err)
	}
	if in.Multiline || string(in.Text) != "one two three" {
		t.Fatalf("multiline %v text %q", in.Multiline, in.Text)
	}
	in.SetText("a\nb")
	if in.Multiline || string(in.Text) != "a b" {
		t.Fatalf("set: multiline %v text %q", in.Multiline, in.Text)
	}
	r, in2 := newlineInput(t, false)
	if err := r.storeYank(Yank{Text: "x\ny\n", Shape: "line"}, "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(Request{Noun: "input", Verb: "paste"}); err != nil {
		t.Fatal(err)
	}
	if in2.Multiline || string(in2.Text) != "x y " {
		t.Fatalf("put: multiline %v text %q", in2.Multiline, in2.Text)
	}
}

func TestRecordFieldAndArgsFormFieldGrowOnPastedNewlines(t *testing.T) {
	t.Parallel()
	r := NewRuntime()
	r.Config().Disable()
	ed, err := r.OpenRecordEditor("record", RecordSpec{Fields: []RecordField{{Name: "title"}}})
	if err != nil {
		t.Fatal(err)
	}
	f := ed.field("title")
	if f.input.Multiline {
		t.Fatal("starts multi-line")
	}
	if err := f.input.Replace(0, 0, nlPasted); err != nil {
		t.Fatal(err)
	}
	if !f.input.Multiline || string(f.input.Text) != nlWant {
		t.Fatalf("multiline %v text %q", f.input.Multiline, f.input.Text)
	}
	if got := ed.Values()["title"]; got != nlWant {
		t.Fatalf("value %q", got)
	}

	r2 := NewRuntime()
	r2.Config().Disable()
	r2.Define("note").Verb("add", func(Ctx) Result { return Done(nil) })
	op := r2.Operations.At(Key("note.add"))
	op.Parameters = map[string]Parameter{"text": {Type: "string", Required: true}}
	r2.Operations.Set(op)
	af, err := r2.OpenArgsForm(Request{Noun: "note", Verb: "add"}, func(Request) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	g := af.field("text")
	if g == nil || g.input == nil || g.input.Multiline {
		t.Fatalf("field %+v", g)
	}
	if err := g.input.Replace(0, 0, nlPasted); err != nil {
		t.Fatal(err)
	}
	if !g.input.Multiline || af.Values()["text"] != nlWant {
		t.Fatalf("multiline %v value %q", g.input.Multiline, af.Values()["text"])
	}
}
