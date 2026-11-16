package ochazuke

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func subjectAskRuntime(t *testing.T, param Parameter, late bool) (*Runtime, *Palette, *[]string) {
	t.Helper()
	r := NewRuntime()
	r.Config().Disable()
	r.Instance("app")
	r.Instance("home")
	r.Define("swatch")
	r.Instance("red", Kind("swatch")).Label = "Red"
	r.Instance("blue", Kind("swatch")).Label = "Blue"
	if err := r.SetFocus("home"); err != nil {
		t.Fatal(err)
	}
	ran := new([]string)
	r.Define("canvas").Verb("paint", func(c Ctx) Result {
		value := StringArg(c.Args, "swatch", "")
		if value == "" {
			if late {
				return Failed(c.Need("swatch", "choose the paint"))
			}
			return Failed(errors.New("paint has no swatch"))
		}
		*ran = append(*ran, value)
		return Done(nil)
	}, "Paint").Params(map[string]Parameter{"swatch": param})
	if err := r.Bind(Binding{Noun: "app", Mode: "normal", Keys: "z", Request: Request{Noun: "canvas", Verb: "paint"}}); err != nil {
		t.Fatal(err)
	}
	return r, NewPalette(r), ran
}

func finishSubjectAsk(t *testing.T, r *Runtime, value string) {
	t.Helper()
	if r.Chooser(FormSuggestionsKind) == nil {
		formKeys(t, r, "enter")
	}
	if r.Chooser(FormSuggestionsKind) == nil {
		t.Fatalf("missing subject picker over %q", r.TopModal())
	}
	if r.Mode.Name == "normal" {
		formKeys(t, r, "/")
	}
	typeKeys(t, r, value)
	formKeys(t, r, "enter", "enter")
}

func TestSubjectAskPaletteAndKeyShareScopes(t *testing.T) {
	for _, route := range []string{"palette", "key"} {
		t.Run(route, func(t *testing.T) {
			r, p, ran := subjectAskRuntime(t, Parameter{Type: "string", Required: true, Noun: "swatch"}, false)
			if route == "palette" {
				paletteRequest(t, r, "open", "palette", nil)
				paletteRequest(t, r, "fill", "palette", Arguments{"text": "canvas paint"})
				if err := p.run(); err != nil {
					t.Fatal(err)
				}
			} else {
				formKeys(t, r, "z")
			}
			formKeys(t, r, "enter")
			c := r.Chooser(FormSuggestionsKind)
			if c == nil || !reflect.DeepEqual(c.Sources(), []string{"list", "walk"}) {
				t.Fatalf("scopes = %v", c)
			}
			finishSubjectAsk(t, r, "blue")
			if !reflect.DeepEqual(*ran, []string{"blue"}) || r.Instances[ArgsFormID] != nil {
				t.Fatalf("ran = %v, form remains = %v", *ran, r.Instances[ArgsFormID] != nil)
			}
		})
	}
}

// A verb guarded off at the focus does not answer a key, whatever it could
// ask for (TestSubjectAskNeverAnswersForAnUnavailableVerb); the palette still
// reaches it, asks for its subject and runs it once the guard holds, as
// before v0.124.0 (palette.reach).
func TestSubjectAskGuardedVerbAsksFromThePaletteNotAKey(t *testing.T) {
	r, p, ran := subjectAskRuntime(t, Parameter{Type: "string", Noun: "canvas", Focus: true}, false)
	r.Instance("ink", Kind("canvas")).Label = "Ink"
	r.DefineGuard("paint-subject", func(_ *Runtime, ev KeyEvent) bool { return StringArg(ev.Args, "swatch", "") != "" })
	if err := r.AddVerbAvailability("canvas.paint", "paint-subject"); err != nil {
		t.Fatal(err)
	}
	handled, err := r.Dispatch(MacroKey{Key: "z"})
	if err != nil || handled || r.TopModal() != "" || len(*ran) != 0 {
		t.Fatalf("the key answered for a verb guarded off: handled=%v err=%v top=%q ran=%v", handled, err, r.TopModal(), *ran)
	}
	paletteRequest(t, r, "open", "palette", nil)
	paletteRequest(t, r, "fill", "palette", Arguments{"text": "canvas paint"})
	if err := p.run(); err != nil || r.RecordEditor(ArgsFormID) == nil {
		t.Fatalf("the palette did not ask: err=%v top=%q", err, r.TopModal())
	}
	finishSubjectAsk(t, r, "ink")
	if !reflect.DeepEqual(*ran, []string{"ink"}) {
		t.Fatalf("ran = %v", *ran)
	}
}

func TestSubjectAskCtxNeedUsesSamePicker(t *testing.T) {
	r, _, ran := subjectAskRuntime(t, Parameter{Type: "string", Noun: "swatch"}, true)
	formKeys(t, r, "z")
	formKeys(t, r, "enter")
	c := r.Chooser(FormSuggestionsKind)
	if c == nil || !reflect.DeepEqual(c.Sources(), []string{"list", "walk"}) {
		t.Fatalf("Ctx.Need did not open subject scopes: top=%q", r.TopModal())
	}
	finishSubjectAsk(t, r, "red")
	if !reflect.DeepEqual(*ran, []string{"red"}) {
		t.Fatalf("ran = %v", *ran)
	}
}

func TestSubjectAskMissingDeclaredRowSource(t *testing.T) {
	r, _, ran := subjectAskRuntime(t, Parameter{Type: "string", Required: true, Noun: "swatch", From: "row:paint"}, false)
	formKeys(t, r, "z")
	finishSubjectAsk(t, r, "blue")
	if !reflect.DeepEqual(*ran, []string{"blue"}) {
		t.Fatalf("ran = %v", *ran)
	}
}

// With subject.ask off every route does what it did before the where-from
// ask (v0.123.0), exactly: a missing required argument still opens the args
// form (the path chooser for a lone path), a handler's Need still asks, the
// palette still asks for the verb's own Focus target, and the subjects the
// ask added (a From source, a Focus parameter of another noun) are left to
// the verb, which refuses. v0.124.0 refused all of these with the setting
// off, a palette line missing a required path among them ("missing swatch
// (path)").
func TestSubjectAskOffIsTheBehaviourBeforeTheAsk(t *testing.T) {
	const form, chooser, refuses = "args form", "path chooser", "refuses"
	root := t.TempDir()
	for _, c := range []struct {
		name               string
		param              Parameter
		palette, key, need string
	}{
		{"required noun", Parameter{Type: "string", Required: true, Noun: "swatch"}, form, form, form},
		{"required path", Parameter{Type: PathParameterType, Required: true, Base: root}, chooser, chooser, chooser},
		{"required from", Parameter{Type: "string", Required: true, Noun: "swatch", From: "row:paint"}, form, form, form},
		{"optional noun", Parameter{Type: "string", Noun: "swatch"}, refuses, refuses, form},
		{"optional focus of the verb's noun", Parameter{Type: "string", Noun: "canvas", Focus: true}, form, refuses, form},
		{"optional focus of another noun", Parameter{Type: "string", Noun: "swatch", Focus: true}, refuses, refuses, form},
		{"optional from", Parameter{Type: "string", Noun: "swatch", From: "row:paint"}, refuses, refuses, form},
	} {
		for route, want := range map[string]string{"palette": c.palette, "key": c.key, "need": c.need} {
			t.Run(c.name+"/"+route, func(t *testing.T) {
				r, p, ran := subjectAskRuntime(t, c.param, route == "need")
				if err := r.Settings().Set(SubjectAskSetting, false); err != nil {
					t.Fatal(err)
				}
				var err error
				if route == "palette" {
					paletteRequest(t, r, "open", "palette", nil)
					paletteRequest(t, r, "fill", "palette", Arguments{"text": "canvas paint"})
					err = p.run()
				} else {
					_, err = r.Dispatch(MacroKey{Key: "z"})
				}
				got := refuses
				switch {
				case r.Chooser(PathChooserKind) != nil:
					got = chooser
				case r.Instances[ArgsFormID] != nil:
					got = form
				}
				if got != want || (err != nil) != (want == refuses) || len(*ran) != 0 {
					t.Fatalf("%s, want %s: err=%v top=%q ran=%v", got, want, err, r.TopModal(), *ran)
				}
				// What opens is the picker it was: no where-from scope.
				if c := r.Chooser(PathChooserKind); c != nil && slices.Contains(c.Sources(), "walk") {
					t.Fatalf("the path chooser offers the walk with the ask off: %q", c.Sources())
				}
				if got == form {
					formKeys(t, r, "enter")
					if c := r.Chooser(FormSuggestionsKind); c != nil && slices.Contains(c.Sources(), "walk") {
						t.Fatalf("the field's picker offers the walk with the ask off: %q", c.Sources())
					}
				}
			})
		}
	}
}

func TestSubjectAskHereAndScopesOptOut(t *testing.T) {
	r, _, _ := subjectAskRuntime(t, Parameter{Type: "string", Required: true, Noun: "swatch"}, false)
	if err := r.SetFocus("red"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.OpenArgsForm(Request{Noun: "canvas", Verb: "paint"}, func(Request) error { return nil }); err != nil {
		t.Fatal(err)
	}
	formKeys(t, r, "enter")
	c := r.Chooser(FormSuggestionsKind)
	if c == nil || !reflect.DeepEqual(c.Sources(), []string{"here", "list", "walk"}) {
		t.Fatalf("missing here/list/walk scopes: top=%q", r.TopModal())
	}
	formKeys(t, r, "enter")
	if got := r.RecordEditor(ArgsFormID).Values()["swatch"]; got != "red" {
		t.Fatalf("here picked %q, want red", got)
	}
	formKeys(t, r, "q")
	if err := r.Settings().Set(PickerScopesSetting, false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.OpenArgsForm(Request{Noun: "canvas", Verb: "paint"}, func(Request) error { return nil }); err != nil {
		t.Fatal(err)
	}
	formKeys(t, r, "enter")
	if c := r.Chooser(FormSuggestionsKind); c == nil || slices.Contains(c.Sources(), "walk") {
		t.Fatal("picker.scopes=false did not restore the noun's own choices")
	}
}

func TestSubjectAskDoesNotChangeHeadlessRequests(t *testing.T) {
	r, _, ran := subjectAskRuntime(t, Parameter{Type: "string", Required: true, Noun: "swatch"}, false)
	_, err := r.Execute(Request{Noun: "canvas", Verb: "paint"})
	if needed(err) == nil || r.Instances[ArgsFormID] != nil || len(*ran) != 0 {
		t.Fatalf("headless err=%v top=%q ran=%v", err, r.TopModal(), *ran)
	}
}

func TestSubjectAskPathWalkAndScopesOptOut(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "sample.txt")
	if err := os.WriteFile(path, []byte("sample"), 0600); err != nil {
		t.Fatal(err)
	}
	r, _, ran := subjectAskRuntime(t, Parameter{Type: PathParameterType, Required: true, Base: root}, false)
	thing := Thing{"id": "red", "path": path}
	r.Define("swatch").Props(
		PropSpec{Name: "id", Key: true},
		PropSpec{Name: "path", Parameter: Parameter{Type: PathParameterType}},
	).Store(Store{
		Get:  func(Ctx, string) (Thing, error) { return thing, nil },
		List: func(Ctx, ListQuery) ([]Thing, error) { return []Thing{thing}, nil },
	})
	r.Instances["red"].Data = thing
	if err := r.SetFocus("red"); err != nil {
		t.Fatal(err)
	}
	formKeys(t, r, "z")
	c := r.Chooser(PathChooserKind)
	if c == nil || !slices.Contains(c.Sources(), "walk") || !slices.Contains(c.Sources(), PathSourceTyped) {
		t.Fatalf("path ask did not offer disk/walk/typed: top=%q", r.TopModal())
	}
	formKeys(t, r, "esc")
	for i := 0; c.Source() != "walk" && i < len(c.Sources()); i++ {
		formKeys(t, r, "S")
	}
	if c.Source() != "walk" {
		t.Fatal("S did not reach path walk")
	}
	formKeys(t, r, "/")
	typeKeys(t, r, "sample.txt")
	formKeys(t, r, "enter")
	if !reflect.DeepEqual(*ran, []string{path}) || r.Chooser(PathChooserKind) != nil {
		t.Fatalf("path walk ran=%v top=%q", *ran, r.TopModal())
	}
	if err := r.Settings().Set(PickerScopesSetting, false); err != nil {
		t.Fatal(err)
	}
	formKeys(t, r, "z")
	if c := r.Chooser(PathChooserKind); c == nil || slices.Contains(c.Sources(), "walk") {
		t.Fatal("picker.scopes=false did not restore the disk path picker")
	}
}

func TestSubjectAskOptionalOwnNounAndMotionExemption(t *testing.T) {
	r, _, ran := subjectAskRuntime(t, Parameter{Type: "string", Noun: "canvas"}, false)
	r.Define("canvas").Props(PropSpec{Name: "id", Key: true}).Store(Store{
		Get:  func(Ctx, string) (Thing, error) { return Thing{"id": "ink"}, nil },
		List: func(Ctx, ListQuery) ([]Thing, error) { return []Thing{{"id": "ink"}}, nil },
	})
	r.Instance("ink", Kind("canvas")).Label = "Ink"
	formKeys(t, r, "z")
	finishSubjectAsk(t, r, "ink")
	if !reflect.DeepEqual(*ran, []string{"ink"}) {
		t.Fatalf("optional declared subject ran = %v", *ran)
	}
	op := r.Operations.At(Key("canvas.paint"))
	op.Motion = true
	r.Operations.Set(op)
	_, err := r.Dispatch(MacroKey{Key: "z"})
	if err == nil || r.Instances[ArgsFormID] != nil || len(*ran) != 1 {
		t.Fatalf("motion should keep its own focus behavior: err=%v top=%q ran=%v", err, r.TopModal(), *ran)
	}
}

// The footer, the help and binding.list offer a row exactly where its key
// answers: an available verb that lacks its subject is offered and asks; a
// verb guarded off, a disabled one and a row whose own guard is false are
// neither offered nor answered.
func TestSubjectAskBindingListingsMatchDispatch(t *testing.T) {
	for _, state := range []string{"asking", "verb-guard", "disabled", "row-guard"} {
		t.Run(state, func(t *testing.T) {
			r, _, ran := subjectAskRuntime(t, Parameter{Type: "string", Noun: "canvas", Focus: true}, false)
			r.Instance("ink", Kind("canvas")).Label = "Ink"
			key := "z"
			if state == "verb-guard" {
				r.DefineGuard("paint-subject", func(_ *Runtime, ev KeyEvent) bool { return StringArg(ev.Args, "swatch", "") != "" })
				if err := r.AddVerbAvailability("canvas.paint", "paint-subject"); err != nil {
					t.Fatal(err)
				}
			}
			if state == "disabled" {
				op := r.Operations.At(Key("canvas.paint"))
				op.Disabled = true
				r.Operations.Set(op)
			}
			if state == "row-guard" {
				key = "Z"
				r.DefineGuard("paint-row", func(*Runtime, KeyEvent) bool { return false })
				if err := r.Bind(Binding{Noun: "app", Mode: "normal", Keys: key, When: "paint-row", Request: Request{Noun: "canvas", Verb: "paint"}}); err != nil {
					t.Fatal(err)
				}
			}
			listed := slices.ContainsFunc(r.ListBindings(r.Focus), func(b Binding) bool { return b.Keys == key })
			hinted := slices.ContainsFunc(HintBindings(r, r.Focus), func(b Binding) bool { return b.Keys == key })
			helped := slices.ContainsFunc(BindingHelp(r, r.Focus), func(row HelpRow) bool { return row.Key == key })
			want := state == "asking"
			if hinted != want || helped != want {
				t.Fatalf("state=%s hinted=%v helped=%v, want %v", state, hinted, helped, want)
			}
			// binding.list intentionally keeps false binding-row guards;
			// unavailable verbs, unlike those conditional rows, are omitted.
			if state != "row-guard" && listed != want {
				t.Fatalf("state=%s listed=%v, want %v", state, listed, want)
			}
			handled, err := r.Dispatch(MacroKey{Key: key})
			if err != nil || handled != want {
				t.Fatalf("state=%s handled=%v err=%v, want handled=%v", state, handled, err, want)
			}
			if (r.RecordEditor(ArgsFormID) != nil) != want {
				t.Fatalf("state=%s top=%q", state, r.TopModal())
			}
			if want {
				finishSubjectAsk(t, r, "ink")
				if !reflect.DeepEqual(*ran, []string{"ink"}) {
					t.Fatalf("ran = %v", *ran)
				}
			} else if len(*ran) != 0 {
				t.Fatalf("blocked row ran = %v", *ran)
			}
		})
	}
}

func TestSubjectHereReadsOpaqueRowProps(t *testing.T) {
	r := NewRuntime()
	r.Config().Disable()
	r.Instance("app")
	r.Define("swatch").Props(PropSpec{Name: "path", Parameter: Parameter{Type: PathParameterType}})
	path := filepath.Join(t.TempDir(), "sample.txt")
	if _, err := r.AddList("swatches", []ListItem{{ID: "opaque-row", Noun: "swatch", Label: "Opaque", Data: "foreign-handle", Props: Thing{"path": path}}}); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFocus("opaque-row"); err != nil {
		t.Fatal(err)
	}
	choices := r.subjectHere(Parameter{Type: PathParameterType}, r.Context())
	if len(choices) != 1 || choices[0].ID != path {
		t.Fatalf("path choices from opaque row = %#v, want %q", choices, path)
	}
}

func TestSubjectAskFocusBoundExemption(t *testing.T) {
	r, _, ran := subjectAskRuntime(t, Parameter{Type: "string", Noun: "canvas", Focus: true}, false)
	op := r.Operations.At(Key("canvas.paint"))
	op.FocusBound = true
	r.Operations.Set(op)
	_, err := r.Dispatch(MacroKey{Key: "z"})
	if err == nil || r.Instances[ArgsFormID] != nil || len(*ran) != 0 {
		t.Fatalf("focus-bound verb must keep its focused context: err=%v top=%q ran=%v", err, r.TopModal(), *ran)
	}
}

func TestSubjectAskKeepsOptionalCreationSourcesOptional(t *testing.T) {
	for _, flags := range []struct {
		name    string
		creates bool
		covers  bool
	}{
		{name: "copy alternative", covers: true},
		{name: "creation reference", creates: true},
		{name: "creation copy", creates: true, covers: true},
	} {
		t.Run(flags.name, func(t *testing.T) {
			r, _, ran := subjectAskRuntime(t, Parameter{Type: "string", Noun: "canvas", Covers: flags.covers}, false)
			op := r.Operations.At(Key("canvas.paint"))
			op.Creates = flags.creates
			op.AppHandler = func(c Ctx) Result {
				*ran = append(*ran, StringArg(c.Args, "swatch", "new"))
				return Done(nil)
			}
			r.Operations.Set(op)
			formKeys(t, r, "z")
			if !reflect.DeepEqual(*ran, []string{"new"}) || r.Instances[ArgsFormID] != nil {
				t.Fatalf("optional input blocked creation: ran=%v top=%q", *ran, r.TopModal())
			}
		})
	}
}

func TestSubjectAskDoesNotInferCommandOrPartSubjects(t *testing.T) {
	for _, kind := range []string{NounKindCommand, NounKindPart} {
		t.Run(kind, func(t *testing.T) {
			r, _, ran := subjectAskRuntime(t, Parameter{Type: "string", Noun: "canvas"}, false)
			r.Define("canvas").Kind(kind)
			r.DefineGuard("canvas-context", func(*Runtime, KeyEvent) bool { return false })
			if err := r.AddVerbAvailability("canvas.paint", "canvas-context"); err != nil {
				t.Fatal(err)
			}
			fallback := 0
			r.Define("app").Verb("fallback", func(Ctx) Result {
				fallback++
				return Done(nil)
			})
			if err := r.Bind(
				Binding{Noun: "app", Mode: "normal", Keys: "z", Request: Request{Noun: "canvas", Verb: "paint"}},
				Binding{Noun: "app", Mode: "normal", Keys: "z", Request: Request{Noun: "app", Verb: "fallback"}},
			); err != nil {
				t.Fatal(err)
			}
			rows := r.ListBindings(r.Focus)
			at := slices.IndexFunc(rows, func(row Binding) bool { return row.Keys == "z" })
			if at < 0 || rows[at].Request.Key() != "app.fallback" {
				t.Fatalf("off-focus %s intercepted key listing: %#v", kind, rows)
			}
			formKeys(t, r, "z")
			if fallback != 1 || len(*ran) != 0 || r.Instances[ArgsFormID] != nil {
				t.Fatalf("off-focus %s intercepted dispatch: fallback=%d ran=%v top=%q", kind, fallback, *ran, r.TopModal())
			}
		})
	}
}

func TestSubjectAskOptionalCrossNounDeclaredSources(t *testing.T) {
	for _, param := range []Parameter{
		{Type: "string", Noun: "swatch", Focus: true},
		{Type: "string", Noun: "swatch", From: "row:paint"},
	} {
		name := param.From
		if param.Focus {
			name = "focus"
		}
		t.Run(name, func(t *testing.T) {
			r, _, ran := subjectAskRuntime(t, param, false)
			formKeys(t, r, "z")
			if r.RecordEditor(ArgsFormID) == nil || len(*ran) != 0 {
				t.Fatalf("missing declared cross-noun source did not ask: top=%q ran=%v", r.TopModal(), *ran)
			}
			finishSubjectAsk(t, r, "blue")
			if !reflect.DeepEqual(*ran, []string{"blue"}) || r.Instances[ArgsFormID] != nil {
				t.Fatalf("declared cross-noun subject ran=%v top=%q", *ran, r.TopModal())
			}
		})
	}
}

func TestSubjectAskKeepsPlainOptionalCrossNounReferenceOptional(t *testing.T) {
	r, _, ran := subjectAskRuntime(t, Parameter{Type: "string", Noun: "swatch"}, false)
	_, err := r.Dispatch(MacroKey{Key: "z"})
	if err == nil || r.Instances[ArgsFormID] != nil || len(*ran) != 0 {
		t.Fatalf("plain optional reference must not become a subject: err=%v top=%q ran=%v", err, r.TopModal(), *ran)
	}
}

func TestSubjectAskPreservesFilesystemWalkStart(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	leaf := filepath.Join(root, "branch", "leaf")
	if err := os.MkdirAll(leaf, 0700); err != nil {
		t.Fatal(err)
	}
	r, _, ran := subjectAskRuntime(t, Parameter{
		Type: PathParameterType, Path: PathDir, Required: true, Base: root, Start: PathSourceWalk,
	}, false)
	r.AddWalkPathSource(func(PathPick) PathWalk { return PathWalk{Roots: []PathRoot{{Path: root, Label: "fixture"}}} })
	land := func() {
		drain(t, r, func(msg tea.Msg) {
			if _, err := r.Deliver(msg); err != nil {
				t.Fatal(err)
			}
		})
	}
	formKeys(t, r, "z")
	c := r.Chooser(PathChooserKind)
	if c == nil {
		t.Fatalf("path ask did not open: top=%q", r.TopModal())
	}
	land()
	if c.Source() == PathSourceWalk || !slices.Contains(c.Sources(), PathSourceWalk) {
		t.Fatalf("explicit filesystem start lost or graph scope missing: source=%q scopes=%v", c.Source(), c.Sources())
	}
	filesystem := c.Source()
	if !slices.ContainsFunc(c.Matches(), func(row PickerRow) bool { return row.ID == leaf }) {
		t.Fatalf("filesystem walk did not list nested leaf: %#v", c.Matches())
	}
	typeKeys(t, r, "leaf")
	formKeys(t, r, "esc", "l")
	land()
	formKeys(t, r, "alt+up")
	land()
	if c.Source() != filesystem || !slices.ContainsFunc(c.Matches(), func(row PickerRow) bool { return row.ID == leaf }) {
		t.Fatalf("filesystem navigation escaped into graph: source=%q rows=%#v", c.Source(), c.Matches())
	}
	formKeys(t, r, "alt+enter")
	if !reflect.DeepEqual(*ran, []string{leaf}) || r.Chooser(PathChooserKind) != nil {
		t.Fatalf("filesystem selection ran=%v top=%q", *ran, r.TopModal())
	}
}

func TestSubjectAskKeepsArchivedScopeCounts(t *testing.T) {
	r, _, ran := subjectAskRuntime(t, Parameter{Type: "string", Required: true, Noun: "swatch"}, false)
	things := []Thing{
		{"id": "red", "title": "Red"},
		{"id": "blue", "title": "Blue"},
		{"id": "old", "title": "Old one", "done": true},
	}
	reads := 0
	r.Define("swatch").Props(
		PropSpec{Name: "id", Key: true},
		PropSpec{Name: "title", Role: RoleLabel},
		PropSpec{Name: "done", Role: RoleArchive, Parameter: Parameter{Type: SettingBoolean}},
	).Store(Store{
		Get: func(_ Ctx, id string) (Thing, error) {
			for _, thing := range things {
				if thing["id"] == id {
					return thing, nil
				}
			}
			return nil, errors.New("unknown swatch")
		},
		List: func(_ Ctx, q ListQuery) ([]Thing, error) {
			reads++
			var out []Thing
			for _, thing := range things {
				if q.Archived || thing["done"] != true {
					out = append(out, thing)
				}
			}
			return out, nil
		},
	})
	formKeys(t, r, "z", "enter")
	c := r.Chooser(FormSuggestionsKind)
	if c == nil || !reflect.DeepEqual(c.Sources(), []string{"all", "archived", "walk"}) {
		t.Fatalf("missing comparable scopes and walk: top=%q", r.TopModal())
	}
	before := reads
	for range 2 {
		if frame := c.View(100, 12).Text; !strings.Contains(frame, "1 more in archived") || strings.Contains(frame, "Old one") {
			t.Fatalf("missing archived teaser over active values:\n%s", frame)
		}
	}
	if reads != before {
		t.Fatalf("rendering performed source IO: reads before=%d after=%d", before, reads)
	}
	formKeys(t, r, "S")
	if c.Source() != "archived" || !slices.ContainsFunc(c.Matches(), func(row PickerRow) bool { return row.ID == "old" }) {
		t.Fatalf("archived scope did not expose Old one: source=%q rows=%#v", c.Source(), c.Matches())
	}
	formKeys(t, r, "S")
	if c.Source() != "walk" {
		t.Fatalf("S skipped the graph scope: %q", c.Source())
	}
	formKeys(t, r, "S")
	if c.Source() != "all" || !strings.Contains(c.View(100, 12).Text, "1 more in archived") {
		t.Fatal("cycling back to all lost the archived teaser")
	}
	finishSubjectAsk(t, r, "blue")
	if !reflect.DeepEqual(*ran, []string{"blue"}) {
		t.Fatalf("scope cycling changed the selected subject: ran=%v", *ran)
	}
}

// aliasNounRuntime has two stored nouns over the same things, ticket and
// verdict (a ticket seen as a verdict), a row of each and a neutral focus.
// ticket.approve takes params and is named on verdict by an alias; a on a
// verdict row runs it by that alias, A anywhere by its own name. ran holds
// the instance each run acted at.
func aliasNounRuntime(t *testing.T, params map[string]Parameter) (*Runtime, *Palette, *[]string) {
	t.Helper()
	r := NewRuntime()
	r.Config().Disable()
	r.Instance("app")
	r.Instance("home")
	store := Store{
		Get:  func(Ctx, string) (Thing, error) { return Thing{"id": "t1"}, nil },
		List: func(Ctx, ListQuery) ([]Thing, error) { return []Thing{{"id": "t1"}}, nil },
	}
	ran := new([]string)
	r.Define("verdict").Props(PropSpec{Name: "id", Key: true}).Store(store)
	r.Define("ticket").Props(PropSpec{Name: "id", Key: true}).Store(store).
		Verb("approve", func(c Ctx) Result {
			*ran = append(*ran, c.Instance.ID+" as "+c.Invoked.Name)
			return Done(nil)
		}, "Approve").Params(params).AliasOn("verdict", "approve")
	r.Instance("ticket-row", Kind("ticket")).Label = "Ticket"
	r.Instance("verdict-row", Kind("verdict")).Label = "Verdict"
	if err := r.Bind(
		Binding{Noun: "verdict", Mode: "normal", Keys: "a", Request: Request{Noun: "ticket", Verb: "approve", Via: "verdict.approve"}},
		Binding{Noun: "app", Mode: "normal", Keys: "A", Request: Request{Noun: "ticket", Verb: "approve"}},
	); err != nil {
		t.Fatal(err)
	}
	return r, NewPalette(r), ran
}

// runAliasNounRoute makes the request by one route at the current focus.
func runAliasNounRoute(t *testing.T, r *Runtime, p *Palette, route string) {
	t.Helper()
	if line, typed := strings.CutPrefix(route, ":"); typed {
		paletteRequest(t, r, "open", "palette", nil)
		paletteRequest(t, r, "fill", "palette", Arguments{"text": line})
		if err := p.run(); err != nil {
			t.Fatalf("%s: %v", route, err)
		}
		return
	}
	formKeys(t, r, route)
}

// A thing of a noun that names the verb's noun's actions by an alias stands
// for the verb's optional subject: a key on it (by the alias or by the
// action's own name) and a palette line act on it and ask nothing, as
// before v0.124.0, which opened the args form for the target.
func TestSubjectAskAliasNounThingStandsForTheSubject(t *testing.T) {
	shapes := map[string]map[string]Parameter{
		"implicit own-noun target": {"target": {Type: "string", Noun: "ticket"}},
		"focus own-noun target":    {"target": {Type: "string", Noun: "ticket", Focus: true}},
	}
	for shape, params := range shapes {
		for route, as := range map[string]string{"a": "verdict.approve", "A": "ticket.approve", ":ticket approve": "ticket.approve", ":verdict approve": "verdict.approve"} {
			t.Run(shape+"/"+route, func(t *testing.T) {
				r, p, ran := aliasNounRuntime(t, params)
				if err := r.SetFocus("verdict-row"); err != nil {
					t.Fatal(err)
				}
				runAliasNounRoute(t, r, p, route)
				if want := []string{"verdict-row as " + as}; !reflect.DeepEqual(*ran, want) || r.Instances[ArgsFormID] != nil {
					t.Fatalf("ran = %v, want %v; top = %q", *ran, want, r.TopModal())
				}
			})
		}
	}
}

// A parameter typed with the naming noun (o-questions' question verbs take
// a decision) is supplied by a thing of the verb's own noun at the focus.
func TestSubjectAskOwnNounThingStandsForAnAliasNounSubject(t *testing.T) {
	for _, route := range []string{"A", ":ticket approve"} {
		t.Run(route, func(t *testing.T) {
			r, p, ran := aliasNounRuntime(t, map[string]Parameter{"target": {Type: "string", Noun: "verdict", Focus: true}})
			if err := r.SetFocus("ticket-row"); err != nil {
				t.Fatal(err)
			}
			runAliasNounRoute(t, r, p, route)
			if want := []string{"ticket-row as ticket.approve"}; !reflect.DeepEqual(*ran, want) || r.Instances[ArgsFormID] != nil {
				t.Fatalf("ran = %v, want %v; top = %q", *ran, want, r.TopModal())
			}
		})
	}
}

// What nothing at the focus supplies is still asked for: away from every
// thing of the verb's noun and of the nouns naming it, and for the own-noun
// subject of a verb that takes the naming noun as a parameter of its own
// (the other side of a verb relating two nouns).
func TestSubjectAskStillAsksWhatTheFocusDoesNotSupply(t *testing.T) {
	relate := map[string]Parameter{"target": {Type: "string", Noun: "ticket", Focus: true}, "verdict": {Type: "string", Noun: "verdict", Focus: true}}
	for name, c := range map[string]struct {
		params map[string]Parameter
		focus  string
	}{
		"neutral focus":          {map[string]Parameter{"target": {Type: "string", Noun: "ticket"}}, "home"},
		"the other side at hand": {relate, "verdict-row"},
	} {
		for _, route := range []string{"A", ":ticket approve"} {
			t.Run(name+"/"+route, func(t *testing.T) {
				r, p, ran := aliasNounRuntime(t, c.params)
				if err := r.SetFocus(c.focus); err != nil {
					t.Fatal(err)
				}
				runAliasNounRoute(t, r, p, route)
				e := r.RecordEditor(ArgsFormID)
				if e == nil || len(*ran) != 0 {
					t.Fatalf("did not ask: ran = %v, top = %q", *ran, r.TopModal())
				}
				if f := e.fields[e.start()]; f.Name != "target" {
					t.Fatalf("asked for %q, want target", f.Name)
				}
			})
		}
	}
}

// pathAsk presses z on a verb whose one required path is missing and
// answers the where-from path ask it opens.
func pathAsk(t *testing.T, r *Runtime) *Chooser {
	t.Helper()
	formKeys(t, r, "z")
	c := r.Chooser(PathChooserKind)
	if c == nil {
		t.Fatalf("path ask did not open: top=%q", r.TopModal())
	}
	return c
}

// closePicker cancels the open path picker.
func closePicker(t *testing.T, r *Runtime) {
	t.Helper()
	if _, err := r.Execute(Request{Noun: PickerNoun, Verb: "cancel"}); err != nil || r.Chooser(PathChooserKind) != nil {
		t.Fatalf("the path picker stayed open: top=%q err=%v", r.TopModal(), err)
	}
}

// A path ask is the path picker it was before v0.124.0 with the relation
// walk as one more source: ctrl+o and alt+o step its sources (v0.124.0
// guarded them off and left only S and alt+s), the footer offers the source
// key while typing, the title says where it is among them, and the source
// last used is remembered. S and alt+s step the same list.
func TestPathAskKeepsItsSourceKeysTitleAndMemory(t *testing.T) {
	root := t.TempDir()
	r, _, _ := subjectAskRuntime(t, Parameter{Type: PathParameterType, Required: true, Base: root}, false)
	c := pathAsk(t, r)
	if got, want := c.Sources(), []string{PathSourceBrowse, PathSourceRecent, PathSourceTyped, PathSourceFind, "walk"}; !slices.Equal(got, want) {
		t.Fatalf("sources %q, want %q", got, want)
	}
	if got, want := c.Title(), "Run canvas.paint · choose swatch · [browse "+displayPath(root)+"] recent typed find walk"; got != want {
		t.Fatalf("title %q, want %q", got, want)
	}
	if r.Mode.Name != "insert" {
		t.Fatalf("the ask opened in %s, not typing", r.Mode.Name)
	}
	hint := func(key, action string) bool {
		return slices.ContainsFunc(HintBindings(r, r.Focus), func(b Binding) bool { return b.Keys == key && b.Request.Key() == action })
	}
	if !hint("ctrl+o", "ochazuke.picker.next-source") {
		t.Fatalf("the footer lacks the source key while typing: %v", hints(HintBindings(r, r.Focus)))
	}
	for _, step := range []struct{ key, source string }{
		{"ctrl+o", PathSourceRecent}, {"ctrl+o", PathSourceTyped}, {"alt+o", PathSourceRecent}, {"alt+s", PathSourceTyped}, {"alt+o", PathSourceRecent},
	} {
		formKeys(t, r, step.key)
		if c.Source() != step.source {
			t.Fatalf("%s reached %q, want %q", step.key, c.Source(), step.source)
		}
	}
	if got, want := c.Title(), "Run canvas.paint · choose swatch · browse [recent] typed find walk"; got != want {
		t.Fatalf("title %q, want %q", got, want)
	}
	// In normal mode S steps the same list, and the source key is still
	// offered beside it.
	formKeys(t, r, "esc")
	if r.Mode.Name != "normal" || !hint("S", "ochazuke.picker.next-scope") || !hint("ctrl+o", "ochazuke.picker.next-source") {
		t.Fatalf("normal mode %q hints %v", r.Mode.Name, hints(HintBindings(r, r.Focus)))
	}
	formKeys(t, r, "S")
	if c.Source() != PathSourceTyped {
		t.Fatalf("S reached %q, want %q", c.Source(), PathSourceTyped)
	}
	formKeys(t, r, "alt+o")
	closePicker(t, r)
	if c = pathAsk(t, r); c.Source() != PathSourceRecent {
		t.Fatalf("the ask reopened on %q, not the source last used", c.Source())
	}
}

// hints are the keys of binding rows, for a failure message.
func hints(rows []Binding) []string {
	out := make([]string, len(rows))
	for i, b := range rows {
		out[i] = b.Keys + " " + b.Request.Key()
	}
	return out
}

// With the sources that existed before v0.124.0 alone (picker.scopes or
// subject.ask off) a path ask's title, sources and keys are the path
// picker's own; with the walk they differ by that one source, last.
func TestPathAskTitleDiffersOnlyByTheWalk(t *testing.T) {
	param := Parameter{Type: PathParameterType, Required: true, Base: t.TempDir()}
	r, _, _ := subjectAskRuntime(t, param, false)
	plain, err := r.OpenPathChooser(PathChooserSpec{Title: "Run canvas.paint · choose swatch", Pick: PathPick{Name: "swatch", Parameter: param}})
	if err != nil {
		t.Fatal(err)
	}
	before, title := plain.Sources(), plain.Title()
	if !strings.HasPrefix(title, "Run canvas.paint · choose swatch · [browse ") || !strings.HasSuffix(title, "] recent typed find") {
		t.Fatalf("the path picker's own title %q", title)
	}
	closePicker(t, r)
	c := pathAsk(t, r)
	if got := c.Sources(); !slices.Equal(got, append(slices.Clone(before), "walk")) || c.Title() != title+" walk" {
		t.Fatalf("with the walk: sources %q title %q", got, c.Title())
	}
	closePicker(t, r)
	for _, off := range []string{PickerScopesSetting, SubjectAskSetting} {
		if err := r.Settings().Set(off, false); err != nil {
			t.Fatal(err)
		}
		if off == SubjectAskSetting {
			// The request is the palette's with the ask off: a key refuses
			// a missing path (TestSubjectAskOffRefusesPaletteKeyAndNeed).
			if _, err := r.OpenArgsForm(Request{Noun: "canvas", Verb: "paint"}, func(Request) error { return nil }); err != nil {
				t.Fatal(err)
			}
			c = r.Chooser(PathChooserKind)
		} else {
			c = pathAsk(t, r)
		}
		if c == nil || !slices.Equal(c.Sources(), before) || c.Title() != title {
			t.Fatalf("%s off: chooser %v", off, c)
		}
		if slices.ContainsFunc(HintBindings(r, r.Focus), func(b Binding) bool { return b.Request.Key() == "ochazuke.picker.next-scope" }) {
			t.Fatalf("%s off: the scope key is offered with no scope", off)
		}
		closePicker(t, r)
		if err := r.Settings().Set(off, true); err != nil {
			t.Fatal(err)
		}
	}
}

// An application's file system source named like the walk keeps its rows and
// its place in the memory: for the ask it is "on disk · walk", and the path
// picker on its own finds it again as walk.
func TestPathAskRemembersARenamedSourceByItsOwnName(t *testing.T) {
	root := t.TempDir()
	param := Parameter{Type: PathParameterType, Path: PathDir, Required: true, Base: root}
	r, _, _ := subjectAskRuntime(t, param, false)
	r.AddWalkPathSource(func(PathPick) PathWalk { return PathWalk{Roots: []PathRoot{{Path: root, Label: "fixture"}}} })
	c := pathAsk(t, r)
	if got, want := c.Sources(), []string{"on disk · walk", PathSourceBrowse, PathSourceRecent, PathSourceTyped, PathSourceFind, "walk"}; !slices.Equal(got, want) {
		t.Fatalf("sources %q, want %q", got, want)
	}
	formKeys(t, r, "ctrl+o", "alt+o")
	if c.Source() != "on disk · walk" {
		t.Fatalf("alt+o came back to %q", c.Source())
	}
	closePicker(t, r)
	plain, err := r.OpenPathChooser(PathChooserSpec{Pick: PathPick{Name: "swatch", Parameter: param}})
	if err != nil {
		t.Fatal(err)
	}
	if plain.Source() != PathSourceWalk {
		t.Fatalf("the path picker opened on %q, not the folder walk last used", plain.Source())
	}
	closePicker(t, r)
	if c = pathAsk(t, r); c.Source() != "on disk · walk" {
		t.Fatalf("the ask reopened on %q, not the folder walk last used", c.Source())
	}
}

// Asking for a missing subject or path never makes an unavailable verb
// answer a key: availability is decided first, as before v0.124.0, and the
// key passes on to the next row and the next level. v0.124.0 let the row of
// a verb that could ask answer where the verb's own guard was false, so a
// key that repeats (one row per place it means something) ran the first
// row's verb everywhere.
func TestSubjectAskNeverAnswersForAnUnavailableVerb(t *testing.T) {
	for name, param := range map[string]Parameter{
		"required path":        {Type: PathParameterType, Required: true, Base: t.TempDir()},
		"required noun":        {Type: "string", Required: true, Noun: "swatch"},
		"optional own subject": {Type: "string", Noun: "canvas", Focus: true},
	} {
		t.Run(name, func(t *testing.T) {
			r, _, ran := subjectAskRuntime(t, param, false)
			r.DefineGuard("paint-allowed", func(*Runtime, KeyEvent) bool { return false })
			if err := r.AddVerbAvailability("canvas.paint", "paint-allowed"); err != nil {
				t.Fatal(err)
			}
			fallback := 0
			r.Define("app").Verb("fallback", func(Ctx) Result {
				fallback++
				return Done(nil)
			})
			// z: the focused thing's own row, then app's (the fixture's),
			// both for the unavailable verb, then the row that answers.
			if err := r.Bind(
				Binding{Noun: "home", Mode: "normal", Keys: "z", Request: Request{Noun: "canvas", Verb: "paint"}},
				Binding{Noun: "app", Mode: "normal", Keys: "z", Request: Request{Noun: "app", Verb: "fallback"}},
			); err != nil {
				t.Fatal(err)
			}
			rows := r.ListBindings(r.Focus)
			if at := slices.IndexFunc(rows, func(row Binding) bool { return row.Keys == "z" }); at < 0 || rows[at].Request.Key() != "app.fallback" {
				t.Fatalf("the listing answers z with the unavailable verb (row %d of %d)", at, len(rows))
			}
			if slices.ContainsFunc(HintBindings(r, r.Focus), func(b Binding) bool { return b.Request.Key() == "canvas.paint" }) {
				t.Fatalf("the footer offers the unavailable verb: %v", hints(HintBindings(r, r.Focus)))
			}
			formKeys(t, r, "z")
			if fallback != 1 || len(*ran) != 0 || r.TopModal() != "" {
				t.Fatalf("z did not pass on to the next row: fallback=%d ran=%v top=%q", fallback, *ran, r.TopModal())
			}
		})
	}
}

// cursorVerbRuntime has a stored noun, value, whose own edit acts on what is
// under the cursor of the focused thing; as an application's verb under a
// core row's name it adopts the key subject (adoptFieldParam: an optional
// Focus parameter of the verb's noun). A table thing and a value thing are
// on the page. r is the row of each of those nouns, R an app row; ran holds
// the instance each run acted at.
func cursorVerbRuntime(t *testing.T) (*Runtime, *[]string) {
	t.Helper()
	r := NewRuntime()
	r.Config().Disable()
	r.Instance("app")
	r.Instance("home")
	ran := new([]string)
	r.Define("table")
	r.Define("value").Props(PropSpec{Name: "id", Key: true}).Store(Store{
		Get:  func(Ctx, string) (Thing, error) { return Thing{"id": "v1"}, nil },
		List: func(Ctx, ListQuery) ([]Thing, error) { return []Thing{{"id": "v1"}}, nil },
	}).Verb("edit", func(c Ctx) Result {
		*ran = append(*ran, c.Instance.ID)
		return Done(nil)
	}, "Edit the value under the cursor")
	if p, ok := r.Operations.At(Key("value.edit")).Parameters["id"]; !ok || !p.Focus || p.Required || p.Noun != "value" {
		t.Fatalf("the verb did not adopt the key subject: %+v", p)
	}
	r.Instance("cell", Kind("table")).Label = "Cell"
	r.Instance("value-row", Kind("value")).Data = Thing{"id": "v1"}
	if err := r.Bind(
		Binding{Noun: "table", Mode: "normal", Keys: "r", Request: Request{Noun: "value", Verb: "edit"}},
		Binding{Noun: "value", Mode: "normal", Keys: "r", Request: Request{Noun: "value", Verb: "edit"}},
		Binding{Noun: "app", Mode: "normal", Keys: "R", Request: Request{Noun: "value", Verb: "edit"}},
	); err != nil {
		t.Fatal(err)
	}
	return r, ran
}

// A key pressed on a focused thing acts on that thing: the row that answers
// at a thing's level is that thing's key, so its verb runs there and nothing
// is asked, whether the thing is of the verb's noun or of the noun the row is
// bound on (a table cell whose r edits the value under the cursor). v0.124.0
// opened the args form for the value's id in the second case; v0.123.0 ran
// the verb at the focus.
func TestSubjectAskKeyOnAFocusedThingActsOnIt(t *testing.T) {
	for _, focus := range []string{"value-row", "cell"} {
		t.Run(focus, func(t *testing.T) {
			r, ran := cursorVerbRuntime(t)
			if err := r.SetFocus(focus); err != nil {
				t.Fatal(err)
			}
			formKeys(t, r, "r")
			if !reflect.DeepEqual(*ran, []string{focus}) || r.TopModal() != "" {
				t.Fatalf("r on %s: ran=%v top=%q", focus, *ran, r.TopModal())
			}
		})
	}
}

// A key of no thing (an app row) still asks for the subject nothing at the
// focus supplies, and acts on a thing of the verb's noun when one is there.
func TestSubjectAskAppKeyAsksOnlyAwayFromTheSubject(t *testing.T) {
	r, ran := cursorVerbRuntime(t)
	if err := r.SetFocus("value-row"); err != nil {
		t.Fatal(err)
	}
	formKeys(t, r, "R")
	if !reflect.DeepEqual(*ran, []string{"value-row"}) || r.TopModal() != "" {
		t.Fatalf("R on a value: ran=%v top=%q", *ran, r.TopModal())
	}
	if err := r.SetFocus("home"); err != nil {
		t.Fatal(err)
	}
	formKeys(t, r, "R")
	if e := r.RecordEditor(ArgsFormID); e == nil || e.fields[e.start()].Name != "id" || len(*ran) != 1 {
		t.Fatalf("R away from every value did not ask for it: ran=%v top=%q", *ran, r.TopModal())
	}
}

// A thing's key still asks for a required argument it lacks; the form it
// opens leaves the verb's subject optional, since the thing holds the request
// (v0.124.0 made the subject a required field, asked first).
func TestSubjectAskThingKeyAsksOnlyForWhatIsRequired(t *testing.T) {
	r, ran := cursorVerbRuntime(t)
	r.Define("value").Verb("note", func(c Ctx) Result {
		*ran = append(*ran, c.Instance.ID+" "+StringArg(c.Args, "text", ""))
		return Done(nil)
	}, "Note the value under the cursor").Params(map[string]Parameter{
		"target": {Type: "string", Noun: "value"},
		"text":   {Type: "string", Required: true},
	})
	if err := r.Bind(Binding{Noun: "table", Mode: "normal", Keys: "n", Request: Request{Noun: "value", Verb: "note"}}); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFocus("cell"); err != nil {
		t.Fatal(err)
	}
	formKeys(t, r, "n")
	e := r.RecordEditor(ArgsFormID)
	if e == nil {
		t.Fatalf("the key did not ask for its required text: top=%q", r.TopModal())
	}
	if f := e.fields[e.start()]; f.Name != "text" {
		t.Fatalf("the form starts on %q, not the required text", f.Name)
	}
	for _, f := range e.fields {
		if f.Name == "target" && f.Required {
			t.Fatal("the form requires the subject of a request a thing holds")
		}
	}
	typeKeys(t, r, "seen")
	formKeys(t, r, "enter")
	if !reflect.DeepEqual(*ran, []string{"cell seen"}) || r.Instances[ArgsFormID] != nil {
		t.Fatalf("ran=%v top=%q", *ran, r.TopModal())
	}
}

// esc in a path ask does what it does in the path picker, whatever
// modal.esc-closes says: off (the default) the first esc leaves typing and
// the picker stays; on, esc closes it in one press. v0.124.0 gave the ask a
// normal mode of its own, which released the search line's pin, so with
// modal.esc-closes on the ask stayed open where the picker closed. The walk
// still enters normal mode by itself, where its keys are.
func TestPathAskEscIsThePathPickers(t *testing.T) {
	param := Parameter{Type: PathParameterType, Required: true, Base: t.TempDir()}
	for _, closes := range []bool{false, true} {
		for _, route := range []string{"picker", "key", "palette"} {
			t.Run(fmt.Sprintf("modal.esc-closes=%v/%s", closes, route), func(t *testing.T) {
				r, p, ran := subjectAskRuntime(t, param, false)
				if err := r.Settings().Set(ModalEscClosesSetting, closes); err != nil {
					t.Fatal(err)
				}
				switch route {
				case "picker":
					if _, err := r.OpenPathChooser(PathChooserSpec{Pick: PathPick{Name: "swatch", Parameter: param}}); err != nil {
						t.Fatal(err)
					}
				case "key":
					formKeys(t, r, "z")
				case "palette":
					paletteRequest(t, r, "open", "palette", nil)
					paletteRequest(t, r, "fill", "palette", Arguments{"text": "canvas paint"})
					if err := p.run(); err != nil {
						t.Fatal(err)
					}
				}
				if r.Chooser(PathChooserKind) == nil || r.Mode.Name != "insert" {
					t.Fatalf("no path picker in insert: top=%q mode=%q", r.TopModal(), r.Mode.Name)
				}
				formKeys(t, r, "esc")
				if open := r.Chooser(PathChooserKind) != nil; open == closes || len(*ran) != 0 {
					t.Fatalf("after one esc the picker is open=%v, want %v: top=%q mode=%q ran=%v", open, !closes, r.TopModal(), r.Mode.Name, *ran)
				}
				if !closes && r.Mode.Name != "normal" {
					t.Fatalf("esc left the search line in %s", r.Mode.Name)
				}
			})
		}
	}
	// The walk opens in normal mode under either value; there i types and
	// esc leaves typing.
	r, _, _ := subjectAskRuntime(t, param, false)
	if err := r.Settings().Set(ModalEscClosesSetting, true); err != nil {
		t.Fatal(err)
	}
	c := pathAsk(t, r)
	formKeys(t, r, "alt+o")
	if c.Source() != "walk" || r.Mode.Name != "normal" {
		t.Fatalf("alt+o reached %q in %s, want the walk in normal", c.Source(), r.Mode.Name)
	}
	formKeys(t, r, "i", "esc")
	if r.Chooser(PathChooserKind) == nil || r.Mode.Name != "normal" {
		t.Fatalf("esc in the walk's search line: top=%q mode=%q", r.TopModal(), r.Mode.Name)
	}
}
