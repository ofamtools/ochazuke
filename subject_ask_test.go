package ochazuke

import (
	"errors"
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

func TestSubjectAskKeyWithGuardedFocusSubject(t *testing.T) {
	r, _, ran := subjectAskRuntime(t, Parameter{Type: "string", Noun: "canvas", Focus: true}, false)
	r.Instance("ink", Kind("canvas")).Label = "Ink"
	r.DefineGuard("paint-subject", func(_ *Runtime, ev KeyEvent) bool { return StringArg(ev.Args, "swatch", "") != "" })
	if err := r.AddVerbAvailability("canvas.paint", "paint-subject"); err != nil {
		t.Fatal(err)
	}
	handled, err := r.Dispatch(MacroKey{Key: "z"})
	if err != nil || !handled || r.RecordEditor(ArgsFormID) == nil {
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
