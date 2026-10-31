package jsontree

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestJSONOrderPathsAndTreeRows(t *testing.T) {
	root, err := Build([]byte(`{"z":1,"odd key":[{"a":"<雪>"}],"a":true,"z":9007199254740993}`))
	if err != nil {
		t.Fatal(err)
	}
	if root.Field("z").Value != "9007199254740993" || root.Field("z").Repeats != 2 {
		t.Fatal(root.Field("z"))
	}
	flat := Flatten(root)
	paths := make([]string, len(flat))
	for i, n := range flat {
		paths[i] = n.Path()
	}
	want := []string{".", ".z", `["odd key"]`, `["odd key"][0]`, `["odd key"][0].a`, ".a"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %q", paths)
	}
	got, err := json.Marshal(root)
	if err != nil || string(got) != `{"z":9007199254740993,"odd key":[{"a":"\u003c雪\u003e"}],"a":true}` {
		t.Fatalf("marshal: %s, %v", got, err)
	}
	pretty := Pretty(root)
	parsed, err := Build([]byte(pretty))
	if err != nil || Pretty(parsed) != pretty {
		t.Fatalf("pretty roundtrip: %s, %v", pretty, err)
	}
	rows := TreeNodes(root, "view/")
	if len(rows) != len(flat) || rows[1].ID != "view/.z" || rows[4].Parent != `view/["odd key"][0]` || rows[4].Data != flat[4] || !reflect.DeepEqual(rows[0].Cells, []string{".", "object", "3"}) {
		t.Fatalf("rows: %+v", rows)
	}
	if len(Flatten(nil)) != 0 || len(TreeNodes(nil, "")) != 0 {
		t.Fatal("nil tree produced rows")
	}
}

func TestJSONValuesAndInvalidInput(t *testing.T) {
	for _, value := range []string{`null`, `false`, `1.25e3`, `"a\n\"b"`, `[]`, `{}`, `[1,null,true]`} {
		n, err := Build([]byte(value))
		if err != nil {
			t.Fatal(err)
		}
		b, err := n.MarshalJSON()
		if err != nil || string(b) != value {
			t.Fatalf("%s -> %s, %v", value, b, err)
		}
	}
	for _, value := range []string{"", `{"a":`, "{\n  bad}", `{} []`, `[1,]`} {
		_, err := Build([]byte(value))
		var p *ParseError
		if !errors.As(err, &p) || p.Line < 1 || p.Col < 1 {
			t.Fatalf("%q: %v", value, err)
		}
	}
}
