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
