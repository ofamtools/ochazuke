package ochazuke

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// fieldRefRig is a completion rig whose focus is the question field of a
// modal ask form: harness (enum), model (a Source over harness, ClearStale),
// a secret token and a read-only note; text.noun-refs on. A "model" noun
// with a picker holds things too, so a field can be seen to shadow it.
func fieldRefRig(t *testing.T, fields bool) (*completionRig, *RecordEditor) {
	t.Helper()
	g := nounRefRig(t, true)
	if err := g.r.Settings().Set(NounRefFieldsSetting, fields); err != nil {
		t.Fatal(err)
	}
	g.r.Define("model").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "title", Role: RoleLabel}).Store(DocStore(NewMemDocTable(g.r, "model"), "model"))
	if _, err := g.r.createNamed("model", "Noun Model"); err != nil {
		t.Fatal(err)
	}
	models := map[string][]string{"omp": {"GPT-6.2-Sol", "GPT-6.2 Astra", "Opus 5.5"}, "codex": {"GPT-6.2-Codex", "GPT-5"}}
	g.r.Register(Operation{Key: "ask.models", Parameters: map[string]Parameter{"harness": {Type: "string"}}, AppHandler: func(c Ctx) Result {
		return Done(models[StringArg(c.Args, "harness", "")])
	}})
	e, err := g.r.OpenRecordEditor("ask", RecordSpec{Fields: []RecordField{
		{Name: "question", Multiline: true},
		{Name: "harness", Parameter: Parameter{Type: "string", Enum: []string{"omp", "codex"}}},
		{Name: "model", Parameter: Parameter{Type: "string", Source: "ask.models", SourceArgs: map[string]string{"harness": "harness"}, ClearStale: true}},
		{Name: "token", Secret: true},
		{Name: "note", ReadOnly: true},
	}, Insert: true, Debounce: time.Nanosecond, Load: func() (Record, error) { return Record{"harness": "omp"}, nil }, Save: func(RecordWrite) Result { return Done(nil) }})
	if err != nil {
		t.Fatal(err)
	}
	g.in = e.field("question").input
	if g.r.Focus != g.in.ID || !typingMode(g.r, g.r.BaseMode()) {
		t.Fatalf("focus %q mode %q", g.r.Focus, g.r.Mode.Name)
	}
	g.drain()
	return g, e
}

func suggestionLabels(rows []Suggestion) []string {
	var out []string
	for _, s := range rows {
		out = append(out, s.Label)
	}
	return out
}

// "#" in a form lists its other fields first (not the one typed in, a
// secret or a read-only one), the field shadowing the model noun; "#mo"
// and tab leave "#model:", which lists the field's own choices for the
// form's harness (omp's, not codex's or the noun's); accepting one sets
// the field, takes the token out of the text and is undone with the
// form's undo; changing harness then clears model (ClearStale).
func TestNounRefFieldSetsField(t *testing.T) {
	g, e := fieldRefRig(t, true)
	g.typeText("test #")
	g.settle()
	got := suggestionLabels(g.c.Suggestions())
	if len(got) < 2 || got[0] != "harness" || got[1] != "model" || slices.Contains(got, "question") || slices.Contains(got, "token") || slices.Contains(got, "note") || slices.Index(got, "tag") < 2 {
		t.Fatalf("# lists %v", got)
	}
	if n := strings.Count(strings.Join(got, ","), "model"); n != 1 {
		t.Fatalf("model listed %d times (the field must shadow the noun): %v", n, got)
	}
	if d := g.c.Suggestions()[0].Detail; d != "omp" {
		t.Fatalf("harness detail %q", d)
	}
	g.typeText("mo")
	g.settle()
	g.keys("tab")
	g.answers()
	if string(g.in.Text) != "test #model:" {
		t.Fatalf("text %q", string(g.in.Text))
	}
	if got := suggestionLabels(g.c.Suggestions()); !slices.Equal(got, []string{"GPT-6.2-Sol", "GPT-6.2 Astra", "Opus 5.5"}) {
		t.Fatalf("#model: lists %v, want omp's models", got)
	}
	g.typeText("sol")
	g.settle()
	g.keys("tab")
	if string(g.in.Text) != "test" || g.c.Open() {
		t.Fatalf("after accept: text %q open %v", string(g.in.Text), g.c.Open())
	}
	if v := e.Values()["model"]; v != "GPT-6.2-Sol" || e.Written()["model"] != "GPT-6.2-Sol" {
		t.Fatalf("model %q written %q", v, e.Written()["model"])
	}
	if g.r.Focus != g.in.ID || !typingMode(g.r, g.r.BaseMode()) {
		t.Fatalf("focus moved: %q %q", g.r.Focus, g.r.Mode.Name)
	}
	// The form's undo walks the question's own edit first, then the
	// model it set.
	undos := 0
	for ; undos < 3 && e.Values()["model"] != ""; undos++ {
		if err := e.history(false); err != nil {
			t.Fatal(err)
		}
	}
	if e.Values()["model"] != "" {
		t.Fatalf("undo never reached the model: %v", e.Values())
	}
	for range undos {
		if err := e.history(true); err != nil {
			t.Fatal(err)
		}
	}
	if e.Values()["model"] != "GPT-6.2-Sol" {
		t.Fatalf("redo: model %q", e.Values()["model"])
	}
	h := e.field("harness")
	h.value = "codex"
	e.seen = e.snapshot()
	if err := e.seal(h); err != nil {
		t.Fatal(err)
	}
	if v := e.Values()["model"]; v != "" {
		t.Fatalf("harness changed but model kept %q", v)
	}
}

// "#model:62" narrows fuzzily (text.noun-refs-match, default fuzzy): the
// 6.2 models come first; substring finds none.
func TestNounRefFieldFuzzy(t *testing.T) {
	for _, mode := range []string{MatchFuzzy, MatchSubstring} {
		g, _ := fieldRefRig(t, true)
		if err := g.r.Settings().Set(NounRefsMatchSetting, mode); err != nil {
			t.Fatal(err)
		}
		g.typeText("#model:62")
		g.settle()
		got := suggestionLabels(g.c.Suggestions())
		if mode == MatchFuzzy && (len(got) < 2 || !strings.Contains(got[0], "6.2") || !strings.Contains(got[1], "6.2") || slices.Contains(got, "Opus 5.5")) {
			t.Fatalf("fuzzy: %v", got)
		}
		if mode == MatchSubstring && slices.ContainsFunc(got, func(s string) bool { return strings.Contains(s, "6.2") }) {
			t.Fatalf("substring: %v", got)
		}
	}
}

// With text.noun-refs-fields off, "#" in a form lists only nouns, and
// "#model:" is the model noun's reference, inserted as today (a chip).
func TestNounRefFieldsOff(t *testing.T) {
	g, e := fieldRefRig(t, false)
	g.typeText("#")
	g.settle()
	if got := suggestionLabels(g.c.Suggestions()); slices.Contains(got, "harness") || !slices.Contains(got, "model") {
		t.Fatalf("# lists %v", got)
	}
	g.typeText("model:nou")
	g.settle()
	g.keys("tab")
	if want := `#model:"Noun Model"`; string(g.in.Text) != want || e.Values()["model"] != "" {
		t.Fatalf("text %q model %q", string(g.in.Text), e.Values()["model"])
	}
}

// A noun with no field of its name stays a reference inside a form.
func TestNounRefInFormUnchanged(t *testing.T) {
	g, _ := fieldRefRig(t, true)
	g.typeText("see #tag:alp")
	g.settle()
	g.keys("tab")
	if want := "see #tag:Alpha"; string(g.in.Text) != want {
		t.Fatalf("text %q, want %q", string(g.in.Text), want)
	}
	if spans := g.r.NounRefsIn(g.in.Text); len(spans) != 1 || spans[0].Ref.Noun != "tag" {
		t.Fatalf("chips %+v", spans)
	}
}

// With modal.esc-closes on, esc with the # list open dismisses it and the
// field stays in insert; the next esc closes the form.
func TestNounRefEscSequence(t *testing.T) {
	g := nounRefRig(t, true)
	if err := g.r.Settings().Set(ModalEscClosesSetting, true); err != nil {
		t.Fatal(err)
	}
	e, err := g.r.OpenRecordEditor("ask", RecordSpec{Fields: []RecordField{{Name: "question", Multiline: true}, {Name: "harness", Parameter: Parameter{Type: "string", Enum: []string{"omp"}}}}, Insert: true, Debounce: time.Nanosecond, Save: func(RecordWrite) Result { return Done(nil) }})
	if err != nil {
		t.Fatal(err)
	}
	g.in = e.field("question").input
	g.drain()
	g.typeText("#")
	g.settle()
	if !g.c.Open() {
		t.Fatal("# list did not open")
	}
	g.keys("esc")
	if g.c.Open() || e.Closed() || !typingMode(g.r, g.r.BaseMode()) {
		t.Fatalf("first esc: open %v closed %v mode %q", g.c.Open(), e.Closed(), g.r.Mode.Name)
	}
	g.keys("esc")
	if e.Closed() || g.r.Mode.Name != "normal" {
		t.Fatalf("second esc: closed %v top %q mode %q", e.Closed(), g.r.TopModal(), g.r.Mode.Name)
	}
	g.keys("esc")
	if e.Closed() {
		t.Fatalf("third esc inside the cooldown closed the form")
	}
	if err := g.r.Settings().Set(ModalEscCooldownSetting, "0s"); err != nil {
		t.Fatal(err)
	}
	g.keys("esc")
	if !e.Closed() || g.r.TopModal() == "ask" {
		t.Fatalf("fourth esc (cooldown off): closed %v top %q mode %q", e.Closed(), g.r.TopModal(), g.r.Mode.Name)
	}
}
