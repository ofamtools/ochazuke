package ochazuke

import (
	"fmt"
	"reflect"
	"slices"
	"testing"
)

// targetFixture: a page holding a box (associated with the thing f, which
// lives off the page) that holds a list of the things a, b and c, and the
// thing o on the page; the focus is on row b; history names the thing r.
// The verb thing.poke takes a required thing.
func targetFixture(t *testing.T) *Runtime {
	t.Helper()
	r := NewRuntime()
	r.Config().Disable()
	r.DefineOwn("thing").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "name", Role: RoleLabel})
	r.Define("box").Props(PropSpec{Name: "subject", Association: true, Parameter: Parameter{Noun: "thing"}})
	if _, err := r.AddPage("page", "Page", func(*Runtime, int, int) Block { return Text("page") }); err != nil {
		t.Fatal(err)
	}
	r.Instance("box", Kind("box"), Parent("page")).Data = map[string]any{"subject": "f"}
	row := func(id string) ListItem {
		return ListItem{ID: "row-" + id, Noun: "thing", Label: id, Data: map[string]any{"id": id, "name": id}}
	}
	if _, err := r.AddList("things", []ListItem{row("a"), row("b"), row("c")}, Parent("box")); err != nil {
		t.Fatal(err)
	}
	r.Instance("far", Kind("thing")).Data = map[string]any{"id": "f", "name": "Far"}
	r.Instance("other", Kind("thing"), Parent("page")).Data = map[string]any{"id": "o", "name": "Other"}
	r.Instance("leaf", Kind("box"), Parent("other"))
	r.history.entries = append(r.history.entries, HistoryEntry{Seq: 1, Kind: HistoryChange, Things: []string{Ref{Noun: "thing", Key: "r"}.String()}})
	r.Define("thing").Verb("poke", func(Ctx) Result { return Done(nil) }, "Poke").Params(map[string]Parameter{"thing": {Type: "string", Required: true, Noun: "thing"}})
	if err := r.OpenPage("page"); err != nil {
		t.Fatal(err)
	}
	if err := r.List("things").Select("row-b"); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFocus("row-b"); err != nil {
		t.Fatal(err)
	}
	return r
}

// ranked is a target's candidates as "ID why", in rank order.
func ranked(t Target) []string {
	out := []string{}
	for _, c := range t.Candidates {
		out = append(out, c.ID+" "+string(c.Why))
	}
	return out
}

// Each source gives its candidates with its reason, in rank order: the
// selection, the marks, the focus, the focus path, the page, what the path
// reaches, the history.
func TestTargetSourcesRankByWhy(t *testing.T) {
	poke := func(r *Runtime) Target {
		op, _ := r.Operation("thing.poke")
		return r.targetOf(op, "thing", r.Focus, nil)
	}
	for _, c := range []struct {
		why   Why
		setup func(t *testing.T, r *Runtime)
		want  []string
	}{
		{WhySelected, func(t *testing.T, r *Runtime) {
			if err := r.List("things").StartVisual(); err != nil {
				t.Fatal(err)
			}
		}, []string{"b selected", "a on-page", "c on-page", "o on-page", "f linked", "r recent"}},
		{WhyMarked, func(t *testing.T, r *Runtime) {
			if err := r.List("things").ToggleMark("row-c"); err != nil {
				t.Fatal(err)
			}
		}, []string{"c marked", "b focused", "a on-page", "o on-page", "f linked", "r recent"}},
		{WhyFocused, func(*testing.T, *Runtime) {}, []string{"b focused", "a on-page", "c on-page", "o on-page", "f linked", "r recent"}},
		{WhyOnPath, func(t *testing.T, r *Runtime) {
			if err := r.SetFocus("leaf"); err != nil {
				t.Fatal(err)
			}
		}, []string{"o on-path", "a on-page", "b on-page", "c on-page", "r recent"}},
		{WhyOnPage, func(*testing.T, *Runtime) {}, []string{"b focused", "a on-page", "c on-page", "o on-page", "f linked", "r recent"}},
		{WhyLinked, func(t *testing.T, r *Runtime) {
			if err := r.SetFocus("box"); err != nil {
				t.Fatal(err)
			}
		}, []string{"a on-page", "b on-page", "c on-page", "o on-page", "f linked", "r recent"}},
		{WhyRecent, func(*testing.T, *Runtime) {}, []string{"b focused", "a on-page", "c on-page", "o on-page", "f linked", "r recent"}},
	} {
		t.Run(string(c.why), func(t *testing.T) {
			r := targetFixture(t)
			c.setup(t, r)
			got := poke(r)
			if !reflect.DeepEqual(ranked(got), c.want) {
				t.Fatalf("candidates %v, want %v", ranked(got), c.want)
			}
			if got.Given || got.All != "" && got.All != "thing list" {
				t.Fatalf("target %+v", got)
			}
			if got.Candidates[0].Command != "thing poke --thing "+got.Candidates[0].ID {
				t.Fatalf("command %q", got.Candidates[0].Command)
			}
		})
	}
}

// A value the request gives wins over every candidate, whatever is
// selected, focused or linked.
func TestTargetGivenWins(t *testing.T) {
	r := targetFixture(t)
	if err := r.List("things").StartVisual(); err != nil {
		t.Fatal(err)
	}
	op, _ := r.Operation("thing.poke")
	got := r.targetOf(op, "thing", r.Focus, Arguments{"thing": "zz"})
	if !got.Given || got.Chosen == nil || got.Chosen.ID != "zz" || got.Chosen.Why != WhyGiven || len(got.Candidates) != 0 {
		t.Fatalf("given lost: %+v", got)
	}
	if got := r.targetOf(op, "thing", r.Focus, nil); got.Chosen == nil || got.Chosen.ID != "b" || got.Chosen.Why != WhySelected {
		t.Fatalf("without a value the selection acts: %+v", got.Chosen)
	}
}

// One, two or N candidates take the same path: a request lacking its thing
// answers needs / target on every route, only its choices differ.
func TestTargetOneTwoManySamePath(t *testing.T) {
	for _, n := range []int{1, 2, 5} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			r := NewRuntime()
			r.Config().Disable()
			r.DefineOwn("thing").Props(PropSpec{Name: "id", Key: true})
			if _, err := r.AddPage("page", "Page", func(*Runtime, int, int) Block { return Text("") }); err != nil {
				t.Fatal(err)
			}
			for i := range n {
				r.Instance(fmt.Sprintf("t%d", i), Kind("thing"), Parent("page")).Data = map[string]any{"id": fmt.Sprintf("k%d", i)}
			}
			r.Define("thing").Verb("poke", func(Ctx) Result { return Done(nil) }, "Poke").Params(map[string]Parameter{"thing": {Type: "string", Required: true, Noun: "thing"}})
			if err := r.OpenPage("page"); err != nil {
				t.Fatal(err)
			}
			if err := r.SetFocus("page"); err != nil {
				t.Fatal(err)
			}
			o := r.ExecuteOutcome(Request{Noun: "thing", Verb: "poke"})
			if o.Kind != OutcomeNeeds || o.Cause != CauseTarget || o.Needs.Target == nil || o.Needs.Target.Chosen != nil || len(o.Needs.Choices) != n {
				t.Fatalf("%d candidates: %+v needs %+v", n, o, o.Needs)
			}
			if o.Needs.Choices[0].Command != "thing poke --thing k0" || o.Needs.Choices[0].Why != WhyOnPage {
				t.Fatalf("choice %+v", o.Needs.Choices[0])
			}
		})
	}
}

// target.linked, one value each: a verb that reads (peek) and one that
// changes (poke) on a box associated with the thing f, from a key.
func TestTargetLinkedSetting(t *testing.T) {
	for _, c := range []struct {
		value      string
		peek, poke bool
	}{
		{"ask-on-change", true, false},
		{"act", true, true},
		{"always-pick", false, false},
	} {
		t.Run(c.value, func(t *testing.T) {
			r := targetFixture(t)
			if err := r.Settings().Set(TargetLinkedSetting, c.value); err != nil {
				t.Fatal(err)
			}
			acted := map[string]string{}
			for _, verb := range []string{"peek", "touch"} {
				r.Define("thing").Verb(verb, func(c Ctx) Result { acted[verb] = c.Instance.ID; return Done(nil) }).ByAssociation()
			}
			op := r.Operations.At(Key("thing.peek"))
			op.HistoryKind = HistoryFetch
			r.Operations.Set(op)
			if err := r.SetFocus("box"); err != nil {
				t.Fatal(err)
			}
			defer r.Sourced("key:r")()
			for verb, want := range map[string]bool{"peek": c.peek, "touch": c.poke} {
				_, _ = r.Execute(Request{Noun: "thing", Verb: verb})
				if (acted[verb] == "far") != want {
					t.Errorf("%s acted on %q, want far %v", verb, acted[verb], want)
				}
			}
			touch, _ := r.Operation("thing.touch")
			if chosen := r.targetOf(touch, "target", "box", nil).Chosen; (chosen != nil) != c.poke {
				t.Errorf("touch's target chose %+v", chosen)
			}
		})
	}
}

// ask.target, one value each: a key whose verb lacks its thing opens the
// args form, with the field's picker on top (picker) or not (form).
func TestAskTargetSetting(t *testing.T) {
	for value, picker := range map[string]bool{"form": false, "picker": true} {
		t.Run(value, func(t *testing.T) {
			r, _, ran := subjectAskRuntime(t, Parameter{Type: "string", Required: true, Noun: "swatch"}, false)
			if err := r.Settings().Set(AskTargetSetting, value); err != nil {
				t.Fatal(err)
			}
			formKeys(t, r, "z")
			if r.RecordEditor(ArgsFormID) == nil || (r.Chooser(FormSuggestionsKind) != nil) != picker || len(*ran) != 0 {
				t.Fatalf("form %v picker %v, want picker %v", r.RecordEditor(ArgsFormID) != nil, r.Chooser(FormSuggestionsKind) != nil, picker)
			}
		})
	}
}

// cli.choices-max: a program is listed at most that many candidates, then
// how many more there are; 0 lists every one.
func TestChoicesMaxSetting(t *testing.T) {
	for _, c := range []struct{ max, shown, more int }{{2, 2, 4}, {0, 6, 0}} {
		r := targetFixture(t)
		if err := r.Settings().Set(ChoicesMaxSetting, c.max); err != nil {
			t.Fatal(err)
		}
		o := r.ChoicesShown(r.ExecuteOutcome(Request{Noun: "thing", Verb: "poke"}), Request{Noun: "thing", Verb: "poke"})
		if len(o.Needs.Choices) != c.shown || o.Needs.More != c.more || len(o.Needs.Target.Candidates) != c.shown {
			t.Errorf("max %d: %d shown, %d more", c.max, len(o.Needs.Choices), o.Needs.More)
		}
		if !slices.ContainsFunc(o.Needs.Choices, func(row ChoiceRow) bool { return row.Why == WhyFocused }) {
			t.Errorf("max %d: the focused thing is not first: %+v", c.max, o.Needs.Choices)
		}
	}
}
