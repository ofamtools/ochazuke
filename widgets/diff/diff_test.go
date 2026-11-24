package diff

import (
	"regexp"
	"strings"
	"testing"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;:]*m|\x1b\]8;[^\x07]*\x07`)

func render(t *testing.T, n ui.ViewNode, w, h int) []string {
	t.Helper()
	f, _ := ui.Render(n, ui.DefaultDarkTheme(), n.ID, w, h)
	rows := strings.Split(ansiRe.ReplaceAllString(f.ANSI, ""), "\n")
	for i := range rows {
		rows[i] = strings.TrimRight(rows[i], " ")
	}
	return rows
}

func cmd(id string, count int) core.Command { return core.Command{ID: id, Count: count} }

const gitDiff = `diff --git a/main.go b/main.go
index 83db48f..bf2a3c1 100644
--- a/main.go
+++ b/main.go
@@ -1,4 +1,5 @@ package main
 import "fmt"
-func main() {}
+func main() {
+	fmt.Println("hi")
+}
 // end
diff --git a/old.txt b/old.txt
deleted file mode 100644
--- a/old.txt
+++ /dev/null
@@ -1,2 +0,0 @@
---gone
-two
`

func TestParseUnified(t *testing.T) {
	doc := ParseUnified(gitDiff)
	if len(doc.Sections) != 2 {
		t.Fatalf("sections = %d: %+v", len(doc.Sections), doc.Sections)
	}
	s := doc.Sections[0]
	if s.ID != "main.go" || s.OldLabel != "a/main.go" || s.NewLabel != "b/main.go" || len(s.Hunks) != 1 {
		t.Fatalf("section 0: %+v", s)
	}
	h := s.Hunks[0]
	if h.OldStart != 1 || h.OldCount != 4 || h.NewStart != 1 || h.NewCount != 5 || !strings.HasPrefix(h.Header, "@@ -1,4 +1,5 @@") || len(h.Lines) != 6 {
		t.Fatalf("hunk: %+v", h)
	}
	want := []Line{
		{LineContext, 1, 1, `import "fmt"`},
		{LineRemoved, 2, 0, "func main() {}"},
		{LineAdded, 0, 2, "func main() {"},
		{LineAdded, 0, 3, "\tfmt.Println(\"hi\")"},
		{LineAdded, 0, 4, "}"},
		{LineContext, 3, 5, "// end"},
	}
	for i, l := range want {
		if h.Lines[i] != l {
			t.Errorf("line %d = %+v, want %+v", i, h.Lines[i], l)
		}
	}
	d := doc.Sections[1]
	if d.ID != "old.txt" || d.NewLabel != "/dev/null" || len(d.Hunks) != 1 || len(d.Hunks[0].Lines) != 2 || d.Hunks[0].Lines[0].Text != "--gone" {
		t.Fatalf("deleted section: %+v", d)
	}
	if add, rem := s.Stats(); add != 3 || rem != 1 {
		t.Errorf("stats = %d,%d", add, rem)
	}
}

func TestParsePlainUnified(t *testing.T) {
	doc := ParseUnified("--- a.txt\t2026-01-01\n+++ b.txt\t2026-01-02\n@@ -1 +1 @@\n-x\n+y\n--- c\n+++ d\n@@ -3 +3 @@\n-p\n+q\n")
	if len(doc.Sections) != 2 || doc.Sections[0].OldLabel != "a.txt" || doc.Sections[1].NewLabel != "d" || doc.Sections[1].Hunks[0].OldStart != 3 {
		t.Fatalf("%+v", doc.Sections)
	}
}

func TestFromStrings(t *testing.T) {
	old := "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n13\n14\n15\n"
	one := FromStrings("T", "a", "b", old, strings.Replace(old, "\n5\n", "\nFIVE\n", 1))
	if len(one.Sections) != 1 || len(one.Sections[0].Hunks) != 1 {
		t.Fatalf("%+v", one)
	}
	h := one.Sections[0].Hunks[0]
	if h.OldStart != 2 || h.OldCount != 7 || h.NewStart != 2 || h.NewCount != 7 || h.Header != "@@ -2,7 +2,7 @@" {
		t.Fatalf("hunk %+v", h)
	}
	if h.Lines[3] != (Line{LineRemoved, 5, 0, "5"}) || h.Lines[4] != (Line{LineAdded, 0, 5, "FIVE"}) {
		t.Errorf("change lines %+v %+v", h.Lines[3], h.Lines[4])
	}

	two := FromStrings("T", "a", "b", old, strings.Replace(strings.Replace(old, "\n2\n", "\nTWO\n", 1), "\n14\n", "\nFOURTEEN\n", 1))
	if n := len(two.Sections[0].Hunks); n != 2 {
		t.Fatalf("far-apart changes: %d hunks", n)
	}
	near := FromStrings("T", "a", "b", old, strings.Replace(strings.Replace(old, "\n2\n", "\nTWO\n", 1), "\n8\n", "\nEIGHT\n", 1))
	if n := len(near.Sections[0].Hunks); n != 1 {
		t.Fatalf("near changes: %d hunks", n)
	}
	if same := FromStrings("T", "a", "b", old, old); len(same.Sections) != 0 {
		t.Errorf("equal texts: %+v", same)
	}
	add := FromStrings("T", "a", "b", "", "x\ny\n")
	if h := add.Sections[0].Hunks[0]; h.Header != "@@ -0,0 +1,2 @@" || len(h.Lines) != 2 {
		t.Errorf("pure add: %+v", h)
	}
	mixed := FromStrings("T", "a", "b", "a\nb\nc\nd\n", "b\nc\nx\nd\ne\n")
	var kinds strings.Builder
	for _, l := range mixed.Sections[0].Hunks[0].Lines {
		kinds.WriteString(l.Kind.sign())
	}
	if kinds.String() != "-  + +" {
		t.Errorf("mixed script %q", kinds.String())
	}
}

func TestFromJSON(t *testing.T) {
	a := map[string]any{"a": map[string]any{"b": []any{1, map[string]any{"c": "x", "d": true}}}, "z": nil}
	reordered := []byte(`{ "z": null,
	  "a": {"b": [1, {"d": true, "c": "x"}]} }`)
	if doc := FromJSON("same", a, reordered); len(doc.Sections) != 0 {
		t.Fatalf("reordered keys produced %+v", doc.Sections)
	}
	b := map[string]any{"a": map[string]any{"b": []any{1, map[string]any{"c": "y", "d": true}}}, "z": nil}
	doc := FromJSON("changed", a, b)
	if len(doc.Sections) != 1 {
		t.Fatalf("sections: %+v", doc.Sections)
	}
	s := doc.Sections[0]
	if s.ID != "$.a.b[1].c" || s.OldLabel != `"x"` || s.NewLabel != `"y"` {
		t.Fatalf("section %+v", s)
	}
	ls := s.Hunks[0].Lines
	if len(ls) != 2 || ls[0] != (Line{LineRemoved, 1, 0, `"x"`}) || ls[1] != (Line{LineAdded, 0, 1, `"y"`}) {
		t.Fatalf("lines %+v", ls)
	}

	c := map[string]any{"a": map[string]any{"b": []any{1}}, "new key": 1.5}
	doc = FromJSON("shape", a, c)
	var ids []string
	for _, s := range doc.Sections {
		ids = append(ids, s.ID)
	}
	if strings.Join(ids, ",") != `$.a.b[1],$["new key"],$.z` {
		t.Fatalf("ids %v", ids)
	}
	if got := doc.Sections[1]; got.OldLabel != "(absent)" || len(got.Hunks[0].Lines) != 1 || got.Hunks[0].Lines[0].Kind != LineAdded {
		t.Errorf("added key: %+v", got)
	}
}

func sample() *Model {
	old := "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n13\n14\n15\n"
	nw := strings.Replace(strings.Replace(old, "\n2\n", "\nTWO\n", 1), "\n14\n", "\nFOURTEEN\n", 1)
	doc := FromStrings("sample", "a", "b", old, nw)
	more := FromStrings("", "c", "d", "x\n", "y\n")
	more.Sections[0].ID, more.Sections[0].Title = "second", "second"
	doc.Sections = append(doc.Sections, more.Sections...)
	m := New(doc)
	m.PageSize = 40
	return m
}

func TestUnifiedRender(t *testing.T) {
	m := FromStringsModel("a\nb\nc\n", "a\nB\nc\n")
	v := m.View("d")
	if v.Kind != ui.KindPanel || v.Scope != "diff" || !v.Focusable || v.CacheKey == 0 || v.Facts["mode"] != "unified" || v.Facts["rowKind"] != "section" {
		t.Fatalf("node %+v", v)
	}
	txt := v.Children[0]
	if txt.Kind != ui.KindText {
		t.Fatalf("child %+v", txt)
	}
	spans := txt.Props["spans"].([]ui.ToneSpan)
	tones := map[string]bool{}
	for _, s := range spans {
		tones[s.Tone] = true
	}
	for _, want := range []string{"success", "error", "accent", "muted"} {
		if !tones[want] {
			t.Errorf("missing tone %q in %+v", want, spans)
		}
	}
	rows := strings.Join(render(t, v, 50, 10), "\n")
	for _, want := range []string{"▾ t +1 -1", "@@ -1,3 +1,3 @@", "   1    1  a", "   2      -b", "        2 +B"} {
		if !strings.Contains(rows, want) {
			t.Errorf("unified missing %q in:\n%s", want, rows)
		}
	}
	if v2 := m.View("d"); v2.CacheKey != v.CacheKey {
		t.Error("cache key not deterministic")
	}
	m.Cursor = 2
	if m.View("d").CacheKey == v.CacheKey {
		t.Error("cache key ignores cursor")
	}
}

// FromStringsModel builds a one-section model titled "t".
func FromStringsModel(a, b string) *Model { return New(FromStrings("t", "old", "new", a, b)) }

func TestSideBySideRender(t *testing.T) {
	m := FromStringsModel("a\nb\nc\n", "a\nB\nc\nd\n")
	m.Width = 46 // text columns inside a 52-wide panel
	if !m.HandleCommand(core.Command{ID: CmdModeToggle}) || m.Mode != ModeSideBySide {
		t.Fatalf("toggle: %v", m.Mode)
	}
	rows := render(t, m.View("d"), 52, 10)
	joined := strings.Join(rows, "\n")
	var change, ctx string
	for _, r := range rows {
		if strings.Contains(r, "-b") {
			change = r
		}
		if strings.Contains(r, "1  a") {
			ctx = r
		}
	}
	if !strings.Contains(change, "-b") || !strings.Contains(change, " │ ") || !strings.Contains(change, "+B") {
		t.Fatalf("paired change not on one row (%q) in:\n%s", change, joined)
	}
	if ctx == "" || strings.Count(ctx, "1  a") != 2 {
		t.Errorf("context row should show both sides: %q", ctx)
	}
	if !strings.Contains(joined, "+d") {
		t.Errorf("unpaired addition missing:\n%s", joined)
	}
	// columns line up: the separator sits at the same column on every line row.
	col := -1
	for _, r := range rows {
		if i := strings.Index(r, " │ "); i >= 0 {
			if col >= 0 && len([]rune(r[:i])) != col {
				t.Errorf("separator misaligned in %q", r)
			}
			col = len([]rune(r[:i]))
		}
	}
	if m.View("d").Facts["mode"] != "side-by-side" {
		t.Error("facts mode")
	}
	m.HandleCommand(core.Command{ID: CmdModeToggle})
	if m.Mode != ModeUnified {
		t.Error("toggle back")
	}
}

func TestModeTogglePreservesLine(t *testing.T) {
	m := FromStringsModel("a\nb\nc\n", "a\nB\nc\nd\n")
	for i, r := range m.Rows() {
		if r.Kind == RowLine && r.New >= 0 && m.Doc.Sections[0].Hunks[0].Lines[r.New].Text == "d" {
			m.Cursor = i
		}
	}
	m.HandleCommand(core.Command{ID: CmdModeToggle})
	r, _ := m.Focused()
	if m.Doc.Sections[0].Hunks[0].Lines[r.New].Text != "d" {
		t.Errorf("cursor moved off line d: %+v", r)
	}
}

func TestMotionsAndHunkJumps(t *testing.T) {
	m := sample()
	rows := m.Rows()
	var hunks []int
	for i, r := range rows {
		if r.Kind == RowHunk {
			hunks = append(hunks, i)
		}
	}
	if len(hunks) != 3 {
		t.Fatalf("hunk rows %v", hunks)
	}
	m.HandleCommand(cmd("motion.block.next", 0))
	if m.Cursor != hunks[0] {
		t.Fatalf("next: %d want %d", m.Cursor, hunks[0])
	}
	m.HandleCommand(cmd("motion.block.next", 0))
	if m.Cursor != hunks[1] {
		t.Fatalf("next2: %d want %d", m.Cursor, hunks[1])
	}
	m.HandleCommand(cmd("motion.down", 3))
	m.HandleCommand(cmd("motion.block.prev", 0))
	if m.Cursor != hunks[1] {
		t.Fatalf("prev from inside hunk goes to its header: %d want %d", m.Cursor, hunks[1])
	}
	m.HandleCommand(cmd("motion.block.prev", 2))
	if m.Cursor != hunks[0] {
		t.Fatalf("prev x2: %d", m.Cursor)
	}
	m.HandleCommand(cmd("motion.block.next", 9))
	if m.Cursor != hunks[2] {
		t.Fatalf("next stops at last hunk: %d", m.Cursor)
	}
	m.HandleCommand(cmd("motion.top", 0))
	m.HandleCommand(cmd("motion.down", 4))
	if m.Cursor != 4 {
		t.Errorf("down x4: %d", m.Cursor)
	}
	m.HandleCommand(cmd("motion.bottom", 0))
	if m.Cursor != len(rows)-1 {
		t.Errorf("bottom: %d", m.Cursor)
	}
	m.PageSize, m.Cursor, m.Offset = 6, 0, 0
	m.HandleCommand(cmd("motion.page.down", 0))
	if m.Cursor != 6 || m.Offset != 1 {
		t.Errorf("page down: cursor %d offset %d", m.Cursor, m.Offset)
	}
	m.HandleCommand(cmd("motion.half.up", 0))
	if m.Cursor != 3 {
		t.Errorf("half up: %d", m.Cursor)
	}
	m.HandleCommand(cmd("motion.page.up", 5))
	if m.Cursor != 0 || m.Offset != 0 {
		t.Errorf("page up clamps: %d/%d", m.Cursor, m.Offset)
	}
	if !m.HandleCommand(cmd("motion.scroll.down", 2)) || m.Offset != 2 || m.Cursor != 2 {
		t.Errorf("scroll down: cursor=%d offset=%d", m.Cursor, m.Offset)
	}
	if !m.HandleCommand(cmd("motion.scroll.up", 1)) || m.Offset != 1 || m.Cursor != 2 {
		t.Errorf("scroll up: cursor=%d offset=%d", m.Cursor, m.Offset)
	}
	if v := m.View("d"); v.Facts["atTop"] != false || v.Facts["atBottom"] != false || v.Facts["scrollable"] != true {
		t.Errorf("viewport facts: %+v", v.Facts)
	}
	if m.HandleCommand(cmd("nope", 0)) {
		t.Error("unknown command handled")
	}
}

func TestSectionCollapse(t *testing.T) {
	m := sample()
	full := len(m.Rows())
	m.HandleCommand(cmd("motion.down", 5))
	if !m.HandleCommand(core.Command{ID: CmdSectionToggle}) {
		t.Fatal("not handled")
	}
	if !m.Doc.Sections[0].Collapsed || m.Cursor != 0 {
		t.Fatalf("collapsed=%v cursor=%d", m.Doc.Sections[0].Collapsed, m.Cursor)
	}
	rows := m.Rows()
	if len(rows) >= full || rows[1].Kind != RowSection || rows[1].Section != 1 {
		t.Fatalf("rows after collapse: %+v", rows)
	}
	v := m.View("d")
	if v.Facts["collapsed"] != true || !strings.Contains(v.Children[0].Props["text"].(string), "▸ sample") {
		t.Errorf("view after collapse: %+v", v.Facts)
	}
	m.HandleCommand(core.Command{ID: CmdSectionToggle})
	if m.Doc.Sections[0].Collapsed || len(m.Rows()) != full {
		t.Error("expand failed")
	}
}

func TestYank(t *testing.T) {
	m := FromStringsModel("a\nb\nc\n", "a\nB\nc\n")
	rows := m.Rows()
	for i, r := range rows {
		if r.Kind == RowLine && r.Old >= 0 && m.Doc.Sections[0].Hunks[0].Lines[r.Old].Text == "b" {
			m.Cursor = i
		}
	}
	m.HandleCommand(core.Command{ID: CmdYank})
	if m.Yanked != "b" {
		t.Errorf("line yank %q", m.Yanked)
	}
	m.HandleCommand(core.Command{ID: CmdYank, Args: map[string]any{"scope": "hunk"}})
	if m.Yanked != "@@ -1,3 +1,3 @@\n a\n-b\n+B\n c" {
		t.Errorf("hunk yank %q", m.Yanked)
	}
	m.Cursor = 1
	m.HandleCommand(core.Command{ID: CmdYank})
	if !strings.HasPrefix(m.Yanked, "@@ -1,3") {
		t.Errorf("hunk-row yank %q", m.Yanked)
	}
	m.Cursor = 0
	m.HandleCommand(core.Command{ID: CmdYank})
	if !strings.HasPrefix(m.Yanked, "--- old\n+++ new\n@@") {
		t.Errorf("section yank %q", m.Yanked)
	}
	m.HandleCommand(core.Command{ID: CmdModeToggle})
	for i, r := range m.Rows() {
		if r.Kind == RowLine && r.Old >= 0 && r.New >= 0 && r.Old != r.New {
			m.Cursor = i
		}
	}
	m.HandleCommand(core.Command{ID: CmdYank})
	if m.Yanked != "-b\n+B" {
		t.Errorf("paired yank %q", m.Yanked)
	}
}

func TestEmptyDocument(t *testing.T) {
	m := New(Document{})
	if !m.HandleCommand(cmd("motion.down", 0)) || m.Cursor != 0 {
		t.Error("empty motion")
	}
	m.HandleCommand(core.Command{ID: CmdYank})
	m.HandleCommand(core.Command{ID: CmdSectionToggle})
	rows := strings.Join(render(t, m.View("d"), 30, 4), "\n")
	if !strings.Contains(rows, "(no changes)") {
		t.Errorf("empty render:\n%s", rows)
	}
}
