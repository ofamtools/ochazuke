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

// A delete undo fully restores asks nothing first (no-refusals D7).
func TestUndoableDeleteDoesNotAsk(t *testing.T) {
	r := NewRuntime()
	table := NewMemDocTable(r, "tag")
	r.Define("tag").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "title", Role: RoleLabel}).Store(DocStore(table, "tag"))
	made := mustRun(t, r, Request{Noun: "tag", Verb: "create", Args: Arguments{"title": "alpha"}}).(map[string]any)
	op, _ := r.Operation("tag.delete")
	if !r.undoRestores(op) {
		t.Skip("the doc store records no undo here")
	}
	if _, err := r.Execute(Request{Noun: "tag", Verb: "delete", Args: Arguments{"id": made["id"]}}); err != nil {
		t.Fatal(err)
	}
	if r.TopModal() != "" {
		t.Fatalf("a confirm opened: %s", r.TopModal())
	}
}
