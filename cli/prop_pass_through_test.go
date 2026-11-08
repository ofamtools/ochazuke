package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

// A mounted extension's computed and collection props are read through its
// own get {prop}: the host never fetches the whole record for them, and
// the extension computes only what was asked.
func TestMountedComputedPropReadsThroughTheExtensionsGet(t *testing.T) {
	ext := ochazuke.NewRuntime()
	computed := 0
	ext.Define("parcel").Props(
		ochazuke.PropSpec{Name: "id", Key: true},
		ochazuke.PropSpec{Name: "title", Role: ochazuke.RoleLabel},
		ochazuke.PropSpec{Name: "shout", Read: func(_ ochazuke.Ctx, t ochazuke.Thing) (any, error) {
			computed++
			return strings.ToUpper(t["title"].(string)), nil
		}},
		ochazuke.CountProp(),
	).Store(ochazuke.Store{
		Get: func(_ ochazuke.Ctx, id string) (ochazuke.Thing, error) {
			return ochazuke.Thing{"id": id, "title": "one"}, nil
		},
		List: func(ochazuke.Ctx, ochazuke.ListQuery) ([]ochazuke.Thing, error) {
			return []ochazuke.Thing{{"id": "p1", "title": "one"}, {"id": "p2", "title": "two"}}, nil
		},
	})
	raw, err := Catalog(ext, "extension", "v1")
	if err != nil {
		t.Fatal(err)
	}
	var doc CatalogDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	var asked []map[string]any
	r := ochazuke.NewRuntime()
	got := MountNouns(r, doc.AppNouns(), HostMount{
		Local: func(n string) string { return "ext-" + n },
		Proxy: func(noun string, v CatalogVerb) any {
			return func(c ochazuke.Ctx) ochazuke.Result {
				if v.Name == "get" {
					asked = append(asked, map[string]any(c.Args))
				}
				value, err := ext.Execute(ochazuke.Request{Noun: noun, Verb: v.Name, Args: c.Args, Confirmed: true})
				if err != nil {
					return ochazuke.Failed(err)
				}
				return ochazuke.Pending(func(context.Context) (any, error) { return value, nil })
			}
		},
	})
	if len(got.Warnings) > 0 {
		t.Fatal(got.Warnings)
	}
	if p, ok := r.PropOf("ext-parcel", "shout"); !ok || !p.Computed {
		t.Fatalf("mounted shout = %+v", p)
	}
	if out := run(t, r, "ext-parcel", "shout", "--id", "p1"); !strings.Contains(out, "ONE") || computed != 1 {
		t.Fatalf("shout %q (computed %d)", out, computed)
	}
	if asked[len(asked)-1]["prop"] != "shout" {
		t.Fatalf("the extension was asked %v, not get {prop: shout}", asked)
	}
	if out := run(t, r, "ext-parcel", "count"); !strings.HasSuffix(strings.TrimSpace(out), "\n2") {
		t.Fatalf("count %q", out)
	}
	if asked[len(asked)-1]["prop"] != "count" {
		t.Fatalf("asked %v", asked)
	}
	if rec := run(t, r, "ext-parcel", "get", "--id", "p1"); strings.Contains(rec, "shout") || computed != 1 {
		t.Fatalf("the default record read the computed prop: %s (%d)", rec, computed)
	}
}

// MCP describe lists the noun's props, marking the collection and the
// computed ones.
func TestMCPDescribeShowsProps(t *testing.T) {
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	sh := New(r, "test", nil, nil, nil)
	got, err := (&mcpBackend{shell: sh, actor: r.Actor()}).describe("ochazuke.setting.get")
	if err != nil {
		t.Fatal(err)
	}
	props := got.(map[string]any)["props"].([]map[string]any)
	found := map[string]map[string]any{}
	for _, p := range props {
		found[p["name"].(string)] = p
	}
	if found["names"]["subject"] != "collection" || found["names"]["computed"] != true || found["name"]["readOnly"] != true {
		t.Fatalf("props %v", found)
	}
}

// A collection prop the host cannot read through the extension (no proxied
// get, as for the framework's own nouns the host has itself) is left out of
// the mount quietly, with no warning.
func TestMountedCollectionPropWithoutGetIsSkippedQuietly(t *testing.T) {
	n := CatalogNoun{Name: "parcel", Kind: ochazuke.Singleton, Props: []ochazuke.PropSpec{
		{Name: "path", Subject: ochazuke.SubjectCollection, Computed: true},
		{Name: "title"},
	}}
	r := ochazuke.NewRuntime()
	got := MountNouns(r, []CatalogNoun{n}, HostMount{Local: func(s string) string { return "ext-" + s }})
	if len(got.Warnings) > 0 {
		t.Fatalf("warnings %v", got.Warnings)
	}
	if _, ok := r.PropOf("ext-parcel", "path"); ok {
		t.Fatal("an unreadable collection prop was mounted")
	}
}
