package ochazuke

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// pageHideFixture is a model over three pages, alpha open.
func pageHideFixture(t *testing.T) (*Runtime, *Model) {
	t.Helper()
	r := NewRuntime()
	for _, id := range []string{"alpha", "beta", "gamma"} {
		pageTestRegister(t, r, id)
	}
	if err := r.OpenPage("alpha"); err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	m.Width, m.Height = 80, 20
	m.View()
	return r, m
}

// tabRow is the frame's tab row (the line under the title), plain.
func tabRow(m *Model) string {
	lines := strings.Split(ansi.Strip(m.View().Content), "\n")
	if len(lines) < 2 {
		return ""
	}
	return lines[1]
}

func tabIDs(r *Runtime) []string {
	var out []string
	for _, page := range r.TabPages() {
		out = append(out, page.ID)
	}
	return out
}

// A hidden page leaves the tabs and ctrl+j/k; page.open still opens it and
// says it is hidden; page.show brings it back in its place.
func TestPageHideLeavesTabsAndCycling(t *testing.T) {
	r, m := pageHideFixture(t)
	paletteRequest(t, r, "hide", "page", Arguments{"id": "beta"})
	if got := tabIDs(r); !reflect.DeepEqual(got, []string{"alpha", "gamma"}) {
		t.Fatalf("tabs %v", got)
	}
	if !strings.Contains(m.Info, "BETA is hidden") {
		t.Fatalf("info %q", m.Info)
	}
	if row := tabRow(m); strings.Contains(row, "BETA") || !strings.Contains(row, "GAMMA") {
		t.Fatalf("tab row %q", row)
	}
	pressKeys(m, "ctrl+j")
	if r.Page != "gamma" {
		t.Fatalf("ctrl+j opened %q, want gamma (beta skipped)", r.Page)
	}
	pressKeys(m, "ctrl+k")
	if r.Page != "alpha" {
		t.Fatalf("ctrl+k opened %q, want alpha", r.Page)
	}
	rows := paletteRequest(t, r, "list", "pages", nil).([]map[string]any)
	if rows[1]["id"] != "beta" || rows[1]["hidden"] != true || rows[0]["hidden"] != false {
		t.Fatalf("pages.list %v", rows)
	}
	paletteRequest(t, r, "open", "page", Arguments{"id": "beta"})
	if r.Page != "beta" || !strings.Contains(m.Info, "BETA is hidden from the tabs") {
		t.Fatalf("page.open: page %q info %q", r.Page, m.Info)
	}
	// While current, its tab shows; cycling leaves it in its place.
	if got := tabIDs(r); !reflect.DeepEqual(got, []string{"alpha", "beta", "gamma"}) {
		t.Fatalf("tabs on the hidden page %v", got)
	}
	pressKeys(m, "ctrl+j")
	if r.Page != "gamma" || !reflect.DeepEqual(tabIDs(r), []string{"alpha", "gamma"}) {
		t.Fatalf("after leaving: page %q tabs %v", r.Page, tabIDs(r))
	}
	paletteRequest(t, r, "show", "page", Arguments{"id": "beta"})
	if got := tabIDs(r); !reflect.DeepEqual(got, []string{"alpha", "beta", "gamma"}) {
		t.Fatalf("tabs after show %v", got)
	}
	if got := r.Settings().Strings(PagesHiddenSetting); len(got) != 0 {
		t.Fatalf("pages.hidden %v", got)
	}
}

// page.hide and page.show answer by availability; the last page in the
// tabs cannot be hidden; an unknown page is refused.
func TestPageHideRefusals(t *testing.T) {
	r, _ := pageHideFixture(t)
	if _, err := r.Execute(Request{Noun: "page", Verb: "show", Args: Arguments{"id": "beta"}}); err == nil || !strings.Contains(err.Error(), "not applicable") {
		t.Fatalf("show on a page in the tabs: %v", err)
	}
	paletteRequest(t, r, "hide", "page", Arguments{"id": "beta"})
	if _, err := r.Execute(Request{Noun: "page", Verb: "hide", Args: Arguments{"id": "beta"}}); err == nil || !strings.Contains(err.Error(), "not applicable") {
		t.Fatalf("hide twice: %v", err)
	}
	paletteRequest(t, r, "hide", "page", Arguments{"id": "gamma"})
	if _, err := r.Execute(Request{Noun: "page", Verb: "hide", Args: Arguments{"id": "alpha"}}); err == nil || !strings.Contains(err.Error(), "only page in the tabs") {
		t.Fatalf("hiding the last page: %v", err)
	}
	if _, err := r.Execute(Request{Noun: "page", Verb: "hide", Args: Arguments{"id": "nope"}}); err == nil || !strings.Contains(err.Error(), "unknown page") {
		t.Fatalf("unknown page: %v", err)
	}
	// With no id, the verbs act on the current page, and say when there
	// is nothing to do.
	if _, err := r.Execute(Request{Noun: "page", Verb: "show"}); err == nil || !strings.Contains(err.Error(), "ALPHA is already in the tabs") {
		t.Fatalf("show on the current page in the tabs: %v", err)
	}
	paletteRequest(t, r, "show", "page", Arguments{"id": "gamma"})
	paletteRequest(t, r, "hide", "page", nil)
	if !r.PageHidden("alpha") || r.Page != "alpha" {
		t.Fatalf("hide with no id: hidden %v page %q", r.HiddenPages(), r.Page)
	}
}

// A page declared hidden by default starts out of the tabs; the setting's
// default is the declared pages, page.show writes the whole list, and a
// reset goes back to the defaults.
func TestPageDefaultHidden(t *testing.T) {
	r := NewRuntime()
	pageTestRegister(t, r, "alpha")
	if _, err := r.AddPage("runs", "Runs", func(*Runtime, int, int) Block { return Text("runs") }, DefaultHidden()); err != nil {
		t.Fatal(err)
	}
	if !r.PageHidden("runs") || r.PageHidden("alpha") {
		t.Fatalf("before the setting is declared: %v", r.HiddenPages())
	}
	r.ensurePagesSetting()
	late := &Page{ID: "late", Title: "Late", DefaultHidden: true, Render: func(*Runtime, int, int) Block { return Text("late") }}
	if err := r.RegisterPage(late); err != nil {
		t.Fatal(err)
	}
	got, _ := r.Settings().Get(PagesHiddenSetting)
	if !reflect.DeepEqual(got.Default, []string{"runs", "late"}) || got.Set {
		t.Fatalf("default %v set %v", got.Default, got.Set)
	}
	if err := r.OpenPage("alpha"); err != nil {
		t.Fatal(err)
	}
	if ids := tabIDs(r); !reflect.DeepEqual(ids, []string{"alpha"}) {
		t.Fatalf("tabs %v", ids)
	}
	paletteRequest(t, r, "show", "page", Arguments{"id": "runs"})
	if ids := tabIDs(r); !reflect.DeepEqual(ids, []string{"alpha", "runs"}) {
		t.Fatalf("tabs after show %v", ids)
	}
	if got := r.Settings().Strings(PagesHiddenSetting); !reflect.DeepEqual(got, []string{"late"}) {
		t.Fatalf("pages.hidden %v", got)
	}
	if err := r.Settings().Reset(PagesHiddenSetting); err != nil {
		t.Fatal(err)
	}
	if !r.PageHidden("runs") {
		t.Fatal("reset did not bring back the default")
	}
}

// The setting applies live: a value set behind the verbs (settings set,
// config.reload, another process) shows on the next frame and motion.
func TestPagesHiddenSettingIsLive(t *testing.T) {
	r, m := pageHideFixture(t)
	r.ensurePagesSetting()
	if err := r.Settings().Set(PagesHiddenSetting, `["gamma"]`); err != nil {
		t.Fatal(err)
	}
	if row := tabRow(m); strings.Contains(row, "GAMMA") || !strings.Contains(row, "BETA") {
		t.Fatalf("tab row %q", row)
	}
	pressKeys(m, "ctrl+k")
	if r.Page != "beta" {
		t.Fatalf("ctrl+k from alpha opened %q, want beta (gamma skipped)", r.Page)
	}
}

// Model.Init leaves a hidden start page for the first page in the tabs.
func TestOpenVisibleStart(t *testing.T) {
	r := NewRuntime()
	pageTestRegister(t, r, "alpha")
	pageTestRegister(t, r, "beta")
	if err := r.HidePage("alpha"); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("alpha"); err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	m.Init()
	if r.Page != "beta" {
		t.Fatalf("start page %q, want beta", r.Page)
	}
	// Later, opening the hidden page stays on it.
	if err := r.OpenPage("alpha"); err != nil || r.Page != "alpha" {
		t.Fatalf("OpenPage(alpha): %v page %q", err, r.Page)
	}
}

// The page picker lists hidden pages, marked.
func TestPagePickerMarksHidden(t *testing.T) {
	r, _ := pageHideFixture(t)
	paletteRequest(t, r, "hide", "page", Arguments{"id": "gamma"})
	chooser, err := r.OpenPagePicker()
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(chooser.spec.Rows, func(row ChooserRow) bool { return row.ID == "gamma" })
	if i < 0 || !reflect.DeepEqual(chooser.spec.Rows[i].Cells, []string{"GAMMA", "hidden"}) {
		t.Fatalf("rows %+v", chooser.spec.Rows)
	}
}
