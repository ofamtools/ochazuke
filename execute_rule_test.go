package ochazuke

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// ruleRuntime has a noun whose verbs are guarded off (the guard "never"),
// each recording that it ran.
func ruleRuntime(t *testing.T) (*Runtime, *[]string) {
	t.Helper()
	r := NewRuntime()
	ran := new([]string)
	rec := func(name string) Handler {
		return func(*Runtime, Context, Arguments) (any, error) { *ran = append(*ran, name); return name, nil }
	}
	r.DefineGuard("never", func(*Runtime, KeyEvent) bool { return false })
	r.Register(Operation{Key: "box.open", Run: rec("open")})
	r.Register(Operation{Key: "box.respawn", Run: rec("respawn")})
	r.Register(Operation{Key: "box.attach", Run: rec("attach"), Fallback: &Request{Noun: "box", Verb: "respawn"}})
	r.Register(Operation{Key: "box.start", Run: rec("start")})
	r.Register(Operation{Key: "box.serve", Run: rec("serve"), Needs: []Requirement{{Kind: "daemon", Name: "boxd", Met: "never", By: "box.start"}}})
	r.Register(Operation{Key: "box.build", Run: rec("build"), Needs: []Requirement{{Kind: "program", Name: "make", Met: "never", Setup: "setup install make"}}})
	for _, verb := range []string{"open", "attach", "serve", "build"} {
		r.SetAvailability("box."+verb, "never")
	}
	return r, ran
}

// An explicit request to a verb guarded off where it acts never answers
// "not applicable" (no-refusals D3): it runs; or its Fallback runs in
// place; or what it Needs is provided first; a need nothing provides
// answers a ChoiceAnswer naming the setup command.
func TestGuardedVerbsRunOnExplicitRequest(t *testing.T) {
	r, ran := ruleRuntime(t)
	for _, verb := range []string{"open", "attach", "serve"} {
		if _, err := r.Execute(Request{Noun: "box", Verb: verb}); err != nil {
			t.Fatalf("%s: %v", verb, err)
		}
	}
	if want := []string{"open", "respawn", "start", "serve"}; !slices.Equal(*ran, want) {
		t.Fatalf("ran %v, want %v", *ran, want)
	}
	_, err := r.Execute(Request{Noun: "box", Verb: "build"})
	var choice *ChoiceAnswer
	if !errors.As(err, &choice) || len(choice.Choices) != 1 || choice.Choices[0].Command != "setup install make" {
		t.Fatalf("build: %v", err)
	}
}

// The original verb's agent rights come before its Fallback and what it
// Needs; a check (CheckRequest) runs neither; the fallback asks its own
// confirm; a Need is provided where the guard holds too.
func TestFallbackAndNeedsAfterRights(t *testing.T) {
	r, ran := ruleRuntime(t)
	r.Register(Operation{Key: "box.mend", Run: func(*Runtime, Context, Arguments) (any, error) { *ran = append(*ran, "mend"); return nil, nil }, Confirm: "Mend?"})
	r.Register(Operation{Key: "box.fix", Run: func(*Runtime, Context, Arguments) (any, error) { return nil, nil }, Fallback: &Request{Noun: "box", Verb: "mend"}})
	r.SetAvailability("box.fix", "never")
	r.Register(Operation{Key: "box.use", Run: func(*Runtime, Context, Arguments) (any, error) { *ran = append(*ran, "use"); return nil, nil },
		Needs: []Requirement{{Kind: "daemon", Name: "boxd", Met: "never", By: "box.start"}}})
	for _, verb := range []string{"attach", "serve"} {
		if err := r.CheckRequest(Request{Noun: "box", Verb: verb}); err != nil {
			t.Fatalf("check %s: %v", verb, err)
		}
	}
	if len(*ran) != 0 {
		t.Fatalf("a check ran %v", *ran)
	}
	if _, err := r.Execute(Request{Noun: "box", Verb: "fix", Confirmed: true}); err != nil || r.TopModal() == "" || len(*ran) != 0 {
		t.Fatalf("fix: %v top %q ran %v", err, r.TopModal(), *ran)
	}
	if err := r.CloseModal(r.contextOf(r.TopModal())); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(Request{Noun: "box", Verb: "use"}); err != nil || !slices.Equal(*ran, []string{"start", "use"}) {
		t.Fatalf("use: %v ran %v", err, *ran)
	}
	*ran = nil
	if err := r.Settings().Set(AgentRightsSetting, `[{"match":"box.attach","right":"never"}]`); err != nil {
		t.Fatal(err)
	}
	defer r.BindActor(testAgent("p"), false)()
	defer r.WithActor(testAgent("p"))()
	var refused *RefusedError
	if _, err := r.Execute(Request{Noun: "box", Verb: "attach"}); !errors.As(err, &refused) || len(*ran) != 0 {
		t.Fatalf("agent attach: %v ran %v", err, *ran)
	}
}

// A component declares a verb not usual on it (Unoffers): availability
// (keys, hints) leaves it out there, an explicit request runs it, and the
// palette lists it after the usual verbs, marked; palette.reach off (the
// opt-out) leaves it out of the palette.
func TestUnofferedVerbs(t *testing.T) {
	for _, reach := range []bool{true, false} {
		r, p, ran := lineRuntime(t)
		if err := r.Settings().Set(PaletteReachSetting, reach); err != nil {
			t.Fatal(err)
		}
		r.Unoffer("sheet", "sheet.save")
		if r.Available(Request{Noun: "sheet", Verb: "save"}, "r1:c1", r.Mode) {
			t.Fatal("sheet.save is available where the sheet unoffers it")
		}
		if _, err := r.Execute(Request{Noun: "sheet", Verb: "save"}); err != nil || len(*ran) != 1 {
			t.Fatalf("explicit save: %v %v", err, *ran)
		}
		typeLine(t, p, "sheet ")
		var unusual []string
		for _, e := range p.completions() {
			if e.Unusual {
				unusual = append(unusual, e.Value)
			}
		}
		if got := slices.Contains(unusual, "save"); got != reach {
			t.Fatalf("reach %v: unusual %v", reach, unusual)
		}
	}
}

// verbs.disabled: a verb whose feature setting is off runs by name (run),
// or is refused and left out of the palette (hide).
func TestVerbsDisabledSetting(t *testing.T) {
	for _, value := range []string{"run", "hide"} {
		r, _ := ruleRuntime(t)
		if err := r.DeclareSetting(Setting{Name: "box.feature", Type: SettingBoolean, Default: false}); err != nil {
			t.Fatal(err)
		}
		r.Register(Operation{Key: "box.extra", EnabledSetting: "box.feature", Run: func(*Runtime, Context, Arguments) (any, error) { return "ran", nil }})
		if err := r.Settings().Set(VerbsDisabledSetting, value); err != nil {
			t.Fatal(err)
		}
		_, err := r.Execute(Request{Noun: "box", Verb: "extra"})
		if (err == nil) != (value == "run") {
			t.Fatalf("%s: %v", value, err)
		}
	}
}

// cli.unknown-name: a name no thing has answers the close names with a
// command each (choices), or creates the thing (create).
func TestUnknownNameSetting(t *testing.T) {
	for _, value := range []string{"choices", "create"} {
		r := NewRuntime()
		r.defineFacts(factsSpec{Noun: "tag", Key: "id", Extra: []PropSpec{{Name: "id"}},
			List: func(Ctx, ListQuery) ([]Thing, error) { return []Thing{{"id": "alpha"}}, nil }})
		r.Register(Operation{Key: "tag.use", Parameters: map[string]Parameter{"tag": {Type: "string", Source: "tag.list"}}, Run: func(*Runtime, Context, Arguments) (any, error) { return "used", nil }})
		if err := r.Settings().Set(UnknownNameSetting, value); err != nil {
			t.Fatal(err)
		}
		_, err := r.Execute(Request{Noun: "tag", Verb: "use", Args: Arguments{"tag": "alphb"}})
		var choice *ChoiceAnswer
		switch value {
		case "choices":
			if !errors.As(err, &choice) || len(choice.Choices) != 1 || !strings.Contains(choice.Choices[0].Command, "--tag alpha") {
				t.Fatalf("choices: %v %+v", err, choice)
			}
		case "create":
			if err != nil {
				t.Fatalf("create: %v", err)
			}
			if _, err := r.Execute(Request{Noun: "tag", Verb: "get", Args: Arguments{"id": "alphb"}}); err != nil {
				t.Fatalf("created tag: %v", err)
			}
		}
	}
}

// cli.unknown-name create makes the thing only for a request that runs:
// not for a check, not for an agent the verb's rights refuse.
func TestUnknownNameCreatesOnlyWhenTheVerbRuns(t *testing.T) {
	r := NewRuntime()
	r.Define("tag").Props(PropSpec{Name: "id", Key: true, CreateOnly: true}, PropSpec{Name: "title", Role: RoleLabel}).Store(DocStore(NewMemDocTable(r, "tag"), "tag"))
	mustRun(t, r, Request{Noun: "tag", Verb: "create", Args: Arguments{"id": "alpha"}})
	r.Register(Operation{Key: "tag.use", Parameters: map[string]Parameter{"tag": {Type: "string", Source: "tag.list"}}, Run: func(*Runtime, Context, Arguments) (any, error) { return "used", nil }})
	if err := r.Settings().Set(UnknownNameSetting, "create"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(Request{Noun: "tag", Verb: "use", Args: Arguments{"tag": "delta"}}); err != nil {
		t.Fatalf("owner use: %v", err)
	}
	exists := func(id string) bool {
		_, err := r.Execute(Request{Noun: "tag", Verb: "get", Args: Arguments{"id": id}})
		return err == nil
	}
	if err := r.CheckRequest(Request{Noun: "tag", Verb: "use", Args: Arguments{"tag": "beta"}}); err != nil || exists("beta") {
		t.Fatalf("check: %v, created %v", err, exists("beta"))
	}
	if err := r.Settings().Set(AgentRightsSetting, `[{"match":"tag.use","right":"never"}]`); err != nil {
		t.Fatal(err)
	}
	restore, unbind := r.WithActor(testAgent("p")), r.BindActor(testAgent("p"), false)
	_, err := r.Execute(Request{Noun: "tag", Verb: "use", Args: Arguments{"tag": "gamma"}})
	unbind()
	restore()
	if err == nil || exists("gamma") || !exists("delta") {
		t.Fatalf("refused agent: %v, created %v; owner's delta %v", err, exists("gamma"), exists("delta"))
	}
}

// A delete undo fully restores asks nothing first (no-refusals D7), and
// only there: a one-shot process (no session to undo in), a store that
// says what a delete costs (ConfirmFor) and a verb that Loses ask, an
// agent whose right is allow included.
func TestUndoableDeleteAsksOnlyWhereUndoRestores(t *testing.T) {
	for _, c := range []struct {
		name                  string
		session, costs, agent bool
		loses                 string
		asks                  bool
	}{
		{name: "session", session: true},
		{name: "one-shot", asks: true},
		{name: "store cost", session: true, costs: true, asks: true},
		{name: "loses, allowed agent", session: true, agent: true, loses: "the tag", asks: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := NewRuntime()
			if c.session {
				r.Settings().StartSession()
			}
			store := DocStore(NewMemDocTable(r, "tag"), "tag")
			if c.costs {
				store.ConfirmFor = func(_ Ctx, _ string, _ []string, prompt string) string { return prompt }
			}
			r.Define("tag").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "title", Role: RoleLabel}).Store(store)
			made := mustRun(t, r, Request{Noun: "tag", Verb: "create", Args: Arguments{"title": "alpha"}}).(map[string]any)
			verb := "delete"
			if c.loses != "" {
				verb = "purge"
				r.Register(Operation{Key: "tag.purge", Confirm: "Purge?", Loses: c.loses, Parameters: map[string]Parameter{"id": {Type: "string"}},
					Run: func(*Runtime, Context, Arguments) (any, error) { return nil, nil }})
			}
			if c.agent {
				if err := r.Settings().Set(AgentRightsSetting, `[{"match":"tag.purge","right":"allow"}]`); err != nil {
					t.Fatal(err)
				}
				defer r.BindActor(testAgent("p"), true)()
				defer r.WithActor(testAgent("p"))()
			}
			if _, err := r.Execute(Request{Noun: "tag", Verb: verb, Args: Arguments{"id": made["id"]}, Confirmed: c.agent}); err != nil {
				t.Fatal(err)
			}
			if asked := r.TopModal() != ""; asked != c.asks {
				t.Fatalf("asked %v, want %v (top %q)", asked, c.asks, r.TopModal())
			}
		})
	}
}

// pickers.create-row: a picker of a noun that can create offers a create
// row (on), or lists only the things there are (off); the row makes the
// thing named by the text.
func TestPickersCreateRowSetting(t *testing.T) {
	for _, on := range []bool{true, false} {
		r := NewRuntime()
		r.Define("tag").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "title", Role: RoleLabel}).Store(DocStore(NewMemDocTable(r, "tag"), "tag"))
		if err := r.Settings().Set(PickersCreateRowSetting, on); err != nil {
			t.Fatal(err)
		}
		if got := r.pickerCreates("tag"); got != on {
			t.Fatalf("%v: create row %v", on, got)
		}
	}
	r := NewRuntime()
	r.Define("tag").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "title", Role: RoleLabel}).Store(DocStore(NewMemDocTable(r, "tag"), "tag"))
	id, err := r.createNamed("tag", "Fresh")
	if err != nil || id == "" {
		t.Fatalf("createNamed: %q %v", id, err)
	}
	if got, _ := r.Execute(Request{Noun: "tag", Verb: "get", Args: Arguments{"id": id, "prop": "title"}}); got != "Fresh" {
		t.Fatalf("made %v", got)
	}
}
