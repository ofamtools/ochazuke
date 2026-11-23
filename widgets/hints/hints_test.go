package hints

import (
	"regexp"
	"strings"
	"testing"

	"ofam.dev/ochazuke/layout"
	"ofam.dev/ochazuke/ui"
)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;:]*m`)

func stripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

func TestGenerateLabelsPrefixFreeAndDeterministic(t *testing.T) {
	if strings.ContainsRune(DefaultAlphabet, 'q') {
		t.Fatalf("DefaultAlphabet must exclude 'q': %q", DefaultAlphabet)
	}

	for _, tc := range []struct {
		name     string
		count    int
		alphabet string
	}{
		{"zero", 0, DefaultAlphabet},
		{"one", 1, DefaultAlphabet},
		{"exact-alphabet", len(DefaultAlphabet), DefaultAlphabet},
		{"just-over", len(DefaultAlphabet) + 1, DefaultAlphabet},
		{"fifty", 50, DefaultAlphabet},
		{"hundred", 100, DefaultAlphabet},
		{"small-alpha-two-tier", 10, "asdf"},
		{"small-alpha-three-tier", 25, "abc"},
		{"invalid-alpha-fallback", 5, "x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := GenerateLabels(tc.count, tc.alphabet)
			got2 := GenerateLabels(tc.count, tc.alphabet)
			if len(got) != tc.count {
				t.Fatalf("len = %d, want %d", len(got), tc.count)
			}
			for i := range got {
				if got[i] != got2[i] {
					t.Fatalf("non-deterministic at %d: %q vs %q", i, got[i], got2[i])
				}
				if got[i] == "" {
					t.Fatalf("empty label at %d", i)
				}
			}
			// Verify strict prefix-free and uniqueness.
			for i := 0; i < len(got); i++ {
				for j := 0; j < len(got); j++ {
					if i == j {
						continue
					}
					if strings.HasPrefix(got[j], got[i]) {
						t.Fatalf("label %q is a prefix of %q (all: %v)", got[i], got[j], got)
					}
				}
			}
		})
	}

	// Home-row single-char labels stay first for n=6 over "asdf".
	small := GenerateLabels(6, "asdf")
	if got := strings.Join(small, ","); got != "a,s,d,fa,fs,fd" {
		t.Fatalf("small labels = %q, want a,s,d,fa,fs,fd", got)
	}
}

func TestSessionTypingModifiersBackspaceAndCancel(t *testing.T) {
	targets := []Target{
		{ID: "p1", Rect: layout.Rect{X: 0, Y: 0, W: 10, H: 4}, Kind: "url", Value: "https://a.example"},
		{ID: "p2", Rect: layout.Rect{X: 12, Y: 0, W: 10, H: 4}, Kind: "url", Value: "https://b.example"},
		{ID: "p3", Rect: layout.Rect{X: 0, Y: 5, W: 10, H: 4}, Kind: "file", Value: "main.go:12"},
		{ID: "p4", Rect: layout.Rect{X: 12, Y: 5, W: 10, H: 4}, Kind: "file", Value: "README.md"},
		{ID: "p5", Rect: layout.Rect{X: 24, Y: 5, W: 10, H: 4}, Kind: "pr", Value: "org/repo#42"},
	}
	// With alphabet "asd" (3 runes) and 5 targets: labels are a, s, da, ds, dd.
	s := NewSession(targets, WithAlphabet("asd"))
	if !s.Active() || len(s.Remaining()) != 5 {
		t.Fatalf("initial session: active=%v rem=%d", s.Active(), len(s.Remaining()))
	}

	// Non-matching rune leaves prefix unchanged.
	res := s.HandleKey("z")
	if res.Status != StatusNoMatch || s.Prefix() != "" || !s.Active() {
		t.Fatalf("nomatch: %+v prefix=%q", res, s.Prefix())
	}

	// Narrow with 'd' -> 3 remaining (da, ds, dd).
	res = s.HandleKey("d")
	if res.Status != StatusActive || s.Prefix() != "d" || len(s.Remaining()) != 3 {
		t.Fatalf("narrow 'd': %+v prefix=%q rem=%d", res, s.Prefix(), len(s.Remaining()))
	}

	// Backspace restores all 5 targets.
	res = s.HandleKey("backspace")
	if res.Status != StatusActive || s.Prefix() != "" || len(s.Remaining()) != 5 {
		t.Fatalf("backspace: %+v prefix=%q rem=%d", res, s.Prefix(), len(s.Remaining()))
	}

	// Complete multi-char label with default action.
	s.HandleKey("d")
	res = s.HandleKey("s")
	if res.Status != StatusMatched || res.Target == nil || res.Target.ID != "p4" || res.Action != ActionDefault {
		t.Fatalf("match ds: %+v", res)
	}
	if s.Active() {
		t.Fatal("session should deactivate after match")
	}

	// Reset and test uppercase secondary action (Shift).
	s.Reset()
	res = s.HandleKey("A")
	if res.Status != StatusMatched || res.Target == nil || res.Target.ID != "p1" || res.Action != ActionSecondary {
		t.Fatalf("uppercase match A: %+v", res)
	}

	// Reset and test ctrl modifier action on multi-char label.
	s.Reset()
	s.HandleKey("d")
	res = s.HandleKey("ctrl+a")
	if res.Status != StatusMatched || res.Target == nil || res.Target.ID != "p3" || res.Action != ActionAlt {
		t.Fatalf("ctrl match d+ctrl+a: %+v", res)
	}

	// Reset and cancel with 'q' (excluded from alphabet).
	s.Reset()
	res = s.HandleKey("q")
	if res.Status != StatusCancelled || s.Active() {
		t.Fatalf("q cancel: %+v active=%v", res, s.Active())
	}

	// Reset and cancel with 'esc'.
	s.Reset()
	res = s.HandleKey("esc")
	if res.Status != StatusCancelled || s.Active() {
		t.Fatalf("esc cancel: %+v active=%v", res, s.Active())
	}
}

func TestTargetsFromFocusOrderAndOverlayRender(t *testing.T) {
	root := ui.ViewNode{
		Kind: ui.KindSplit,
		Children: []ui.ViewNode{
			{Kind: ui.KindPanel, ID: "left", Title: "Left", Focusable: true, Children: []ui.ViewNode{
				{Kind: ui.KindText, Props: map[string]any{"text": "hello left"}},
			}},
			{Kind: ui.KindPanel, ID: "right", Title: "Right", Focusable: true, Children: []ui.ViewNode{
				{Kind: ui.KindText, Props: map[string]any{"text": "hello right"}},
			}},
		},
	}
	frame, solved := ui.Render(root, ui.DefaultDarkTheme(), "left", 30, 5)
	targets := TargetsFromFocusOrder(solved)
	if len(targets) != 2 {
		t.Fatalf("TargetsFromFocusOrder len = %d, want 2 (%+v)", len(targets), targets)
	}
	if targets[0].ID != "left" || targets[0].Label != "a" || targets[1].ID != "right" || targets[1].Label != "s" {
		t.Fatalf("unexpected focus targets: %+v", targets)
	}

	s := NewSession(targets)
	composited := RenderOverlay(frame.ANSI, 30, 5, s, OverlayStyleFromTheme(ui.DefaultDarkTheme()))
	plain := stripANSI(composited)
	lines := strings.Split(plain, "\n")
	if !strings.HasPrefix(lines[0], "[a]") {
		t.Fatalf("expected [a] badge at left panel origin, row 0 = %q", lines[0])
	}
	if !strings.Contains(lines[0], "[s]") {
		t.Fatalf("expected [s] badge at right panel origin, row 0 = %q", lines[0])
	}

	// View and ViewOverlay render cleanly through ui.Render.
	stripFrame, _ := ui.Render(s.View("hint-bar"), ui.DefaultDarkTheme(), "", 60, 1)
	if !strings.Contains(stripANSI(stripFrame.ANSI), "HINTS _") {
		t.Fatalf("hint strip render = %q", stripANSI(stripFrame.ANSI))
	}
	ovFrame, _ := ui.Render(s.ViewOverlay("hint-ov"), ui.DefaultDarkTheme(), "", 40, 8)
	ovPlain := stripANSI(ovFrame.ANSI)
	if !strings.Contains(ovPlain, "[a] focus · left") || !strings.Contains(ovPlain, "[s] focus · right") {
		t.Fatalf("hint overlay render:\n%s", ovPlain)
	}
}
