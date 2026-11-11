package ochazuke

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
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

func TestSubjectAskKeyWithGuardedFocusSubject(t *testing.T) {
	r, _, ran := subjectAskRuntime(t, Parameter{Type: "string", Noun: "canvas", Focus: true}, false)
	r.Instance("ink", Kind("canvas")).Label = "Ink"
	r.DefineGuard("paint-subject", func(_ *Runtime, ev KeyEvent) bool { return StringArg(ev.Args, "swatch", "") != "" })
	if err := r.AddVerbAvailability("canvas.paint", "paint-subject"); err != nil {
		t.Fatal(err)
	}
	handled, err := r.Dispatch(MacroKey{Key: "z"})
	if err != nil || !handled || r.Chooser(FormSuggestionsKind) == nil {
		t.Fatalf("key handled=%v err=%v top=%q", handled, err, r.TopModal())
	}
	finishSubjectAsk(t, r, "ink")
	if !reflect.DeepEqual(*ran, []string{"ink"}) {
		t.Fatalf("ran = %v", *ran)
	}
}

func TestSubjectAskCtxNeedUsesSamePicker(t *testing.T) {
	r, _, ran := subjectAskRuntime(t, Parameter{Type: "string", Noun: "swatch"}, true)
	formKeys(t, r, "z")
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

func TestSubjectAskOffRefusesPaletteKeyAndNeed(t *testing.T) {
	for _, route := range []string{"palette", "key", "need"} {
		t.Run(route, func(t *testing.T) {
			param := Parameter{Type: "string", Required: true, Noun: "swatch"}
			if route == "need" {
				param.Required = false
			}
			r, p, ran := subjectAskRuntime(t, param, route == "need")
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
			if err == nil || len(*ran) != 0 || r.Instances[ArgsFormID] != nil || r.Chooser(FormSuggestionsKind) != nil {
				t.Fatalf("err=%v ran=%v top=%q", err, *ran, r.TopModal())
			}
		})
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
		Get: func(Ctx, string) (Thing, error) { return Thing{"id": "ink"}, nil },
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

func TestSubjectAskBindingListingsMatchDispatch(t *testing.T) {
	for _, state := range []string{"asking", "disabled", "row-guard"} {
		t.Run(state, func(t *testing.T) {
			r, _, ran := subjectAskRuntime(t, Parameter{Type: "string", Noun: "canvas", Focus: true}, false)
			r.Instance("ink", Kind("canvas")).Label = "Ink"
			r.DefineGuard("paint-subject", func(_ *Runtime, ev KeyEvent) bool { return StringArg(ev.Args, "swatch", "") != "" })
			if err := r.AddVerbAvailability("canvas.paint", "paint-subject"); err != nil {
				t.Fatal(err)
			}
			key := "z"
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
			// disabled verbs, unlike those conditional rows, are omitted.
			if state != "row-guard" && listed != want {
				t.Fatalf("state=%s listed=%v, want %v", state, listed, want)
			}
			_, err := r.Dispatch(MacroKey{Key: key})
			if err != nil {
				t.Fatal(err)
			}
			if (r.Chooser(FormSuggestionsKind) != nil) != want {
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
	m := NewModel(r)
	m.Width, m.Height = 100, 30
	formKeys(t, r, "z")
	c := r.Chooser(PathChooserKind)
	if c == nil {
		t.Fatalf("path ask did not open: top=%q", r.TopModal())
	}
	landAll(m, r.PendingCommands())
	if c.Source() == PathSourceWalk || !slices.Contains(c.Sources(), PathSourceWalk) {
		t.Fatalf("explicit filesystem start lost or graph scope missing: source=%q scopes=%v", c.Source(), c.Sources())
	}
	filesystem := c.Source()
	if !slices.ContainsFunc(c.Matches(), func(row PickerRow) bool { return row.ID == leaf }) {
		t.Fatalf("filesystem walk did not list nested leaf: %#v", c.Matches())
	}
	typeKeys(t, r, "leaf")
	formKeys(t, r, "esc", "l")
	landAll(m, r.PendingCommands())
	formKeys(t, r, "alt+up")
	landAll(m, r.PendingCommands())
	if c.Source() != filesystem || !slices.ContainsFunc(c.Matches(), func(row PickerRow) bool { return row.ID == leaf }) {
		t.Fatalf("filesystem navigation escaped into graph: source=%q rows=%#v", c.Source(), c.Matches())
	}
	formKeys(t, r, "alt+enter")
	if !reflect.DeepEqual(*ran, []string{leaf}) || r.Chooser(PathChooserKind) != nil {
		t.Fatalf("filesystem selection ran=%v top=%q", *ran, r.TopModal())
	}
}
