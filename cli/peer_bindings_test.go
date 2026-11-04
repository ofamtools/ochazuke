package cli

import (
	"encoding/json"
	"ofam.dev/ochazuke"
	"testing"
)

func TestCatalogPeerBindingsStayHostOnly(t *testing.T) {
	t.Parallel()
	producer := ochazuke.NewRuntime()
	producer.Define("lane").Verb("list", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) })
	row := ochazuke.Binding{Noun: "lane.lane", Mode: "normal", Keys: "g s", Request: ochazuke.Request{Noun: "md.doc", Verb: "send-notes"}, From: map[string]string{"target": "row:doc"}}
	raw, err := Catalog(producer, "o-lane", "0.1.0", PeerBindings(row))
	if err != nil {
		t.Fatal(err)
	}
	var doc CatalogDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.PeerBindings) != 1 || doc.PeerBindings[0].Request.Key() != "md.doc.send-notes" {
		t.Fatalf("catalog: %+v", doc.PeerBindings)
	}
	for _, b := range producer.BindingsDoc().Rows() {
		if b.Request.Key() == "md.doc.send-notes" {
			t.Fatal("producer installed a peer row")
		}
	}
	host := ochazuke.NewRuntime()
	got := MountCatalog(host, doc, HostMount{Local: func(n string) string { return "lane." + n }, Proxy: func(string, CatalogVerb) any { return func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) } }})
	if len(got.Warnings) > 0 {
		t.Fatal(got.Warnings)
	}
	raw, err = Catalog(host, "host", "v1")
	if err != nil {
		t.Fatal(err)
	}
	var mounted CatalogDoc
	if err := json.Unmarshal(raw, &mounted); err != nil {
		t.Fatal(err)
	}
	if len(mounted.Extensions) != 1 || mounted.Extensions[0].Version != "0.1.0" {
		t.Fatalf("metadata: %+v", mounted.Extensions)
	}
}
