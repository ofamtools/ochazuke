package cli

import (
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

func relatedDoc() CatalogDoc {
	doc := sectionDoc()
	doc.Pages[0].Detail.Sections = append(doc.Pages[0].Detail.Sections, CatalogDetailSection{Kind: "related"})
	return doc
}

// relatedPage mounts a page whose detail has a related section, in a
// runtime that has a note noun with an any-noun "about" prop.
func relatedPage(t *testing.T, notes []ochazuke.Thing) (*ochazuke.Model, *PageView) {
	t.Helper()
	return relatedPageWith(t, notes, relatedDoc(), PageConfig{})
}

func relatedPageWith(t *testing.T, notes []ochazuke.Thing, doc CatalogDoc, cfg PageConfig) (*ochazuke.Model, *PageView) {
	t.Helper()
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	r.Define("note").Props(ochazuke.PropSpec{Name: "id", Key: true}, ochazuke.PropSpec{Name: "text", Role: ochazuke.RoleLabel},
		ochazuke.PropSpec{Name: "about", Parameter: ochazuke.Parameter{Noun: ochazuke.AnyNoun, Free: true}}).Store(ochazuke.Store{
		Kind: ochazuke.KindCollection, AcceptsKey: true,
		List: func(_ ochazuke.Ctx, q ochazuke.ListQuery) ([]ochazuke.Thing, error) {
			var out []ochazuke.Thing
			for _, n := range notes {
				match := true
				for _, f := range q.Filters {
					match = match && n[f.Prop] == f.Value
				}
				if match {
					out = append(out, n)
				}
			}
			return out, nil
		},
	})
	if cfg.Related != nil && cfg.Related.Empty != "" {
		if err := r.DeclareSetting(ochazuke.Setting{Name: cfg.Related.Empty, Type: ochazuke.SettingString, Default: ochazuke.RelatedEmptyHide}); err != nil {
			t.Fatal(err)
		}
	}
	m := ochazuke.NewModel(r)
	m.Width, m.Height = 120, 40
	m.Appearance.Theme = "mono"
	views, err := MountPages(m, doc, cfg)
	if err != nil || len(views) != 1 {
		t.Fatalf("MountPages = %v, %v", views, err)
	}
	if err := r.OpenPage(views[0].Page()); err != nil {
		t.Fatal(err)
	}
	return m, views[0]
}

func TestCatalogRelatedSectionKindValidates(t *testing.T) {
	doc := relatedDoc()
	if err := checkPage(doc.Pages[0]); err != nil {
		t.Fatal(err)
	}
	p := doc.Pages[0]
	d := *p.Detail
	p.Detail = &d
	d.Sections = []CatalogDetailSection{{Kind: "related", Region: "side"}}
	if err := checkPage(p); err == nil {
		t.Fatal("a related section was accepted in the side region")
	}
}

func TestCatalogRelatedSectionFollowsTheSelectedRow(t *testing.T) {
	notes := []ochazuke.Thing{
		{"id": "n1", "text": "first thoughts", "about": "item:one"},
		{"id": "n2", "text": "second thoughts", "about": "item:two"},
	}
	m, v := relatedPage(t, notes)
	if err := v.SetRows("sections/list", []Row{sectionRow("one"), sectionRow("two")}); err != nil {
		t.Fatal(err)
	}
	frame := frameOf(m)
	if !strings.Contains(frame, "Notes (about) (1)") || !strings.Contains(frame, "first thoughts") || strings.Contains(frame, "second thoughts") {
		t.Fatalf("notes of row one:\n%s", frame)
	}
	press(m, "j")
	frame = frameOf(m)
	if !strings.Contains(frame, "second thoughts") || strings.Contains(frame, "first thoughts") {
		t.Fatalf("notes of row two:\n%s", frame)
	}
	if err := m.Runtime.Settings().Set(ochazuke.RelatedDetailSetting, false); err != nil {
		t.Fatal(err)
	}
	if frame = frameOf(m); strings.Contains(frame, "Notes") || strings.Contains(frame, "second thoughts") {
		t.Fatalf("detail.related off still shows notes:\n%s", frame)
	}
	if !strings.Contains(frame, "readable") {
		t.Fatalf("the rest of the detail is gone:\n%s", frame)
	}
}

// The host's Related config puts the stack under every detail: the record
// view and declared sections without a related section of their own, and
// nothing under a row without notes when the empty mode hides.
func TestPageConfigRelatedShowsNotesUnderAnyDetail(t *testing.T) {
	notes := []ochazuke.Thing{{"id": "n1", "text": "first thoughts", "about": "item:one"}}
	record := sectionDoc()
	record.Pages[0].Detail = &CatalogDetail{}
	declared := sectionDoc()
	for name, doc := range map[string]CatalogDoc{"record": record, "sections": declared} {
		t.Run(name, func(t *testing.T) {
			m, v := relatedPageWith(t, notes, doc, PageConfig{Related: &RelatedConfig{Empty: "rel.empty"}})
			if err := v.SetRows("sections/list", []Row{sectionRow("one"), sectionRow("two")}); err != nil {
				t.Fatal(err)
			}
			if frame := frameOf(m); !strings.Contains(frame, "first thoughts") {
				t.Fatalf("row one has no notes:\n%s", frame)
			}
			press(m, "j")
			if frame := frameOf(m); strings.Contains(frame, "Notes") || strings.Contains(frame, "first thoughts") {
				t.Fatalf("row two (no notes) shows a section:\n%s", frame)
			}
		})
	}
}
