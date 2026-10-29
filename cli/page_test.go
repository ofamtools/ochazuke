package cli

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
	"github.com/unixsuperhero/ochazuke/internal/keytest"
)

// pageDoc is an extension's catalog: skills (with columns) listed by
// skill.list, tools by mcp.tools (rows of mcp-tool), and an admin noun
// with no row source, whose verbs become its rows.
func pageDoc(layout string, detail bool) CatalogDoc {
	doc := CatalogDoc{
		Schema: Schema, Name: "ai", Version: "0.1.0",
		Nouns: []CatalogNoun{
			{Name: "admin", Verbs: []CatalogVerb{{Name: "sync", Description: "Sync now"}, {Name: "fold", Description: "A framework verb", Builtin: true}, {Name: "purge", Params: map[string]ochazuke.Parameter{"older": {Type: "string", Required: true}}}}},
			{Name: "mcp", Verbs: []CatalogVerb{{Name: "tools", Rows: "mcp-tool"}}},
			{Name: "mcp-tool", Verbs: []CatalogVerb{{Name: "show"}}},
			{Name: "skill", Columns: []ochazuke.Column{{Title: "Name", Min: 8}, {Title: "Origin"}}, Verbs: []CatalogVerb{{Name: "list", Rows: "skill"}, {Name: "show"}}},
		},
	}
	p := CatalogPage{ID: "ai", Title: "AI", Layout: layout, Lists: []CatalogList{
		{ID: "ai/skills", Noun: "skill"},
		{ID: "ai/tools", Noun: "mcp", Source: "mcp.tools", Title: "tools"},
		{ID: "ai/admin", Noun: "admin"},
	}}
	if detail {
		p.Detail = &CatalogDetail{Title: "record", Fields: []string{"path", "origin"}}
	}
	doc.Pages = []CatalogPage{p}
	return doc
}

// answers is a Fetch over canned stdout by noun.verb.
func answers(out map[string]string) func(context.Context, string, string) ([]byte, error) {
	return func(_ context.Context, noun, verb string) ([]byte, error) {
		s, ok := out[noun+"."+verb]
		if !ok {
			return nil, errors.New("exit status 1: no such verb")
		}
		return []byte(s), nil
	}
}

var pageAnswers = map[string]string{
	"skill.list": `[{"id":"s1","name":"architect","cells":["architect","claude"],"path":"~/.claude/skills/architect","origin":"claude"},{"id":"s2","name":"review","cells":["review","ai-tools"],"path":"~/proj/ai-tools/review","origin":"ai-tools"}]`,
	"mcp.tools":  "{\"noun\":\"mcp\",\"id\":\"t1\",\"label\":\"tuios_send_text\"}\nnot json\n{\"noun\":\"mcp\",\"id\":\"t2\",\"label\":\"tuios_capture\"}\n",
}

func mountedPage(t testing.TB, doc CatalogDoc, cfg PageConfig, w, h int) (*ochazuke.Model, *PageView) {
	t.Helper()
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	m := ochazuke.NewModel(r)
	m.Width, m.Height = w, h
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

func frameOf(m *ochazuke.Model) string { return ansi.Strip(m.View().Content) }

func press(m *ochazuke.Model, keys ...string) {
	for _, k := range keys {
		m.Update(keytest.Msg(k, false, false))
	}
}

func TestMountPageLoadsListsThroughFetch(t *testing.T) {
	m, v := mountedPage(t, pageDoc("", true), PageConfig{Fetch: answers(pageAnswers)}, 100, 30)
	if err := v.Reload(); err != nil {
		t.Fatal(err)
	}
	if f := frameOf(m); !strings.Contains(f, "loading…") {
		t.Fatalf("before the loads land the lists say loading:\n%s", f)
	}
	if err := Settle(m.Runtime, nil); err != nil {
		t.Fatal(err)
	}
	f := frameOf(m)
	for _, want := range []string{"skill (2)", "Name", "Origin", "architect", "ai-tools", "tools (2)", "tuios_send_text", "admin (2)", "sync  Sync now", "purge (palette: older)", "record · architect", "path    ~/.claude/skills/architect", "origin  claude"} {
		if !strings.Contains(f, want) {
			t.Errorf("frame lacks %q:\n%s", want, f)
		}
	}
	if strings.Contains(f, "catalog-load") {
		t.Errorf("loads must stay off the jobs ticker:\n%s", f)
	}
	for _, it := range m.Runtime.List("ai/admin").Items() {
		if it.ID == "ai/admin/fold" {
			t.Errorf("the builtin admin.fold is listed as the tool's: %+v", it)
		}
	}
	inst := m.Runtime.Instances["ai/skills/s1"]
	if inst == nil || inst.Noun != "skill" || inst.Data != "s1" {
		t.Fatalf("row instance = %+v, want noun skill, data s1 (the --target)", inst)
	}
	if inst := m.Runtime.Instances["ai/tools/t2"]; inst == nil || inst.Noun != "mcp-tool" {
		t.Fatalf("a source's rows answer to its rows noun, got %+v", inst)
	}
	// j moves the selection, and the detail follows it.
	press(m, "j")
	if f := frameOf(m); !strings.Contains(f, "record · review") || !strings.Contains(f, "~/proj/ai-tools/review") {
		t.Fatalf("detail does not follow the selection:\n%s", f)
	}
	// Spill past the last skill into tools: the detail follows the focused list.
	press(m, "j")
	if got := m.Runtime.Focus; got != "ai/tools/t1" {
		t.Fatalf("focus = %s, want ai/tools/t1", got)
	}
	if f := frameOf(m); !strings.Contains(f, "id: t1") {
		t.Fatalf("a row without a record shows its id and label:\n%s", f)
	}
}

func TestMountPageHostNounsAndVerbRows(t *testing.T) {
	ran := 0
	doc := pageDoc(LayoutStack, false)
	local := func(n string) string { return "ai-" + n }
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	r.Define("ai-admin").Verb("sync", func(ochazuke.Ctx) ochazuke.Result { ran++; return ochazuke.Done(nil) })
	m := ochazuke.NewModel(r)
	m.Width, m.Height = 90, 30
	m.Appearance.Theme = "mono"
	v, err := MountPage(m, doc, doc.Pages[0], PageConfig{Noun: local})
	if err != nil {
		t.Fatal(err)
	}
	if err := v.SetRows("ai/skills", []Row{{ID: "s1", Label: "architect"}, {ID: "s1"}, {ID: ""}}); err != nil {
		t.Fatal(err)
	}
	if got := v.List("ai/skills").Len(); got != 1 {
		t.Fatalf("an empty or repeated id is dropped, got %d rows", got)
	}
	if inst := r.Instances["ai/skills/s1"]; inst.Noun != "ai-skill" {
		t.Fatalf("row noun = %s, want the host's ai-skill", inst.Noun)
	}
	if err := r.OpenPage("ai"); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFocus("ai/admin/sync"); err != nil {
		t.Fatal(err)
	}
	press(m, "enter")
	if ran != 1 {
		t.Fatalf("enter on a verb row runs the host's verb, ran %d", ran)
	}
	if err := v.SetRows("ai/admin", nil); err == nil {
		t.Fatal("a verb-row list takes no rows")
	}
}

func TestMountPageStateFilesRowsByNoun(t *testing.T) {
	doc := pageDoc(LayoutSplit, true)
	doc.State = "skill.state"
	doc.Nouns[3].Verbs = append(doc.Nouns[3].Verbs, CatalogVerb{Name: "state"})
	state := "{\"noun\":\"skill\",\"id\":\"s9\",\"label\":\"late\",\"data\":{\"path\":\"/x\"}}\n{\"noun\":\"mcp\",\"id\":\"t1\",\"label\":\"tool one\"}\n"
	m, v := mountedPage(t, doc, PageConfig{Fetch: answers(map[string]string{"skill.state": state})}, 100, 24)
	if err := v.Reload(); err != nil {
		t.Fatal(err)
	}
	if err := Settle(m.Runtime, nil); err != nil {
		t.Fatal(err)
	}
	f := frameOf(m)
	for _, want := range []string{"skill (1)", "late", "tools (1)", "tool one", "record · late", "path  /x"} {
		if !strings.Contains(f, want) {
			t.Errorf("frame lacks %q:\n%s", want, f)
		}
	}
	// Split: the lists on the left, the detail on the right of the same rows.
	for _, line := range strings.Split(f, "\n") {
		if strings.Contains(line, "late") && strings.Contains(line, "path") {
			t.Fatalf("split keeps lists and detail in their own columns, got %q", line)
		}
	}
	lines := strings.Split(f, "\n")
	found := false
	for _, line := range lines {
		if strings.Contains(line, "skill (1)") && strings.Contains(line, "record · late") {
			found = true
		}
	}
	if !found {
		t.Fatalf("split puts the detail beside the lists:\n%s", f)
	}
}

func TestMountPageFailedLoadKeepsRows(t *testing.T) {
	m, v := mountedPage(t, pageDoc("", false), PageConfig{Fetch: answers(pageAnswers)}, 90, 30)
	if err := v.Reload(); err != nil {
		t.Fatal(err)
	}
	if err := Settle(m.Runtime, nil); err != nil {
		t.Fatal(err)
	}
	v.cfg.Fetch = answers(map[string]string{})
	if err := v.Reload(); err != nil {
		t.Fatal(err)
	}
	if err := Settle(m.Runtime, nil); err == nil {
		t.Fatal("settling failed loads reports them")
	}
	f := frameOf(m)
	if !strings.Contains(f, "architect") || !strings.Contains(f, "skill list: exit status 1: no such verb") {
		t.Fatalf("a failed reload keeps the rows and says why:\n%s", f)
	}
}

func TestPageConfigOnRowsFollowsEveryLanding(t *testing.T) {
	type landing struct {
		list string
		ids  []string
	}
	var got []landing
	onRows := func(list string, rows []Row) {
		var ids []string
		for _, row := range rows {
			ids = append(ids, row.ID+"="+row.Label)
		}
		got = append(got, landing{list, ids})
	}
	m, v := mountedPage(t, pageDoc("", false), PageConfig{Fetch: answers(pageAnswers), OnRows: onRows}, 90, 30)
	if err := v.Reload(); err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("OnRows runs when rows land, not when a load starts: %v", got)
	}
	if err := Settle(m.Runtime, nil); err != nil {
		t.Fatal(err)
	}
	byList := map[string][]string{}
	for _, l := range got {
		byList[l.list] = l.ids
	}
	if len(got) != 2 || strings.Join(byList["ai/skills"], ",") != "s1=architect,s2=review" || strings.Join(byList["ai/tools"], ",") != "t1=tuios_send_text,t2=tuios_capture" {
		t.Fatalf("one call per list with its rows as filed, got %v", got)
	}
	// A host push lands too, as filed: a repeated id dropped, a missing label the id.
	got = nil
	if err := v.SetRows("ai/skills", []Row{{ID: "s3"}, {ID: "s3", Label: "again"}}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].list != "ai/skills" || strings.Join(got[0].ids, ",") != "s3=s3" {
		t.Fatalf("SetRows calls OnRows with the filed rows, got %v", got)
	}
	// A failed reload keeps the rows and calls nothing.
	got = nil
	v.cfg.Fetch = answers(map[string]string{})
	if err := v.Reload(); err != nil {
		t.Fatal(err)
	}
	_ = Settle(m.Runtime, nil)
	if len(got) != 0 {
		t.Fatalf("a failed load lands no rows, got %v", got)
	}
}

func TestPageConfigOnRowsFollowsAStateAnswer(t *testing.T) {
	doc := pageDoc(LayoutStack, false)
	doc.State = "skill.state"
	doc.Nouns[3].Verbs = append(doc.Nouns[3].Verbs, CatalogVerb{Name: "state"})
	got := map[string]int{}
	cfg := PageConfig{
		Fetch:  answers(map[string]string{"skill.state": "{\"noun\":\"skill\",\"id\":\"s9\"}\n"}),
		OnRows: func(list string, rows []Row) { got[list] = len(rows) + 1 },
	}
	m, v := mountedPage(t, doc, cfg, 90, 24)
	if err := v.Reload(); err != nil {
		t.Fatal(err)
	}
	if err := Settle(m.Runtime, nil); err != nil {
		t.Fatal(err)
	}
	// skills got one row, tools none (emptied), admin lists verbs and takes no rows.
	if got["ai/skills"] != 2 || got["ai/tools"] != 1 || len(got) != 2 {
		t.Fatalf("a state answer lands on every row list, an empty one included, got %v", got)
	}
}

func TestMountPageGroupsLayout(t *testing.T) {
	m, v := mountedPage(t, pageDoc(LayoutGroups, false), PageConfig{Fetch: answers(pageAnswers)}, 100, 20)
	if err := v.Reload(); err != nil {
		t.Fatal(err)
	}
	if err := Settle(m.Runtime, nil); err != nil {
		t.Fatal(err)
	}
	r := m.Runtime
	if r.Focus != "ai/groups/ai/skills" {
		t.Fatalf("a groups page starts on the rail, focus %s", r.Focus)
	}
	f := frameOf(m)
	if !strings.Contains(f, "Groups") || !strings.Contains(f, "architect") || strings.Contains(f, "tuios_send_text") {
		t.Fatalf("only the selected group's list is drawn:\n%s", f)
	}
	press(m, "j")
	if f := frameOf(m); !strings.Contains(f, "tuios_send_text") || strings.Contains(f, "architect") {
		t.Fatalf("j on the rail shows the next group's list:\n%s", f)
	}
	press(m, "l")
	if r.Focus != "ai/tools/t1" {
		t.Fatalf("l moves into the group's list, focus %s", r.Focus)
	}
	press(m, "j", "j", "j")
	if r.Focus != "ai/tools/t2" {
		t.Fatalf("a group's list keeps the focus at its edge, focus %s", r.Focus)
	}
	press(m, "h")
	if r.Focus != "ai/groups/ai/tools" {
		t.Fatalf("h moves back to the group, focus %s", r.Focus)
	}
	press(m, "}")
	if r.Focus != "ai/tools/t2" {
		t.Fatalf("} reaches only the shown list, focus %s", r.Focus)
	}
}

func TestDeclarePagePublishesAndMountsTheSamePage(t *testing.T) {
	ext := ochazuke.NewRuntime()
	ext.Config().Disable()
	ext.Define("skill").Columns(ochazuke.Column{Title: "Name"}).Verb("list", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) }).Rows("skill")
	want := CatalogPage{ID: "ai", Title: "AI", Layout: LayoutSplit, Lists: []CatalogList{{ID: "ai/skills", Noun: "skill"}}, Detail: &CatalogDetail{Fields: []string{"path"}}}
	if err := DeclarePage(ext, want); err != nil {
		t.Fatal(err)
	}
	if _, err := ext.AddPage("code", "Code", func(*ochazuke.Runtime, int, int) ochazuke.Block { return ochazuke.Block{} }); err != nil {
		t.Fatal(err)
	}
	data, err := Catalog(ext, "ai", "0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	var doc CatalogDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Pages) != 2 || doc.Pages[1].IsData() || !doc.Pages[0].IsData() {
		t.Fatalf("pages = %+v, want the data page then the Go page", doc.Pages)
	}
	got, _ := json.Marshal(doc.Pages[0])
	exp, _ := json.Marshal(want)
	if string(got) != string(exp) {
		t.Fatalf("published page = %s, want %s", got, exp)
	}
	if !strings.Contains(string(data), `"layout": "split"`) {
		t.Fatalf("catalog lacks the layout:\n%s", data)
	}
	// A host mounts every data page and skips the Go-drawn one.
	host := ochazuke.NewRuntime()
	host.Config().Disable()
	views, err := MountPages(ochazuke.NewModel(host), doc, PageConfig{})
	if err != nil || len(views) != 1 || host.PageOf("code") != nil {
		t.Fatalf("MountPages = %d views, %v; code page %v", len(views), err, host.PageOf("code"))
	}
	if cols := views[0].List("ai/skills").Columns(); len(cols) != 1 || cols[0].Title != "Name" {
		t.Fatalf("the list draws its noun's columns, got %+v", cols)
	}
	// The extension mounts its own declared page over itself.
	if _, err := MountPage(ochazuke.NewModel(ext), doc, doc.Pages[0], PageConfig{}); err != nil {
		t.Fatalf("mounting a declared page: %v", err)
	}
	if ext.PageOf("ai").Focus != "ai/skills" {
		t.Fatalf("the declared page now starts on its list, focus %s", ext.PageOf("ai").Focus)
	}
}

// A catalog page declared hidden by default is published so and mounted
// out of the host's tabs, through the host's pages.hidden default.
func TestCatalogPageDefaultHidden(t *testing.T) {
	ext := ochazuke.NewRuntime()
	ext.Config().Disable()
	ext.Define("skill").Columns(ochazuke.Column{Title: "Name"}).Verb("list", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) }).Rows("skill")
	if err := DeclarePage(ext, CatalogPage{ID: "ai", Title: "AI", DefaultHidden: true, Lists: []CatalogList{{ID: "ai/skills", Noun: "skill"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := ext.AddPage("code", "Code", func(*ochazuke.Runtime, int, int) ochazuke.Block { return ochazuke.Block{} }, ochazuke.DefaultHidden()); err != nil {
		t.Fatal(err)
	}
	data, err := Catalog(ext, "ai", "0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	var doc CatalogDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Pages) != 2 || !doc.Pages[0].DefaultHidden || !doc.Pages[1].DefaultHidden {
		t.Fatalf("pages = %+v, want both declared hidden by default", doc.Pages)
	}
	host := ochazuke.NewRuntime()
	host.Config().Disable()
	if _, err := host.AddPage("home", "Home", func(*ochazuke.Runtime, int, int) ochazuke.Block { return ochazuke.Block{} }); err != nil {
		t.Fatal(err)
	}
	if _, err := MountPages(ochazuke.NewModel(host), doc, PageConfig{}); err != nil {
		t.Fatal(err)
	}
	if !host.PageHidden("ai") || host.PageHidden("home") {
		t.Fatalf("host hidden pages %v, want [ai]", host.HiddenPages())
	}
	if err := host.ShowPage("ai"); err != nil || host.PageHidden("ai") {
		t.Fatalf("page.show ai: %v, hidden %v", err, host.HiddenPages())
	}
}

func TestPageRefusals(t *testing.T) {
	r := ochazuke.NewRuntime()
	for _, p := range []CatalogPage{
		{ID: "", Title: "X", Lists: []CatalogList{{ID: "l", Noun: "n"}}},
		{ID: "x", Title: "X", Layout: "grid", Lists: []CatalogList{{ID: "l", Noun: "n"}}},
		{ID: "x", Title: "X"},
		{ID: "x", Title: "X", Lists: []CatalogList{{ID: "l", Noun: "n"}, {ID: "l", Noun: "n"}}},
		{ID: "x", Title: "X", Lists: []CatalogList{{ID: "l", Noun: "n", Source: "list"}}},
	} {
		if err := DeclarePage(r, p); err == nil {
			t.Errorf("DeclarePage(%+v) accepted", p)
		}
	}
	m := ochazuke.NewModel(ochazuke.NewRuntime())
	doc := pageDoc("", false)
	if _, err := MountPage(m, doc, CatalogPage{ID: "x", Title: "X", Lists: []CatalogList{{ID: "l", Noun: "ghost"}}}, PageConfig{}); err == nil {
		t.Error("a list of a noun not in the catalog was mounted")
	}
	if _, err := MountPage(m, doc, CatalogPage{ID: "y", Title: "Y", Lists: []CatalogList{{ID: "l", Noun: "skill", Source: "skill.ghost"}}}, PageConfig{}); err == nil {
		t.Error("a list whose source is not in the catalog was mounted")
	}
}

func TestDecodeRows(t *testing.T) {
	rows, err := DecodeRows("skill", []byte(`[{"ID":"a","Title":"Alpha"},{"name":"beta"},"gamma",{"id":"d","data":{"k":1},"cells":["d",2]}]`))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(rows)
	want := `[{"noun":"skill","id":"a","label":"Alpha","data":{"ID":"a","Title":"Alpha"}},{"noun":"skill","id":"beta","label":"beta","data":{"name":"beta"}},{"noun":"skill","id":"gamma","label":"gamma"},{"noun":"skill","id":"d","label":"d","data":{"k":1},"cells":["d","2"]}]`
	if string(got) != want {
		t.Fatalf("DecodeRows =\n%s\nwant\n%s", got, want)
	}
	if rows, err := DecodeRows("x", []byte("  \n")); err != nil || rows != nil {
		t.Fatalf("empty answer = %v, %v", rows, err)
	}
	if _, err := DecodeRows("x", []byte("nope\nnope")); !errors.Is(err, ErrNoRows) {
		t.Fatalf("all-malformed lines = %v, want ErrNoRows", err)
	}
}

func widgetDoc() CatalogDoc {
	doc := pageDoc(LayoutStack, false)
	doc.Pages[0].Lists = doc.Pages[0].Lists[:1]
	doc.Pages[0].Widgets = []CatalogWidget{
		{ID: "usage", Kind: WidgetKV, Title: "usage", Source: "usage.show", Fields: []string{"skills", "agents", "absent"}},
		{ID: "sync", Kind: WidgetStatus, Title: "sync", Source: "sync.status"},
		{ID: "recent", Kind: WidgetTable, Title: "recent", Source: "skill.recent", Rows: 2,
			Columns: []ochazuke.Column{{Title: "Name", Min: 8}, {Title: "When"}}, Fields: []string{"name", "at"}},
		{ID: "pushed", Kind: WidgetStatus, Title: "pushed"},
		{ID: "broken", Kind: WidgetKV, Title: "broken", Source: "ghost.show"},
	}
	return doc
}

func TestWidgetsDrawFromTheirSources(t *testing.T) {
	out := map[string]string{
		"skill.list":   pageAnswers["skill.list"],
		"usage.show":   `{"agents": 3, "skills": 12, "extra": true}`,
		"sync.status":  `{"text": "copying", "done": 5, "total": 10, "state": "running"}`,
		"skill.recent": `[{"name": "architect", "at": "1m"}, {"name": "review", "at": "2m"}, {"name": "old", "at": "1d"}]`,
	}
	m, v := mountedPage(t, widgetDoc(), PageConfig{Fetch: answers(out)}, 100, 40)
	if f := frameOf(m); !strings.Contains(f, "pushed  no data") {
		t.Fatalf("a widget without data says so:\n%s", f)
	}
	if err := v.Reload(); err != nil {
		t.Fatal(err)
	}
	if f := frameOf(m); !strings.Contains(f, "sync  loading…") {
		t.Fatalf("a loading widget says so:\n%s", f)
	}
	_ = Settle(m.Runtime, nil) // ghost.show fails
	if err := v.SetWidget("pushed", "all good"); err != nil {
		t.Fatal(err)
	}
	f := frameOf(m)
	for _, want := range []string{
		"usage", "skills  12", "agents  3",
		"sync  copying  [██████████░░░░░░░░░░] 50%  running",
		"recent", "Name", "When", "architect  1m", "review", "… 1 more",
		"pushed  all good",
		"ghost show: exit status 1: no such verb",
		"skill (2)",
	} {
		if !strings.Contains(f, want) {
			t.Errorf("frame lacks %q:\n%s", want, f)
		}
	}
	for _, not := range []string{"extra", "absent", "old"} {
		if strings.Contains(f, not) {
			t.Errorf("frame has %q:\n%s", not, f)
		}
	}
	if err := v.SetWidget("ghost", 1); err == nil {
		t.Error("SetWidget accepted an unknown widget")
	}
	if got := statusText(map[string]any{"progress": 45.0}, 60); got != "[█████████░░░░░░░░░░░] 45%" {
		t.Errorf("a percentage progress = %q", got)
	}
}

func TestWidgetsArePublishedAndChecked(t *testing.T) {
	ext := ochazuke.NewRuntime()
	ext.Config().Disable()
	p := widgetDoc().Pages[0]
	if err := DeclarePage(ext, p); err != nil {
		t.Fatal(err)
	}
	data, err := Catalog(ext, "ai", "0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	var doc CatalogDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if got := doc.Pages[0].Widgets; len(got) != 5 || got[2].Kind != WidgetTable || got[2].Columns[1].Title != "When" {
		t.Fatalf("published widgets = %+v", got)
	}
	for _, w := range []CatalogWidget{{Kind: WidgetKV}, {ID: "x", Kind: "chart"}, {ID: "x", Kind: WidgetTable}, {ID: "x", Kind: WidgetKV, Source: "show"}} {
		bad := p
		bad.ID, bad.Title = "bad", "Bad"
		bad.Widgets = []CatalogWidget{w}
		if err := DeclarePage(ext, bad); err == nil {
			t.Errorf("widget %+v accepted", w)
		}
	}
	twice := p
	twice.ID, twice.Title = "twice", "Twice"
	twice.Widgets = []CatalogWidget{{ID: "a", Kind: WidgetKV}, {ID: "a", Kind: WidgetStatus}}
	if err := DeclarePage(ext, twice); err == nil {
		t.Error("a widget id declared twice was accepted")
	}
}

// TestMountPageSplitFollowsThePaneKeys: > on a split catalog page moves
// the lists/detail divider right, < back, side by side and stacked; a
// page without a detail draws no split and > does nothing there.
func TestMountPageSplitFollowsThePaneKeys(t *testing.T) {
	// divider is the cell (side by side) or line (stacked) of the detail
	// panel's top-left corner.
	divider := func(f string, stacked bool) int {
		for i, line := range strings.Split(f, "\n") {
			if at := strings.Index(line, " record"); at >= 0 {
				if stacked {
					return i
				}
				return len([]rune(line[:strings.LastIndex(line[:at], "╭")]))
			}
		}
		t.Fatalf("no detail panel:\n%s", f)
		return 0
	}
	for _, tc := range []struct {
		layout  string
		width   int
		stacked bool
	}{{LayoutSplit, 100, false}, {"", 100, true}, {LayoutSplit, 60, true}} {
		m, v := mountedPage(t, pageDoc(tc.layout, true), PageConfig{Fetch: answers(pageAnswers)}, tc.width, 30)
		if err := v.Reload(); err != nil {
			t.Fatal(err)
		}
		if err := Settle(m.Runtime, nil); err != nil {
			t.Fatal(err)
		}
		before := divider(frameOf(m), tc.stacked)
		press(m, ">")
		grown := divider(frameOf(m), tc.stacked)
		press(m, "<", "<")
		shrunk := divider(frameOf(m), tc.stacked)
		if !(shrunk < before && before < grown) {
			t.Fatalf("%q at %d: divider shrunk %d, default %d, grown %d", tc.layout, tc.width, shrunk, before, grown)
		}
		if !strings.Contains(frameOf(m), ">/< resize") {
			t.Fatalf("%q at %d: no resize hint:\n%s", tc.layout, tc.width, frameOf(m))
		}
	}
	m, _ := mountedPage(t, pageDoc(LayoutSplit, false), PageConfig{Fetch: answers(pageAnswers)}, 100, 30)
	press(m, ">")
	if shift := m.Runtime.PaneShift("ai"); shift != 0 || strings.Contains(frameOf(m), "resize") {
		t.Fatalf("no detail, no split: shift %d\n%s", shift, frameOf(m))
	}
}
