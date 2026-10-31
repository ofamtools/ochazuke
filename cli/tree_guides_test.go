package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

func TestCatalogTreeGuides(t *testing.T) {
	doc := pageDoc(LayoutStack, false)
	doc.Pages[0].Lists = doc.Pages[0].Lists[:1]
	doc.Pages[0].Lists[0].Tree = true
	doc.Pages[0].Lists[0].Guides = true
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	m, v := mountedPage(t, doc, PageConfig{}, 80, 24)
	rows, err := DecodeRows("skill", []byte(`[{"id":"r","label":"Root"},{"id":"a","parent":"r","label":"Alpha"},{"id":"b","parent":"r","label":"Beta"}]`))
	if err != nil || len(rows) != 3 || rows[1].Parent != "r" {
		t.Fatalf("decoded rows: %+v, %v", rows, err)
	}
	if err := v.SetRows("ai/skills", rows); err != nil {
		t.Fatal(err)
	}
	answer, err := DecodeState([]byte("{\"state\":1,\"cache\":\"fixture\"}\n{\"noun\":\"skill\",\"id\":\"a\",\"parent\":\"r\",\"label\":\"Alpha\"}\n"))
	if err != nil || len(answer.Rows) != 1 || answer.Rows[0].Parent != "r" {
		t.Fatalf("state lost parent: %+v, %v", answer, err)
	}
	frame := frameOf(m)
	for _, want := range []string{"├─   Alpha", "└─   Beta"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("missing %q:\n%s", want, frame)
		}
	}
	if err := m.Runtime.Settings().Set(ochazuke.TreeGuidesSetting, "off"); err != nil {
		t.Fatal(err)
	}
	if frame := frameOf(m); strings.ContainsAny(frame, "├└") {
		t.Fatal(frame)
	}
	if err := v.SetRows("ai/skills", []Row{{ID: "orphan", Parent: "missing"}}); err == nil {
		t.Fatal("tree accepted a missing parent")
	}
}
