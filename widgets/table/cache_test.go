package table

import (
	"fmt"
	"strings"
	"testing"
	"unicode"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
)

func TestTableCacheInvalidation(t *testing.T) {
	m := sample()
	// Warm filter, items, and view caches.
	if got := m.FilteredCount(); got != 5 {
		t.Fatalf("initial FilteredCount = %d, want 5", got)
	}
	v0 := m.View("t")

	// Direct Query mutation invalidates filter/items/view caches.
	m.Query = "go"
	if got := m.FilteredCount(); got != 3 {
		t.Fatalf("after Query=go FilteredCount = %d, want 3", got)
	}
	if ids(m.Filtered()) != "1,2,5" {
		t.Fatalf("after Query=go Filtered = %s, want 1,2,5", ids(m.Filtered()))
	}

	// Direct MatchMode mutation invalidates filter cache and updates FilterErr.
	m.MatchMode = MatchRegex
	m.Query = "(["
	if got := m.FilteredCount(); got != 0 || m.FilterErr == "" {
		t.Fatalf("invalid regex: count=%d err=%q", got, m.FilterErr)
	}
	// Cache hit must still preserve FilterErr.
	m.FilterErr = ""
	if got := m.FilteredCount(); got != 0 || m.FilterErr == "" {
		t.Fatalf("cached invalid regex must preserve FilterErr: count=%d err=%q", got, m.FilterErr)
	}

	// Direct FilterField mutation invalidates filter cache.
	m.MatchMode = MatchSubstring
	m.Query = "md"
	m.FilterField = "name"
	if got := m.FilteredCount(); got != 0 {
		t.Fatalf("FilterField=name query=md: count=%d, want 0", got)
	}
	m.FilterField = "kind"
	if got := m.FilteredCount(); got != 2 {
		t.Fatalf("FilterField=kind query=md: count=%d, want 2", got)
	}

	// Direct Rows replacement invalidates filter cache.
	m.Query, m.FilterField = "", ""
	m.Rows = append(m.Rows, Row{ID: "6", Cells: map[string]any{"name": "extra.go", "kind": "go", "size": 10}})
	if got := m.FilteredCount(); got != 6 {
		t.Fatalf("after append Rows FilteredCount = %d, want 6", got)
	}

	// Direct Marked mutation invalidates view cache.
	vPreMark := m.View("t")
	m.Marked = map[string]bool{"1": true}
	vPostMark := m.View("t")
	if vPostMark.CacheKey == vPreMark.CacheKey {
		t.Fatal("direct m.Marked edit did not invalidate View CacheKey")
	}
	fr, _ := ui.Render(vPostMark, ui.DefaultDarkTheme(), "t", 50, 10)
	if !strings.Contains(fr.ANSI, "● render.go") {
		t.Fatalf("direct m.Marked edit missing mark prefix:\n%s", fr.ANSI)
	}

	// Direct Columns mutation invalidates view cache.
	m.Columns = append(m.Columns, Column{Key: "extra", Title: "Extra"})
	vCols := m.View("t")
	if vCols.CacheKey == vPostMark.CacheKey || len(vCols.Props["columns"].([]any)) != 4 {
		t.Fatal("direct m.Columns edit did not invalidate view cache")
	}

	// In-place visible cell & tone edit on warm view cache reflects immediately.
	m.Rows[0].Cells["name"] = "renamed.go"
	m.Rows[0].Tone = "row.cancelled"
	vEdit := m.View("t")
	if vEdit.CacheKey == vCols.CacheKey {
		t.Fatal("in-place row edit did not invalidate View CacheKey")
	}
	frEdit, _ := ui.Render(vEdit, ui.DefaultDarkTheme(), "t", 50, 10)
	if !strings.Contains(frEdit.ANSI, "renamed.go") {
		t.Fatalf("in-place cell edit missing from render:\n%s", frEdit.ANSI)
	}

	// Copy() must not share mutable cache with original.
	cp := m.Copy()
	cp.Query = "README"
	if cp.FilteredCount() != 1 {
		t.Fatalf("copy FilteredCount = %d, want 1", cp.FilteredCount())
	}
	if m.FilteredCount() != 6 {
		t.Fatalf("original FilteredCount mutated by copy: %d, want 6", m.FilteredCount())
	}
	_ = v0
}

func TestTableFilteredCountZeroAllocOnCacheHit(t *testing.T) {
	m := largeTable(2000)
	m.Filter("active", MatchSubstring)
	want := len(m.Filtered())
	if got := m.FilteredCount(); got != want {
		t.Fatalf("FilteredCount = %d, want %d", got, want)
	}
	allocs := testing.AllocsPerRun(50, func() {
		if m.FilteredCount() != want {
			t.Fatal("unexpected count")
		}
	})
	if allocs != 0 {
		t.Fatalf("warm FilteredCount allocated %.1f times/op, want 0", allocs)
	}
}

func TestContainsFoldASCIIExhaustiveUnicode(t *testing.T) {
	needles := []string{"a", "i", "k", "s", "z", "go", "pk", "in", "kit"}
	terms := make([]foldTerm, len(needles))
	for i, n := range needles {
		terms[i] = makeFoldTerm(n)
	}
	// Check every Unicode rune against single-char ASCII needles that could fold.
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if !unicode.IsPrint(r) && r > 0x7f {
			continue
		}
		if r >= 0xd800 && r <= 0xdfff {
			continue
		}
		s := "x" + string(r) + "y"
		ls := strings.ToLower(s)
		for idx := 0; idx < 5; idx++ {
			want := strings.Contains(ls, terms[idx].lower)
			got := terms[idx].match(s)
			if got != want {
				t.Fatalf("rune U+%04X (%q) needle %q: got %v, want %v", r, s, needles[idx], got, want)
			}
		}
	}
	// Extra explicit cases: Turkish İ, Kelvin K, emojis, CJK, accented.
	cases := []struct{ hay, needle string }{
		{"İstanbul", "istanbul"},
		{"Kelvin", "kelvin"},
		{"● Context · Entry — Alpha", "alpha"},
		{"● Context · Entry — Alpha", "BETA"},
		{"日本語テスト", "テスト"},
		{"Café résumé", "café"},
		{"", ""},
		{"abc", ""},
		{"a", "ab"},
	}
	for _, tc := range cases {
		ft := makeFoldTerm(tc.needle)
		want := strings.Contains(strings.ToLower(tc.hay), strings.ToLower(tc.needle))
		if got := ft.match(tc.hay); got != want {
			t.Errorf("foldTerm(%q).match(%q) = %v, want %v", tc.needle, tc.hay, got, want)
		}
	}
}

func TestTableViewCacheReusesRowsAcrossCursorSteps(t *testing.T) {
	m := largeTable(1000)
	v0 := m.View("t")
	rows0 := v0.Props["rows"].([]any)
	m.HandleCommand(core.Command{ID: "motion.down"})
	v1 := m.View("t")
	rows1 := v1.Props["rows"].([]any)
	if v1.CacheKey == v0.CacheKey {
		t.Fatal("cursor move must still update ViewNode.CacheKey")
	}
	if &rows0[0] != &rows1[0] {
		t.Fatal("pure cursor move reallocated projected rows slice instead of reusing cache")
	}
	// Verify all visible cells remain accurate after scrolling far down.
	m.HandleCommand(core.Command{ID: "motion.down", Count: 500})
	v2 := m.View("t")
	fr, _ := ui.Render(v2, ui.DefaultDarkTheme(), "t", 100, 32)
	wantID := fmt.Sprintf("row-%d", m.SelectedIdx)
	if !strings.Contains(fr.ANSI, wantID) {
		t.Fatalf("scrolled render missing %s:\n%s", wantID, fr.ANSI)
	}
}

func BenchmarkTableFilter7k(b *testing.B) {
	m := largeTable(7023)
	queries := []string{"p", "pr", "pro", "proj", "proje", "projec", "project"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q := queries[i%len(queries)]
		m.Filter(q, MatchSubstring)
		_ = m.View("ctx")
		_ = m.FilteredCount()
		_ = m.View("ctx")
		_ = m.FilteredCount()
	}
}

func BenchmarkTableCursorStep7k(b *testing.B) {
	m := largeTable(7023)
	_ = m.View("ctx")
	down := core.Command{ID: "motion.down"}
	up := core.Command{ID: "motion.up"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i%2 == 0 {
			m.HandleCommand(down)
		} else {
			m.HandleCommand(up)
		}
		_ = m.View("ctx")
	}
}
