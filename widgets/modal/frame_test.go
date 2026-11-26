package modal_test

import (
	"strings"
	"testing"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
	"ofam.dev/ochazuke/widgets/modal"
)

func TestFrameDefaultsAndBuildOverlay(t *testing.T) {
	ref := &core.Ref{Kind: "item", ID: "i1"}
	f := modal.Frame{
		ID:    "query",
		Title: "Filter",
		Ref:   ref,
		Facts: map[string]any{"mode": "filter"},
	}.WithDefaults(modal.Frame{
		ID:        "fallback",
		Scope:     "query.scope",
		Title:     "Fallback",
		Tone:      "accent",
		WidthPct:  74,
		HeightPct: 60,
		Badges:    []ui.Badge{{Text: "default"}},
	})

	if f.ID != "query" || f.Scope != "query.scope" || f.Title != "Filter" || f.Tone != "accent" {
		t.Fatalf("unexpected Frame after WithDefaults: %+v", f)
	}
	if f.WidthPct != 74 || f.HeightPct != 60 || len(f.Badges) != 1 || f.Ref == nil || *f.Ref != *ref {
		t.Fatalf("unexpected Frame sizing/metadata: %+v", f)
	}

	body := ui.ViewNode{
		ID:        "query.body",
		Kind:      ui.KindText,
		Focusable: true,
		Facts:     map[string]any{"child": true},
		Props:     map[string]any{"text": "hello"},
	}
	ov := modal.BuildOverlay(f, body)
	if ov.ID != "modal.query" || ov.Kind != ui.KindOverlay || ov.Scope != "query.scope" || ov.Title != "Filter" || ov.Tone != "accent" {
		t.Fatalf("unexpected overlay header: %+v", ov)
	}
	if ov.Props["widthPct"] != 74 || ov.Props["heightPct"] != 60 {
		t.Fatalf("unexpected overlay size props: %+v", ov.Props)
	}
	if ov.Facts["mode"] != "filter" || ov.Facts["child"] != true {
		t.Fatalf("unexpected overlay facts: %+v", ov.Facts)
	}
	if ov.ByID("query.body") == nil {
		t.Fatal("expected overlay to contain query.body")
	}
}

func TestFitHeightOverlayShortAndTallContent(t *testing.T) {
	frame := modal.Frame{
		ID:        "zf",
		Scope:     "zf.scope",
		Title:     "Zero-Field Query",
		WidthPct:  72,
		HeightPct: 80, // FitHeightOverlay must clear this so content fits tightly.
	}

	// 1. Short body fits naturally without filling half the screen or wrapping in scroll.
	shortBody := ui.ViewNode{
		ID:        "zf.body",
		Kind:      ui.KindText,
		Focusable: true,
		Facts:     map[string]any{"stage": "filter"},
		Props:     map[string]any{"text": "line 1\nline 2\nline 3"},
	}
	shortOv := modal.FitHeightOverlay(frame, shortBody, 100, 30)
	if _, hasHP := shortOv.Props["heightPct"]; hasHP {
		t.Fatalf("expected FitHeightOverlay to omit heightPct, got %+v", shortOv.Props)
	}
	if shortOv.Box.FixedH != 0 {
		t.Fatalf("expected short content to avoid forced FixedH, got %d", shortOv.Box.FixedH)
	}
	if len(shortOv.Children) != 1 || shortOv.Children[0].ID != "zf.body" {
		t.Fatalf("expected direct body child for short content, got %+v", shortOv.Children)
	}
	solvedShort := ui.Measure(shortOv, ui.DefaultDarkTheme(), "zf.body", 100, 30)
	rShort := solvedShort.Rects[shortOv.ID]
	if rShort.H != 5 { // 3 text lines + 2 border rows
		t.Fatalf("short overlay height = %d, want 5", rShort.H)
	}

	// 2. Tall body on a short terminal caps cleanly inside viewport chrome and scrolls.
	lines := make([]string, 24)
	for i := range lines {
		lines[i] = "candidate row"
	}
	tallBody := ui.ViewNode{
		ID:        "zf.body",
		Kind:      ui.KindText,
		Focusable: true,
		Facts:     map[string]any{"stage": "filter"},
		Props:     map[string]any{"text": strings.Join(lines, "\n")},
	}
	tallOv := modal.FitHeightOverlay(frame, tallBody, 100, 12)
	if tallOv.Box.FixedH != 10 { // 12 - 2 leaves top/bottom chrome intact
		t.Fatalf("tall overlay FixedH = %d, want 10", tallOv.Box.FixedH)
	}
	if len(tallOv.Children) != 1 || tallOv.Children[0].Kind != ui.KindScroll {
		t.Fatalf("expected overflowing body wrapped in KindScroll, got %+v", tallOv.Children)
	}
	if tallOv.ByID("zf.body") == nil {
		t.Fatal("expected focusable zf.body to remain addressable via ByID")
	}
	if tallOv.Facts["stage"] != "filter" {
		t.Fatalf("expected child facts mirrored onto fitted overlay, got %+v", tallOv.Facts)
	}
	solvedTall := ui.Measure(tallOv, ui.DefaultDarkTheme(), "zf.body", 100, 12)
	rTall := solvedTall.Rects[tallOv.ID]
	if rTall.H != 10 || rTall.Y < 1 {
		t.Fatalf("tall overlay rect = %+v, want H=10 with top chrome margin Y>=1", rTall)
	}

	// 3. Zero dimensions still return a valid natural-height overlay.
	zeroOv := modal.FitHeightOverlay(frame, shortBody, 0, 0)
	if _, hasHP := zeroOv.Props["heightPct"]; hasHP || zeroOv.Box.FixedH != 0 {
		t.Fatalf("zero-dim FitHeightOverlay = props:%+v fixedH:%d", zeroOv.Props, zeroOv.Box.FixedH)
	}
}
