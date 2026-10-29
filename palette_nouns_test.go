package ochazuke

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// A palette noun's instances complete after the noun (and as its default
// verb's argument), read live on every listing; one runs the default verb.
func TestPalettePageInstancesAreLive(t *testing.T) {
	r := NewRuntime()
	p := NewPalette(r)
	for i := range 60 {
		page := pageTestRegister(t, r, fmt.Sprintf("page-%02d", i))
		page.Title = fmt.Sprintf("Workspace %02d", i)
	}
	paletteRequest(t, r, "open", "page", Arguments{"id": "page-00"})
	paletteRequest(t, r, "open", "palette", nil)
	paletteRequest(t, r, "set", "palette", Arguments{"text": "page "})
	instances, verbs := 0, 0
	for _, entry := range p.completions() {
		switch entry.Kind {
		case "choice":
			instances++
		case "verb":
			verbs++
		}
	}
	// open, pick, next, previous, hide and show.
	if instances != 60 || verbs != 6 {
		t.Fatalf("page completes %d instances and %d verbs, want 60 and 6", instances, verbs)
	}
	pageTestRegister(t, r, "late-page").Title = "Newly registered workspace"
	paletteRequest(t, r, "set", "palette", Arguments{"text": "page late"})
	entries := p.completions()
	if len(entries) != 1 || entries[0].ID != "late-page" || !strings.Contains(entries[0].Label, "Newly registered workspace") {
		t.Fatalf("live instance = %#v", entries)
	}
	if !strings.Contains(p.View(), "late-page") {
		t.Fatalf("view hides the instance:\n%s", p.View())
	}
	paletteRequest(t, r, "run", "palette", nil)
	if p.IsOpen() || r.Page != "late-page" || len(r.Modals()) != 0 {
		t.Fatalf("instance did not open its page: page=%s frames=%d", r.Page, len(r.Modals()))
	}
	// The default verb's argument completes from the same source.
	paletteRequest(t, r, "open", "palette", nil)
	if c := p.parse("open page page-5"); c.Err == nil || !strings.Contains(c.Err.Error(), "ambiguous") {
		t.Fatalf("page-5 is a prefix of ten pages: %v", c.Err)
	}
	paletteLine(t, r, "open page page-59")
	if r.Page != "page-59" || p.IsOpen() {
		t.Fatalf("open page page-59: page=%s open=%v", r.Page, p.IsOpen())
	}
}

// A verb wins over an instance of the same name after a noun; the instance
// is still reachable as the verb's argument.
func TestPaletteNounVerbsAndInstancesDoNotCollide(t *testing.T) {
	r := NewRuntime()
	for _, id := range []string{"home", "open"} {
		pageTestRegister(t, r, id)
	}
	p := NewPalette(r)
	paletteRequest(t, r, "open", "page", Arguments{"id": "home"})
	paletteRequest(t, r, "open", "palette", nil)
	if c := p.parse("page open"); c.Op.Key() != "ochazuke.page.open" || c.Missing != "id" {
		t.Fatalf("page open = %s missing %q", c.Op.Key(), c.Missing)
	}
	paletteLine(t, r, "page open open")
	if r.Page != "open" || p.IsOpen() {
		t.Fatalf("page open open: page=%s", r.Page)
	}
}

func TestPaletteInstanceUsesConfiguredDefaultAndCapturedOrigin(t *testing.T) {
	r := NewRuntime()
	pageTestRegister(t, r, "home")
	pageTestRegister(t, r, "target")
	paletteRequest(t, r, "open", "page", Arguments{"id": "home"})
	paletteRequest(t, r, "focus", "input", Arguments{"id": "home-input"})
	origin := r.Context()
	var gotContext Context
	var gotArgs Arguments
	r.Register(Operation{Verb: "inspect", Noun: "ochazuke.page", Parameters: map[string]Parameter{"pageID": {Type: "string", Required: true}}, Run: func(_ *Runtime, _ Context, _ Arguments) (any, error) {
		t.Fatal("instance bypassed layered dispatch")
		return nil, nil
	}})
	r.Define("home-editor").Action("ochazuke.page.inspect", func(_ *Runtime, context Context, args Arguments) (any, error) {
		gotContext, gotArgs = context, args
		return nil, nil
	})
	p := NewPalette(r)
	if err := p.RegisterNoun("ochazuke.page", PaletteNoun{Source: PagesPaletteSource, DefaultVerb: "inspect", Argument: "pageID"}); err != nil {
		t.Fatal(err)
	}
	paletteRequest(t, r, "open", "palette", nil)
	paletteLine(t, r, "page target")
	if !reflect.DeepEqual(gotContext, origin) || !reflect.DeepEqual(gotArgs, Arguments{"pageID": "target"}) || r.Page != "home" || p.IsOpen() {
		t.Fatalf("configured dispatch: context=%#v args=%#v page=%s open=%v", gotContext, gotArgs, r.Page, p.IsOpen())
	}
}

func TestPaletteInstanceFailurePreservesModalAndOrigin(t *testing.T) {
	r := NewRuntime()
	pageTestRegister(t, r, "home")
	pageTestRegister(t, r, "target")
	paletteRequest(t, r, "open", "page", Arguments{"id": "home"})
	paletteRequest(t, r, "focus", "input", Arguments{"id": "home-input"})
	if err := r.Push("dialog", r.Focus); err != nil {
		t.Fatal(err)
	}
	origin := r.Context()
	p := NewPalette(r)
	paletteRequest(t, r, "open", "palette", nil)
	paletteRequest(t, r, "set", "palette", Arguments{"text": "page target"})
	_, err := r.Execute(Request{Verb: "run", Noun: "ochazuke.palette"})
	if err == nil || !strings.Contains(err.Error(), "modal") || !p.IsOpen() || len(r.Modals()) != 2 || r.Modals()[0].ID != "dialog" || r.Page != "home" || !reflect.DeepEqual(p.origin, origin) || p.Text() != "page target" {
		t.Fatalf("failed instance lost modal/origin: err=%v state=%#v", err, p.state())
	}
	paletteRequest(t, r, "close", "palette", nil)
	if !reflect.DeepEqual(r.Context(), origin) || len(r.Modals()) != 1 {
		t.Fatal("closing failed palette lost modal focus")
	}
	paletteRequest(t, r, "pop", "modal", nil)
	paletteRequest(t, r, "open", "palette", nil)
	paletteLine(t, r, "page target")
	if p.IsOpen() || r.Page != "target" {
		t.Fatal("page instance failed after modal closed")
	}
}

func TestPaletteInstanceUsesRuntimeValidationAndRestoresOnError(t *testing.T) {
	r := NewRuntime()
	var captured Context
	origin := r.Context()
	var sourceContext Context
	r.Register(Operation{Verb: "use", Noun: "thing", Parameters: map[string]Parameter{"id": {Type: "string", Required: true}}, Run: func(_ *Runtime, c Context, _ Arguments) (any, error) {
		captured = c
		return nil, errors.New("thing rejected")
	}})
	p := NewPalette(r)
	if err := p.RegisterNoun("thing", PaletteNoun{Source: func(_ *Runtime, c Context) []PaletteInstance {
		sourceContext = c
		return []PaletteInstance{{ID: "x", Label: "Example"}}
	}, DefaultVerb: "use", Argument: "id"}); err != nil {
		t.Fatal(err)
	}
	paletteRequest(t, r, "open", "palette", nil)
	paletteRequest(t, r, "set", "palette", Arguments{"text": "thing x"})
	_, err := r.Execute(Request{Verb: "run", Noun: "ochazuke.palette"})
	if err == nil || err.Error() != "thing rejected" || !p.IsOpen() || !reflect.DeepEqual(captured, origin) || !reflect.DeepEqual(sourceContext, origin) || p.Text() != "thing x" {
		t.Fatalf("failure dispatch/restoration: %v %#v", err, p.state())
	}
	op := r.Operations["thing.use"]
	op.Parameters["id"] = Parameter{Type: "integer", Required: true}
	captured = Context{}
	_, err = r.Execute(Request{Verb: "run", Noun: "ochazuke.palette"})
	if err == nil || !strings.Contains(err.Error(), "integer") || !p.IsOpen() || len(captured.Path) != 0 {
		t.Fatalf("instance bypassed live validation: %v", err)
	}
}

func TestPaletteNounRegistrationValidation(t *testing.T) {
	r := NewRuntime()
	r.Register(Operation{Verb: "use", Noun: "thing", Parameters: map[string]Parameter{"count": {Type: "integer", Required: true}, "id": {Type: "string", Required: true}}, Run: func(*Runtime, Context, Arguments) (any, error) { return nil, nil }})
	p := NewPalette(r)
	for _, test := range []struct {
		name, noun, verb, argument, message string
		source                              PaletteSource
	}{
		{"unknown noun", "missing", "open", "id", "unknown noun", PagesPaletteSource},
		{"nil source", "page", "open", "id", "source is required", nil},
		{"unknown action", "page", "missing", "id", "unknown default action", PagesPaletteSource},
		{"unknown argument", "page", "open", "missing", "string instance argument", PagesPaletteSource},
		{"non-string argument", "thing", "use", "count", "string instance argument", PagesPaletteSource},
		{"additional required argument", "thing", "use", "id", "also requires argument", PagesPaletteSource},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := p.RegisterNoun(test.noun, PaletteNoun{Source: test.source, DefaultVerb: test.verb, Argument: test.argument})
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("error = %v, want %q", err, test.message)
			}
		})
	}
}

// A verb guarded with Verb(...).When is offered only while its guard holds
// at the focus and mode the palette opened over; the mode the palette's own
// line pins does not count. A default verb cannot be guarded.
func TestPaletteHidesGuardedVerbs(t *testing.T) {
	r := NewRuntime()
	if err := r.DefineMode("selecting", false); err != nil {
		t.Fatal(err)
	}
	r.DefineGuard("selecting", func(_ *Runtime, ev KeyEvent) bool { return ev.Mode.Name == "selecting" })
	r.DefineGuard("on-sheet", func(r *Runtime, ev KeyEvent) bool { return slices.Contains(r.Ancestry(ev.Focus), "sheet") })
	run := func(*Runtime, Context, Arguments) (any, error) { return nil, nil }
	r.Define("sheet").Verb("save", run)
	r.Define("chosen").Verb("grab", run).When("selecting").Verb("drop", run).When("selecting").Verb("count", run)
	r.Define("rowish").Verb("delete", run).When("on-sheet")
	r.Instance("sheet")
	r.Instance("cell", Parent("sheet"))
	r.Instance("elsewhere")
	if r.VerbGuard("chosen.grab") != "selecting" || r.VerbGuard("chosen.count") != "" {
		t.Fatalf("guards: %v", r.Nouns["chosen"].Guards)
	}
	p := NewPalette(r)
	offered := func() map[string]bool {
		out := map[string]bool{}
		for _, op := range r.Catalog() {
			if p.offered(op) {
				out[op.Key()] = true
			}
		}
		return out
	}
	values := func(line string) []string {
		paletteRequest(t, r, "set", "palette", Arguments{"text": line})
		return completionValues(p)
	}
	if err := r.SetFocus("cell"); err != nil {
		t.Fatal(err)
	}
	paletteRequest(t, r, "open", "palette", nil)
	if got := offered(); got["chosen.grab"] || got["chosen.drop"] || !got["chosen.count"] || !got["rowish.delete"] {
		t.Fatalf("normal mode on the sheet offered %v", got)
	}
	if v := values(""); slices.Contains(v, "verb:grab") || !slices.Contains(v, "noun:chosen") || !slices.Contains(v, "verb:count") {
		t.Fatalf("root entries: %v", v)
	}
	if c := p.parse("grab chosen"); c.Err == nil {
		t.Fatal("a guarded verb parsed")
	}
	if v := values("chosen "); !reflect.DeepEqual(v, []string{"verb:count"}) {
		t.Fatalf("chosen verbs in normal mode: %v", v)
	}
	paletteRequest(t, r, "close", "palette", nil)

	r.Mode = ModeState{Name: "selecting"}
	paletteRequest(t, r, "open", "palette", nil)
	if r.Mode.Name == "selecting" {
		t.Fatal("the palette field did not take its own mode; the test proves nothing")
	}
	if v := values("chosen "); !reflect.DeepEqual(v, []string{"verb:count", "verb:drop", "verb:grab"}) {
		t.Fatalf("chosen verbs while selecting: %v", v)
	}
	paletteRequest(t, r, "close", "palette", nil)

	r.Mode = ModeState{Name: "normal"}
	if err := r.SetFocus("elsewhere"); err != nil {
		t.Fatal(err)
	}
	paletteRequest(t, r, "open", "palette", nil)
	if got := offered(); got["rowish.delete"] || !got["sheet.save"] {
		t.Fatalf("off the sheet offered %v", got)
	}
	if v := values(""); slices.Contains(v, "noun:rowish") {
		t.Fatalf("a noun with only hidden verbs is listed: %v", v)
	}
	paletteRequest(t, r, "close", "palette", nil)

	// A palette noun's default verb is never guarded.
	r.Register(Operation{Verb: "use", Noun: "rowish", Parameters: map[string]Parameter{"id": {Type: "string", Required: true}}, Run: run})
	r.Define("rowish").Verb("use", run).When("on-sheet")
	err := p.RegisterNoun("rowish", PaletteNoun{Source: func(*Runtime, Context) []PaletteInstance { return nil }, DefaultVerb: "use", Argument: "id"})
	if err == nil || !strings.Contains(err.Error(), "never guarded") {
		t.Fatalf("guarded default verb accepted: %v", err)
	}
}

// The first word lists the nouns of the focus stack first, the focused noun
// first, then the rest of the catalog by name.
func TestPaletteListsFocusStackNounsFirst(t *testing.T) {
	r := NewRuntime()
	run := func(*Runtime, Context, Arguments) (any, error) { return nil, nil }
	for _, noun := range []string{"sheet", "zcell", "arow", "unrelated"} {
		r.Define(noun).Verb("poke", run)
	}
	r.Instance("sheet", Kind("sheet"))
	r.Instance("r1", Parent("sheet"), Kind("arow"))
	r.Instance("r1:c1", Parent("r1"), Kind("zcell"))
	if err := r.SetFocus("r1:c1"); err != nil {
		t.Fatal(err)
	}
	p := NewPalette(r)
	paletteRequest(t, r, "open", "palette", nil)
	var nouns []string
	for _, e := range p.completions() {
		if e.Kind == "noun" {
			nouns = append(nouns, e.Label)
		}
	}
	if len(nouns) < 5 || !reflect.DeepEqual(nouns[:4], []string{"zcell", "arow", "sheet", "app"}) || slices.Index(nouns, "unrelated") < 4 {
		t.Fatalf("noun order = %v", nouns)
	}
}
