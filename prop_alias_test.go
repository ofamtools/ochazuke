package ochazuke

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"ofam.dev/ochazuke/refdoc"
)

func propAliasFixture(t *testing.T) (*Runtime, *List) {
	t.Helper()
	r := NewRuntime()
	r.Config().Dir = t.TempDir()
	r.Define("ticket").Props(PropSpec{Name: "state", Aliases: []string{"status"}, Parameter: Parameter{Enum: []string{"todo", "done"}}})
	l, err := r.AddList("tickets", []ListItem{
		{ID: "a", Noun: "ticket", Data: Thing{"state": "todo", "status": "done"}},
		{ID: "b", Noun: "ticket", Data: Thing{"state": "done", "status": "todo"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	r.Focus = "a"
	return r, l
}

func TestPropAliasFilterResolution(t *testing.T) {
	r, l := propAliasFixture(t)
	if _, err := r.Execute(Request{Noun: "list", Verb: "filter-prop", Args: Arguments{"id": l.id, "prop": "status", "value": "todo"}}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(shownIDs(l), []string{"a"}) || l.PropFilters()[0].Prop != "state" {
		t.Fatalf("%v %v", shownIDs(l), l.PropFilters())
	}
	if strip := l.propStrip(); !strings.Contains(strip, "state = todo") || strings.Contains(strip, "status") {
		t.Fatal(strip)
	}
	if _, err := r.Execute(Request{Noun: "list", Verb: "filter-prop-clear", Args: Arguments{"id": l.id, "prop": "status"}}); err != nil {
		t.Fatal(err)
	}
	if len(l.PropFilters()) != 0 {
		t.Fatal(l.PropFilters())
	}
	if err := l.pickPropFilter("status", false, false); err != nil {
		t.Fatal(err)
	}
	c := r.Chooser("list-filter-value")
	if c == nil || c.spec.Title != "Filter · state" || len(c.Matches()) != 2 {
		t.Fatalf("%+v", c)
	}
}

func TestPropAliasFilterFormAndChooserOnlyMain(t *testing.T) {
	r, l := propAliasFixture(t)
	if got := l.filterProps(); !slices.Equal(got, []string{"state"}) {
		t.Fatal(got)
	}
	if err := l.openFilterForm(); err != nil {
		t.Fatal(err)
	}
	form := r.RecordEditor(listFilterFormID)
	if len(form.spec.Fields) != 1 || form.spec.Fields[0].Name != "state" {
		t.Fatalf("%+v", form.spec.Fields)
	}
	form.Leave()
	if err := r.Settings().Set(ListFilterFormSetting, false); err != nil {
		t.Fatal(err)
	}
	if err := l.pickPropFilter("", false, false); err != nil {
		t.Fatal(err)
	}
	rows := r.Chooser("list-filter-prop").Matches()
	if len(rows) != 1 || rows[0].ID != "state" {
		t.Fatal(rows)
	}
}

func TestPropAliasSavedFilterLoadAndSave(t *testing.T) {
	r, l := propAliasFixture(t)
	text := `[{"name":"old-where","noun":"ticket","where":{"status":"todo"}},{"name":"old-condition","list":"tickets","conditions":[{"prop":"status","value":"done"}]}]`
	if err := r.Settings().Set("list.filters", text); err != nil {
		t.Fatal(err)
	}
	saved, err := l.SavedFilters()
	if err != nil {
		t.Fatal(err)
	}
	if saved[0].Where["state"] != "todo" || saved[1].Conditions[0].Prop != "state" {
		t.Fatal(saved)
	}
	if r.Settings().String("list.filters") != text {
		t.Fatal("loading rewrote settings")
	}
	for name, id := range map[string]string{"old-where": "a", "old-condition": "b"} {
		if err := l.ApplyFilter(name); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(shownIDs(l), []string{id}) {
			t.Fatal(shownIDs(l))
		}
	}
	if err := l.SaveFilter("old-condition"); err != nil {
		t.Fatal(err)
	}
	var rows []SavedFilter
	if err := json.Unmarshal([]byte(r.Settings().String("list.filters")), &rows); err != nil {
		t.Fatal(err)
	}
	if rows[1].Conditions[0].Prop != "state" {
		t.Fatal(rows)
	}
	r.Define("ticket").Filters(SavedFilter{Name: "default", Where: map[string]any{"status": "todo"}})
	filters, err := r.namedPropFilter(r.Noun("ticket"), "default")
	if err != nil || len(filters) != 1 || filters[0].Prop != "state" {
		t.Fatalf("%v %v", filters, err)
	}
}

func TestPropAliasStandardRequests(t *testing.T) {
	r := NewRuntime()
	r.Config().Disable()
	thing := Thing{"id": "a", "state": "todo"}
	var query ListQuery
	r.Define("ticket").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "state", Aliases: []string{"status"}}).Store(Store{Kind: Collection,
		Get: func(Ctx, string) (Thing, error) { return thing, nil },
		Patch: func(_ Ctx, _ string, changes Thing) (Thing, error) {
			for k, v := range changes {
				thing[k] = v
			}
			return thing, nil
		},
		List: func(_ Ctx, q ListQuery) ([]Thing, error) { query = q; return []Thing{thing}, nil },
	})
	for _, q := range []Request{
		{Noun: "ticket", Verb: "get", Args: Arguments{"id": "a", "prop": "status"}},
		{Noun: "ticket", Prop: "status", Args: Arguments{"id": "a"}},
	} {
		v, err := r.Execute(q)
		if err != nil || v != "todo" {
			t.Fatalf("%v %v", v, err)
		}
	}
	v, err := r.Execute(Request{Noun: "ticket", Verb: "list", Args: Arguments{"where": "status=todo", "props": "status"}})
	if err != nil || !reflect.DeepEqual(query.Where, map[string]any{"state": "todo"}) {
		t.Fatalf("%v %v %v", v, query, err)
	}
	for _, args := range []Arguments{{"prop": "status", "value": "done"}, {"changes": map[string]any{"status": "done"}}, {"prop": "status=done"}} {
		args["id"] = "a"
		if _, err := r.Execute(Request{Noun: "ticket", Verb: "set", Args: args}); err != nil {
			t.Fatal(err)
		}
		if thing["state"] != "done" || len(thing) != 2 {
			t.Fatal(thing)
		}
	}
	if word := r.ReadNounWord("ticket", "status", nil); word.Value != "state" {
		t.Fatal(word)
	}
	if p, ok := r.PropOf("ticket", "status"); !ok || p.Name != "state" {
		t.Fatal(p, ok)
	}
	if names := propNames(r.Noun("ticket")); !slices.Equal(names, []string{"id", "state"}) {
		t.Fatal(names)
	}
	if _, _, err := setChanges(r.Noun("ticket"), Arguments{"changes": map[string]any{"state": "todo", "status": "done"}}); err == nil {
		t.Fatal("duplicate canonical writes accepted")
	}
}

func TestPropAliasDeclarationClashesAndCopies(t *testing.T) {
	for _, alias := range []string{"", "two words", "a.b", "-x", "state", "get", "status"} {
		t.Run(alias, func(t *testing.T) {
			r := NewRuntime()
			b := r.Define("ticket").Props(PropSpec{Name: "other", Aliases: []string{"status"}})
			defer func() {
				if recover() == nil {
					t.Fatal("accepted alias", alias)
				}
			}()
			b.Props(PropSpec{Name: "state", Aliases: []string{alias}})
		})
	}
	r, l := propAliasFixture(t)
	p, _ := r.PropOf("ticket", "state")
	p.Aliases[0] = "bad"
	props := r.Props("ticket")
	props[0].Aliases[0] = "bad"
	if _, ok := r.PropOf("ticket", "status"); !ok {
		t.Fatal("caller mutated alias")
	}
	r.Define("other").Props(PropSpec{Name: "phase", Aliases: []string{"status"}})
	if err := l.SetItems(append(l.Items(), ListItem{ID: "c", Noun: "other", Data: Thing{"phase": "todo"}})); err != nil {
		t.Fatal(err)
	}
	if err := l.SetPropFilters([]PropFilter{{Prop: "status", Value: "todo"}}); err == nil {
		t.Fatal("ambiguous alias accepted")
	}
}

func TestPropAliasCreateAndDuplicate(t *testing.T) {
	r := NewRuntime()
	r.Config().Disable()
	var written Thing
	r.Define("ticket").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "state", Aliases: []string{"status"}}).Store(Store{Kind: Collection,
		Create: func(_ Ctx, values Thing) (Thing, error) { written = values; values["id"] = "a"; return values, nil },
	})
	for _, args := range []Arguments{{"status": "todo"}, {"pairs": "status=todo"}, {"state": "status=todo"}} {
		if _, err := r.Execute(Request{Noun: "ticket", Verb: "create", Args: args}); err != nil {
			t.Fatal(err)
		}
		if written["state"] != "todo" || written["status"] != nil {
			t.Fatal(written)
		}
	}
	changes, order, err := duplicateOverrides(r.Noun("ticket"), Arguments{"pairs": "status=done"})
	if err != nil || changes["state"] != "done" || !slices.Equal(order, []string{"state"}) {
		t.Fatal(changes, order, err)
	}
}

func TestPropAliasFilterOptOut(t *testing.T) {
	r, l := propAliasFixture(t)
	r.Noun("ticket").Props[0].NoFilter = true
	if props := l.filterProps(); len(props) != 0 {
		t.Fatal(props)
	}
	if err := l.SetPropFilters([]PropFilter{{Prop: "status", Value: "todo"}}); err == nil {
		t.Fatal("alias bypassed NoFilter")
	}
}

func TestPropAliasReferenceListsMainOnce(t *testing.T) {
	r, _ := propAliasFixture(t)
	model := r.ReferenceModel()
	i := slices.IndexFunc(model.Nouns, func(n refdoc.Noun) bool { return n.Name == "ticket" })
	if i < 0 || len(model.Nouns[i].Props) != 1 || !slices.Equal(model.Nouns[i].Props[0].Aliases, []string{"status"}) {
		t.Fatal(model.Nouns)
	}
	text := refdoc.Render(model, refdoc.Options{})[refdoc.StandardVerbsFile]
	if strings.Count(text, "| `state` |") != 1 || strings.Contains(text, "| `status` |") || !strings.Contains(text, "also: status") {
		t.Fatal(text)
	}
}
