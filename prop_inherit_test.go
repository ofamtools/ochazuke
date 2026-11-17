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

// countingLocal is a copy store's LocalStore that counts its writes.
type countingLocal struct {
	*MemoryLocal
	puts int
}

func (l *countingLocal) Put(key string, t Thing) error { l.puts++; return l.MemoryLocal.Put(key, t) }

// A read never writes (spine S6): an inherit walk to parents held by a
// copy store reads each parent once for a list's batch and copies none
// in, on a list and on a get.
func TestInheritWalkReadsParentsWithoutWriting(t *testing.T) {
	r := NewRuntime()
	r.Config().Disable()
	gets := 0
	shelves := map[string]Thing{"s1": {"id": "s1", "room": "r1"}, "s2": {"id": "s2", "room": "r2"}}
	local := &countingLocal{MemoryLocal: NewMemoryLocal()}
	r.Define("room").Props(PropSpec{Name: "id", Key: true}).Store(DocStore(NewMemDocTable(r, "room"), "room"))
	r.Define("shelf").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "room", Parameter: Parameter{Ref: []string{"room"}}}).
		Store(CopyStore(Store{
			Get: func(_ Ctx, key string) (Thing, error) { gets++; return shelves[key], nil },
			List: func(Ctx, ListQuery) ([]Thing, error) {
				return []Thing{shelves["s1"], shelves["s2"]}, nil
			},
		}, local))
	bins := NewMemDocTable(r, "bin")
	r.Define("bin").Props(PropSpec{Name: "id", Key: true},
		PropSpec{Name: "shelf", Parameter: Parameter{Ref: []string{"shelf"}}},
		PropSpec{Name: "room", Inherit: "shelf", Parameter: Parameter{Ref: []string{"room"}}}).Store(DocStore(bins, "bin"))
	for i, shelf := range []string{"s1", "s1", "s2", "s2"} {
		id := fmt.Sprint("b", i)
		_ = bins.Put(id, Thing{"id": id, "shelf": shelf})
	}
	rows, err := r.Execute(Request{Noun: "bin", Verb: "list"})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows.(ListAnswer).Items {
		if want := map[string]string{"s1": "r1", "s2": "r2"}[fmt.Sprint(row["shelf"])]; cellValueText(row["room"]) != want {
			t.Errorf("%v room = %v, want %s", row["id"], row["room"], want)
		}
	}
	if gets != 2 {
		t.Errorf("the list read %d parents, want 2 (each once)", gets)
	}
	if _, err := r.Execute(Request{Noun: "bin", Verb: "get", Args: Arguments{"id": "b0"}}); err != nil {
		t.Fatal(err)
	}
	if local.puts != 0 {
		t.Errorf("the walk wrote %d copies, want none", local.puts)
	}
}
