package picker

import (
	"strings"
	"testing"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
)

func items() []Item {
	return []Item{
		{ID: "1", Title: "alpha", Subtitle: "first", Ref: &core.Ref{Kind: "task", ID: "1"}, Scope: "task"},
		{ID: "2", Title: "beta", Keywords: []string{"second"}, Unread: true},
		{ID: "3", Title: "gamma ray", Badges: []ui.Badge{{Text: "new", Tone: "accent"}}},
		{ID: "4", Title: "delta"},
	}
}

func ids(f []Item) string {
	var s []string
	for _, it := range f {
		s = append(s, it.ID)
	}
	return strings.Join(s, ",")
}

func TestMatching(t *testing.T) {
	m := New("t", items())
	m.SetQuery("A")
	if got := ids(m.Filtered()); got != "1,2,3,4" {
		t.Fatalf("substring: %s", got)
	}
	m.SetQuery("second")
	if got := ids(m.Filtered()); got != "2" {
		t.Fatalf("keyword: %s", got)
	}
	m.SetMatchMode(MatchFuzzy)
	m.SetQuery("gry")
	if got := ids(m.Filtered()); got != "3" {
		t.Fatalf("fuzzy: %s", got)
	}
	m.SetMatchMode(MatchRegex)
	m.SetQuery("^(al|de)")
	if got := ids(m.Filtered()); got != "1,4" || m.FilterErr != "" {
		t.Fatalf("regex: %s %q", got, m.FilterErr)
	}
	m.SetQuery("(")
	if m.FilterErr == "" || len(m.Filtered()) != 4 {
		t.Fatalf("invalid regex should flag and match all: %q %d", m.FilterErr, len(m.Filtered()))
	}
	m.SetQuery("al")
	if m.FilterErr != "" {
		t.Fatal("error not cleared")
	}
	m.CycleMatchMode()
	if m.MatchMode != MatchSubstring {
		t.Fatalf("cycle: %s", m.MatchMode)
	}
	m.AppendQuery("p")
	m.BackspaceQuery()
	m.BackspaceQuery()
	if m.Query != "a" {
		t.Fatalf("query %q", m.Query)
	}
}

func TestMotions(t *testing.T) {
	m := New("t", items())
	m.PageSize = 2
	run := func(id string, count int) {
		if _, ok := m.HandleCommand(core.Command{ID: id, Count: count}); !ok {
			t.Fatalf("%s unhandled", id)
		}
	}
	run("motion.down", 2)
	if m.SelectedIdx != 2 {
		t.Fatalf("down: %d", m.SelectedIdx)
	}
	run("motion.down", 9)
	if m.SelectedIdx != 3 {
		t.Fatalf("clamp: %d", m.SelectedIdx)
	}
	run("motion.up", 0)
	run("motion.half.up", 0)
	if m.SelectedIdx != 1 {
		t.Fatalf("up/half: %d", m.SelectedIdx)
	}
	run("motion.page.down", 0)
	if m.SelectedIdx != 3 {
		t.Fatalf("page: %d", m.SelectedIdx)
	}
	run("motion.top", 0)
	if m.SelectedIdx != 0 {
		t.Fatal("top")
	}
	run("motion.bottom", 0)
	if m.SelectedIdx != 3 {
		t.Fatal("bottom")
	}
	if _, ok := m.HandleCommand(core.Command{ID: "nope"}); ok {
		t.Fatal("unknown handled")
	}
}

func TestConfirmAndMarks(t *testing.T) {
	m := New("t", items())
	if p, _ := m.HandleCommand(core.Command{ID: CmdConfirm}); p == nil || p.ID != "1" {
		t.Fatalf("confirm: %v", p)
	}
	m.HandleCommand(core.Command{ID: CmdMarkToggle})
	if len(m.MarkedItems()) != 0 {
		t.Fatal("marks need MultiSelect")
	}
	m.MultiSelect = true
	m.HandleCommand(core.Command{ID: CmdMarkToggle})
	m.HandleCommand(core.Command{ID: "motion.bottom"})
	m.HandleCommand(core.Command{ID: CmdMarkToggle})
	if got := ids(m.MarkedItems()); got != "1,4" {
		t.Fatalf("marked %s", got)
	}
	m.HandleCommand(core.Command{ID: CmdMarkToggle})
	if got := ids(m.MarkedItems()); got != "1" {
		t.Fatalf("unmark %s", got)
	}
	m.SetQuery("zzz")
	if p, _ := m.HandleCommand(core.Command{ID: CmdConfirm}); p != nil {
		t.Fatal("confirm on empty list")
	}
	m.SetItems(items()[1:])
	if len(m.Marked) != 0 {
		t.Fatalf("stale marks %v", m.Marked)
	}
}

func TestView(t *testing.T) {
	m := New("Pick", items())
	v := m.View("p")
	if v.Kind != ui.KindPicker || !v.Focusable || v.ID != "p" || v.Scope != "task" || v.Ref == nil || v.Ref.ID != "1" || v.CacheKey == 0 {
		t.Fatalf("bad view: %+v", v)
	}
	if v.Facts["count"] != 4 || v.Facts["matchMode"] != "substring" {
		t.Fatalf("facts %v", v.Facts)
	}
	if v.CacheKey != m.View("p").CacheKey {
		t.Fatal("cache key not deterministic")
	}
	m.HandleCommand(core.Command{ID: "motion.down"})
	if m.View("p").CacheKey == v.CacheKey {
		t.Fatal("cache key ignores selection")
	}
	f, _ := ui.Render(m.View("p"), ui.DefaultDarkTheme(), "p", 40, 8)
	if !strings.Contains(f.ANSI, "alpha") || !strings.Contains(f.ANSI, "gamma ray") {
		t.Fatalf("render: %q", f.ANSI)
	}

	m.PreviewFn = func(it Item) *ui.ViewNode {
		return &ui.ViewNode{Kind: ui.KindText, Props: map[string]any{"text": "preview of " + it.Title}}
	}
	sv := m.View("p")
	if sv.Kind != ui.KindSplit || len(sv.Children) != 2 || sv.Children[0].ID != "p" {
		t.Fatalf("split: %+v", sv)
	}
	f, _ = ui.Render(sv, ui.DefaultDarkTheme(), "p", 80, 8)
	if !strings.Contains(f.ANSI, "preview of beta") {
		t.Fatalf("preview render: %q", f.ANSI)
	}
	m.SetQuery("zzz")
	if e := m.View("p"); e.Kind != ui.KindPicker || e.Props["selected"] != -1 {
		t.Fatalf("empty: %+v", e)
	}
}

func TestPinnedAndRank(t *testing.T) {
	m := New("t", items())
	m.MultiSelect = true
	m.Pinned = &Item{ID: "p", Title: "pinned"}
	m.Rank = func(it Item, q string) int { return len(it.Title) }
	for q, want := range map[string]string{"": "p,1,2,3,4", "a": "p,2,1,4,3", "zzz": "p"} {
		m.SetQuery(q)
		if got := ids(m.Filtered()); got != want {
			t.Fatalf("query %q: %s, want %s", q, got, want)
		}
	}
	m.SetMatchMode(MatchRegex)
	m.SetQuery("a")
	if got := ids(m.Filtered()); got != "p,1,2,3,4" {
		t.Fatalf("regex keeps order: %s", got)
	}
	if it, _ := m.Focused(); it.ID != "p" {
		t.Fatalf("pinned row focused at 0: %v", it.ID)
	}
	m.ToggleMark()
	if len(m.Marked) != 0 {
		t.Fatal("pinned row is never marked")
	}
	v := m.View("p")
	if v.Facts["total"] != 4 || v.Facts["count"] != 4 || v.Facts["pinned"] != true || v.Facts["query"] != "a" {
		t.Fatalf("facts: %v", v.Facts)
	}
	if rows := v.Props["items"].([]any); rows[0].(map[string]any)["label"] != "    pinned" || rows[1].(map[string]any)["label"] != "[ ] alpha" {
		t.Fatalf("pinned row is not markable: %v", rows[:2])
	}
	m.HandleCommand(core.Command{ID: "motion.bottom"})
	if m.SelectedIdx != 4 {
		t.Fatalf("bottom counts the pinned row: %d", m.SelectedIdx)
	}
}
