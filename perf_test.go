package ochazuke_deps_test

import (
	"fmt"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"ofam.dev/ochazuke/core"
	otea "ofam.dev/ochazuke/tea"
	"ofam.dev/ochazuke/ui"
	"ofam.dev/ochazuke/widgets/table"
)

const (
	perfTableRows = 100_000
	perfWidth     = 120
	perfHeight    = 36
)

func newPerfTable() *table.Model {
	rows := make([]table.Row, perfTableRows)
	for i := range rows {
		label := fmt.Sprintf("row-%06d", i)
		group := "included"
		if i == 79_980 {
			group = "excluded"
		}
		rows[i] = table.Row{ID: label, Cells: map[string]any{"label": label, "rank": i, "group": group}}
	}
	return table.New([]table.Column{{Key: "label", Title: "Row"}, {Key: "rank", Title: "Rank", Align: "right"}}, rows)
}

func TestPerf100kRowTable(t *testing.T) {
	model := newPerfTable()
	model.Query, model.FilterField = "included", "group"
	model.SortField, model.SortDir = "rank", table.SortDesc
	model.Offset, model.SelectedIdx, model.PageSize = 20_000, 20_010, perfHeight-2
	started := time.Now()
	node := model.View("perf-table")
	t.Logf("table view preparation took %s", time.Since(started))
	if node.Kind != ui.KindTable || len(node.Children) != 0 {
		t.Fatalf("table view has kind %q and %d child nodes", node.Kind, len(node.Children))
	}
	if rows, _ := node.Props["rows"].([]any); len(rows) != perfTableRows-1 {
		t.Fatalf("filtered view holds %d rows, want %d", len(rows), perfTableRows-1)
	}

	renderer, theme := ui.NewRenderer(1), ui.DefaultDarkTheme()
	var slowest time.Duration
	for range 5 {
		renderer.Reset()
		started := time.Now()
		frame, solved := renderer.Render(node, theme, node.ID, perfWidth, perfHeight)
		slowest = max(slowest, time.Since(started))
		if renderer.Stats.CacheMisses != 1 || renderer.Stats.CacheHits != 0 {
			t.Fatalf("expected one uncached table draw, got %+v", renderer.Stats)
		}
		if rect := solved.Rects[node.ID]; len(solved.Rects) != 1 || rect.W != perfWidth || rect.H != perfHeight {
			t.Fatalf("expected one %dx%d table rectangle, got %v", perfWidth, perfHeight, solved.Rects)
		}
		for _, want := range []string{"row-079999", "row-079965", "20034/99999"} {
			if !strings.Contains(frame.ANSI, want) {
				t.Errorf("visible table window omitted %q", want)
			}
		}
		for _, hidden := range []string{"row-080000", "row-079980", "row-079964", "row-099999"} {
			if strings.Contains(frame.ANSI, hidden) {
				t.Errorf("found off-window or filtered row %q", hidden)
			}
		}
	}
	budget := 15 * time.Millisecond
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "-race" && setting.Value == "true" {
				budget = 50 * time.Millisecond
				break
			}
		}
	}
	t.Logf("slowest uncached render took %s (budget %s)", slowest, budget)
	if slowest >= budget {
		t.Fatalf("uncached table render took %s, want under %s", slowest, budget)
	}
}

func TestPerfSubtreeCacheHits(t *testing.T) {
	tree := func() ui.ViewNode {
		return ui.ViewNode{Kind: ui.KindVStack, Children: []ui.ViewNode{
			{Kind: ui.KindText, ID: "first", CacheKey: 1, Props: map[string]any{"text": "first subtree"}},
			{Kind: ui.KindText, ID: "second", CacheKey: 2, Props: map[string]any{"text": "second subtree"}},
		}}
	}
	renderer, theme := ui.NewRenderer(8), ui.DefaultDarkTheme()
	first, _ := renderer.Render(tree(), theme, "", perfWidth, perfHeight)
	if renderer.Stats.CacheMisses != 2 || renderer.Stats.CacheHits != 0 {
		t.Fatalf("initial render did not fill both cache entries: %+v", renderer.Stats)
	}
	for range 5 {
		frame, _ := renderer.Render(tree(), theme, "", perfWidth, perfHeight)
		if renderer.Stats.CacheMisses != 0 || renderer.Stats.CacheHits != 2 {
			t.Fatalf("unchanged cache keys did not hit both entries: %+v", renderer.Stats)
		}
		if frame != first {
			t.Fatal("cache hits changed the rendered frame")
		}
	}
}

func TestPerfIdleZeroRedraws(t *testing.T) {
	builds := 0
	view := func(core.State) ui.ViewNode {
		builds++
		return ui.ViewNode{Kind: ui.KindText, ID: "idle", Props: map[string]any{"text": "idle"}}
	}
	shell := otea.New(core.NewEngine(nil), core.NewState(core.Frame{ID: "main"}), view, ui.DefaultDarkTheme())
	if shell.Init() != nil {
		t.Fatal("idle shell scheduled a command on startup")
	}
	shell.Update(tea.WindowSizeMsg{Width: perfWidth, Height: perfHeight})
	before := shell.Renders
	_, scheduled := shell.Update(core.EffectResultEvent{Slot: "background"})
	if scheduled == nil || shell.Renders != before {
		t.Fatal("background event should schedule a frame without rendering yet")
	}
	// Deliver the shell's scheduled frame tick once while dirty, then deliver
	// duplicates to exercise the same tick handler while the shell is clean.
	tick := scheduled()
	if _, cmd := shell.Update(tick); cmd != nil || shell.Renders != before+1 {
		t.Fatal("scheduled frame did not render exactly once")
	}
	baseline, buildBaseline, renderBaseline := shell.View().Content, builds, shell.Renders
	for range 1_000 {
		if _, cmd := shell.Update(tick); cmd != nil {
			t.Fatal("clean frame tick scheduled a command")
		}
		if shell.View().Content != baseline {
			t.Fatal("clean frame tick changed the cached view")
		}
	}
	if shell.Renders != renderBaseline || builds != buildBaseline {
		t.Fatalf("idle ticks caused %d redraws and %d view builds", shell.Renders-renderBaseline, builds-buildBaseline)
	}
}

func BenchmarkRender100kTable(b *testing.B) {
	node := newPerfTable().View("perf-table")
	renderer, theme := ui.NewRenderer(1), ui.DefaultDarkTheme()
	b.ReportAllocs()
	for b.Loop() {
		// Each frame gets an unused key so the benchmark measures actual drawing.
		node.CacheKey++
		renderer.Render(node, theme, node.ID, perfWidth, perfHeight)
	}
}

func BenchmarkResolveKey(b *testing.B) {
	keymap, err := core.NewKeymap([]core.Mode{{Name: "normal"}}, core.StandardBindings("normal"))
	if err != nil {
		b.Fatal(err)
	}
	query := core.KeyQuery{Mode: "normal", Key: "j", Chain: []core.ContextFrame{{Scope: "table"}, {Scope: "app"}}}
	if result := keymap.Resolve(query); result.Outcome != core.Matched || result.Command == nil || result.Command.ID != "motion.down" {
		b.Fatalf("unexpected benchmark resolution: %+v", result)
	}
	b.ReportAllocs()
	for b.Loop() {
		keymap.Resolve(query)
	}
}
