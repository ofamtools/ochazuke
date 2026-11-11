package cli

import (
	"context"
	"encoding/json"
	"fmt"
	gomaps "maps"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

func TestMountedThingWalk(t *testing.T) {
	ext := ochazuke.NewRuntime()
	ext.Config().Disable()
	things := map[string]ochazuke.Thing{
		"parcel": {"id": "p1", "title": "Field notes", "shelf": "s1"},
		"shelf":  {"id": "s1", "title": "Reference shelf", "other": "/fixture/other.md", "file": "/fixture/reference.md"},
	}
	props := map[string][]ochazuke.PropSpec{
		"parcel": {{Name: "shelf", Role: ochazuke.RoleOwner, Parameter: ochazuke.Parameter{Noun: "shelf"}}},
		"shelf": {
			{Name: "other", Parameter: ochazuke.Parameter{Type: ochazuke.PropPath}},
			{Name: "file", Parameter: ochazuke.Parameter{Type: ochazuke.PropPath}},
		},
	}
	for _, noun := range []string{"parcel", "shelf"} {
		thing := things[noun]
		ext.Define(noun).Props(
			ochazuke.PropSpec{Name: "id", Key: true},
			ochazuke.PropSpec{Name: "title", Role: ochazuke.RoleLabel},
		).Props(props[noun]...).Store(ochazuke.Store{
			Get: func(_ ochazuke.Ctx, id string) (ochazuke.Thing, error) {
				if id != thing["id"] {
					return nil, fmt.Errorf("no thing %q", id)
				}
				return gomaps.Clone(thing), nil
			},
			List: func(ochazuke.Ctx, ochazuke.ListQuery) ([]ochazuke.Thing, error) {
				return []ochazuke.Thing{gomaps.Clone(thing)}, nil
			},
		})
	}
	raw, err := Catalog(ext, "extension", "test")
	if err != nil {
		t.Fatal(err)
	}
	var doc CatalogDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	var opened []string
	r.SetOpener(ochazuke.Opener{GOOS: "darwin", Start: func(name string, args ...string) error {
		opened = append(opened, strings.Join(append([]string{name}, args...), " "))
		return nil
	}})
	mounted := MountNouns(r, doc.AppNouns(), HostMount{
		Local: func(noun string) string { return "extension." + noun },
		Proxy: func(noun string, verb CatalogVerb) any {
			return func(c ochazuke.Ctx) ochazuke.Result {
				switch verb.Name {
				case "get":
					if c.Args["id"] != things[noun]["id"] {
						return ochazuke.Failed(fmt.Errorf("no %s %v", noun, c.Args["id"]))
					}
					value := gomaps.Clone(things[noun])
					return ochazuke.Pending(func(context.Context) (any, error) { return value, nil })
				case "list":
					value := []ochazuke.Thing{gomaps.Clone(things[noun])}
					return ochazuke.Pending(func(context.Context) (any, error) { return value, nil })
				default:
					return ochazuke.Failed(fmt.Errorf("unexpected extension call %s.%s", noun, verb.Name))
				}
			}
		},
	})
	if len(mounted.Warnings) != 0 {
		t.Fatal(mounted.Warnings)
	}
	if err := r.AddInput(ochazuke.NewInput("origin", "", false)); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFocus("origin"); err != nil {
		t.Fatal(err)
	}
	settle := func() {
		t.Helper()
		if err := Settle(r, nil); err != nil {
			t.Fatal(err)
		}
	}
	press := func(key string) {
		t.Helper()
		if _, err := r.HandleBinding(key); err != nil {
			t.Fatal(err)
		}
		settle()
	}
	choose := func(query string) ochazuke.PickerRow {
		t.Helper()
		c := r.Chooser(ochazuke.ThingWalkKind)
		if c == nil {
			t.Fatalf("walk is not open: modal %q", r.TopModal())
		}
		c.SetQuery(query)
		settle()
		row, ok := c.Highlighted()
		if !ok {
			t.Fatalf("no walk choice for %q: %+v", query, c.Matches())
		}
		return row
	}
	open := func() {
		t.Helper()
		if _, err := r.Execute(ochazuke.Request{Noun: "ochazuke.thing", Verb: "walk"}); err != nil {
			t.Fatal(err)
		}
		settle()
		choose("parcel")
		press("l")
		row := choose("Field notes")
		if row.Noun != "extension.parcel" || row.Props["id"] != "p1" || row.Props["shelf"] != "s1" {
			t.Fatalf("mounted parcel choice: %+v", row)
		}
	}

	open()
	press("l")
	owner := choose("Reference shelf")
	if owner.Noun != "extension.shelf" || owner.Props["id"] != "s1" {
		t.Fatalf("mounted owner choice: %+v", owner)
	}
	press("l")
	referrer := choose("Field notes")
	if referrer.Noun != "extension.parcel" || referrer.Props["shelf"] != "s1" {
		t.Fatalf("mounted inbound reference choice: %+v", referrer)
	}
	path := choose("reference.md")
	if !strings.Contains(path.Label, "file") || path.Props["file"] != "/fixture/reference.md" {
		t.Fatalf("mounted path choice: %+v", path)
	}
	press("enter")
	if len(opened) != 1 || opened[0] != "open -- /fixture/reference.md" || r.TopModal() != "" || r.Focus != "origin" {
		t.Fatalf("mounted path open: %+v, modal %q focus %q", opened, r.TopModal(), r.Focus)
	}

	open()
	press("enter")
	if r.TopModal() != "" || r.Page != "nouns/extension.parcel" {
		t.Fatalf("mounted thing follow: modal %q page %q focus %q", r.TopModal(), r.Page, r.Focus)
	}
}

func TestMountedThingWalkQueryRelationEndpoints(t *testing.T) {
	ext := ochazuke.NewRuntime()
	ext.Config().Disable()
	for _, noun := range []string{"parcel", "shelf"} {
		table := ochazuke.NewMemDocTable(ext, noun)
		ext.Define(noun).Props(
			ochazuke.PropSpec{Name: "id", Key: true},
			ochazuke.PropSpec{Name: "title", Role: ochazuke.RoleLabel},
		).Store(ochazuke.DocStore(table, noun))
		count := 161
		if noun == "parcel" {
			count = 3
		}
		for i := 1; i <= count; i++ {
			id := fmt.Sprintf("%c%03d", noun[0], i)
			title := fmt.Sprintf("%s %03d", noun, i)
			if err := table.Put(id, ochazuke.Thing{"id": id, "title": title}); err != nil {
				t.Fatal(err)
			}
		}
	}
	raw, err := Catalog(ext, "extension", "test")
	if err != nil {
		t.Fatal(err)
	}
	var doc CatalogDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	var refused []string
	mounted := MountNouns(r, doc.AppNouns(), HostMount{
		Local: func(noun string) string { return "extension." + noun },
		Proxy: func(noun string, verb CatalogVerb) any {
			return func(c ochazuke.Ctx) ochazuke.Result {
				if verb.Name == "list" {
					limit, ok := c.Args["per-page"].(int)
					if !ok || limit < 1 || limit > 80 {
						why := fmt.Sprintf("unbounded %s.list: %v", noun, c.Args)
						refused = append(refused, why)
						return ochazuke.Failed(fmt.Errorf("%s", why))
					}
				}
				// Execute the fixture store on the loop. The worker owns only
				// the completed answer, never either runtime or a context.
				value, err := ext.Execute(ochazuke.Request{Noun: noun, Verb: verb.Name, Args: gomaps.Clone(c.Args), Confirmed: true})
				if err != nil {
					return ochazuke.Failed(err)
				}
				return ochazuke.Pending(func(context.Context) (any, error) { return value, nil })
			}
		},
	})
	if len(mounted.Warnings) != 0 {
		t.Fatal(mounted.Warnings)
	}
	if n := r.Noun("extension.shelf"); n.Store == nil || n.Store.Query == nil {
		t.Fatal("the mounted shelf must advertise a queryable store")
	}
	if err := r.DeclareRelationKind(ochazuke.RelationKind{
		Name: "storage", From: "extension.parcel", To: "extension.shelf", Prop: "shelves", Inverse: "parcels",
	}); err != nil {
		t.Fatal(err)
	}
	relations := ochazuke.NewMemRelations()
	r.SetRelationStore(relations)
	for i := 1; i <= 160; i++ {
		if err := relations.Put(ochazuke.Relation{
			Kind: "storage", From: "extension.parcel:p001", To: fmt.Sprintf("extension.shelf:s%03d", i), Position: i,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := relations.Put(ochazuke.Relation{
		Kind: "storage", From: "extension.parcel:p002", To: "extension.shelf:s160", Position: 161,
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.AddInput(ochazuke.NewInput("origin", "", false)); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFocus("origin"); err != nil {
		t.Fatal(err)
	}
	settle := func() {
		t.Helper()
		if err := Settle(r, nil); err != nil {
			t.Fatal(err)
		}
		if len(refused) != 0 {
			t.Fatalf("the graph attempted a whole-table read: %v", refused)
		}
	}
	press := func(key string) {
		t.Helper()
		if _, err := r.HandleBinding(key); err != nil {
			t.Fatal(err)
		}
		settle()
	}
	choose := func(query, noun, id string) {
		t.Helper()
		c := r.Chooser(ochazuke.ThingWalkKind)
		if c == nil {
			t.Fatalf("walk is not open: modal %q", r.TopModal())
		}
		c.SetQuery(query)
		settle()
		row, ok := c.Highlighted()
		if !ok || noun != "" && (row.Noun != noun || row.Props["id"] != id) {
			t.Fatalf("walk choice for %q: %+v (present %t)", query, row, ok)
		}
	}
	ids := func(noun string) []string {
		t.Helper()
		var out []string
		for _, row := range r.Chooser(ochazuke.ThingWalkKind).Matches() {
			if !row.Header && row.Noun == noun && row.Props["id"] != nil {
				out = append(out, fmt.Sprint(row.Props["id"]))
			}
		}
		return out
	}
	if _, err := r.Execute(ochazuke.Request{Noun: "ochazuke.thing", Verb: "walk"}); err != nil {
		t.Fatal(err)
	}
	settle()
	choose("shelf", "", "")
	press("l")
	if got := ids("extension.shelf"); len(got) != 80 || got[0] != "s001" || got[79] != "s080" {
		t.Fatalf("bounded mounted noun page: %v", got)
	}
	choose("shelf 161", "extension.shelf", "s161")
	press("h")
	choose("parcel", "", "")
	press("l")
	choose("parcel 001", "extension.parcel", "p001")
	press("l")
	if got := ids("extension.shelf"); len(got) != 80 || got[0] != "s001" || got[79] != "s080" {
		t.Fatalf("first relation endpoint window: %v", got)
	}
	choose("shelf 160", "extension.shelf", "s160")
	if got := ids("extension.shelf"); len(got) != 1 || got[0] != "s160" {
		t.Fatalf("search reaches an endpoint beyond the first relation window: %v", got)
	}
	r.Chooser(ochazuke.ThingWalkKind).SetQuery("shelf 161")
	settle()
	if got := ids("extension.shelf"); len(got) != 0 {
		t.Fatalf("relation search included an unrelated stored shelf: %v", got)
	}
	choose("", "extension.shelf", "s001")
	press("G")
	if row, ok := r.Chooser(ochazuke.ThingWalkKind).Highlighted(); !ok || row.Label != "Next page" {
		t.Fatalf("relation next page: %+v (present %t)", row, ok)
	}
	press("l")
	if got := ids("extension.shelf"); len(got) != 80 || got[0] != "s081" || got[79] != "s160" {
		t.Fatalf("second relation endpoint window: %v", got)
	}
	press("G")
	if row, ok := r.Chooser(ochazuke.ThingWalkKind).Highlighted(); !ok || row.Props["id"] != "s160" {
		t.Fatalf("last related shelf: %+v (present %t)", row, ok)
	}
	press("l")
	if got := ids("extension.parcel"); len(got) != 2 || got[0] != "p001" || got[1] != "p002" {
		t.Fatalf("incoming relation endpoints exclude unrelated p003: %v", got)
	}
	choose("parcel 002", "extension.parcel", "p002")
	press("l")
	if got := ids("extension.shelf"); len(got) != 1 || got[0] != "s160" {
		t.Fatalf("the second parcel's outgoing endpoint: %v", got)
	}
}
