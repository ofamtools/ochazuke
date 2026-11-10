package cli

import (
	"encoding/json"
	"testing"

	"ofam.dev/ochazuke"
)

// kindsRuntime is a tool with a record noun over a paging store, a
// command noun and a part noun, and a verb that creates.
func kindsRuntime() *ochazuke.Runtime {
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	done := func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) }
	things := []ochazuke.Thing{{"id": "a"}}
	r.Define("parcel").Props(ochazuke.PropSpec{Name: "id", Key: true}).Store(ochazuke.Store{
		Kind: ochazuke.KindCollection,
		Get:  func(ochazuke.Ctx, string) (ochazuke.Thing, error) { return things[0], nil },
		Query: func(ochazuke.Ctx, ochazuke.ListQuery) (ochazuke.ListPage, error) {
			return ochazuke.ListPage{Items: things, Total: 1}, nil
		},
		ChangeToken: func(ochazuke.Ctx) (string, error) { return "1", nil },
		Undo:        true,
	})
	r.Define("lane").Verb("define", done, "Define lanes").Creates()
	r.Define("setup").Verb("run", done, "Run the setup")
	r.Define("turn-line").Verb("fold", func(*ochazuke.Runtime, ochazuke.Context, ochazuke.Arguments) (any, error) { return nil, nil }, "Fold")
	r.Define("daemon-row").Kind(ochazuke.NounKindPart).Verb("restart", done, "Restart")
	return r
}

func catalogOf(t *testing.T, r *ochazuke.Runtime) CatalogDoc {
	t.Helper()
	out, err := Catalog(r, "tool", "1")
	if err != nil {
		t.Fatal(err)
	}
	var doc CatalogDoc
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

// The catalog publishes every noun's kind, a stored noun's store
// capabilities and facts mark, and the verbs that create.
func TestCatalogPublishesKindsAndStores(t *testing.T) {
	doc := catalogOf(t, kindsRuntime())
	want := map[string]string{"parcel": ochazuke.NounKindRecord, "lane": ochazuke.NounKindCommand, "setup": ochazuke.NounKindCommand, "turn-line": ochazuke.NounKindPart, "daemon-row": ochazuke.NounKindPart, "ochazuke.noun": ochazuke.NounKindFacts, "ochazuke.page": ochazuke.NounKindInstances}
	for _, n := range doc.Nouns {
		if n.NounKind == "" {
			t.Errorf("%s publishes no kind", n.Name)
		}
		if k, ok := want[n.Name]; ok && n.NounKind != k {
			t.Errorf("%s kind %q, want %q", n.Name, n.NounKind, k)
		}
		if (n.Kind != "") != (n.Store != nil) {
			t.Errorf("%s: a store kind %q needs store capabilities %+v", n.Name, n.Kind, n.Store)
		}
	}
	parcel, _ := doc.noun("parcel")
	if s := parcel.Store; s == nil || !s.Query || !s.ChangeToken || !s.Undo || s.Remote || s.Watch {
		t.Fatalf("parcel store %+v", parcel.Store)
	}
	if facts, _ := doc.noun("ochazuke.noun"); !facts.Facts {
		t.Fatal("ochazuke.noun is facts")
	}
	creates := map[string]bool{}
	for _, n := range doc.Nouns {
		for _, v := range n.Verbs {
			if v.Creates {
				creates[n.Name+"."+v.Name] = true
			}
		}
	}
	if !creates["lane.define"] || !creates["parcel.create"] || creates["setup.run"] {
		t.Fatalf("creating verbs %v", creates)
	}
}

// A catalog written before the fields (v0.117 and older) has no nounKind,
// facts, store or creates: KindOf reads the same kind from the rest of
// each entry, and the nouns mount as they always did.
func TestOldCatalogReadsTheSameKinds(t *testing.T) {
	r := kindsRuntime()
	ochazuke.NewModel(r)
	doc := catalogOf(t, r)
	old := doc
	old.Nouns = make([]CatalogNoun, len(doc.Nouns))
	for i, n := range doc.Nouns {
		n.NounKind, n.Facts, n.Store = "", false, nil
		verbs := make([]CatalogVerb, len(n.Verbs))
		for j, v := range n.Verbs {
			v.Creates = false
			verbs[j] = v
		}
		n.Verbs = verbs
		old.Nouns[i] = n
	}
	for i, n := range old.Nouns {
		if n.Name == "daemon-row" {
			continue // a declared kind is new data an older catalog cannot hold
		}
		if got, want := n.KindOf(), doc.Nouns[i].NounKind; got != want {
			t.Errorf("%s: an old catalog reads %q, the new one says %q", n.Name, got, want)
		}
	}
	// Mounted, both read the same kinds in the host.
	for _, d := range []CatalogDoc{doc, old} {
		host := ochazuke.NewRuntime()
		host.Config().Disable()
		got := MountNouns(host, d.AppNouns(), HostMount{
			Local: Namespace("tool"),
			Proxy: func(string, CatalogVerb) any { return func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) } },
		})
		if len(got.Warnings) > 0 {
			t.Fatalf("warnings %v", got.Warnings)
		}
		for noun, kind := range map[string]string{"tool.parcel": ochazuke.NounKindRecord, "tool.setup": ochazuke.NounKindCommand, "tool.turn-line": ochazuke.NounKindPart} {
			if host.NounKind(noun) != kind {
				t.Errorf("mounted %s: %q, want %q", noun, host.NounKind(noun), kind)
			}
		}
	}
}
