package ochazuke

import (
	"context"
	"strings"
	"testing"
)

type widgetRow struct {
	Name  string   `json:"name"`
	State string   `json:"state"`
	Size  int      `json:"size"`
	Tags  []string `json:"tags"`
	Skip  string   `json:"-"`
}

// widgetRuntime has a noun whose own list verb (a fetch answering its rows,
// as a tool declares it) answers through answer, made a thing noun by
// DefineListFacts.
func widgetRuntime(t *testing.T, spec ListFacts, answer func(Ctx) Result) *Runtime {
	t.Helper()
	r := NewRuntime()
	NewModel(r)
	r.DefineOwn(spec.Noun).Verb("list", answer, "List the widgets").Rows(spec.Noun).Fetch()
	spec.List = answer
	r.DefineListFacts(spec)
	return r
}

// thingAnswer is a get's answer as a Thing.
func thingAnswer(v any) Thing {
	t, _ := ThingOf(v)
	return t
}

func widgetRows(c Ctx) Result {
	rows := []ListItem{
		{ID: "widget/a", Label: "Alpha", Noun: "widget", Data: widgetRow{Name: "alpha", State: "ok", Size: 2, Tags: []string{"x"}}},
		{ID: "widget/b", Label: "Beta", Noun: "widget", Data: widgetRow{Name: "beta", State: "bad", Size: 3}},
	}
	if repo, _ := c.Args["repo"].(string); repo != "" {
		rows = rows[:1]
	}
	return Done(rows)
}

// The noun's props are the row's id (key and human key), its label and the
// sample's json fields, renamed where asked, every one read-only; get reads
// a row through the noun's own list by its id, with or without the noun
// prefix, and the tool's list verb stays.
func TestListFactsPropsAndGet(t *testing.T) {
	t.Parallel()
	r := widgetRuntime(t, ListFacts{
		Noun: "widget", Sample: widgetRow{}, Rename: map[string]string{"state": "condition"},
		Docs:       map[string]string{"name": "The widget's name", "condition": "How it is"},
		ListParams: map[string]Parameter{"repo": {Type: "string", Description: "only the first"}},
	}, widgetRows)
	n := r.Noun("widget")
	if n == nil || n.Store == nil || !n.Store.Facts || !n.Store.Remote {
		t.Fatalf("widget store %+v", n.Store)
	}
	var names []string
	for _, p := range n.Props {
		names = append(names, p.Name)
		if !p.ReadOnly {
			t.Fatalf("%s is writable", p.Name)
		}
	}
	if got := strings.Join(names, " "); got != "id label name condition size tags" {
		t.Fatalf("props %q", got)
	}
	if key := n.keyProp(); key == nil || key.Name != "id" || !key.Human {
		t.Fatalf("key %+v", key)
	}
	if p := n.prop("condition"); p.Description != "How it is" {
		t.Fatalf("condition %+v", p)
	}
	if p := n.prop("tags"); p.Type != SettingList {
		t.Fatalf("tags %+v", p)
	}
	if _, ok := n.Store.ListParams["repo"]; !ok {
		t.Fatal("list params dropped")
	}
	for _, key := range []string{"widget/b", "b"} {
		v, err := r.Execute(Request{Noun: "widget", Verb: "get", Args: Arguments{"id": key}})
		if err != nil {
			t.Fatalf("get %s: %v", key, err)
		}
		got := thingAnswer(v)
		if got["id"] != "widget/b" || got["label"] != "Beta" || got["condition"] != "bad" || got["state"] != nil || got["name"] != "beta" {
			t.Fatalf("get %s: %+v", key, got)
		}
	}
	if _, err := r.Execute(Request{Noun: "widget", Verb: "get", Args: Arguments{"id": "nope"}}); err == nil || !strings.Contains(err.Error(), `no widget "nope"`) {
		t.Fatalf("unknown widget: %v", err)
	}
	if _, err := r.Execute(Request{Noun: "widget", Verb: "create", Confirmed: true}); err == nil || !strings.Contains(err.Error(), FactReason) {
		t.Fatalf("create: %v", err)
	}
	if v, err := r.Execute(Request{Noun: "widget", Verb: "list"}); err != nil || len(v.([]ListItem)) != 2 {
		t.Fatalf("the tool's own list: %v %v", v, err)
	}
}

// List hands the list's arguments to the verb.
func TestListFactsPassesListArgs(t *testing.T) {
	t.Parallel()
	r := widgetRuntime(t, ListFacts{Noun: "widget", Sample: widgetRow{}}, widgetRows)
	n := r.Noun("widget")
	c := Ctx{actor: r.Actor(), Runtime: r, Context: r.Context(), tape: &tape{}}
	things, err := n.Store.List(c, ListQuery{Args: Arguments{"repo": "r"}})
	if err != nil || len(things) != 1 || things[0]["id"] != "widget/a" {
		t.Fatalf("filtered list: %v %v", things, err)
	}
}

// Without a sample the row's data is one object prop; a row's Props win
// over its data with one.
func TestListFactsDataAndProps(t *testing.T) {
	t.Parallel()
	answer := func(Ctx) Result {
		return Done([]ListItem{{ID: "w1", Label: "One", Data: map[string]any{"k": "v"}, Props: Thing{"name": "from props"}}})
	}
	r := widgetRuntime(t, ListFacts{Noun: "gadget"}, answer)
	if p := r.Noun("gadget").prop("data"); p == nil || p.Type != PropObject {
		t.Fatalf("data prop %+v", p)
	}
	v, err := r.Execute(Request{Noun: "gadget", Verb: "get", Args: Arguments{"id": "w1"}})
	if data, _ := thingAnswer(v)["data"].(map[string]any); err != nil || data["k"] != "v" {
		t.Fatalf("gadget get %+v %v", v, err)
	}
	r = widgetRuntime(t, ListFacts{Noun: "gizmo", Sample: widgetRow{}}, answer)
	v, err = r.Execute(Request{Noun: "gizmo", Verb: "get", Args: Arguments{"id": "w1"}})
	if got := thingAnswer(v); err != nil || got["name"] != "from props" || got["label"] != "One" {
		t.Fatalf("gizmo get %+v %v", v, err)
	}
}

// An answer that is not rows fails the read with the noun's name.
func TestListFactsRefusesNonRows(t *testing.T) {
	t.Parallel()
	r := widgetRuntime(t, ListFacts{Noun: "widget"}, func(Ctx) Result { return Done("text") })
	if _, err := r.Execute(Request{Noun: "widget", Verb: "get", Args: Arguments{"id": "a"}}); err == nil || !strings.Contains(err.Error(), "widget list answered string") {
		t.Fatalf("non-rows: %v", err)
	}
}

// The verb's work runs off the loop as a job and its Then (the tool's TUI
// list update) never runs for the store: a list fed by the noun fills.
func TestListFactsRunsVerbWorkOffTheLoop(t *testing.T) {
	t.Parallel()
	thenRan := false
	answer := func(c Ctx) Result {
		rows, _ := widgetRows(c).Await(context.Background())
		return Pending(func(context.Context) (any, error) { return rows, nil }).
			Then(func(Ctx, any, error) { thenRan = true })
	}
	r := widgetRuntime(t, ListFacts{Noun: "widget", Sample: widgetRow{}}, answer)
	l, err := r.AddList("widgets", nil, From("widget"))
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Items()) != 0 {
		t.Fatal("filled on the loop")
	}
	pumpRemote(t, r)
	if len(l.Items()) != 2 || thenRan {
		t.Fatalf("rows %d, then ran %v", len(l.Items()), thenRan)
	}
}
