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

func TestMultiKeySort(t *testing.T) {
	m := sample()
	// Start with primary sort on kind asc, then add secondary sort on size desc.
	m.CycleSort("kind")
	if got := ids(m.Filtered()); got != "1,2,5,3,4" {
		t.Fatalf("primary kind asc: %s", got)
	}
	if !m.HandleCommand(core.Command{ID: "table.sort.add", Args: map[string]any{"field": "size"}}) {
		t.Fatal("table.sort.add not handled")
	}
	// kind asc, size asc (ties broken by ID): go(400:2,5; 1200:1), md(7:4, 90:3)
	if got := ids(m.Filtered()); got != "2,5,1,4,3" {
		t.Fatalf("kind asc + size asc: %s", got)
	}
	if m.SortField != "kind" || m.SortDir != SortAsc || len(m.SortKeys) != 2 {
		t.Fatalf("SortField/SortDir not synced: %q %v %+v", m.SortField, m.SortDir, m.SortKeys)
	}
	// View shows ▲1 / ▲2 for multi-key sort.
	cols := m.View("t").Props["columns"].([]any)
	kindTitle := cols[1].(map[string]any)["title"].(string)
	sizeTitle := cols[2].(map[string]any)["title"].(string)
	if !strings.Contains(kindTitle, "▲1") || !strings.Contains(sizeTitle, "▲2") {
		t.Fatalf("multi-key titles: kind=%q size=%q", kindTitle, sizeTitle)
	}
	// Adding size again cycles it to desc.
	m.AddSort("size")
	if got := ids(m.Filtered()); got != "1,2,5,3,4" {
		t.Fatalf("kind asc + size desc: %s", got)
	}
	sizeTitle = m.View("t").Props["columns"].([]any)[2].(map[string]any)["title"].(string)
	if !strings.Contains(sizeTitle, "▼2") {
		t.Fatalf("size desc title: %q", sizeTitle)
	}
	// Adding size a third time removes it, returning to single-key ▲ without number.
	m.AddSort("size")
	if len(m.SortKeys) != 1 || m.SortField != "kind" {
		t.Fatalf("after removing secondary: %+v", m.SortKeys)
	}
	kindTitle = m.View("t").Props["columns"].([]any)[1].(map[string]any)["title"].(string)
	if !strings.HasSuffix(kindTitle, "▲") || strings.Contains(kindTitle, "▲1") {
		t.Fatalf("single-key title: %q", kindTitle)
	}
}

func TestMultiConditionFilter(t *testing.T) {
	cols := []Column{{Key: "name"}, {Key: "status"}, {Key: "prio"}}
	mk := func(id, name, status, prio string) Row {
		return Row{ID: id, Cells: map[string]any{"name": name, "status": status, "prio": prio}}
	}
	m := New(cols, []Row{
		mk("1", "auth", "active", "P1"),
		mk("2", "billing", "done", "P1"),
		mk("3", "cache", "active", "P2"),
		mk("4", "docs", "archived", "P2"),
		mk("5", "edge:case", "active", "P3"),
	})

	for _, tc := range []struct{ q, want string }{
		{"active !done", "1,3,5"},
		{"P1|P2 !archived", "1,2,3"},
		{"P1|P2 !=done !archived", "1,3"},
		{"status:active prio:P1|P2", "1,3"},
		{"status!=active", "2,4"},
		{"edge:case", "5"}, // unknown field falls back to plain substring
		{"!active", "2,4"},
	} {
		m.Query = tc.q
		if got := ids(m.Filtered()); got != tc.want {
			t.Errorf("query %q: got %s, want %s", tc.q, got, tc.want)
		}
	}
}

func TestVisualAndCellSelect(t *testing.T) {
	m := sample()
	// Visual range selection and marking.
	m.SelectedIdx = 1
	if !m.HandleCommand(core.Command{ID: "table.visual.toggle"}) {
		t.Fatal("table.visual.toggle not handled")
	}
	m.HandleCommand(core.Command{ID: "motion.down", Count: 2}) // anchor=1, selected=3
	vrows := m.VisualRows()
	if got := ids(vrows); got != "2,3,4" {
		t.Fatalf("VisualRows = %s, want 2,3,4", got)
	}
	v := m.View("t")
	if v.Props["visualFrom"] != 1 || v.Props["visualTo"] != 3 {
		t.Fatalf("visual props: %+v", v.Props)
	}
	if v.Facts["visualActive"] != true || v.Facts["visualCount"] != 3 {
		t.Fatalf("visual facts: %+v", v.Facts)
	}
	// MarkVisual marks all visual rows and exits visual mode.
	if !m.HandleCommand(core.Command{ID: "table.visual.mark"}) {
		t.Fatal("table.visual.mark not handled")
	}
	if m.VisualActive || ids(m.MarkedRows()) != "2,3,4" {
		t.Fatalf("after MarkVisual: active=%v marked=%s", m.VisualActive, ids(m.MarkedRows()))
	}
	// Esc clears visual mode when active, returns false when inactive.
	if m.HandleCommand(core.Command{ID: "esc"}) {
		t.Fatal("esc should not be handled when visual mode is inactive")
	}
	m.ToggleVisual()
	if !m.HandleCommand(core.Command{ID: "esc"}) || m.VisualActive {
		t.Fatal("esc should clear active visual mode")
	}

	// Cell-select mode.
	if m.HandleCommand(core.Command{ID: "motion.right"}) {
		t.Fatal("motion.right should not be handled when CellSelect is false")
	}
	if _, has := m.View("t").Props["selectedCol"]; has {
		t.Fatal("selectedCol should be absent when CellSelect is false")
	}
	if !m.HandleCommand(core.Command{ID: "table.cell.toggle"}) || !m.CellSelect {
		t.Fatal("table.cell.toggle failed")
	}
	if !m.HandleCommand(core.Command{ID: "motion.right", Count: 2}) || m.SelectedCol != 2 {
		t.Fatalf("motion.right: SelectedCol=%d, want 2", m.SelectedCol)
	}
	m.HandleCommand(core.Command{ID: "motion.right", Count: 5}) // clamped
	if m.SelectedCol != 2 {
		t.Fatalf("clamped right: %d", m.SelectedCol)
	}
	m.HandleCommand(core.Command{ID: "motion.left"})
	if m.SelectedCol != 1 {
		t.Fatalf("motion.left: %d", m.SelectedCol)
	}
	colKey, val, ok := m.FocusedCell()
	if !ok || colKey != "kind" || val != "md" {
		t.Fatalf("FocusedCell = (%q, %v, %v), want (kind, md, true)", colKey, val, ok)
	}
	cv := m.View("t")
	if cv.Props["selectedCol"] != 1 || cv.Facts["cellSelect"] != true || cv.Facts["selectedCol"] != 1 {
		t.Fatalf("cell view props/facts: %+v %+v", cv.Props, cv.Facts)
	}
}

func TestRowStylingProjectionAndCacheKey(t *testing.T) {
	cols := []Column{{Key: "status", Title: "Status"}, {Key: "title", Title: "Title"}}
	m := New(cols, []Row{
		{
			ID:        "1",
			Cells:     map[string]any{"status": "working", "title": "Active item"},
			Tone:      "row.active",
			CellTones: map[string]string{"status": "state.working"},
			CellIcons: map[string]string{"status": "●"},
			IconTones: map[string]string{"status": "state.working"},
		},
		{
			ID:    "2",
			Cells: map[string]any{"status": "plain", "title": "Plain item"},
		},
	})

	v1 := m.View("t")
	rawRows := v1.Props["rows"].([]any)
	spec, ok := rawRows[0].(ui.TableRowSpec)
	if !ok {
		t.Fatalf("styled row should project to ui.TableRowSpec, got %T", rawRows[0])
	}
	if spec.Tone != "row.active" || spec.CellTones[0] != "state.working" || spec.CellIcons[0] != "●" || spec.IconTones[0] != "state.working" {
		t.Fatalf("unexpected projected spec: %+v", spec)
	}
	if _, isPlain := rawRows[1].([]any); !isPlain {
		t.Fatalf("unstyled row should stay plain []any, got %T", rawRows[1])
	}

	// CacheKey invalidates on tone or icon mutation.
	k1 := v1.CacheKey
	m.Rows[0].Tone = "row.cancelled"
	k2 := m.View("t").CacheKey
	if k2 == k1 {
		t.Error("CacheKey did not invalidate on Row.Tone change")
	}
	m.Rows[0].CellIcons["status"] = "✕"
	k3 := m.View("t").CacheKey
	if k3 == k2 {
		t.Error("CacheKey did not invalidate on Row.CellIcons change")
	}
	m.Rows[0].IconTones["status"] = "state.cancelled"
	k4 := m.View("t").CacheKey
	if k4 == k3 {
		t.Error("CacheKey did not invalidate on Row.IconTones change")
	}

	// Group headers emit ui.TableRowSpec with Tone "table.group".
	m.GroupField = "status"
	gv := m.View("t")
	gRows := gv.Props["rows"].([]any)
	hdr, ok := gRows[0].(ui.TableRowSpec)
	if !ok || hdr.Tone != "table.group" {
		t.Fatalf("group header should emit ui.TableRowSpec with table.group tone, got %#v", gRows[0])
	}
}
