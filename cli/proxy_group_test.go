package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"ofam.dev/ochazuke"
)

func TestProxyWindowGroupingRoundTrip(t *testing.T) {
	t.Parallel()
	ext := ochazuke.NewRuntime()
	ext.Config().Disable()
	table := ochazuke.NewMemDocTable(ext, "parcel")
	ext.Define("parcel").Props(
		ochazuke.PropSpec{Name: "id", Key: true},
		ochazuke.PropSpec{Name: "title", Role: ochazuke.RoleLabel},
		ochazuke.PropSpec{Name: "kind"},
	).Store(ochazuke.DocStore(table, "parcel"))
	for i := 1; i <= 12; i++ {
		kind := "box"
		if i%2 == 0 {
			kind = "bag"
		}
		id := fmt.Sprintf("p%02d", i)
		if err := table.Put(id, ochazuke.Thing{"id": id, "title": "Parcel " + id, "kind": kind}); err != nil {
			t.Fatal(err)
		}
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
	for name, value := range map[string]any{ochazuke.ListWindowSetting: 4, ochazuke.ListGroupBySetting: "kind"} {
		if _, err := host.Settings().SetSession(name, value); err != nil {
			t.Fatal(err)
		}
	}
	var asked []ochazuke.Arguments
	got := MountNouns(host, doc.AppNouns(), HostMount{
		Local: func(n string) string { return "ext-" + n },
		Proxy: func(noun string, v CatalogVerb) any {
			return func(c ochazuke.Ctx) ochazuke.Result {
				if v.Standard == "list" {
					asked = append(asked, c.Args)
				}
				value, err := ext.Execute(ochazuke.Request{Noun: noun, Verb: v.Name, Args: c.Args})
				if err != nil {
					return ochazuke.Failed(err)
				}
				// Use the wire representation too, not only a typed ListAnswer.
				wire, err := json.Marshal(value)
				if err != nil {
					return ochazuke.Failed(err)
				}
				var decoded any
				if err := json.Unmarshal(wire, &decoded); err != nil {
					return ochazuke.Failed(err)
				}
				return ochazuke.Pending(func(context.Context) (any, error) { return decoded, nil })
			}
		},
	})
	if len(got.Warnings) > 0 {
		t.Fatal(got.Warnings)
	}
	list, err := host.AddList("parcels", nil, ochazuke.From("ext-parcel"))
	if err != nil {
		t.Fatal(err)
	}
	p := newPump(t, host)
	p.until("the grouped window", p.idle)
	for _, job := range host.Jobs() {
		if job.Status == ochazuke.JobFailed {
			t.Fatalf("grouped window read failed: %s", job.Error)
		}
	}
	if len(asked) != 1 || asked[0]["group-by"] != "kind" || asked[0]["per-page"] != 4 {
		t.Fatalf("bounded grouping was not forwarded: %v", asked)
	}
	if table.Reads != 4 || list.WindowTotal() != 12 || len(list.Items()) != 4 || list.Items()[0].Section != "box" {
		t.Fatalf("first page reads=%d total=%d items=%+v jobs=%+v", table.Reads, list.WindowTotal(), list.Items(), host.Jobs())
	}
	if _, err := host.Execute(ochazuke.Request{Noun: "ochazuke.collection", Verb: "group", Args: ochazuke.Arguments{"id": "parcels", "by": "kind", "order": "kind:desc"}}); err != nil {
		t.Fatal(err)
	}
	p.until("the reordered grouped window", p.idle)
	if last := asked[len(asked)-1]; last["group-sort"] != "kind:desc" || last["per-page"] != 4 {
		t.Fatalf("group sort was not forwarded: %v", last)
	}
	if err := list.SetPage(2); err != nil {
		t.Fatal(err)
	}
	p.until("the continued group", p.idle)
	text := ansi.Strip(list.View(80, 14))
	if table.Reads != 12 || !strings.Contains(text, "box (2 on page)") || !strings.Contains(text, "bag (2 on page)") {
		t.Fatalf("continued group counts, reads=%d:\n%s", table.Reads, text)
	}
	if err := host.SetFocus(list.Items()[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := host.Execute(ochazuke.Request{Noun: "ochazuke.collection", Verb: "move", Args: ochazuke.Arguments{"delta": -1}}); err != nil {
		t.Fatal(err)
	}
	p.until("the previous page's last row", p.idle)
	if item, _ := list.Selected(); item.ID != "parcels/p07" || host.Focus != item.ID {
		t.Fatalf("remote upward edge selected=%+v focus=%s", item, host.Focus)
	}
	if _, err := host.Execute(ochazuke.Request{Noun: "ochazuke.collection", Verb: "group-fold", Args: ochazuke.Arguments{"id": "parcels"}}); err != nil {
		t.Fatal(err)
	}
	if list.Shown() != 1 {
		t.Fatalf("all-folded remote page has %d rows", list.Shown())
	}
	if _, err := host.Execute(ochazuke.Request{Noun: "ochazuke.collection", Verb: "move", Args: ochazuke.Arguments{"delta": 1}}); err != nil {
		t.Fatal(err)
	}
	p.until("the continued folded remote group", p.idle)
	if item, _ := list.Selected(); item.Noun != "ochazuke.collection" || !strings.Contains(item.Label, "box (2 on page)") || host.Focus != item.ID {
		t.Fatalf("remote downward edge selected=%+v focus=%s", item, host.Focus)
	}
	if table.Reads != 20 {
		t.Fatalf("remote page/fold reads=%d, want 20", table.Reads)
	}
}
