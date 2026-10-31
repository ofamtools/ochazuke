package cli

import (
	"encoding/json"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
)

func sectionDoc() CatalogDoc {
	return CatalogDoc{Schema: Schema, Name: "sections", Nouns: []CatalogNoun{{Name: "item", Verbs: []CatalogVerb{{Name: "list", Rows: "item"}}}}, Pages: []CatalogPage{{ID: "sections", Title: "Sections", Layout: LayoutSplit, Lists: []CatalogList{{ID: "sections/list", Noun: "item"}}, Detail: &CatalogDetail{Sections: []CatalogDetailSection{
		{Kind: "markdown", Field: "question", Heading: true},
		{Kind: "thread", Field: "answers"},
		{Kind: "fields", Region: "side", Title: "Metadata", Fields: []CatalogDetailField{{Label: "Pane", Field: "source.pane", Format: "short-id"}, {Label: "Status", Field: "status", Format: "badge"}}},
	}}}}}
}

func sectionRow(id string) Row {
	return Row{Noun: "item", ID: id, Label: id, Data: map[string]any{
		"question": "A **readable** question?", "source": map[string]any{"pane": "12345678-1234-5678-abcd-123456789abc"}, "status": "answered",
		"answers": []any{
			map[string]any{"author": "newest", "at": "2026-10-31T12:00:00Z", "body": "Last answer."},
			map[string]any{"author": "oldest", "at": "2026-10-30T12:00:00Z", "body": "First answer."},
		},
	}}
}

func TestCatalogDetailSectionsJSON(t *testing.T) {
	doc := sectionDoc()
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var got CatalogDoc
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(doc, got) {
		t.Fatalf("catalog sections lost in JSON: %s", b)
	}
	if err := checkPage(got.Pages[0]); err != nil {
		t.Fatal(err)
	}
	for _, section := range []CatalogDetailSection{
		{Kind: "unknown"}, {Kind: "markdown"}, {Kind: "thread", Field: "answers", Region: "footer"},
		{Kind: "fields"}, {Kind: "fields", Fields: []CatalogDetailField{{Label: "Name", Field: "name", Format: "yaml"}}},
		{Kind: "fields", Fields: []CatalogDetailField{{Field: "name"}}},
		{Kind: "markdown", Field: "question", ReadOnly: new(false)},
	} {
		p := got.Pages[0]
		d := *p.Detail
		p.Detail = &d
		d.Sections = []CatalogDetailSection{section}
		if err := checkPage(p); err == nil {
			t.Errorf("accepted invalid section %+v", section)
		}
	}
}

func TestDetailFormats(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		format        string
		value         any
		display, full string
	}{
		{"text", "first\nlast", "first\nlast", "first\nlast"},
		{"badge", "answered", "[answered]", "answered"},
		{"short-id", "123456789abcdef", "12345678", "123456789abcdef"},
		{"path", "/Volumes/never-stat/long/path", "/Volumes/never-stat/long/path", "/Volumes/never-stat/long/path"},
		{"list", []any{"one", "two"}, "one, two", "one, two"},
		{"text", true, "true", "true"},
		{"text", map[string]any{"b": 2, "a": "one"}, "a: one; b: 2", "a: one; b: 2"},
		{"json", map[string]any{"b": 2}, "{\n  \"b\": 2\n}", "{\n  \"b\": 2\n}"},
		{"relative-time", "2026-10-31T10:00:00Z", "2 hours ago", "2026-10-31T10:00:00Z"},
		{"relative-time", "2026-10-31T12:01:00Z", "in 1 minute", "2026-10-31T12:01:00Z"},
		{"time", "2026-10-31T12:00:00Z", "2026-10-31 12:00 UTC", "2026-10-31T12:00:00Z"},
		{"time", "invalid", "invalid", "invalid"},
	} {
		d, f := formatDetail(tc.value, tc.format, now)
		if d != tc.display || f != tc.full {
			t.Errorf("%s: (%q,%q), want (%q,%q)", tc.format, d, f, tc.display, tc.full)
		}
	}
	if got := detailValue(map[string]any{"a.b": "exact", "a": map[string]any{"b": "nested"}}, "a.b"); got != "exact" {
		t.Fatal(got)
	}
}

func TestDetailSectionsLayoutAndScroll(t *testing.T) {
	m, v := mountedPage(t, sectionDoc(), PageConfig{}, 200, 32)
	row := sectionRow("one")
	row.Data.(map[string]any)["answers"] = []any{map[string]any{"author": "agent", "body": strings.Repeat("every word remains readable ", 180) + "FINAL_SENTENCE"}}
	if err := v.SetRows("sections/list", []Row{row, sectionRow("two")}); err != nil {
		t.Fatal(err)
	}
	frame := frameOf(m)
	if !strings.Contains(frame, "Metadata") || !strings.Contains(frame, " │ ") {
		t.Fatal(frame)
	}
	if strings.Contains(frame, "FINAL_SENTENCE") {
		t.Fatal("fixture does not overflow")
	}
	press(m, "9", "9", "9", "J")
	frame = frameOf(m)
	if !strings.Contains(frame, "FINAL_SENTENCE") {
		t.Fatal("answer truncated:\n" + frame)
	}
	press(m, "j")
	frame = frameOf(m)
	if !strings.Contains(frame, "readable") || v.detail.Offset() != 0 {
		t.Fatal("selection did not reset scroll:\n" + frame)
	}
	// Main and side are composed in reading order when the detail narrows.
	text := ansi.Strip(v.sectionsPanel(ochazuke.Layout{Styles: ochazuke.NewStyles(*m.Appearance)}, 70, 60).Text)
	if strings.Contains(text, " │ ") || strings.Index(text, "Metadata") < strings.Index(text, "Last answer") {
		t.Fatal(text)
	}
	if strings.Index(text, "First answer") > strings.Index(text, "Last answer") {
		t.Fatal("thread is not chronological:\n" + text)
	}
	if err := m.Runtime.Settings().Set("detail.side", false); err != nil {
		t.Fatal(err)
	}
	if frame := frameOf(m); strings.Contains(frame, "Metadata") || strings.Contains(frame, "12345678") {
		t.Fatal(frame)
	}
	if err := m.Runtime.Settings().Set("detail.sections", false); err != nil {
		t.Fatal(err)
	}
	if frame := frameOf(m); !strings.Contains(frame, "question") {
		t.Fatal("legacy opt-out missing:\n" + frame)
	}
}

func TestDetailFieldsCopyExpandReadOnly(t *testing.T) {
	m, v := mountedPage(t, sectionDoc(), PageConfig{}, 200, 32)
	if err := v.SetRows("sections/list", []Row{sectionRow("one")}); err != nil {
		t.Fatal(err)
	}
	frameOf(m)
	press(m, "g", "m")
	frame := frameOf(m)
	if !strings.Contains(frame, "Detail fields") {
		t.Fatal(frame)
	}
	press(m, "y")
	yank, err := m.Runtime.ReadYank("")
	if err != nil || yank.Text != "12345678-1234-5678-abcd-123456789abc" {
		t.Fatalf("copy = %+v, %v", yank, err)
	}
	press(m, "enter")
	frame = frameOf(m)
	if !strings.Contains(frame, yank.Text) {
		t.Fatal("full value absent:\n" + frame)
	}
	in := m.Runtime.Input(m.Runtime.Focus)
	if in == nil || !in.ReadOnly {
		t.Fatal("expanded field is editable")
	}
	press(m, "esc", "esc")
	if m.Runtime.TopModal() != "" {
		t.Fatal("fields did not close")
	}
	if err := m.Runtime.Settings().Set("detail.side", false); err != nil {
		t.Fatal(err)
	}
	press(m, "g", "m")
	if m.Runtime.TopModal() != "" {
		t.Fatal("hidden metadata action still available")
	}
}

func TestDetailSectionsPageOverrides(t *testing.T) {
	doc := sectionDoc()
	doc.Pages[0].Detail.Side = new(false)
	m, v := mountedPage(t, doc, PageConfig{}, 200, 30)
	_ = v.SetRows("sections/list", []Row{sectionRow("one")})
	if strings.Contains(frameOf(m), "Metadata") {
		t.Fatal("page override ignored")
	}
	v.spec.Detail.SideSetting = "app.metadata"
	if err := m.Runtime.DeclareSetting(ochazuke.Setting{Name: "app.metadata", Type: ochazuke.SettingBoolean, Default: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(frameOf(m), "Metadata") {
		t.Fatal("app setting ignored")
	}
}

func TestDetailSectionsOptOutEqualsLegacy(t *testing.T) {
	doc := sectionDoc()
	doc.Pages[0].Detail.Fields = []string{"question", "status"}
	m, v := mountedPage(t, doc, PageConfig{}, 100, 24)
	if err := v.SetRows("sections/list", []Row{sectionRow("one")}); err != nil {
		t.Fatal(err)
	}
	if err := m.Runtime.Settings().Set("detail.sections", false); err != nil {
		t.Fatal(err)
	}
	got := frameOf(m)
	doc.Pages[0].Detail.Sections = nil
	legacy, lv := mountedPage(t, doc, PageConfig{}, 100, 24)
	if err := lv.SetRows("sections/list", []Row{sectionRow("one")}); err != nil {
		t.Fatal(err)
	}
	if want := frameOf(legacy); got != want {
		t.Fatalf("legacy output changed:\n%s\nwant:\n%s", got, want)
	}
}

func TestDetailSectionsMarkdownStyles(t *testing.T) {
	m, v := mountedPage(t, sectionDoc(), PageConfig{}, 200, 32)
	m.Appearance.Theme = "dark"
	row := sectionRow("styled")
	row.Data.(map[string]any)["question"] = "A **strong** question with `inline()` and [the guide](https://example.test/guide)."
	if err := v.SetRows("sections/list", []Row{row}); err != nil {
		t.Fatal(err)
	}
	frame := m.View().Content
	if !regexp.MustCompile("\x1b\\[38;[0-9;]+mthe guide").MatchString(frame) || !strings.Contains(frame, "https://example.test/guide") {
		t.Fatalf("link lost its style or destination: %q", frame)
	}
}

func TestDetailSectionsKeepsAppMetadataKey(t *testing.T) {
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	ran := false
	r.Define("app").Verb("metadata", func(c ochazuke.Ctx) ochazuke.Result { ran = true; return ochazuke.Done(nil) }).Mode("normal").Bind("g m", ochazuke.Do("app.metadata"))
	m := ochazuke.NewModel(r)
	m.Width, m.Height = 200, 32
	views, err := MountPages(m, sectionDoc(), PageConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if err := views[0].SetRows("sections/list", []Row{sectionRow("one")}); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("sections"); err != nil {
		t.Fatal(err)
	}
	frameOf(m)
	press(m, "g", "m")
	if !ran || r.TopModal() != "" {
		t.Fatal("catalog default replaced the application's g m")
	}
}
