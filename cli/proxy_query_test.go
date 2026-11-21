package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

// A mounted noun whose extension store pages (catalog store.query) asks the
// extension for one page with the query's sort, conditions and search,
// never its whole list; the host's list verb pages through it.
func TestProxyStoreForwardsQuery(t *testing.T) {
	ext := ochazuke.NewRuntime()
	ext.Config().Disable()
	table := ochazuke.NewMemDocTable(ext, "parcel")
	ext.Define("parcel").Props(
		ochazuke.PropSpec{Name: "id", Key: true, CreateOnly: true},
		ochazuke.PropSpec{Name: "title", Role: ochazuke.RoleLabel},
		ochazuke.PropSpec{Name: "kind", Parameter: ochazuke.Parameter{Enum: []string{"box", "bag"}}},
	).Store(ochazuke.DocStore(table, "parcel"))
	for i := 1; i <= 500; i++ {
		kind := "box"
		if i%2 == 0 {
			kind = "bag"
		}
		_ = table.Put(fmt.Sprintf("p%03d", i), ochazuke.Thing{"id": fmt.Sprintf("p%03d", i), "title": fmt.Sprintf("parcel %03d", i), "kind": kind})
	}
	raw, err := Catalog(ext, "extension", "v1")
	if err != nil {
		t.Fatal(err)
	}
	var doc CatalogDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	host := ochazuke.NewRuntime()
	host.Config().Disable()
	var asked []string
	got := MountNouns(host, doc.AppNouns(), HostMount{
		Local: func(n string) string { return "ext-" + n },
		Proxy: func(noun string, v CatalogVerb) any {
			return func(c ochazuke.Ctx) ochazuke.Result {
				asked = append(asked, fmt.Sprintf("%s %v", v.Name, c.Args))
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
	if n := host.Noun("ext-parcel"); n.Store.Query == nil {
		t.Fatal("a paging extension store mounts with Query")
	}
	table.Reads = 0
	out := run(t, host, "ext-parcel", "list", "--where", "kind=bag", "--sort", "title:desc", "--per-page", "3", "--page", "2", "--search", "parcel")
	if !strings.Contains(out, "parcel 494") || !strings.Contains(out, `"total": 250`) {
		t.Fatalf("page 2 of the bags, title descending:\n%s", out)
	}
	if table.Reads > 3 {
		t.Fatalf("the extension read %d documents for a page of 3 (asked %v)", table.Reads, asked)
	}
}

// When a mounted extension store receives a regex or fuzzy text query, proxy
// query falls back to filtering in Go over list so mounted lists support regex
// and fuzzy filtering cleanly.
func TestProxyStoreRegexQueryFallsBackInGo(t *testing.T) {
	ext := ochazuke.NewRuntime()
	ext.Config().Disable()
	table := ochazuke.NewMemDocTable(ext, "parcel")
	ext.Define("parcel").Props(
		ochazuke.PropSpec{Name: "id", Key: true, CreateOnly: true},
		ochazuke.PropSpec{Name: "title", Role: ochazuke.RoleLabel},
	).Store(ochazuke.DocStore(table, "parcel"))
	for _, doc := range []ochazuke.Thing{
		{"id": "p1", "title": "search parcel"},
		{"id": "p2", "title": "filter parcel"},
		{"id": "p3", "title": "other box"},
	} {
		_ = table.Put(doc["id"].(string), doc)
	}
	raw, err := Catalog(ext, "extension", "v1")
	if err != nil {
		t.Fatal(err)
	}
	var doc CatalogDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	host := ochazuke.NewRuntime()
	host.Config().Disable()
	got := MountNouns(host, doc.AppNouns(), HostMount{
		Local: func(n string) string { return "ext-" + n },
		Proxy: func(noun string, v CatalogVerb) any {
			return func(c ochazuke.Ctx) ochazuke.Result {
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
	if _, err := host.Settings().SetSession(ochazuke.ListWindowSetting, 2); err != nil {
		t.Fatal(err)
	}
	list, err := host.AddList("parcels", nil, ochazuke.From("ext-parcel"))
	if err != nil {
		t.Fatal(err)
	}
	p := newPump(t, host)
	p.until("initial window", p.idle)
	if !list.Windowed() || list.WindowTotal() != 3 {
		t.Fatalf("initial windowed=%v total=%d", list.Windowed(), list.WindowTotal())
	}
	if err := list.Focus(); err != nil {
		t.Fatal(err)
	}
	if _, err := host.Execute(ochazuke.Request{Noun: "ochazuke.collection", Verb: "filter", Args: ochazuke.Arguments{"query": "search|filter"}}); err != nil {
		t.Fatal(err)
	}
	p.until("substring window", p.idle)
	if list.WindowTotal() != 0 {
		t.Fatalf("substring search|filter total=%d, want 0", list.WindowTotal())
	}
	if _, err := host.Execute(ochazuke.Request{Noun: "ochazuke.search", Verb: "set-match", Args: ochazuke.Arguments{"mode": ochazuke.MatchRegex}}); err != nil {
		t.Fatal(err)
	}
	p.until("regex window", p.idle)
	if list.WindowTotal() != 2 {
		t.Fatalf("regex search|filter total=%d, want 2", list.WindowTotal())
	}
}
