package ochazuke

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func createCollisionRuntime(t *testing.T) (*Runtime, *derivedStore) {
	t.Helper()
	r := NewRuntime()
	r.Config().Disable()
	s := &derivedStore{key: "id", things: []Thing{{"id": "t1", "from": "project:old", "to": "task:old", "size": 2}}}
	r.Define("link").Props(
		PropSpec{Name: "id", Key: true},
		PropSpec{Name: "from", Aliases: []string{"source"}, Parameter: Parameter{Required: true, Noun: AnyNoun}},
		PropSpec{Name: "to", Parameter: Parameter{Required: true, Noun: AnyNoun}},
		PropSpec{Name: "size", Parameter: Parameter{Type: SettingInteger}},
		PropSpec{Name: "computed", ReadOnly: true},
	).Store(s.store())
	return r, s
}

func TestCreateOwnFromAndPropsObject(t *testing.T) {
	for _, args := range []Arguments{
		{"from": "project:x", "to": "task:y", "size": 3},
		{"props": map[string]any{"source": "project:x", "to": "task:y", "size": 3}},
		{"pairs": `source=project:x to=task:y size=3`},
		{"props": map[string]any{"source": "project:x"}, "pairs": `to=task:y size=3`},
	} {
		t.Run(fmt.Sprint(args), func(t *testing.T) {
			r, s := createCollisionRuntime(t)
			got, err := r.Execute(Request{Noun: "link", Verb: "create", Args: args})
			if err != nil {
				t.Fatal(err)
			}
			want := Thing{"id": "t2", "from": "project:x", "to": "task:y", "size": 3}
			if !reflect.DeepEqual(s.find("t2"), want) {
				t.Fatalf("created %v, answer %v; want %v", s.find("t2"), got, want)
			}
		})
	}
}

func TestCreatePropsValidation(t *testing.T) {
	for _, tc := range []struct {
		args Arguments
		want string
	}{
		{Arguments{"props": map[string]any{"from": "project:x"}}, "to is required"},
		{Arguments{"from": "project:x"}, "to is required"},
		{Arguments{"props": map[string]any{"from": "project:x", "to": "task:y", "unknown": true}}, "unknown"},
		{Arguments{"props": map[string]any{"from": "project:x", "to": "task:y", "computed": "x"}}, "computed"},
		{Arguments{"props": map[string]any{"from": "project:x", "to": "task:y", "size": "many"}}, "size"},
		{Arguments{"from": "project:x", "props": map[string]any{"source": "project:y", "to": "task:y"}}, "written twice"},
		{Arguments{"props": []any{"from", "project:x"}}, "object"},
		{Arguments{"from": "project:x", "to": "task:y", "pairs": 3}, "pairs must be string"},
	} {
		r, s := createCollisionRuntime(t)
		_, err := r.Execute(Request{Noun: "link", Verb: "create", Args: tc.args})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%v: error %v, want %q", tc.args, err, tc.want)
		}
		if len(s.things) != 1 {
			t.Fatalf("invalid create wrote %v", s.things)
		}
	}
}

func TestCreateCollisionCopyAndDuplicate(t *testing.T) {
	r, s := createCollisionRuntime(t)
	mustRun(t, r, Request{Noun: "link", Verb: "create", Args: Arguments{"create-from": "t1", "props": map[string]any{"source": "project:new"}}})
	mustRun(t, r, Request{Noun: "link", Verb: "duplicate", Args: Arguments{"id": "t1", "from": "project:dup"}})
	for id, want := range map[string]Thing{
		"t2": {"id": "t2", "from": "project:new", "to": "task:old", "size": 2},
		"t3": {"id": "t3", "from": "project:dup", "to": "task:old", "size": 2},
	} {
		if !reflect.DeepEqual(s.find(id), want) {
			t.Fatalf("%s = %v, want %v", id, s.find(id), want)
		}
	}
}

func TestCreatePalettePropsRequired(t *testing.T) {
	for _, line := range []string{
		`link create from=project:x to=task:y size=3`,
		`link create source=project:x to=task:y size=3`,
		`link create --props "{\"source\":\"project:x\",\"to\":\"task:y\",\"size\":3}"`,
		`link create project:x task:y --props "{\"size\":3}"`,
	} {
		t.Run(line, func(t *testing.T) {
			r, s := createCollisionRuntime(t)
			NewPalette(r)
			paletteRequest(t, r, "open", "palette", nil)
			paletteLine(t, r, line)
			want := Thing{"id": "t2", "from": "project:x", "to": "task:y", "size": 3}
			if !reflect.DeepEqual(s.find("t2"), want) {
				t.Fatalf("created %v, want %v; modals %v", s.find("t2"), want, r.Modals())
			}
		})
	}
}

func TestCreateControlNamesYieldToPropAliases(t *testing.T) {
	r := NewRuntime()
	s := &derivedStore{key: "id", things: []Thing{{"id": "t1", "origin": "project:old", "create-from": "own", "props": "own object", "pairs": "own pairs"}}}
	r.Define("edge").Props(
		PropSpec{Name: "id", Key: true},
		PropSpec{Name: "origin", Aliases: []string{"from"}},
		PropSpec{Name: "create-from"},
		PropSpec{Name: "props"},
		PropSpec{Name: "pairs"},
	).Store(s.store())
	mustRun(t, r, Request{Noun: "edge", Verb: "create", Args: Arguments{
		"create-create-from": "t1",
		"create-props":       map[string]any{"from": "project:new", "props": "object value"},
		"create-pairs":       `pairs="pair value"`,
	}})
	want := Thing{"id": "t2", "origin": "project:new", "create-from": "own", "props": "object value", "pairs": "pair value"}
	if !reflect.DeepEqual(s.find("t2"), want) {
		t.Fatalf("created %v, want %v", s.find("t2"), want)
	}
}

func TestCreatePaletteRequiredTypedWords(t *testing.T) {
	for _, line := range []string{
		`measurement create enabled=true quantity=3`,
		`measurement create quantity=3 enabled=true`,
		`measurement create 3 true`,
	} {
		t.Run(line, func(t *testing.T) {
			r := NewRuntime()
			s := &derivedStore{key: "id"}
			r.Define("measurement").Props(
				PropSpec{Name: "id", Key: true},
				PropSpec{Name: "quantity", Parameter: Parameter{Type: SettingInteger, Required: true}},
				PropSpec{Name: "enabled", Parameter: Parameter{Type: SettingBoolean, Required: true}},
			).Store(s.store())
			NewPalette(r)
			paletteRequest(t, r, "open", "palette", nil)
			paletteLine(t, r, line)
			want := Thing{"id": "t1", "quantity": 3, "enabled": true}
			if !reflect.DeepEqual(s.find("t1"), want) {
				t.Fatalf("created %v, want %v", s.find("t1"), want)
			}
		})
	}
}

func TestCreatePropsObjectWritesCreateOnlyKeyAndRole(t *testing.T) {
	r := NewRuntime()
	var written Thing
	r.Define("entry").Props(
		PropSpec{Name: "id", Key: true, CreateOnly: true, Parameter: Parameter{Required: true}},
		PropSpec{Name: "title", Role: RoleLabel, Parameter: Parameter{Required: true}},
	).Store(Store{Stamps: true, Create: func(_ Ctx, v Thing) (Thing, error) {
		written = v
		return v, nil
	}})
	mustRun(t, r, Request{Noun: "entry", Verb: "create", Args: Arguments{
		"props": map[string]any{"id": "chosen", "@label": "Named"},
	}})
	if want := (Thing{"id": "chosen", "title": "Named"}); !reflect.DeepEqual(written, want) {
		t.Fatalf("wrote %v, want %v", written, want)
	}
}

func TestSetPalettePropsObject(t *testing.T) {
	r, s := newDerivedRuntime(t)
	NewPalette(r)
	paletteRequest(t, r, "open", "palette", nil)
	paletteLine(t, r, `task set --id t1 --props "{\"title\":\"Via object\",\"status\":\"done\"}"`)
	want := Thing{"id": "t1", "title": "Via object", "status": "done", "project": "home", "archived": false}
	if !reflect.DeepEqual(s.find("t1"), want) {
		t.Fatalf("wrote %v, want %v", s.find("t1"), want)
	}
}
