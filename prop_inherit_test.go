package ochazuke

import (
	"fmt"
	"testing"
)

// Inheritance is declared data (spine S5 rule 3, decisions 38 and 40): a
// bin's polymorphic owner, empty or cleared, reads its shelf's; its room
// and box props read through the owner (the owner itself when it is one,
// else the box's room, walked up the box's parents); a loop of parents
// ends; a list filters on the inherited value; the owner's picker lists
// rooms and boxes together; and code off the loop
// walks the same declarations over records it reads itself.
func TestInheritWalksDeclaredReferences(t *testing.T) {
	r := NewRuntime()
	r.Config().Disable()
	tables := map[string]*MemDocTable{}
	define := func(noun string, props ...PropSpec) {
		tables[noun] = NewMemDocTable(r, noun)
		r.Define(noun).Props(append([]PropSpec{{Name: "id", Key: true, CreateOnly: true}}, props...)...).Store(DocStore(tables[noun], noun))
	}
	define("room")
	define("box", PropSpec{Name: "parent", Parameter: Parameter{Ref: []string{"box"}}}, PropSpec{Name: "room", Inherit: "parent", Parameter: Parameter{Ref: []string{"room"}}})
	define("shelf", PropSpec{Name: "owner", Role: RoleOwner, Parameter: Parameter{Ref: []string{"room", "box"}}})
	define("bin",
		PropSpec{Name: "shelf", Parameter: Parameter{Ref: []string{"shelf"}}},
		PropSpec{Name: "owner", Role: RoleOwner, Inherit: "shelf", Parameter: Parameter{Ref: []string{"room", "box"}}},
		PropSpec{Name: "room", Inherit: "owner", Parameter: Parameter{Ref: []string{"room"}}},
		PropSpec{Name: "box", Inherit: "owner", Parameter: Parameter{Ref: []string{"box"}}})
	put := func(noun string, t Thing) { _ = tables[noun].Put(fmt.Sprint(t["id"]), t) }
	put("room", Thing{"id": "r1"})
	put("box", Thing{"id": "x1", "room": "r2"})
	put("box", Thing{"id": "x2", "parent": "x1", "room": ""})
	put("box", Thing{"id": "y1", "parent": "y2"})
	put("box", Thing{"id": "y2", "parent": "y1"})
	put("shelf", Thing{"id": "s1", "owner": "room:r1"})
	put("bin", Thing{"id": "b1", "shelf": "s1", "owner": ""}) // explicitly unassociated: follows its shelf
	put("bin", Thing{"id": "b2", "shelf": "s1", "owner": "box:x2"})
	put("bin", Thing{"id": "b3", "owner": "box:y1"})
	get := func(id string) Thing {
		got, err := r.Execute(Request{Noun: "bin", Verb: "get", Args: Arguments{"id": id}})
		if err != nil {
			t.Fatal(err)
		}
		return Thing(got.(map[string]any))
	}
	for _, c := range []struct{ id, owner, room, box string }{
		{"b1", "room:r1", "r1", ""},
		{"b2", "box:x2", "r2", "x2"},
		{"b3", "box:y1", "", "y1"},
	} {
		got := get(c.id)
		if fmt.Sprint(got["owner"]) != c.owner || cellValueText(got["room"]) != c.room || cellValueText(got["box"]) != c.box {
			t.Errorf("%s = owner %v room %v box %v, want %s %s %s", c.id, got["owner"], got["room"], got["box"], c.owner, c.room, c.box)
		}
	}
	if doc, _, _ := tables["bin"].Get("b1"); doc["owner"] != "" {
		t.Errorf("the inherited owner was written: %v", doc)
	}
	rows, err := r.Execute(Request{Noun: "bin", Verb: "list", Args: Arguments{"where": "room=r1"}})
	if items := rows.(ListAnswer).Items; err != nil || len(items) != 1 || items[0]["id"] != "b1" {
		t.Errorf("list room=r1 = %v, %v", rows, err)
	}
	// The owner's picker lists the things of every noun it may name, flat.
	choices, err := r.Choices(r.Context(), r.Noun("bin").prop("owner").Parameter)
	ids := map[string]bool{}
	for _, c := range choices {
		ids[c.ID] = true
	}
	if err != nil || !ids["room:r1"] || !ids["box:x2"] || len(ids) != 5 {
		t.Errorf("owner choices = %v, %v", choices, err)
	}
	h := r.Inheritance()
	read := func(noun, key string) Thing { doc, _, _ := tables[noun].Get(key); return doc }
	if v := h.Value("bin", read("bin", "b2"), "room", read); v != "r2" {
		t.Errorf("off-loop walk = %v", v)
	}
}
