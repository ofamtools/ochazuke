package ochazuke

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"ofam.dev/ochazuke/keytest"
)

func TestAnyRefSplitsAtTheFirstColon(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		value, noun, key string
		ok               bool
	}{
		{"widget:W12", "widget", "W12", true},
		{"md.doc:/Users/me/notes/a:b.md", "md.doc", "/Users/me/notes/a:b.md", true},
		{"cmd.cmd:deploy/prod", "cmd.cmd", "deploy/prod", true},
		{"task:", "task", "", true},
		{"nocolon", "", "", false},
		{":x", "", "", false},
		{"", "", "", false},
	} {
		noun, key, ok := SplitAnyRef(c.value)
		if noun != c.noun || key != c.key || ok != c.ok {
			t.Errorf("SplitAnyRef(%q) = %q, %q, %v; want %q, %q, %v", c.value, noun, key, ok, c.noun, c.key, c.ok)
		}
		if c.ok && AnyRef(noun, key) != c.value {
			t.Errorf("AnyRef(%q, %q) = %q, want %q", noun, key, AnyRef(noun, key), c.value)
		}
	}
}

// anyFixture is a runtime with three nouns that notes can be about (widget,
// lane, doc: keys with a colon and slashes), a note noun whose "about" names
// a thing of any noun, a list of mixed rows, and a detail stack following it.
type anyFixture struct {
	r     *Runtime
	m     *Model
	rel   *Related
	list  *Collection
	notes *relMem
}

func newAnyFixture(t *testing.T) *anyFixture {
	t.Helper()
	f := &anyFixture{notes: &relMem{}}
	r := NewRuntime()
	f.r = r
	for _, n := range []struct{ noun, key, name string }{{"widget", "w1", "shell"}, {"lane", "w1", "build"}, {"doc", "/a/b:c.md", "readme"}} {
		mem := &relMem{}
		mem.add("id", n.key, "name", n.name)
		r.Define(n.noun).Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "name", Role: RoleLabel}).Store(mem.store())
	}
	f.notes.add("id", "n1", "text", "about the widget", "about", "widget:w1")
	f.notes.add("id", "n2", "text", "about the lane", "about", "lane:w1")
	f.notes.add("id", "n3", "text", "about the doc", "about", "doc:/a/b:c.md")
	r.Define("note").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "text", Role: RoleLabel},
		PropSpec{Name: "about", Parameter: Parameter{Noun: AnyNoun, Free: true}}).Store(f.notes.store())
	r.Instance("page")
	list, err := r.AddList("page/rows", []ListItem{
		{ID: "page/rows/widget", Label: "shell", Noun: "widget", Data: "w1"},
		{ID: "page/rows/lane", Label: "build", Noun: "lane", Data: "w1"},
		{ID: "page/rows/doc", Label: "readme", Noun: "doc", Data: "/a/b:c.md"},
	}, Parent("page"))
	if err != nil {
		t.Fatal(err)
	}
	f.list = list
	rel, err := r.AddRelatedDetail("page/detail", func() *Collection { return list }, Parent("page"))
	if err != nil {
		t.Fatal(err)
	}
	f.rel = rel
	if _, err := r.AddPage("page", "Page", func(r *Runtime, w, h int) Block { return Text(list.View(w, 3) + "\n" + rel.View(w, h-3)) }, Focus(list.FocusTarget())); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("page"); err != nil {
		t.Fatal(err)
	}
	f.m = NewModel(r)
	f.m.Width, f.m.Height = 60, 24
	f.frame()
	return f
}

func (f *anyFixture) frame() string {
	f.m.View()
	return ansi.Strip(f.rel.View(60, 18))
}

func (f *anyFixture) press(keys ...string) {
	for _, k := range keys {
		keytest.Press(f.m, k)
		f.m.View()
	}
}

func (f *anyFixture) has(t *testing.T, want, absent []string) {
	t.Helper()
	text := f.frame()
	for _, w := range want {
		if !strings.Contains(text, w) {
			t.Fatalf("frame lacks %q:\n%s", w, text)
		}
	}
	for _, w := range absent {
		if strings.Contains(text, w) {
			t.Fatalf("frame holds %q:\n%s", w, text)
		}
	}
}

func TestAnyReferrersListedForEveryNoun(t *testing.T) {
	t.Parallel()
	f := newAnyFixture(t)
	for _, noun := range []string{"widget", "lane", "doc"} {
		var got []Referrer
		for _, ref := range f.r.Referrers(noun) {
			if ref.Noun == "note" {
				got = append(got, ref)
			}
		}
		if len(got) != 1 || got[0].Prop != "about" || !got[0].Any || got[0].ID() != "note.about" {
			t.Errorf("Referrers(%q) notes: %+v", noun, got)
		}
		if got[0].Value(noun, "k:1") != noun+":k:1" {
			t.Errorf("Value = %q", got[0].Value(noun, "k:1"))
		}
	}
	// A noun that nothing references still lists the any-noun prop, and a
	// plain reference stays a plain one.
	if refs := f.r.Referrers("note"); len(refs) != 1 || !refs[0].Any {
		t.Errorf("Referrers(note) = %+v", refs)
	}
	f.r.Define("tag").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "widget", Parameter: Parameter{Noun: "widget"}})
	for _, ref := range f.r.Referrers("widget") {
		if ref.Noun == "tag" && ref.Any {
			t.Errorf("a plain reference is marked Any: %+v", ref)
		}
	}
	if refs := f.r.Referrers("lane"); slices.ContainsFunc(refs, func(r Referrer) bool { return r.Noun == "tag" }) {
		t.Errorf("a plain widget reference is listed for lane: %+v", refs)
	}
}

func TestAnyRelatedStackFiltersByNounAndKey(t *testing.T) {
	t.Parallel()
	f := newAnyFixture(t)
	// Selected: the widget. Same key "w1" as the lane, other noun.
	f.has(t, []string{"Notes (about) (1)", "about the widget"}, []string{"about the lane", "about the doc"})
	if f.rel.Noun() != "widget" || f.rel.Key() != "w1" {
		t.Fatalf("stack on %q %q", f.rel.Noun(), f.rel.Key())
	}
	f.list.Move(1)
	f.has(t, []string{"Notes (about) (1)", "about the lane"}, []string{"about the widget", "about the doc"})
	// A key with a colon and slashes.
	f.list.Move(1)
	f.has(t, []string{"about the doc"}, []string{"about the widget", "about the lane"})
	if f.rel.Noun() != "doc" || f.rel.Key() != "/a/b:c.md" {
		t.Fatalf("stack on %q %q", f.rel.Noun(), f.rel.Key())
	}
}

func TestAnyRelatedNewAndAssociateFillNounAndKey(t *testing.T) {
	t.Parallel()
	f := newAnyFixture(t)
	f.list.Move(2) // the doc, whose key holds a colon and slashes
	f.frame()
	if _, err := f.r.Execute(Request{Noun: RelatedNoun, Verb: "new", Args: Arguments{"section": "note.about"}}); err != nil {
		t.Fatal(err)
	}
	last := f.notes.rows[len(f.notes.rows)-1]
	if last["about"] != "doc:/a/b:c.md" {
		t.Fatalf("new note about %v, want doc:/a/b:c.md", last["about"])
	}
	// Associate an existing note with the lane.
	f.list.Move(-1)
	f.frame()
	if _, err := f.r.Execute(Request{Noun: RelatedNoun, Verb: "associate", Args: Arguments{"section": "note.about", "thing": "n1"}}); err != nil {
		t.Fatal(err)
	}
	if got := f.notes.rows[0]["about"]; got != "lane:w1" {
		t.Fatalf("n1 about %v, want lane:w1", got)
	}
	f.has(t, []string{"Notes (about) (2)", "about the widget", "about the lane"}, []string{"about the doc"})
}

func TestAnyRefChoicesNounThenThing(t *testing.T) {
	t.Parallel()
	f := newAnyFixture(t)
	p := Parameter{Type: "string", Noun: AnyNoun, Free: true}
	if !f.r.ChoicesTakeQuery(p) {
		t.Fatal("an any-noun reference does not take the typed text")
	}
	nouns, err := f.r.ChoicesFor(f.r.Context(), p, "")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, ch := range nouns {
		if !ch.Opens || !ch.NoPick {
			t.Errorf("noun row %q is not browse-only: %+v", ch.ID, ch)
		}
		ids = append(ids, ch.ID)
	}
	for _, want := range []string{"widget:", "lane:", "doc:", "note:"} {
		if !slices.Contains(ids, want) {
			t.Errorf("noun choices %v lack %q", ids, want)
		}
	}
	narrowed, _ := f.r.ChoicesFor(f.r.Context(), p, "wid")
	if len(narrowed) != 1 || narrowed[0].ID != "widget:" {
		t.Errorf("noun text narrows to %+v", narrowed)
	}
	things, err := f.r.ChoicesFor(f.r.Context(), p, "doc:")
	if err != nil {
		t.Fatal(err)
	}
	if len(things) != 1 || things[0].ID != "doc:/a/b:c.md" || things[0].Opens {
		t.Fatalf("doc choices %+v", things)
	}
	if got, _ := f.r.ChoicesFor(f.r.Context(), p, "doc:zzz"); len(got) != 0 {
		t.Errorf("text narrows nothing: %+v", got)
	}
	if got, _ := f.r.ChoicesFor(f.r.Context(), p, "nothing:"); len(got) != 0 {
		t.Errorf("an unknown noun lists %+v", got)
	}
	if got := f.r.choicesPlural(p); got != "things" {
		t.Errorf("plural %q", got)
	}
}

func TestAnyRefChooserBrowsesIntoANoun(t *testing.T) {
	t.Parallel()
	f := newAnyFixture(t)
	p := Parameter{Type: "string", Noun: AnyNoun, Free: true}
	c, err := f.r.openChoiceChooser(ChooserSpec{Kind: FormSuggestionsKind, Title: "About"}, choiceLoad{
		ctx: f.r.Context(), param: p, label: func(ch Choice) string { return ch.Label }, siblings: func() Arguments { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	src := c.current()
	if src == nil || src.Open == nil || src.Up == nil {
		t.Fatalf("the chooser source cannot browse: %+v", src)
	}
	q, ok := src.Open(ChooserRow{ID: "doc:", Opens: true})
	if !ok || q != "doc:" {
		t.Fatalf("Open = %q, %v", q, ok)
	}
	if _, ok := src.Open(ChooserRow{ID: "doc:/a", Opens: false}); ok {
		t.Fatal("a thing row opens")
	}
	if q, ok := src.Up("doc:rea"); !ok || q != "" {
		t.Fatalf("Up = %q, %v", q, ok)
	}
	if _, ok := src.Up("doc"); ok {
		t.Fatal("Up from the noun list goes somewhere")
	}
	res := src.Load("doc:")
	rows, _ := res.value.([]ChooserRow)
	if len(rows) != 1 || rows[0].ID != "doc:/a/b:c.md" {
		t.Fatalf("rows for doc: %+v (%v)", rows, res.err)
	}
	if fmt.Sprint(rows[0].Opens) != "false" {
		t.Fatal("a thing row is browse-only")
	}
}

func sectionIDs(rel *Related) []string {
	var out []string
	for _, s := range rel.order() {
		out = append(out, s.ref.ID())
	}
	return out
}

func TestRelatedDetailFollowsSelectionAcrossNouns(t *testing.T) {
	t.Parallel()
	f := newAnyFixture(t)
	tags := &relMem{}
	tags.add("id", "g1", "label", "urgent", "note", "n1")
	f.r.Define("tag").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "label", Role: RoleLabel},
		PropSpec{Name: "note", Parameter: Parameter{Noun: "note"}}).Store(tags.store())
	if err := f.list.SetItems(append(f.list.Items(), ListItem{ID: "page/rows/note", Label: "idea", Noun: "note", Data: "n1"})); err != nil {
		t.Fatal(err)
	}
	f.frame()
	if got := strings.Join(sectionIDs(f.rel), " "); got != "note.about" {
		t.Fatalf("widget sections %q", got)
	}
	f.list.Move(3)
	f.has(t, []string{"Tags (note) (1)", "urgent", "Notes (about) (0)"}, []string{"about the widget"})
	if got := strings.Join(sectionIDs(f.rel), " "); got != "note.about tag.note" {
		t.Fatalf("note sections %q", got)
	}
	if f.rel.Noun() != "note" || f.rel.Key() != "n1" {
		t.Fatalf("stack on %q %q", f.rel.Noun(), f.rel.Key())
	}
	f.list.Move(-3)
	f.has(t, []string{"about the widget"}, []string{"urgent"})
	if got := strings.Join(sectionIDs(f.rel), " "); got != "note.about" {
		t.Fatalf("back on a widget: sections %q", got)
	}
	// The old sections are gone from the runtime, not hidden.
	for id := range f.r.Instances {
		if strings.Contains(id, "page/detail/tag.note") {
			t.Fatalf("section instance %q outlived its noun", id)
		}
	}
}

func TestRelatedDetailSettingsOffAndHidden(t *testing.T) {
	t.Parallel()
	f := newAnyFixture(t)
	f.has(t, []string{"about the widget"}, nil)
	if err := f.r.Settings().Set(RelatedDetailHiddenSetting, []string{"note.about"}); err != nil {
		t.Fatal(err)
	}
	f.has(t, nil, []string{"about the widget", "Notes"})
	if err := f.r.Settings().Set(RelatedDetailHiddenSetting, []string{"note.*"}); err != nil {
		t.Fatal(err)
	}
	f.has(t, nil, []string{"Notes"})
	if err := f.r.Settings().Set(RelatedDetailHiddenSetting, []string{}); err != nil {
		t.Fatal(err)
	}
	f.has(t, []string{"about the widget"}, nil)
	if err := f.r.Settings().Set(RelatedDetailSetting, false); err != nil {
		t.Fatal(err)
	}
	if text := f.frame(); strings.TrimSpace(text) != "" || f.rel.Text(60) != "" {
		t.Fatalf("detail.related off still draws:\n%s", text)
	}
	if err := f.r.Settings().Set(RelatedDetailSetting, true); err != nil {
		t.Fatal(err)
	}
	f.has(t, []string{"about the widget"}, nil)
}

func TestRelatedDetailReadsOnSelectionNotOnDraws(t *testing.T) {
	t.Parallel()
	f := newAnyFixture(t)
	calls := 0
	links := f.r.Define("link").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "name", Role: RoleLabel},
		PropSpec{Name: "about", Parameter: Parameter{Noun: AnyNoun}})
	_ = links
	rel, err := f.r.AddRelated("page/hooked", RelatedSpec{Target: f.r.SelectionTarget(func() *Collection { return f.list }), Enabled: RelatedDetailSetting,
		Rows: func(_ Ctx, ref Referrer, value string) ([]Thing, error) {
			if ref.Noun == "link" {
				calls++
				if !strings.HasPrefix(value, "widget:") && !strings.HasPrefix(value, "lane:") && !strings.HasPrefix(value, "doc:") {
					t.Errorf("hook value %q is not noun:key", value)
				}
			}
			return nil, nil
		}}, Parent("page"))
	if err != nil {
		t.Fatal(err)
	}
	rel.Refill()
	base := calls
	if base == 0 {
		t.Fatal("the hook was never read")
	}
	for i := 0; i < 50; i++ {
		rel.View(60, 10)
		f.m.View()
	}
	if calls != base {
		t.Fatalf("50 draws read the hook %d times", calls-base)
	}
	f.list.Move(1)
	rel.View(60, 10)
	if calls != base+1 {
		t.Fatalf("a selection change read the hook %d times, want 1", calls-base)
	}
	// Off reads nothing; on again reads once.
	if err := f.r.Settings().Set(RelatedDetailSetting, false); err != nil {
		t.Fatal(err)
	}
	f.list.Move(1)
	for i := 0; i < 5; i++ {
		rel.View(60, 10)
	}
	if calls != base+1 {
		t.Fatalf("a stack that is off read the hook (%d)", calls-base-1)
	}
	if err := f.r.Settings().Set(RelatedDetailSetting, true); err != nil {
		t.Fatal(err)
	}
	rel.View(60, 10)
	if calls != base+2 {
		t.Fatalf("turning it on read the hook %d times, want 1", calls-base-1)
	}
}

func TestRelatedDetailNoSelectionShowsNothing(t *testing.T) {
	t.Parallel()
	f := newAnyFixture(t)
	if err := f.list.SetItems(nil); err != nil {
		t.Fatal(err)
	}
	if text := f.frame(); strings.TrimSpace(text) != "" {
		t.Fatalf("an empty list shows a stack:\n%s", text)
	}
	if f.rel.Noun() != "" || len(f.rel.sections) != 0 {
		t.Fatalf("stack on %q with %d sections", f.rel.Noun(), len(f.rel.sections))
	}
}

// An any-noun value is kept as written: a noun or a thing that does not
// exist is no reason to refuse it (record first, never refuse), with or
// without Free, and the CLI/MCP form "noun:key" round-trips.
func TestAnyRefValueIsKeptAsWritten(t *testing.T) {
	t.Parallel()
	for _, free := range []bool{true, false} {
		r := NewRuntime()
		mem := &relMem{}
		r.Define("note").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "about", Parameter: Parameter{Noun: AnyNoun, Free: free}}).Store(mem.store())
		for _, about := range []string{"widget:w1", "gone.noun:/a/b:c", "plain"} {
			res, err := r.Execute(Request{Noun: "note", Verb: "create", Args: Arguments{"about": about}})
			if err != nil {
				t.Fatalf("free=%v about %q: %v", free, about, err)
			}
			_ = res
			if last := mem.rows[len(mem.rows)-1]; last["about"] != about {
				t.Fatalf("free=%v about %q stored as %v", free, about, mem.rows)
			}
		}
	}
}

func TestAnyRefNounsSetting(t *testing.T) {
	t.Parallel()
	f := newAnyFixture(t)
	p := Parameter{Type: "string", Noun: AnyNoun, Free: true}
	ids := func() []string {
		chs, err := f.r.ChoicesFor(f.r.Context(), p, "")
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, ch := range chs {
			out = append(out, ch.ID)
		}
		return out
	}
	stored := ids()
	for _, want := range []string{"widget:", "lane:", "doc:", "note:"} {
		if !slices.Contains(stored, want) {
			t.Errorf("stored lacks %q: %v", want, stored)
		}
	}
	for _, id := range stored {
		if strings.HasPrefix(id, "ochazuke.") {
			t.Errorf("stored offers the framework noun %q", id)
		}
	}
	if err := f.r.Settings().Set(AnyRefNounsSetting, "pickers"); err != nil {
		t.Fatal(err)
	}
	all := ids()
	if len(all) <= len(stored) || !slices.Contains(all, "ochazuke.referrer:") || !slices.Contains(all, "widget:") {
		t.Errorf("pickers lists %d nouns (stored %d): %v", len(all), len(stored), all)
	}
}
