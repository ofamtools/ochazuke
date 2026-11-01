package cli

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/jsontree"
)

func jsonDoc() CatalogDoc {
	doc := sectionDoc()
	doc.Pages[0].Detail = &CatalogDetail{Padding: new(0), Sections: []CatalogDetailSection{
		{Kind: "fields", Title: "Metadata", Fields: []CatalogDetailField{{Label: "Status", Field: "status"}}},
		{Kind: "json", Title: "Request", Field: "request"},
		{Kind: "json", Title: "Response", Field: "response", Parse: true},
	}}
	return doc
}

func jsonRow(id string) Row {
	return Row{Noun: "item", ID: id, Label: id, Data: map[string]any{"status": "ok", "request": map[string]any{"alpha": 12, "nested": map[string]any{"ok": true}}, "response": `{"z":1,"a":["first","second"]}`}}
}

func TestDetailJSONCatalogRoundTripAndValidation(t *testing.T) {
	doc := jsonDoc()
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var got CatalogDoc
	if err = json.Unmarshal(b, &got); err != nil || !reflect.DeepEqual(doc, got) {
		t.Fatalf("roundtrip: %s, %v", b, err)
	}
	if err = checkPage(got.Pages[0]); err != nil {
		t.Fatal(err)
	}
	for _, s := range []CatalogDetailSection{{Kind: "json"}, {Kind: "json", Field: "x", Region: "side"}, {Kind: "markdown", Field: "x", Parse: true}, {Kind: "json", Field: "x", ReadOnly: new(false)}} {
		d := CatalogDetail{Sections: []CatalogDetailSection{s}}
		if err := checkDetailSections(d); err == nil {
			t.Fatalf("accepted %+v", s)
		}
	}
}

func TestDetailJSONNavigationContextAndValues(t *testing.T) {
	m, v := mountedPage(t, jsonDoc(), PageConfig{}, 180, 54)
	_ = v.SetRows("sections/list", []Row{jsonRow("one"), jsonRow("two")})
	frame := frameOf(m)
	if !strings.Contains(frame, "Structure") || !strings.Contains(frame, " │ ") || strings.Index(frame, "Metadata") > strings.Index(frame, "Request") {
		t.Fatal(frame)
	}
	press(m, "ctrl+w", "l", "j")
	if frame = frameOf(m); !strings.Contains(frame, "Data · .alpha") {
		t.Fatal(frame)
	}
	var subject any
	m.Runtime.Define("item").Verb("probe", func(c ochazuke.Ctx) ochazuke.Result { subject = c.Instance.Data; return ochazuke.Done(nil) }).Mode("normal").Bind("p", ochazuke.Do("item.probe"))
	press(m, "p")
	if subject != "one" {
		t.Fatalf("domain action subject = %v", subject)
	}
	s := v.jsonSections[0]
	n, ok := s.tree.Selected()
	if !ok || n.Data.(*jsontree.Node).Value != "12" {
		t.Fatal(n)
	}
	parent := m.Runtime.Instances[v.jsonParent]
	if parent.Noun != "item" || parent.Data != "one" {
		t.Fatalf("row context: %+v", parent)
	}
	press(m, "y")
	y, err := m.Runtime.ReadYank("")
	if err != nil || y.Text != "12" {
		t.Fatalf("copy: %+v, %v", y, err)
	}
	press(m, "g", "v")
	if in := m.Runtime.Input(m.Runtime.Focus); in == nil || !in.ReadOnly || string(in.Text) != "12" {
		t.Fatal("show must be read-only")
	}
	press(m, "esc")
	// Standard tree fold and motions remain available, and focus steps in
	// drawing order to the second JSON section.
	press(m, "k", "enter")
	if s.tree.List().Shown() != 1 {
		t.Fatal("enter did not fold root")
	}
	press(m, "enter", "ctrl+w", "l", "j")
	if frame = frameOf(m); !strings.Contains(frame, "Data · .z") {
		t.Fatal(frame)
	}
	if !v.jsonSections[1].tree.List().Focused() {
		t.Fatal("second section is not a stop")
	}
	press(m, "ctrl+w", "h", "ctrl+w", "h", "j", "ctrl+w", "l")
	frameOf(m)
	if m.Runtime.Instances[v.jsonParent].Data != "two" {
		t.Fatal("detail did not follow source selection")
	}
}

func TestDetailJSONNarrowFallbackAndOptOut(t *testing.T) {
	m, v := mountedPage(t, jsonDoc(), PageConfig{}, 54, 54)
	row := jsonRow("one")
	row.Data.(map[string]any)["response"] = "not JSON\nsecond line"
	_ = v.SetRows("sections/list", []Row{row})
	frameOf(m)
	press(m, "ctrl+w", "l", "j")
	frame := frameOf(m)
	if !strings.Contains(frame, "Data · .alpha") {
		t.Fatal(frame)
	}
	for _, line := range strings.Split(frame, "\n") {
		if strings.Contains(line, "Structure") && strings.Contains(line, "Data ·") {
			t.Fatal("narrow columns did not stack")
		}
	}
	if v.jsonSections[1].active {
		t.Fatal("unparseable string became tree")
	}
	// Inspect the composed detail at a generous height to check full fallback.
	text := v.sectionsPanel(ochazuke.Layout{Styles: ochazuke.NewStyles(*m.Appearance)}, 54, 70).Text
	if !strings.Contains(text, "not JSON") || !strings.Contains(text, "second line") {
		t.Fatal(text)
	}
	if err := m.Runtime.Settings().Set("detail.sections", false); err != nil {
		t.Fatal(err)
	}
	frame = frameOf(m)
	if v.jsonFocused() || strings.Contains(frame, "Structure") {
		t.Fatal("opt-out left a focused/visible tree:\n" + frame)
	}
	press(m, "ctrl+w", "l")
	if v.jsonFocused() {
		t.Fatal("opt-out tree remained in focus order")
	}
}

func TestDetailJSONValueScrollRefreshAndSmallSizes(t *testing.T) {
	m, v := mountedPage(t, jsonDoc(), PageConfig{}, 180, 30)
	row := jsonRow("one")
	row.Data.(map[string]any)["request"] = map[string]any{"lines": strings.Repeat("long value ", 80)}
	_ = v.SetRows("sections/list", []Row{row})
	frameOf(m)
	press(m, "ctrl+w", "l", "9", "9", "J")
	frameOf(m)
	if v.jsonSections[0].value.Offset() == 0 {
		t.Fatal("J did not scroll data")
	}
	press(m, "j")
	frameOf(m)
	if v.jsonSections[0].value.Offset() != 0 {
		t.Fatal("node change kept value offset")
	}
	// A changed value refreshes in place even while focused within the tree.
	row.Data.(map[string]any)["request"] = map[string]any{"lines": "changed"}
	_ = v.SetRows("sections/list", []Row{row})
	if frame := frameOf(m); !strings.Contains(frame, "changed") {
		t.Fatal(frame)
	}
	for width := 1; width < 72; width++ {
		text := v.jsonSectionsText(ochazuke.Layout{Styles: ochazuke.NewStyles(*m.Appearance)}, "", width, 12)
		for _, line := range strings.Split(text, "\n") {
			if ansi.StringWidth(line) > width {
				t.Fatalf("width %d overflow: %q", width, line)
			}
		}
	}
}
