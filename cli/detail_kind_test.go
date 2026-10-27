package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/unixsuperhero/ochazuke"
)

// fakeKind is a detail view that logs what it is shown and draws the
// row's path, with a focusable list of one line under its parent.
type fakeKind struct {
	h     DetailHost
	shown []string
	list  *ochazuke.List
}

func (f *fakeKind) Show(row Row, ok bool) error {
	if !ok {
		f.shown = append(f.shown, "-")
		return nil
	}
	f.shown = append(f.shown, row.ID+" "+f.h.Field(row, f.h.Spec.PathField))
	return nil
}

func (f *fakeKind) Block(w, h int) ochazuke.Block {
	return ochazuke.Text(f.list.View(w, h))
}

func (f *fakeKind) Focused() bool { return f.list.Focused() }

func fakeFactory(made **fakeKind) DetailFactory {
	return func(h DetailHost) (DetailView, error) {
		h.Model.Runtime.Define("fake-line")
		list, err := h.Model.Runtime.AddList(h.Parent+"/lines", []ochazuke.ListItem{{ID: h.Parent + "/lines/0", Label: "fake view line", Noun: "fake-line"}}, ochazuke.Parent(h.Parent))
		if err != nil {
			return nil, err
		}
		*made = &fakeKind{h: h, list: list}
		return *made, nil
	}
}

func kindDoc(kind string) CatalogDoc {
	doc := pageDoc(LayoutSplit, true)
	doc.Pages[0].Detail = &CatalogDetail{Kind: kind, PathField: "path"}
	return doc
}

// TestDetailKindFollowsTheSelectedRow: a page naming a kind the mount
// registers draws that kind's view, told the selected row whenever it
// changes (not on every frame); the view's parent stands for the row, so
// the row noun's verbs answer from inside it with the row's id; the view
// is a focus stop after the lists.
func TestDetailKindFollowsTheSelectedRow(t *testing.T) {
	var made *fakeKind
	cfg := PageConfig{Fetch: answers(pageAnswers), DetailKinds: map[string]DetailFactory{"fake": fakeFactory(&made)}}
	m, v := mountedPage(t, kindDoc("fake"), cfg, 100, 20)
	if err := v.Reload(); err != nil {
		t.Fatal(err)
	}
	if err := Settle(m.Runtime, nil); err != nil {
		t.Fatal(err)
	}
	frame := frameOf(m)
	frameOf(m)
	if made == nil || !strings.Contains(frame, "fake view line") || !strings.Contains(frame, "architect") {
		t.Fatalf("kind view not drawn:\n%s", frame)
	}
	if got := strings.Join(made.shown, ","); got != "s1 ~/.claude/skills/architect" {
		t.Fatalf("shown %q; want the first row once", got)
	}
	press(m, "j")
	frameOf(m)
	if got := made.shown[len(made.shown)-1]; got != "s2 ~/proj/ai-tools/review" {
		t.Fatalf("after j shown %q", got)
	}
	parent := m.Runtime.Instances[v.DetailParent()]
	if parent == nil || parent.Noun != "skill" || parent.Data != "s2" || parent.Label != "review" {
		t.Fatalf("row stand-in = %+v", parent)
	}
	if v.DetailView() != made {
		t.Fatal("DetailView is not the kind's view")
	}
	// The row noun's verbs act on the row from inside the view.
	var target any
	m.Runtime.Define("skill").Verb("open", func(c ochazuke.Ctx) ochazuke.Result {
		target = c.Instance.Data
		return ochazuke.Done(nil)
	}, "Open")
	if err := m.Runtime.SetFocus(made.list.FocusTarget()); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Runtime.Execute(ochazuke.Request{Noun: "skill", Verb: "open"}); err != nil || target != "s2" {
		t.Fatalf("skill.open from the view: target %v, err %v", target, err)
	}
	if !made.Focused() {
		t.Fatal("the view's list is not focused")
	}
}

// TestDetailKindRefreshesAChangedRecord: the same row with a changed
// record (a reload) is shown again.
func TestDetailKindRefreshesAChangedRecord(t *testing.T) {
	var made *fakeKind
	cfg := PageConfig{DetailKinds: map[string]DetailFactory{"fake": fakeFactory(&made)}}
	m, v := mountedPage(t, kindDoc("fake"), cfg, 100, 20)
	rows := []Row{{ID: "a", Label: "a", Data: map[string]any{"path": "/one"}}}
	if err := v.SetRows("ai/skills", rows); err != nil {
		t.Fatal(err)
	}
	frameOf(m)
	frameOf(m)
	rows[0].Data = map[string]any{"path": "/one", "modified": "later"}
	if err := v.SetRows("ai/skills", rows); err != nil {
		t.Fatal(err)
	}
	frameOf(m)
	if got := strings.Join(made.shown, ","); got != "a /one,a /one" {
		t.Fatalf("shown %q; want the row twice", got)
	}
}

// TestDetailKindUnregisteredShowsTheRecord: a kind the host did not
// register is the record view with a line saying so; the registry is
// process-wide and refuses the record kind.
func TestDetailKindUnregisteredShowsTheRecord(t *testing.T) {
	m, v := mountedPage(t, kindDoc("nope"), PageConfig{Fetch: answers(pageAnswers)}, 100, 20)
	if err := v.Reload(); err != nil {
		t.Fatal(err)
	}
	if err := Settle(m.Runtime, nil); err != nil {
		t.Fatal(err)
	}
	frame := frameOf(m)
	if !strings.Contains(frame, `detail kind "nope" is not registered`) || !strings.Contains(frame, "~/.claude/skills/architect") {
		t.Fatalf("frame:\n%s", frame)
	}
	if v.DetailView() != nil || v.DetailParent() != "" {
		t.Fatal("an unregistered kind has a view")
	}
	if err := RegisterDetailKind(DetailRecord, nil); err == nil {
		t.Fatal("the record kind was replaced")
	}
	var made *fakeKind
	if err := RegisterDetailKind("fake-global", fakeFactory(&made)); err != nil {
		t.Fatal(err)
	}
	defer RegisterDetailKind("fake-global", nil)
	found := false
	for _, k := range DetailKinds() {
		found = found || k == "fake-global"
	}
	if !found {
		t.Fatalf("DetailKinds = %v", DetailKinds())
	}
	m, _ = mountedPage(t, kindDoc("fake-global"), PageConfig{}, 100, 20)
	if frameOf(m); made == nil {
		t.Fatal("the registered kind was not built")
	}
}

// TestCatalogPublishesTheDetailKind: kind and pathField round-trip
// through the catalog.
func TestCatalogPublishesTheDetailKind(t *testing.T) {
	r := ochazuke.NewRuntime()
	r.Define("doc").Verb("list", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) }, "List")
	if err := DeclarePage(r, CatalogPage{ID: "docs", Title: "Docs", Layout: LayoutSplit, Lists: []CatalogList{{ID: "docs/docs", Noun: "doc"}}, Detail: &CatalogDetail{Kind: "markdown", PathField: "path"}}); err != nil {
		t.Fatal(err)
	}
	data, err := Catalog(r, "o-md", "v1")
	if err != nil {
		t.Fatal(err)
	}
	var doc CatalogDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if d := doc.Pages[0].Detail; d == nil || d.Kind != "markdown" || d.PathField != "path" {
		t.Fatalf("detail = %+v", d)
	}
}
