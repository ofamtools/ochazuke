package ochazuke

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"ofam.dev/ochazuke/keytest"
)

func showRelatedFixture(t *testing.T, configure func(*Runtime, *RelatedSpec)) (*relFixture, *ShowForm) {
	t.Helper()
	var related RelatedSpec
	f := newRelFixture(t, func(spec *RelatedSpec) {
		spec.History = true
		related = *spec
	})
	r := f.r
	r.Config().Disable()
	if err := r.RemoveInstance(f.rel.ID()); err != nil {
		t.Fatal(err)
	}
	if configure != nil {
		configure(r, &related)
	}
	left := r.List("page/left")
	if err := left.SetItems([]ListItem{
		{ID: "p1", Label: "Alpha", Noun: "project", Data: "p1"},
		{ID: "p2", Label: "Beta", Noun: "project", Data: "p2"},
	}); err != nil {
		t.Fatal(err)
	}
	sf, err := r.AddShowForm("page/show", ShowFormSpec{
		List: func() *Collection { return left }, Related: &related,
	}, Parent("page"))
	if err != nil {
		t.Fatal(err)
	}
	f.rel = sf.Related()
	r.PageOf("page").Render = func(_ *Runtime, w, h int) Block {
		return sf.Block(left.View(w, 2)+"\npage detail", w, h)
	}
	f.m.Width, f.m.Height = 90, 50
	if err := r.SetFocus(left.FocusTarget()); err != nil {
		t.Fatal(err)
	}
	sf.Sync()
	return f, sf
}

func TestShowFormRelatedStackSettingsAndWrites(t *testing.T) {
	f, sf := showRelatedFixture(t, func(r *Runtime, spec *RelatedSpec) {
		if err := r.DeclareSetting(Setting{Name: "rel.enabled", Type: SettingBoolean, Default: true}); err != nil {
			t.Fatal(err)
		}
		spec.Enabled = "rel.enabled"
		// The supplied target must not split the form from its sections.
		spec.Target = func() (string, string) { return "project", "p2" }
	})
	r := f.r
	set := func(name string, value any) {
		t.Helper()
		if _, err := r.Settings().SetSession(name, value); err != nil {
			t.Fatal(err)
		}
	}
	frame := func() string { return ansi.Strip(f.m.View().Content) }
	set("rel.hidden", []string{"note.*"})
	set("rel.empty", RelatedEmptyHide)
	set("rel.order", []string{"ochazuke.history-entry.history", "link.project", "task.project"})
	text := frame()
	for _, want := range []string{"Alpha", "Links (1)", "docs", "write"} {
		if !strings.Contains(text, want) {
			t.Fatalf("custom stack lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Notes") || strings.Contains(text, "History") || strings.Index(text, "docs") > strings.Index(text, "write") {
		t.Fatalf("hidden, empty or section order ignored:\n%s", text)
	}
	left := r.List("page/left")
	if err := left.Select("p2"); err != nil {
		t.Fatal(err)
	}
	sf.Sync()
	if sf.Form().Values()["name"] != "Beta" || sf.Related().Key() != "p2" {
		t.Fatalf("form and stack did not follow p2: %v / %s", sf.Form().Values(), sf.Related().Key())
	}
	text = frame()
	if !strings.Contains(text, "repo") || !strings.Contains(text, "ship") || strings.Contains(text, "docs") {
		t.Fatalf("hook and stored sections did not follow p2:\n%s", text)
	}
	if err := sf.Form().Focus(); err != nil {
		t.Fatal(err)
	}
	keytest.Press(f.m, "i")
	keytest.Press(f.m, "ctrl+u")
	typeText(f.m, "Gamma")
	keytest.Press(f.m, "esc")
	got, err := r.Execute(Request{Noun: "project", Verb: "get", Target: "p2"})
	if err != nil || got.(map[string]any)["name"] != "Gamma" {
		t.Fatalf("form field did not write p2: %v, %v", got, err)
	}
	text = frame()
	if !strings.Contains(text, "History (1)") || strings.Index(text, "History (1)") < strings.Index(text, "ship") {
		t.Fatalf("write history is not last:\n%s", text)
	}
	set("rel.enabled", false)
	text = frame()
	if !strings.Contains(text, "Gamma") || strings.Contains(text, "History") || strings.Contains(text, "Tasks") {
		t.Fatalf("stack off must leave the form visible:\n%s", text)
	}
	set("rel.enabled", true)
	set("rel.empty", RelatedEmptyHeader)
	if text = frame(); !strings.Contains(text, "History (1)") || !strings.Contains(text, "repo") {
		t.Fatalf("stack did not return:\n%s", text)
	}
	set("rel.hidden", []string{"note.*", "ochazuke.*"})
	if text = frame(); !strings.Contains(text, "repo") || strings.Contains(text, "History") {
		t.Fatalf("history must honor the app's hidden patterns:\n%s", text)
	}
	set("rel.hidden", []string{"note.*"})
	set(DetailShowFormSetting, false)
	sf.Sync()
	text = frame()
	if sf.Form() != nil || !strings.Contains(text, "page detail") || strings.Contains(text, "History") {
		t.Fatalf("show form off must leave only page text:\n%s", text)
	}
	set(DetailShowFormSetting, true)
	sf.Sync()
	if sf.Form().Values()["name"] != "Gamma" || !strings.Contains(frame(), "History (1)") {
		t.Fatal("show form did not return with the stored value and history")
	}
}

func TestShowFormRelatedPlacedView(t *testing.T) {
	var scoped string
	var tree *Tree
	f, sf := showRelatedFixture(t, func(r *Runtime, spec *RelatedSpec) {
		var err error
		tree, err = r.AddTree("page/custom", nil, Parent("page"))
		if err != nil {
			t.Fatal(err)
		}
		r.Define("task").RelatedView(tree.ID())
		if err := r.DeclareSetting(Setting{Name: "rel.enabled", Type: SettingBoolean, Default: true}); err != nil {
			t.Fatal(err)
		}
		spec.Enabled = "rel.enabled"
		spec.Scope = func(c Ctx, ref Referrer, view, key string) error {
			scoped = view + "@" + key
			answer, err := c.Runtime.Execute(Request{Noun: ref.Noun, Verb: "list"})
			if err != nil {
				return err
			}
			var nodes []TreeNode
			for _, row := range AnswerRows(answer).([]map[string]any) {
				if row[ref.Prop] == key {
					nodes = append(nodes, TreeNode{ID: fmt.Sprint(row["id"]), Label: "custom " + fmt.Sprint(row["title"]), Noun: ref.Noun, Data: Thing(row)})
				}
			}
			return tree.SetNodes(nodes)
		}
	})
	text := ansi.Strip(f.m.View().Content)
	if scoped != "page/custom@p1" || !strings.Contains(text, "custom write") || !strings.Contains(text, "custom test") {
		t.Fatalf("placed renderer was not scoped: %s\n%s", scoped, text)
	}
	if err := f.r.List("page/left").Select("p2"); err != nil {
		t.Fatal(err)
	}
	sf.Sync()
	text = ansi.Strip(f.m.View().Content)
	if scoped != "page/custom@p2" || !strings.Contains(text, "custom ship") || strings.Contains(text, "custom write") {
		t.Fatalf("placed renderer did not follow target: %s\n%s", scoped, text)
	}
	if err := f.r.SetFocus(tree.FocusTarget()); err != nil {
		t.Fatal(err)
	}
	left := f.r.List("page/left")
	for _, enabled := range []bool{false, true} {
		if _, err := f.r.Settings().SetSession("rel.enabled", enabled); err != nil {
			t.Fatal(err)
		}
		text = ansi.Strip(f.m.View().Content)
		if f.r.Instances[f.r.Focus] == nil || !enabled && strings.Contains(text, "custom ship") {
			t.Fatalf("disabled view must be hidden with a valid focus: %s\n%s", f.r.Focus, text)
		}
	}
	if f.r.Tree("page/custom") != tree || f.r.Focus != left.FocusTarget() || !strings.Contains(text, "custom ship") {
		t.Fatalf("stack toggle lost the borrowed view or visible-list fallback: %s\n%s", f.r.Focus, text)
	}
	if err := tree.Focus(); err != nil {
		t.Fatal(err)
	}
	if f.r.Focus != tree.FocusTarget() {
		t.Fatal("reenabled custom view cannot be focused again")
	}
	if err := left.SetItems([]ListItem{{ID: "t1", Label: "write", Noun: "task", Data: "t1"}}); err != nil {
		t.Fatal(err)
	}
	sf.Sync()
	_ = f.m.View()
	if f.r.Tree("page/custom") != tree || f.r.Instances[tree.ID()].Parent != "page" {
		t.Fatal("changing target noun destroyed the borrowed view or failed to restore its owner")
	}
	if err := left.SetItems([]ListItem{{ID: "p2", Label: "Beta", Noun: "project", Data: "p2"}}); err != nil {
		t.Fatal(err)
	}
	sf.Sync()
	text = ansi.Strip(f.m.View().Content)
	if !strings.Contains(text, "custom ship") {
		t.Fatalf("returning to the noun lost its borrowed view:\n%s", text)
	}
}

func TestShowFormExplicitTargetAndSectionChoices(t *testing.T) {
	f, _ := showRelatedFixture(t, nil)
	r := f.r
	noun, key, revision := "task", "t1", "first"
	related := RelatedSpec{Target: func() (string, string) { return "project", "p2" }, History: true}
	sf, err := r.AddShowForm("page/explicit", ShowFormSpec{
		List:   func() *Collection { return r.List("page/left") },
		Target: func() (string, string) { return noun, key }, Data: func() string { return revision }, Related: &related,
	}, Parent("page"))
	if err != nil {
		t.Fatal(err)
	}
	sf.Sync()
	if sf.Form().Values()["title"] != "write" || sf.Related().Noun() != "task" || sf.Related().Key() != "t1" {
		t.Fatalf("explicit target lost to List: %v / %s:%s", sf.Form().Values(), sf.Related().Noun(), sf.Related().Key())
	}
	if n, k := related.Target(); n != "project" || k != "p2" {
		t.Fatalf("caller spec was mutated: %s:%s", n, k)
	}
	f.tasks.rows[0]["title"] = "updated outside"
	revision = "second"
	sf.Sync()
	if sf.Form().Values()["title"] != "updated outside" {
		t.Fatalf("explicit Data did not reload the form: %v", sf.Form().Values())
	}
	noun, key = "project", "p2"
	sf.Sync()
	if sf.Form().Values()["name"] != "Beta" || sf.Related().Noun() != "project" || sf.Related().Key() != "p2" {
		t.Fatal("form and sections did not follow the changed noun")
	}
	for _, tc := range []struct {
		id          string
		related     *RelatedSpec
		noSections  bool
		wantStack   bool
		wantHistory bool
	}{
		{id: "native", wantStack: true, wantHistory: true},
		{id: "custom-no-history", related: &RelatedSpec{}, wantStack: true},
		{id: "no-sections", related: &related, noSections: true},
	} {
		t.Run(tc.id, func(t *testing.T) {
			form, err := r.AddShowForm("page/"+tc.id, ShowFormSpec{
				Target: func() (string, string) { return "project", "p1" }, Related: tc.related, NoSections: tc.noSections,
			}, Parent("page"))
			if err != nil {
				t.Fatal(err)
			}
			form.Sync()
			text := ansi.Strip(form.Block("", 90, 50).Text)
			if !strings.Contains(text, "Alpha") || strings.Contains(text, "History") != tc.wantHistory || strings.Contains(text, "Tasks") != tc.wantStack {
				t.Fatalf("section choice not respected:\n%s", text)
			}
		})
	}
}
