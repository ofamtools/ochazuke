package ochazuke

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func associationFixture(t *testing.T, declared bool) *Runtime {
	t.Helper()
	r := NewRuntime()
	r.Define("thing").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "name", Role: RoleLabel})
	r.Define("surface").Props(PropSpec{Name: "subject", Association: declared, Parameter: Parameter{Noun: "thing"}})
	if _, err := r.AddPage("page", "Page", func(*Runtime, int, int) Block { return Text("page") }); err != nil {
		t.Fatal(err)
	}
	_, err := r.AddList("things", []ListItem{
		{ID: "row-a", Noun: "thing", Label: "Alpha", Data: map[string]any{"id": "a", "name": "Alpha"}},
		{ID: "row-b", Noun: "thing", Label: "Beta", Data: map[string]any{"id": "b", "name": "Beta"}},
	}, Parent("page"))
	if err != nil {
		t.Fatal(err)
	}
	r.Instance("surface", Kind("surface"), Parent("page"))
	r.Instances["surface"].Data = map[string]any{"subject": "a"}
	if err := r.SetFocus("surface"); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestAssociationFillIsDeclared(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, source              string
		prop, verb, enabled, want bool
	}{
		{"declared-key", "key:r", true, true, true, true},
		{"declared-palette", "palette", true, true, true, true},
		{"declared-macro", "macro:a", true, true, true, true},
		{"undeclared-prop", "key:r", false, true, true, false},
		{"undeclared-verb", "key:r", true, false, true, false},
		{"program-api", "api", true, true, true, false},
		{"program-cli", "cli", true, true, true, false},
		{"program-mcp", "mcp", true, true, true, false},
		{"program-hook", "hook:changed", true, true, true, false},
		{"setting-off", "key:r", true, true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := associationFixture(t, tc.prop)
			var acted string
			b := r.Define("thing").Verb("rename", func(c Ctx) Result {
				if c.Instance.Noun != "thing" {
					return Failed(errors.New("no thing at focus"))
				}
				acted = c.Instance.ID
				return Done(nil)
			})
			if tc.verb {
				b.ByAssociation()
			}
			if err := r.Settings().Set(SubjectFromAssociationSetting, tc.enabled); err != nil {
				t.Fatal(err)
			}
			defer r.Sourced(tc.source)()
			_, err := r.Execute(Request{Noun: "thing", Verb: "rename"})
			if tc.want {
				if err != nil || acted != "row-a" {
					t.Fatalf("acted %q: %v", acted, err)
				}
				h := r.History(0)
				if h[len(h)-1].Request.Target != "row-a" {
					t.Fatalf("history target: %+v", h[len(h)-1])
				}
			} else if err == nil || acted != "" {
				t.Fatalf("undeclared fill acted %q: %v", acted, err)
			}
			if r.Focus != "surface" {
				t.Fatalf("focus moved to %q", r.Focus)
			}
		})
	}
}

func TestAssociationResolutionBoundaries(t *testing.T) {
	t.Parallel()
	r := associationFixture(t, true)
	var acted string
	r.Define("thing").Verb("touch", func(c Ctx) Result { acted = c.Instance.ID; return Done(nil) }).ByAssociation()
	defer r.Sourced("key:r")()
	if _, err := r.Execute(Request{Noun: "thing", Verb: "touch", Target: "row-b"}); err != nil || acted != "row-b" {
		t.Fatalf("explicit target %q: %v", acted, err)
	}
	r.Instances["surface"].Data = map[string]any{"subject": []string{"a", "row-a"}}
	if id, many := r.nearestOf("thing", "surface"); id != "row-a" || len(many) != 0 {
		t.Fatalf("duplicate candidate %q, %+v", id, many)
	}
	r.Instances["surface"].Data = map[string]any{"subject": []any{"a", "b"}}
	acted = ""
	if _, err := r.Execute(Request{Noun: "thing", Verb: "touch"}); err == nil || !strings.Contains(err.Error(), "Alpha (a), Beta (b)") || acted != "" {
		t.Fatalf("ambiguity acted %q: %v", acted, err)
	}
	r.Instances["surface"].Data = map[string]any{"subject": "missing"}
	if got := r.Associations("surface", "thing"); len(got) != 0 {
		t.Fatalf("stale association %+v", got)
	}
	if id, _ := r.nearestOf("thing", "row-b"); id != "row-b" {
		t.Fatalf("noun path: %q", id)
	}
}

func TestAssociationGuardsAndRequiredParameters(t *testing.T) {
	t.Parallel()
	r := associationFixture(t, true)
	var acted string
	r.DefineGuard("alpha", func(_ *Runtime, ev KeyEvent) bool { return ev.Focus == "row-a" })
	r.Define("thing").Verb("touch", func(c Ctx) Result { acted = c.Instance.ID; return Done(nil) }).ByAssociation().When("alpha")
	defer r.Sourced("key:r")()
	if _, err := r.Execute(Request{Noun: "thing", Verb: "touch"}); err != nil || acted != "row-a" {
		t.Fatalf("guard target %q: %v", acted, err)
	}
	op := r.Operations.At(Key("thing.touch"))
	op.Parameters = map[string]Parameter{"name": {Type: "string", Required: true, Focus: true, Noun: "thing"}}
	r.Operations.Set(op)
	acted = ""
	if _, err := r.Execute(Request{Noun: "thing", Verb: "touch"}); err == nil || !strings.Contains(err.Error(), "name is required") || acted != "" {
		t.Fatalf("required filled %q: %v", acted, err)
	}
	if _, ok := r.focusValue(op, "name", "surface"); ok {
		t.Fatal("required Focus parameter filled")
	}
	op.Parameters["name"] = Parameter{Type: "string", Focus: true, Noun: "thing"}
	if got, ok := r.focusValue(op, "name", "surface"); !ok || got.Key != "a" || got.Label != "Alpha" {
		t.Fatalf("optional fill %+v, %v", got, ok)
	}
}

func TestAssociationListReadsDataOnly(t *testing.T) {
	t.Parallel()
	r := associationFixture(t, true)
	got, err := r.Execute(Request{Noun: "ochazuke.association", Verb: "list", Args: Arguments{"noun": "thing"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []map[string]any{{"id": "surface/subject/thing/a", "noun": "thing", "key": "a", "label": "Alpha", "via": "subject", "from": "surface"}}
	if !reflect.DeepEqual(AnswerRows(got), any(want)) {
		t.Fatalf("associations %+v, want %+v", got, want)
	}
}

func TestAssociationModalReturnsToLiveFocus(t *testing.T) {
	t.Parallel()
	r := associationFixture(t, true)
	if err := r.SetMode("terminal"); err != nil {
		t.Fatal(err)
	}
	if err := r.List("things").Select("row-b"); err != nil {
		t.Fatal(err)
	}
	r.Define("thing").Verb("rename-prompt", func(c Ctx) Result {
		if c.Instance.ID != "row-a" {
			return Failed(errors.New("acted on unrelated selection"))
		}
		_, err := c.Runtime.OpenModal("rename", Payload{Prompt: "Rename " + c.Instance.Label},
			WithInput(NewInput("name", c.Instance.Label, false), Kind("ochazuke.prompt")))
		if err != nil {
			return Failed(err)
		}
		return Done(nil)
	}).ByAssociation()
	defer r.Sourced("key:r")()
	if _, err := r.Execute(Request{Noun: "thing", Verb: "rename-prompt"}); err != nil {
		t.Fatal(err)
	}
	if r.TopModal() != "rename" || r.Instances["rename"].Payload.Prompt != "Rename Alpha" {
		t.Fatalf("wrong modal: %q", r.TopModal())
	}
	if err := r.Pop(); err != nil {
		t.Fatal(err)
	}
	if r.Focus != "surface" || r.BaseMode() != "terminal" {
		t.Fatalf("return: focus %q mode %q", r.Focus, r.BaseMode())
	}
}

func TestAssociationGuardedRepeatFollowsNewSubject(t *testing.T) {
	t.Parallel()
	r := associationFixture(t, true)
	var acted []string
	r.DefineGuard("thing-row", func(r *Runtime, ev KeyEvent) bool {
		return r.Instances[ev.Focus].Noun == "thing"
	})
	r.Define("thing").Verb("touch", func(c Ctx) Result {
		acted = append(acted, c.Instance.ID)
		return Done(nil)
	}).ByAssociation().When("thing-row")
	op := r.Operations.At(Key("thing.touch"))
	op.Repeatable = true
	r.Operations.Set(op)
	if !r.VerbShown(op.Key.Short(), "surface", r.Mode) {
		t.Fatal("palette hid the associated verb")
	}
	defer r.Sourced("key:r")()
	if _, err := r.Execute(Request{Noun: "thing", Verb: "touch"}); err != nil {
		t.Fatal(err)
	}
	r.Instances["surface"].Data = map[string]any{"subject": "b"}
	if _, err := r.Execute(Request{Noun: "ochazuke.history-entry", Verb: "repeat"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(acted, []string{"row-a", "row-b"}) {
		t.Fatalf("repeat subjects %v", acted)
	}
}
