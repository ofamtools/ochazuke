package ochazuke

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// formKeys dispatches keys wherever the focus is (the args form, its
// chooser, the palette).
func formKeys(t *testing.T, r *Runtime, keys ...string) {
	t.Helper()
	for _, key := range keys {
		if _, err := r.Dispatch(MacroKey{Key: key}); err != nil {
			t.Fatalf("key %q: %v", key, err)
		}
	}
}

func typeKeys(t *testing.T, r *Runtime, text string) {
	t.Helper()
	for _, ch := range text {
		key := string(ch)
		if ch == ' ' {
			key = "space"
		}
		formKeys(t, r, key)
	}
}

// formRuntime is lineRuntime plus a rename with two required words, a
// noun-typed pick and a tag whose optional colour has choices.
func formRuntime(t *testing.T) (*Runtime, *Palette, *[]string) {
	t.Helper()
	r, p, ran := lineRuntime(t)
	run := r.Operations["note.add"].Run
	r.Register(Operation{Verb: "rename", Noun: "sheet", Positional: []string{"from", "to"}, Parameters: map[string]Parameter{
		"from": {Type: "string", Required: true},
		"to":   {Type: "string", Required: true, Description: "The new name"},
	}, Run: run})
	r.Define("swatch")
	r.Instance("s1", Parent("sheet"), Kind("swatch")).Label = "Red"
	r.Instance("s2", Parent("sheet"), Kind("swatch")).Label = "Blue"
	r.Register(Operation{Verb: "pick", Noun: "sheet", Parameters: map[string]Parameter{"swatch": {Type: "string", Required: true, Noun: "swatch"}}, Run: run})
	return r, p, ran
}

func TestPaletteMissingParamOpensArgsForm(t *testing.T) {
	r, p, ran := formRuntime(t)
	paletteRequest(t, r, "set", "palette", Arguments{"text": "note add"})
	paletteRequest(t, r, "run", "palette", nil)
	if r.TopModal() != ArgsFormID || r.Focus != ArgsFormID+"/text" || r.Mode.Name != "insert" {
		t.Fatalf("form not open on text in insert: top=%q focus=%q mode=%q", r.TopModal(), r.Focus, r.Mode.Name)
	}
	ed := r.RecordEditor(ArgsFormID)
	if ed.Runnable() {
		t.Fatal("an empty required field must not be runnable")
	}
	typeKeys(t, r, "hi there")
	formKeys(t, r, "enter")
	if p.IsOpen() || r.TopModal() != "" || r.Focus != "r1:c1" {
		t.Fatalf("after run: palette open=%v top=%q focus=%q", p.IsOpen(), r.TopModal(), r.Focus)
	}
	if want := []string{`r1:c1 text="hi there"`}; !reflect.DeepEqual(*ran, want) {
		t.Fatalf("ran %v, want %v", *ran, want)
	}
	if !ed.Ran() {
		t.Fatal("the editor did not report it ran")
	}
}

func TestArgsFormPrefillsTypedWordsAndStartsOnTheMissingOne(t *testing.T) {
	r, _, ran := formRuntime(t)
	paletteLine(t, r, "sheet rename old")
	ed := r.RecordEditor(ArgsFormID)
	if ed == nil || r.Focus != ArgsFormID+"/to" {
		t.Fatalf("focus %q, want the missing to field", r.Focus)
	}
	if got := ed.Values(); got["from"] != "old" || got["to"] != "" {
		t.Fatalf("values %v", got)
	}
	// enter on an empty required field stays on it.
	formKeys(t, r, "enter")
	if r.Focus != ArgsFormID+"/to" || r.TopModal() != ArgsFormID || len(*ran) != 0 {
		t.Fatalf("enter on an empty required field: focus=%q top=%q ran=%v", r.Focus, r.TopModal(), *ran)
	}
	typeKeys(t, r, "new")
	formKeys(t, r, "enter")
	if want := []string{`sheet from="old" to="new"`}; !reflect.DeepEqual(*ran, want) {
		t.Fatalf("ran %v, want %v", *ran, want)
	}
}

// enter on a valid field moves to the field that still needs a value.
func TestArgsFormEnterMovesToTheNextMissingField(t *testing.T) {
	r, _, ran := formRuntime(t)
	paletteLine(t, r, "sheet rename")
	if r.Focus != ArgsFormID+"/from" {
		t.Fatalf("focus %q", r.Focus)
	}
	typeKeys(t, r, "a")
	formKeys(t, r, "enter")
	if r.Focus != ArgsFormID+"/to" || r.Mode.Name != "insert" || len(*ran) != 0 {
		t.Fatalf("focus=%q mode=%q ran=%v", r.Focus, r.Mode.Name, *ran)
	}
	typeKeys(t, r, "b")
	formKeys(t, r, "enter")
	if len(*ran) != 1 {
		t.Fatalf("ran %v", *ran)
	}
}

func TestArgsFormNounParamAutocompletes(t *testing.T) {
	r, _, ran := formRuntime(t)
	paletteLine(t, r, "sheet pick")
	if r.Focus != ArgsFormID+"/swatch" || r.Mode.Name != "normal" {
		t.Fatalf("focus=%q mode=%q", r.Focus, r.Mode.Name)
	}
	ed := r.RecordEditor(ArgsFormID)
	if got := ed.fields[0].choices; len(got) != 2 || got[0].ID != "s1" {
		t.Fatalf("noun choices %v", got)
	}
	formKeys(t, r, "enter")
	c := r.Chooser(FormSuggestionsKind)
	if c == nil {
		t.Fatalf("no chooser: top=%q", r.TopModal())
	}
	typeKeys(t, r, "blu")
	formKeys(t, r, "enter")
	if r.Focus != ArgsFormID+"/swatch" || ed.Values()["swatch"] != "s2" {
		t.Fatalf("after pick: focus=%q values=%v", r.Focus, ed.Values())
	}
	formKeys(t, r, "enter")
	if want := []string{`sheet swatch="s2"`}; !reflect.DeepEqual(*ran, want) {
		t.Fatalf("ran %v, want %v", *ran, want)
	}
}

func TestArgsFormChoicesParam(t *testing.T) {
	r, _, ran := formRuntime(t)
	paletteLine(t, r, "row transform")
	if r.Focus != ArgsFormID+"/name" {
		t.Fatalf("focus %q", r.Focus)
	}
	formKeys(t, r, "enter")
	if r.Chooser(FormSuggestionsKind) == nil {
		t.Fatal("enter on a missing choice field opens its chooser")
	}
	typeKeys(t, r, "low")
	formKeys(t, r, "enter", "enter")
	if want := []string{`r1 name="lower"`}; !reflect.DeepEqual(*ran, want) {
		t.Fatalf("ran %v, want %v", *ran, want)
	}
}

func TestArgsFormEscCancelsBackToTheLine(t *testing.T) {
	r, p, ran := formRuntime(t)
	paletteLine(t, r, "sheet rename old")
	typeKeys(t, r, "x")
	formKeys(t, r, "esc", "esc")
	if r.TopModal() != paletteID || p.Text() != "sheet rename old" || len(*ran) != 0 {
		t.Fatalf("top=%q line=%q ran=%v", r.TopModal(), p.Text(), *ran)
	}
	if r.Instances[ArgsFormID] != nil {
		t.Fatal("the form is still an instance")
	}
}

func TestArgsFormSettingOffNamesTheMissingWord(t *testing.T) {
	r, p, ran := formRuntime(t)
	if err := r.DeclareSetting(Setting{Name: MissingOpensFormSetting, Type: SettingBoolean, Default: false}); err != nil {
		t.Fatal(err)
	}
	paletteRequest(t, r, "set", "palette", Arguments{"text": "note add"})
	_, err := r.Execute(Request{Verb: "run", Noun: "ochazuke.palette"})
	if err == nil || !strings.Contains(err.Error(), "missing text (string)") {
		t.Fatalf("err %v", err)
	}
	if r.TopModal() != paletteID || p.Text() != "note add" || len(*ran) != 0 {
		t.Fatalf("top=%q line=%q", r.TopModal(), p.Text())
	}
}

func TestArgsFormForABindingWithMissingArgs(t *testing.T) {
	r, p, ran := formRuntime(t)
	p.Close()
	if err := r.Bind(Binding{Noun: "app", Mode: "normal", Keys: "R", Request: Request{Noun: "sheet", Verb: "rename", Args: Arguments{"from": "a"}}}); err != nil {
		t.Fatal(err)
	}
	formKeys(t, r, "R")
	if r.TopModal() != ArgsFormID || r.Focus != ArgsFormID+"/to" {
		t.Fatalf("top=%q focus=%q", r.TopModal(), r.Focus)
	}
	typeKeys(t, r, "b")
	formKeys(t, r, "enter")
	if want := []string{`r1:c1 from="a" to="b"`}; !reflect.DeepEqual(*ran, want) {
		t.Fatalf("ran %v, want %v", *ran, want)
	}
	if r.Focus != "r1:c1" || r.Mode.Name != "normal" {
		t.Fatalf("focus=%q mode=%q", r.Focus, r.Mode.Name)
	}
	i := slices.IndexFunc(r.history.entries, func(e HistoryEntry) bool { return e.Request.Key() == "sheet.rename" })
	if i < 0 || r.history.entries[i].Context.Source != "key:R" {
		t.Fatalf("history %+v", r.history.entries)
	}
}

func TestArgsFormNeverForTheAPI(t *testing.T) {
	r, _, ran := formRuntime(t)
	_, err := r.Execute(Request{Noun: "note", Verb: "add"})
	var bad *ArgumentError
	if !errors.As(err, &bad) || !strings.Contains(err.Error(), "text is required") {
		t.Fatalf("err %v", err)
	}
	if r.Instances[ArgsFormID] != nil || len(*ran) != 0 {
		t.Fatal("the API opened a form")
	}
}

// A form that runs once never calls Save, and a failed run reopens it
// with the values and the error.
func TestRecordSubmitFailureReopens(t *testing.T) {
	r := NewRuntime()
	r.Instance("a", Kind("x"))
	_ = r.SetFocus("a")
	saved, calls := 0, 0
	spec := RecordSpec{Fields: []RecordField{{Name: "n", Parameter: Parameter{Type: "string", Required: true}}}, Insert: true,
		Save: func(RecordWrite) Result { saved++; return Done(nil) },
		Submit: func(Record) Result {
			calls++
			if calls == 1 {
				return Failed(errors.New("boom"))
			}
			return Done(nil)
		}}
	if _, err := r.OpenRecordEditor("f", spec); err != nil {
		t.Fatal(err)
	}
	typeKeys(t, r, "v")
	if _, err := r.Dispatch(MacroKey{Key: "enter"}); err == nil {
		t.Fatal("a failed run says so")
	}
	ed := r.RecordEditor("f")
	if ed == nil || r.TopModal() != "f" || ed.Values()["n"] != "v" || ed.WriteError() != "boom" {
		t.Fatalf("not reopened: %v", r.TopModal())
	}
	if !strings.Contains(ed.Block(40, 10).Text, "! not run: boom") {
		t.Fatalf("block %q", ed.Block(40, 10).Text)
	}
	formKeys(t, r, "enter")
	if calls != 2 || r.TopModal() != "" || saved != 0 {
		t.Fatalf("calls=%d top=%q saved=%d", calls, r.TopModal(), saved)
	}
}

// A noun-typed field is a choice field over the noun's things (it was
// free text before).
func TestRecordFieldsNounParamIsAChoice(t *testing.T) {
	r := NewRuntime()
	r.Define("swatch")
	r.Instance("s1", Kind("swatch"))
	r.Instance("a", Kind("x"))
	_ = r.SetFocus("a")
	op := Operation{Noun: "x", Verb: "pick", Parameters: map[string]Parameter{"swatch": {Type: "string", Required: true, Noun: "swatch"}}}
	ed, err := r.OpenRecordEditor("f", RecordSpec{Fields: RecordFields(op)})
	if err != nil {
		t.Fatal(err)
	}
	f := ed.fields[0]
	if !f.choose || f.input != nil || len(f.choices) != 1 || f.choices[0].ID != "s1" {
		t.Fatalf("field choose=%v choices=%v", f.choose, f.choices)
	}
	if r.Instances["f/swatch"].Noun != FormChoiceNoun {
		t.Fatalf("noun %q", r.Instances["f/swatch"].Noun)
	}
}
