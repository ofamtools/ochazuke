package table

import (
	"strings"
	"testing"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
)

func sample() *Model {
	cols := []Column{{Key: "name"}, {Key: "kind"}, {Key: "size", Align: "right"}}
	mk := func(id, name, kind string, size any) Row {
		return Row{ID: id, Ref: &core.Ref{Kind: "file", ID: id}, Cells: map[string]any{"name": name, "kind": kind, "size": size}}
	}
	return New(cols, []Row{
		mk("1", "render.go", "go", 1200), mk("2", "Theme.go", "go", 400),
		mk("3", "README", "md", 90), mk("4", "notes", "md", 7), mk("5", "main.go", "go", 400),
	})
}

func ids(rs []Row) string {
	var s []string
	for _, r := range rs {
		s = append(s, r.ID)
	}
	return strings.Join(s, ",")
}

func TestFilter(t *testing.T) {
	m := sample()
	m.Query = "THEME"
	if got := ids(m.Filtered()); got != "2" {
		t.Errorf("substring all: %s", got)
	}
	m.Query, m.FilterField = "go", "kind"
	if got := ids(m.Filtered()); got != "1,2,5" {
		t.Errorf("substring field: %s", got)
	}
	m.Query, m.FilterField, m.MatchMode = "rdr", "", MatchFuzzy
	if got := ids(m.Filtered()); got != "1" {
		t.Errorf("fuzzy: %s", got)
	}
	m.Query, m.MatchMode, m.FilterField = `^m.*\.go$`, MatchRegex, "name"
	if got := ids(m.Filtered()); got != "5" || m.FilterErr != "" {
		t.Errorf("regex: %s %q", got, m.FilterErr)
	}
	m.Query = "(["
	if got := ids(m.Filtered()); got != "" || m.FilterErr == "" {
		t.Errorf("invalid regex: %q %q", got, m.FilterErr)
	}
}

func TestSort(t *testing.T) {
	m := sample()
	m.SortField, m.SortDir = "size", SortAsc
	if got := ids(m.Filtered()); got != "4,3,2,5,1" {
		t.Errorf("numeric asc (tie by id): %s", got)
	}
	m.SortDir = SortDesc
	if got := ids(m.Filtered()); got != "1,2,5,3,4" {
		t.Errorf("numeric desc: %s", got)
	}
	m.SortField, m.SortDir = "name", SortAsc
	if got := ids(m.Filtered()); got != "5,4,3,1,2" {
		t.Errorf("string asc: %s", got)
	}
	m.SortField, m.SortDir = "nonexistent", SortAsc
	if got := ids(m.Filtered()); got != "1,2,3,4,5" {
		t.Errorf("unknown field: %s", got)
	}
}

func TestGroup(t *testing.T) {
	m := sample()
	m.GroupField = "kind"
	it := m.Items()
	if len(it) != 7 || !it[0].Header || it[0].Count != 3 || !it[4].Header {
		t.Fatalf("groups: %+v", it)
	}
	m.HandleCommand(core.Command{ID: "motion.block.next"})
	if m.SelectedIdx != 4 {
		t.Errorf("block next: %d", m.SelectedIdx)
	}
	m.HandleCommand(core.Command{ID: "motion.down"})
	m.HandleCommand(core.Command{ID: "motion.block.prev"})
	if m.SelectedIdx != 4 {
		t.Errorf("block prev from row: %d", m.SelectedIdx)
	}
	m.HandleCommand(core.Command{ID: "motion.block.prev"})
	if m.SelectedIdx != 0 {
		t.Errorf("block prev from header: %d", m.SelectedIdx)
	}
	m.ToggleGroup()
	if len(m.Items()) != 4 || !m.Items()[0].Collapsed {
		t.Errorf("collapse: %+v", m.Items())
	}
	m.ExpandAllGroups()
	if len(m.Items()) != 7 {
		t.Error("expand all")
	}
	m.CollapseAllGroups()
	if len(m.Items()) != 2 {
		t.Error("collapse all")
	}
	// All-unique field still groups.
	m = sample()
	m.GroupField = "name"
	if len(m.Items()) != 10 {
		t.Errorf("unique grouping: %d", len(m.Items()))
	}
}

func TestMarksContextAndRender(t *testing.T) {
	m := sample()
	m.GroupField = "kind"
	m.HandleCommand(core.Command{ID: "motion.down"})
	m.HandleCommand(core.Command{ID: "table.mark.toggle"})
	m.HandleCommand(core.Command{ID: "motion.down", Count: 2})
	m.ToggleMark()
	cf := m.ContextFrame("table", "t")
	if cf.Subject == nil || cf.Subject.ID != "5" || len(cf.Subjects) != 2 {
		t.Fatalf("frame: %+v", cf)
	}
	if cf.Facts["markedCount"] != 2 || cf.Facts["rowCount"] != 5 || cf.Facts["groupField"] != "kind" || cf.Facts["isGroupHeader"] != false {
		t.Errorf("facts: %v", cf.Facts)
	}
	n := m.View("t")
	if n.Kind != ui.KindTable || !n.Focusable || n.CacheKey == 0 || n.Ref == nil {
		t.Fatalf("node: %+v", n)
	}
	if k := m.View("t").CacheKey; k != n.CacheKey {
		t.Error("cache key not deterministic")
	}
	m.HandleCommand(core.Command{ID: "motion.up"})
	if m.View("t").CacheKey == n.CacheKey {
		t.Error("cache key ignores selection")
	}
	f, _ := ui.Render(n, ui.DefaultDarkTheme(), "t", 40, 12)
	for _, want := range []string{"▾ go (3)", "▾ md (2)", "● render.go", "render.go"} {
		if !strings.Contains(f.ANSI, want) {
			t.Errorf("render missing %q:\n%s", want, f.ANSI)
		}
	}
	m.SortField, m.SortDir = "size", SortDesc
	if !strings.Contains(m.View("t").Props["columns"].([]any)[2].(map[string]any)["title"].(string), "▼") {
		t.Error("sort indicator")
	}
	if m.HandleCommand(core.Command{ID: "nope"}) {
		t.Error("unknown handled")
	}
}
