package ochazuke

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func dependentFixture(t *testing.T, pending bool) (*Runtime, Parameter) {
	t.Helper()
	r := NewRuntime()
	r.Config().Disable()
	r.Register(Operation{Noun: "options", Verb: "list", Parameters: map[string]Parameter{"group": {Type: "string"}, "query": {Type: "string"}}, App: func(c Ctx) Result {
		group := StringArg(c.Args, "group", "default")
		values := []string{group + "-a", group + "-b"}
		if pending {
			return Pending(func(context.Context) (any, error) { return values, nil })
		}
		return Done(values)
	}})
	return r, Parameter{Type: "string", Source: "options.list", SourceArgs: map[string]string{"group": "category"}}
}

func TestSourceArgsChoicesAndValidation(t *testing.T) {
	r, p := dependentFixture(t, false)
	params := map[string]Parameter{"category": {Type: "string"}, "item": p}
	r.Register(Operation{Noun: "record", Verb: "create", Parameters: params, Run: func(*Runtime, Context, Arguments) (any, error) { return true, nil }})
	for _, tc := range []struct {
		args Arguments
		want string
	}{{nil, "default-a"}, {Arguments{"category": "other"}, "other-a"}, {Arguments{"category": ""}, "default-a"}} {
		choices, err := r.ChoicesFor(r.Context(), p, "", tc.args)
		if err != nil || len(choices) != 2 || choices[0].ID != tc.want {
			t.Fatalf("%v: %v %v", tc.args, choices, err)
		}
	}
	for _, tc := range []struct {
		value string
		valid bool
	}{{"other-a", true}, {"default-a", false}} {
		_, err := r.Execute(Request{Noun: "record", Verb: "create", Args: Arguments{"category": "other", "item": tc.value}})
		if (err == nil) != tc.valid {
			t.Fatalf("%s: %v", tc.value, err)
		}
	}
	p.Free = true
	params["item"] = p
	if err := r.checkArguments(Operation{Parameters: params}, Arguments{"category": "other", "item": "custom"}); err != nil {
		t.Fatal(err)
	}
}

func TestSourceArgsSchema(t *testing.T) {
	r, p := dependentFixture(t, false)
	for _, tc := range []struct {
		mapping map[string]string
		want    string
	}{{map[string]string{"bad": "category"}, "has no parameter"}, {map[string]string{"group": "missing"}, "no sibling"}, {map[string]string{"group": "item"}, "another field"}} {
		p.SourceArgs = tc.mapping
		if err := r.CheckSourceArgs(map[string]Parameter{"item": p, "category": {Type: "string"}}); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%v: %v", tc.mapping, err)
		}
	}
	p.SourceArgs = map[string]string{"group": "category"}
	p.Source = "absent.list"
	defer func() {
		if recover() == nil {
			t.Error("Register accepted an unknown dependent source")
		}
	}()
	r.Register(Operation{Noun: "record", Verb: "bad", Parameters: map[string]Parameter{"category": {}, "item": p}, Run: func(*Runtime, Context, Arguments) (any, error) { return nil, nil }})
}

func TestDependentRecordChoicesRetainValues(t *testing.T) {
	for _, free := range []bool{false, true} {
		t.Run(map[bool]string{false: "closed", true: "free"}[free], func(t *testing.T) {
			r, p := dependentFixture(t, false)
			p.Free = free
			e, err := r.AddRecordEditor("rec", RecordSpec{Fields: []RecordField{{Name: "item", Parameter: p}, {Name: "category", Parameter: Parameter{Type: "string"}}}, Load: func() (Record, error) { return Record{"category": "one", "item": "one-a"}, nil }, Save: func(RecordWrite) Result { return Done(nil) }})
			if err != nil {
				t.Fatal(err)
			}
			f := e.field("item")
			if !hasChoice(f.choices, "one-a") {
				t.Fatal(f.choices)
			} // dependency may appear after its consumer
			if err := e.set(e.field("category"), "two"); err != nil {
				t.Fatal(err)
			}
			if err := e.Flush(); err != nil {
				t.Fatal(err)
			}
			if !hasChoice(f.choices, "two-a") || f.text() != "one-a" || !e.staleChoice(f) {
				t.Fatalf("%+v %q", f.choices, f.text())
			}
			if free && e.Problem("item") != "" {
				t.Fatal(e.Problem("item"))
			}
			if !free && e.Problem("item") == "" {
				t.Fatal("closed stale value must be flagged")
			}
			if err := r.Settings().Set(DependentChoicesSetting, "false"); err != nil {
				t.Fatal(err)
			}
			if e.staleChoice(f) {
				t.Fatal("warning setting ignored")
			}
		})
	}
}

func TestDependentPendingChoicesIgnoreOldAnswer(t *testing.T) {
	r, p := dependentFixture(t, true)
	e, err := r.AddRecordEditor("rec", RecordSpec{Fields: []RecordField{{Name: "category", Parameter: Parameter{Type: "string"}}, {Name: "item", Parameter: p}}, Load: func() (Record, error) { return Record{"category": "one"}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	old := runCommands(r.PendingCommands())
	if err := e.set(e.field("category"), "two"); err != nil {
		t.Fatal(err)
	}
	e.refreshChoices()
	// Deliver both loads after the second dependency value was chosen.
	for _, msg := range runCommands(r.PendingCommands()) {
		r.Deliver(msg)
	}
	for _, msg := range old {
		r.Deliver(msg)
	}
	if got := e.field("item").choices; !hasChoice(got, "two-a") || hasChoice(got, "one-a") {
		t.Fatal(got)
	}
}

func TestSourceArgsQueryAndTypedValues(t *testing.T) {
	op := Operation{Parameters: map[string]Parameter{"query": {}, "n": {}, "flag": {}}}
	p := Parameter{SourceArgs: map[string]string{"n": "count", "flag": "enabled"}}
	got := choicesSourceArgs(op, p, "typed", Arguments{"count": 3, "enabled": false})
	if !reflect.DeepEqual(got, Arguments{"query": "typed", "n": 3, "flag": false}) {
		t.Fatal(got)
	}
	p.SourceArgs["query"] = "search"
	got = choicesSourceArgs(op, p, "typed", Arguments{})
	if _, ok := got["query"]; ok {
		t.Fatal(got)
	}
}

func TestDependentChoicesSourceFailureKeepsValue(t *testing.T) {
	r, p := dependentFixture(t, false)
	op := r.Operations["options.list"]
	op.App = func(Ctx) Result { return Failed(errors.New("offline")) }
	r.Operations[op.Key()] = op
	e, err := r.AddRecordEditor("rec", RecordSpec{Fields: []RecordField{{Name: "category"}, {Name: "item", Parameter: p}}, Load: func() (Record, error) { return Record{"category": "one", "item": "kept"}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	if e.Values()["item"] != "kept" || e.staleChoice(e.field("item")) {
		t.Fatal(e.Values())
	}
}

func TestDependentArgsFormAndPropFields(t *testing.T) {
	r, p := dependentFixture(t, false)
	p.Free = true
	op := Operation{Noun: "record", Verb: "create", Parameters: map[string]Parameter{"category": {Type: "string"}, "item": p}, Run: func(*Runtime, Context, Arguments) (any, error) { return true, nil }}
	r.Register(op)
	ed, err := r.OpenArgsForm(Request{Noun: op.Noun, Verb: op.Verb, Args: Arguments{"category": "two"}}, func(Request) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if !hasChoice(ed.field("item").choices, "two-a") {
		t.Fatal(ed.field("item").choices)
	}
	fields := []RecordField{PropField(PropSpec{Name: "category", Parameter: Parameter{Type: "string"}}), PropField(PropSpec{Name: "item", Parameter: p})}
	if _, err := r.AddRecordEditor("props", RecordSpec{Fields: fields, Load: func() (Record, error) { return Record{"category": "one"}, nil }}); err != nil {
		t.Fatal(err)
	}
}

func TestDependentPaletteChoicesUseCurrentSibling(t *testing.T) {
	r, p := dependentFixture(t, false)
	op := Operation{Noun: "record", Verb: "create", Parameters: map[string]Parameter{"category": {Type: "string"}, "item": p}, Run: func(*Runtime, Context, Arguments) (any, error) { return nil, nil }}
	r.Register(op)
	palette := NewPalette(r)
	palette.origin = r.Context()
	for _, group := range []string{"one", "two"} {
		got, err := palette.paramChoices(op, "item", "", Arguments{"category": group})
		if err != nil || !hasChoice(got, group+"-a") {
			t.Fatalf("%v %v", got, err)
		}
	}
}

func TestDependentSourceChecksAreRespected(t *testing.T) {
	r, p := dependentFixture(t, false)
	if err := r.AddVerbCheck("options.list", VerbCheck{Name: "read-check", Check: func(*Runtime, Context, Arguments) Verdict { return Refuse("blocked") }}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Choices(r.Context(), p, Arguments{"category": "one"}); err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatal(err)
	}
	called := false
	r.loadChoices(r.Context(), p, "app", func(_ []Choice, err error) {
		called = true
		if err == nil || !strings.Contains(err.Error(), "blocked") {
			t.Fatal(err)
		}
	}, Arguments{"category": "one"})
	if !called {
		t.Fatal("missing failure callback")
	}
}

func TestSourceArgsSynchronousReadsKeepHistory(t *testing.T) {
	r, p := dependentFixture(t, true)
	_, err := r.Choices(r.Context(), p, Arguments{"category": "two"})
	if err != nil {
		t.Fatal(err)
	}
	history := r.History(1)
	if len(history) != 1 || history[0].Request.Key() != "options.list" || history[0].Request.Args["group"] != "two" {
		t.Fatal(history)
	}
}

func TestDependentOpenChooserDropsOldSiblingResponses(t *testing.T) {
	r, p := dependentFixture(t, true)
	p.Free = true
	e, err := r.AddRecordEditor("rec", RecordSpec{Fields: []RecordField{{Name: "category", Parameter: Parameter{Type: "string"}}, {Name: "item", Parameter: p}}, Load: func() (Record, error) { return Record{"category": "one"}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.SetFocus(e.field("item").id); err != nil {
		t.Fatal(err)
	}
	if err := e.suggest(e.field("item")); err != nil {
		t.Fatal(err)
	}
	old := runCommands(r.PendingCommands())
	if err := e.set(e.field("category"), "two"); err != nil {
		t.Fatal(err)
	}
	e.refreshChoices()
	for _, msg := range runCommands(r.PendingCommands()) {
		r.Deliver(msg)
	}
	for _, msg := range old {
		r.Deliver(msg)
	}
	chooser := r.Chooser(FormSuggestionsKind)
	matches := chooser.Matches()
	if len(matches) != 2 || matches[0].ID != "two-a" || matches[1].ID != "two-b" {
		t.Fatal(matches)
	}
}

// A sibling edit must reach both the cached form hints and a chooser that is
// already drawn, including when background frames share the render budget.
func TestDependentChoicesInvalidateRenderedChooserAndHints(t *testing.T) {
	r, p := dependentFixture(t, false)
	m := NewModel(r)
	now := time.Unix(100, 0)
	m.Now = func() time.Time { return now }
	e, err := r.OpenRecordEditor("rec", RecordSpec{
		Fields: []RecordField{{Name: "category", Parameter: Parameter{Type: "string"}}, {Name: "item", Parameter: p}},
		Load:   func() (Record, error) { return Record{"category": "one", "item": "one-a"}, nil },
		Submit: func(Record) Result { return Done(nil) },
	})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		m.View()
		if hints := BindingHints(r, r.Focus); !strings.Contains(hints, "form.run") {
			t.Fatalf("valid form lacks run hint: %s", hints)
		}
	}
	if err := e.set(e.field("category"), "two"); err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: m.Width, Height: m.Height})
	m.View()
	if hints := BindingHints(r, r.Focus); strings.Contains(hints, "form.run") {
		t.Fatalf("stale value kept run hint: %s", hints)
	}
	if err := r.SetFocus(e.field("item").id); err != nil {
		t.Fatal(err)
	}
	if err := e.choose(e.field("item")); err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: m.Width, Height: m.Height})
	frame := ansi.Strip(m.View().Content)
	if !strings.Contains(frame, "two-b") || strings.Contains(frame, "one-b") {
		t.Fatalf("first chooser: %s", frame)
	}
	if err := e.set(e.field("category"), "three"); err != nil {
		t.Fatal(err)
	}
	m.Update(struct{}{})
	m.View() // this background frame may reuse the preceding composition
	if m.redraw.pending.IsZero() {
		t.Fatal("test did not exercise the render budget")
	}
	now = m.redraw.pending
	m.Update(renderFrameMsg{m, now})
	frame = ansi.Strip(m.View().Content)
	if !strings.Contains(frame, "three-b") || strings.Contains(frame, "two-b") {
		t.Fatalf("stale rendered chooser after sibling change: %s", frame)
	}
}
