package ochazuke

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// keyedStore is a map-backed collection of notes: id, name (the label),
// owner, archived.
type keyedStore struct {
	things []Thing
	reads  int
}

func newKeyedStore() *keyedStore {
	return &keyedStore{things: []Thing{
		{"id": "t1", "name": "Alpha", "owner": "p1", "archived": false},
		{"id": "t10", "name": "Beta", "owner": "p1", "archived": false},
		{"id": "t2", "name": "Twin", "owner": "p2", "archived": false},
		{"id": "t3", "name": "Twin", "owner": "p2", "archived": true},
		{"id": "t4", "name": "t1", "owner": "p2", "archived": false}, // a name that is another thing's id
	}}
}

func (s *keyedStore) find(id string) Thing {
	for _, t := range s.things {
		if t["id"] == id {
			return t
		}
	}
	return nil
}

func (s *keyedStore) store() Store {
	return Store{
		Get: func(_ Ctx, id string) (Thing, error) {
			s.reads++
			if t := s.find(id); t != nil {
				return Thing(copyThing(t)), nil
			}
			return nil, fmt.Errorf("no note %q", id)
		},
		List: func(Ctx, ListQuery) ([]Thing, error) { return s.things, nil },
		Patch: func(_ Ctx, id string, changes Thing) (Thing, error) {
			t := s.find(id)
			if t == nil {
				return nil, fmt.Errorf("no note %q", id)
			}
			for k, v := range changes {
				t[k] = v
			}
			return copyThing(t), nil
		},
	}
}

func copyThing(t Thing) Thing {
	out := Thing{}
	for k, v := range t {
		out[k] = v
	}
	return out
}

func keyedProps() []PropSpec {
	return []PropSpec{
		{Name: "id", Key: true},
		{Name: "name", Role: RoleLabel},
		{Name: "owner", Role: RoleOwner, Parameter: Parameter{Noun: "note"}},
		{Name: "archived", Role: RoleArchive, Parameter: Parameter{Type: SettingBoolean}},
	}
}

func newKeyedRuntime(t *testing.T, props ...PropSpec) (*Runtime, *keyedStore) {
	t.Helper()
	if len(props) == 0 {
		props = keyedProps()
	}
	r := NewRuntime()
	s := newKeyedStore()
	r.Define("note").Props(props...).Store(s.store())
	return r, s
}

func getNote(r *Runtime, target string, args Arguments) (any, error) {
	a := Arguments{"id": target}
	for k, v := range args {
		a[k] = v
	}
	return r.Execute(Request{Noun: "note", Verb: "get", Args: a})
}

// An id wins; else an exact human key; a text neither finds goes to the
// store untouched; and nothing is ever taken as a prefix: t1 is not t10.
func TestTargetResolvesIdThenHumanKey(t *testing.T) {
	t.Parallel()
	r, _ := newKeyedRuntime(t)
	for _, c := range []struct{ target, wantID string }{
		{"t1", "t1"},    // an id beats the thing named "t1"
		{"t10", "t10"},  // t1 versus t10
		{"Alpha", "t1"}, // the label prop is the human key by default
		{"Beta", "t10"},
	} {
		v, err := getNote(r, c.target, Arguments{"prop": "id"})
		if err != nil || v != c.wantID {
			t.Errorf("target %q: %v, %v; want %s", c.target, v, err, c.wantID)
		}
	}
	for _, text := range []string{"t", "Alph", "alpha", "t100"} {
		if _, err := getNote(r, text, nil); err == nil || !strings.Contains(err.Error(), "no note") {
			t.Errorf("target %q: want the store's not-found error, got %v", text, err)
		}
	}
}

// A human key more than one thing holds refuses and names them.
func TestAmbiguousHumanKeyRefusesWithCandidates(t *testing.T) {
	t.Parallel()
	r, s := newKeyedRuntime(t)
	_, err := getNote(r, "Twin", nil)
	var arg *ArgumentError
	if !asArgumentError(err, &arg) || !strings.Contains(err.Error(), "t2, t3") || !strings.Contains(err.Error(), `"Twin"`) {
		t.Fatalf("ambiguous name: %v", err)
	}
	// A write refuses too, and writes nothing.
	_, err = r.Execute(Request{Noun: "note", Verb: "set", Args: Arguments{"id": "Twin", "prop": "owner", "value": "p9"}})
	if err == nil || !strings.Contains(err.Error(), "t2, t3") {
		t.Fatalf("ambiguous set: %v", err)
	}
	if s.find("t2")["owner"] != "p2" || s.find("t3")["owner"] != "p2" {
		t.Fatalf("a refused set wrote: %v", s.things)
	}
}

func asArgumentError(err error, target **ArgumentError) bool {
	for err != nil {
		if a, ok := err.(*ArgumentError); ok {
			*target = a
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// PropSpec.Human names the human key; it replaces the label default.
func TestHumanPropOverridesLabelDefault(t *testing.T) {
	t.Parallel()
	props := keyedProps()
	props[1].Role = ""
	props[1].Human = false
	props = append(props, PropSpec{Name: "slug", Human: true}, PropSpec{Name: "title", Role: RoleLabel})
	r := NewRuntime()
	s := newKeyedStore()
	for i, slug := range []string{"alpha", "beta", "twin-a", "twin-b", "t1-slug"} {
		s.things[i]["slug"] = slug
		s.things[i]["title"] = "T " + slug
	}
	r.Define("note").Props(props...).Store(s.store())
	if v, err := getNote(r, "beta", Arguments{"prop": "id"}); err != nil || v != "t10" {
		t.Fatalf("slug: %v, %v", v, err)
	}
	if _, err := getNote(r, "T beta", nil); err == nil {
		t.Fatal("the label is not the human key once another prop is")
	}
}

func TestHumanKeyDeclarationErrors(t *testing.T) {
	t.Parallel()
	mustPanic(t, "both the Human key", func() {
		r := NewRuntime()
		r.Define("note").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "a", Human: true}, PropSpec{Name: "b", Human: true}).Store(newKeyedStore().store())
	})
	mustPanic(t, "secret", func() {
		r := NewRuntime()
		r.Define("note").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "a", Human: true, Secret: true}).Store(newKeyedStore().store())
	})
	mustPanic(t, "must be one word", func() {
		r := NewRuntime()
		r.Define("note").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "@label"}).Store(newKeyedStore().store())
	})
}

// Props are addressed by role: get, set (prop, prop=value words, changes)
// and the where filter of list.
func TestPropsAddressedByRole(t *testing.T) {
	t.Parallel()
	r, s := newKeyedRuntime(t)
	if v, err := getNote(r, "t1", Arguments{"prop": "@label"}); err != nil || v != "Alpha" {
		t.Fatalf("get @label: %v, %v", v, err)
	}
	if v, err := getNote(r, "t1", Arguments{"prop": "@owner"}); err != nil || v != "p1" {
		t.Fatalf("get @owner: %v, %v", v, err)
	}
	mustRun(t, r, Request{Noun: "note", Verb: "set", Args: Arguments{"id": "t1", "prop": "@archive", "value": true}})
	if s.find("t1")["archived"] != true {
		t.Fatalf("set @archive: %v", s.find("t1"))
	}
	mustRun(t, r, Request{Noun: "note", Verb: "set", Args: Arguments{"id": "t1", "changes": map[string]any{"@archive": false, "@label": "Alpha 2"}}})
	if got := s.find("t1"); got["archived"] != false || got["name"] != "Alpha 2" {
		t.Fatalf("set changes by role: %v", got)
	}
	mustRun(t, r, Request{Noun: "note", Verb: "set", Args: Arguments{"id": "t1", "prop": "@owner=p7"}})
	if s.find("t1")["owner"] != "p7" {
		t.Fatalf("set @owner=p7: %v", s.find("t1"))
	}
	rows, err := r.Execute(Request{Noun: "note", Verb: "list", Args: Arguments{"where": "@owner=p7"}})
	if err != nil || len(AnswerRows(rows).([]map[string]any)) != 1 {
		t.Fatalf("list where @owner: %v, %v", rows, err)
	}
	// A role the noun does not declare is an unknown prop, not a guess.
	if _, err := getNote(r, "t1", Arguments{"prop": "@nonesuch"}); err == nil {
		t.Fatal("get @nonesuch should fail")
	}
	// The catalog offers the role names beside the props.
	got := getParams(r.Noun("note"))["prop"].Enum
	for _, want := range []string{"name", "@label", "@owner", "@archive"} {
		found := false
		for _, e := range got {
			found = found || e == want
		}
		if !found {
			t.Errorf("get's prop choices %v lack %s", got, want)
		}
	}
}

// A row's thing is read by one function: the key in the row's Data, a Data
// that is the key, the key in ListItem.Props; never the row's instance id.
func TestRowKeyReaders(t *testing.T) {
	t.Parallel()
	r, s := newKeyedRuntime(t)
	r.Instance("notes")
	if _, err := r.AddList("notes/list", []ListItem{
		{ID: "r-data", Label: "a", Noun: "note", Data: Thing{"id": "t1"}},
		{ID: "r-string", Label: "b", Noun: "note", Data: "t10"},
		{ID: "r-props", Label: "c", Noun: "note", Data: struct{ Handle int }{7}, Props: Thing{"id": "t2"}},
		{ID: "t4", Label: "d", Noun: "note"}, // its instance id is a note's id, and still not its key
	}, Parent("notes")); err != nil {
		t.Fatal(err)
	}
	for row, want := range map[string]string{"r-data": "t1", "r-string": "t10", "r-props": "t2"} {
		if err := r.SetFocus(row); err != nil {
			t.Fatal(err)
		}
		s.reads = 0
		v, err := r.Execute(Request{Noun: "note", Verb: "get", Args: Arguments{"prop": "id"}})
		if err != nil || v != want {
			t.Errorf("row %s: %v, %v; want %s", row, v, err, want)
		}
		if s.reads != 1 {
			t.Errorf("row %s: the store was read %d times; a row's key needs no probe", row, s.reads)
		}
	}
	if err := r.SetFocus("t4"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(Request{Noun: "note", Verb: "get"}); err == nil || !strings.Contains(err.Error(), `holds no id`) {
		t.Fatalf("a row with no key: %v", err)
	}
}

// An instances noun's things are its instances: a row's key is its id.
func TestInstanceNounRowKeyIsItsId(t *testing.T) {
	t.Parallel()
	r := NewRuntime()
	r.Define("view").Props(PropSpec{Name: "id", Key: true}).Store(Store{Kind: Instances})
	r.Instance("v1")
	n := r.Noun("view")
	if got, err := r.rowKey(n, r.Instances["v1"]); err != nil || got != "v1" {
		t.Fatalf("instances row key = %q, %v", got, err)
	}
	if n.resolvable() {
		t.Fatal("an instances noun's targets are ids, never human keys")
	}
}

type everyType struct {
	Name     string         `json:"name"`
	Count    int            `json:"count"`
	Ratio    float64        `json:"ratio"`
	Done     bool           `json:"done"`
	Every    time.Duration  `json:"every"`
	Tags     []string       `json:"tags"`
	Extra    map[string]any `json:"extra"`
	Untagged string
}

// Into reads a thing into a struct, every prop type round-tripping: from a
// typed thing, from its file form and from ThingOf's encoding of a struct.
func TestThingIntoRoundTripsEveryPropType(t *testing.T) {
	t.Parallel()
	want := everyType{Name: "n", Count: 3, Ratio: 1.5, Done: true, Every: 90 * time.Second, Tags: []string{"a", "b"}, Extra: map[string]any{"k": "v"}, Untagged: "u"}
	typed := Thing{"name": "n", "count": 3, "ratio": 1.5, "done": true, "every": 90 * time.Second, "tags": []string{"a", "b"}, "extra": map[string]any{"k": "v"}, "untagged": "u", "unknown": 1}
	fileForms := Thing{"name": "n", "count": float64(3), "ratio": 1.5, "done": true, "every": "1m30s", "tags": []any{"a", "b"}, "extra": map[string]any{"k": "v"}, "Untagged": "u"}
	th, err := ThingOf(want)
	if err != nil {
		t.Fatal(err)
	}
	for name, thing := range map[string]Thing{"typed": typed, "file form": fileForms, "ThingOf": th} {
		var got everyType
		if err := thing.Into(&got); err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %+v, want %+v", name, got, want)
		}
	}
	if err := (Thing{}).Into(everyType{}); err == nil {
		t.Error("Into a non-pointer should fail")
	}
	if err := (Thing{"every": "soon"}).Into(&everyType{}); err == nil {
		t.Error("a bad duration should fail")
	}
}

func newTimesRuntime(t *testing.T, store func(*Store), props ...PropSpec) (*Runtime, *keyedStore) {
	t.Helper()
	s := newKeyedStore()
	st := s.store()
	st.Create = func(_ Ctx, v Thing) (Thing, error) {
		v["id"] = "t9"
		s.things = append(s.things, copyThing(v))
		return copyThing(v), nil
	}
	if store != nil {
		store(&st)
	}
	if len(props) == 0 {
		props = keyedProps()
	}
	r := NewRuntime()
	r.Define("note").Props(props...).Store(st)
	return r, s
}

// Every thing noun whose store writes has the base times: read-only text,
// create stamps both and set stamps updated; Stamps leaves the times to
// the store; a facts store (system facts) and a store that writes nothing
// have none.
func TestBaseTimesOnThingNouns(t *testing.T) {
	t.Parallel()
	r, _ := newTimesRuntime(t, func(st *Store) { st.Facts = true })
	if p := r.Noun("note").prop("created"); p != nil {
		t.Fatalf("a facts store declares %+v", p)
	}
	r, _ = newTimesRuntime(t, func(st *Store) { st.Create, st.Patch = nil, nil })
	if p := r.Noun("note").prop("created"); p != nil {
		t.Fatalf("a store that writes nothing declares %+v", p)
	}

	r, s := newTimesRuntime(t, nil)
	for _, name := range []string{"created", "updated"} {
		if p := r.Noun("note").prop("@" + name); p == nil || !p.ReadOnly || p.Name != name {
			t.Fatalf("@%s = %+v", name, p)
		}
	}
	mustRun(t, r, Request{Noun: "note", Verb: "create", Args: Arguments{"name": "New"}})
	made := s.find("t9")
	if made["created"] == "" || made["created"] != made["updated"] {
		t.Fatalf("create stamps: %v", made)
	}
	if _, err := time.Parse(time.RFC3339, made["created"].(string)); err != nil {
		t.Fatalf("created is not RFC 3339: %v", err)
	}
	mustRun(t, r, Request{Noun: "note", Verb: "set", Args: Arguments{"id": "t1", "prop": "owner", "value": "p5"}})
	if s.find("t1")["updated"] == nil || s.find("t1")["created"] != nil {
		t.Fatalf("set stamps updated only: %v", s.find("t1"))
	}
	if _, err := r.Execute(Request{Noun: "note", Verb: "set", Args: Arguments{"id": "t1", "prop": "updated", "value": "x"}}); err == nil {
		t.Fatal("the times are read-only")
	}

	r, s = newTimesRuntime(t, func(st *Store) { st.Stamps = true })
	mustRun(t, r, Request{Noun: "note", Verb: "set", Args: Arguments{"id": "t1", "prop": "owner", "value": "p5"}})
	if _, ok := s.find("t1")["updated"]; ok {
		t.Fatalf("Stamps leaves the time to the store: %v", s.find("t1"))
	}

	// A noun's own prop with the role is stamped, not duplicated.
	own := append(keyedProps(), PropSpec{Name: "made", Role: RoleCreated, ReadOnly: true})
	r, s = newTimesRuntime(t, nil, own...)
	if r.Noun("note").prop("created") != nil {
		t.Fatal("a noun with its own created-role prop declares no second one")
	}
	mustRun(t, r, Request{Noun: "note", Verb: "create", Args: Arguments{"name": "New"}})
	if s.find("t9")["made"] == nil {
		t.Fatalf("own created prop not stamped: %v", s.find("t9"))
	}
}
