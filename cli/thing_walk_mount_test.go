package cli

import (
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

func TestMountedThingWalk(t *testing.T) {
	ext := ochazuke.NewRuntime()
	ext.Config().Disable()
	things := map[string]ochazuke.Thing{
		"parcel": {"id": "p1", "title": "Field notes", "shelf": "s1"},
		"shelf":  {"id": "s1", "title": "Reference shelf", "file": "/fixture/reference.md"},
	}
	props := map[string][]ochazuke.PropSpec{
		"parcel": {{Name: "shelf", Role: ochazuke.RoleOwner, Parameter: ochazuke.Parameter{Noun: "shelf"}}},
		"shelf":  {{Name: "file", Parameter: ochazuke.Parameter{Type: ochazuke.PropPath}}},
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
				return maps.Clone(thing), nil
			},
			List: func(ochazuke.Ctx, ochazuke.ListQuery) ([]ochazuke.Thing, error) {
				return []ochazuke.Thing{maps.Clone(thing)}, nil
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
	var opened []hostCall
	mounted := MountNouns(r, doc.AppNouns(), HostMount{
		Local: func(noun string) string { return "extension." + noun },
		Proxy: func(noun string, verb CatalogVerb) any {
			return func(c ochazuke.Ctx) ochazuke.Result {
				switch verb.Name {
				case "get":
					if c.Args["id"] != things[noun]["id"] {
						return ochazuke.Failed(fmt.Errorf("no %s %v", noun, c.Args["id"]))
					}
					return ochazuke.Done(maps.Clone(things[noun]))
				case "list":
					return ochazuke.Done([]ochazuke.Thing{maps.Clone(things[noun])})
				case "open":
					opened = append(opened, hostCall{noun, verb.Name, maps.Clone(c.Args)})
					return ochazuke.Done(nil)
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
	if len(opened) != 1 || opened[0].noun != "shelf" || opened[0].args["id"] != "s1" || opened[0].args["prop"] != "file" || r.TopModal() != "" || r.Focus != "origin" {
		t.Fatalf("mounted path open: %+v, modal %q focus %q", opened, r.TopModal(), r.Focus)
	}

	open()
	press("enter")
	if r.TopModal() != "" || r.Page != "nouns/extension.parcel" {
		t.Fatalf("mounted thing follow: modal %q page %q focus %q", r.TopModal(), r.Page, r.Focus)
	}
}
