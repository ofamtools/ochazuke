package hints

import (
	"regexp"
	"strings"
	"testing"

	"ofam.dev/ochazuke/core"
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

func TestActionMetaAltKeyHandling(t *testing.T) {
	targets := []Target{
		{ID: "t1", Label: "a", Rect: layout.Rect{X: 0, Y: 0, W: 5, H: 1}},
		{ID: "t2", Label: "c", Rect: layout.Rect{X: 6, Y: 0, W: 5, H: 1}},
		{ID: "t3", Label: "h", Rect: layout.Rect{X: 12, Y: 0, W: 5, H: 1}},
		{ID: "t4", Label: "da", Rect: layout.Rect{X: 0, Y: 1, W: 5, H: 1}},
		{ID: "t5", Label: "ds", Rect: layout.Rect{X: 6, Y: 1, W: 5, H: 1}},
	}
	s := NewSession(targets)

	// Single-key alt+<rune> resolves to ActionMeta.
	res := s.HandleKey("alt+a")
	if res.Status != StatusMatched || res.Target == nil || res.Target.ID != "t1" || res.Action != ActionMeta {
		t.Fatalf("alt+a = %+v, want t1 ActionMeta", res)
	}

	// alt+c matches 'c' (does not cancel like ctrl+c).
	s.Reset()
	res = s.HandleKey("alt+c")
	if res.Status != StatusMatched || res.Target == nil || res.Target.ID != "t2" || res.Action != ActionMeta {
		t.Fatalf("alt+c = %+v, want t2 ActionMeta", res)
	}

	// alt+h matches 'h' (does not backspace).
	s.Reset()
	res = s.HandleKey("alt+h")
	if res.Status != StatusMatched || res.Target == nil || res.Target.ID != "t3" || res.Action != ActionMeta {
		t.Fatalf("alt+h = %+v, want t3 ActionMeta", res)
	}

	// Multi-char: lowercase prefix then alt+final resolves to ActionMeta.
	s.Reset()
	s.HandleKey("d")
	res = s.HandleKey("alt+a")
	if res.Status != StatusMatched || res.Target == nil || res.Target.ID != "t4" || res.Action != ActionMeta {
		t.Fatalf("d + alt+a = %+v, want t4 ActionMeta", res)
	}

	// Multi-char: alt+prefix carries pending override onto lowercase final; backspace clears it.
	s.Reset()
	res = s.HandleKey("alt+d")
	if res.Status != StatusActive || s.Prefix() != "d" || s.Facts()["action"] != string(ActionMeta) {
		t.Fatalf("alt+d narrow = %+v prefix=%q facts=%v", res, s.Prefix(), s.Facts())
	}
	res = s.HandleKey("s")
	if res.Status != StatusMatched || res.Target == nil || res.Target.ID != "t5" || res.Action != ActionMeta {
		t.Fatalf("alt+d + s = %+v, want t5 ActionMeta", res)
	}

	s.Reset()
	s.HandleKey("alt+d")
	s.HandleKey("backspace")
	res = s.HandleKey("a")
	if res.Status != StatusMatched || res.Action != ActionDefault {
		t.Fatalf("after backspace alt+d then a = %+v, want ActionDefault", res)
	}
}

func TestSmartCtrlHHandling(t *testing.T) {
	// DefaultAlphabet has 'h' at index 5 ("asdfgh...").
	var six []Target
	for i := 0; i < 6; i++ {
		six = append(six, Target{ID: string(rune('1' + i)), Rect: layout.Rect{X: i * 4, Y: 0, W: 3, H: 1}})
	}
	s := NewSession(six)
	if got := s.Targets()[5].Label; got != "h" {
		t.Fatalf("expected 6th target to have label 'h', got %q", got)
	}

	// When 'h' is a candidate and modifiers are enabled, ctrl+h resolves as ActionAlt for 'h'.
	res := s.HandleKey("ctrl+h")
	if res.Status != StatusMatched || res.Target == nil || res.Target.ID != "6" || res.Action != ActionAlt {
		t.Fatalf("ctrl+h matching 'h' = %+v, want target 6 ActionAlt", res)
	}

	// Multi-char candidate "dh": prefix "d" + ctrl+h resolves to "dh" with ActionAlt.
	multi := NewSession([]Target{
		{ID: "da", Label: "da", Rect: layout.Rect{X: 0, Y: 0, W: 2, H: 1}},
		{ID: "dh", Label: "dh", Rect: layout.Rect{X: 3, Y: 0, W: 2, H: 1}},
		{ID: "ha", Label: "ha", Rect: layout.Rect{X: 6, Y: 0, W: 2, H: 1}},
		{ID: "hs", Label: "hs", Rect: layout.Rect{X: 9, Y: 0, W: 2, H: 1}},
	})
	multi.HandleKey("d")
	res = multi.HandleKey("ctrl+h")
	if res.Status != StatusMatched || res.Target == nil || res.Target.ID != "dh" || res.Action != ActionAlt {
		t.Fatalf("d + ctrl+h = %+v, want dh ActionAlt", res)
	}

	// Multi-char prefix starting with 'h': ctrl+h narrows to "h" with pending ActionAlt.
	multi.Reset()
	res = multi.HandleKey("ctrl+h")
	if res.Status != StatusActive || multi.Prefix() != "h" || len(multi.Remaining()) != 2 {
		t.Fatalf("ctrl+h narrow = %+v prefix=%q rem=%d", res, multi.Prefix(), len(multi.Remaining()))
	}
	res = multi.HandleKey("s")
	if res.Status != StatusMatched || res.Target == nil || res.Target.ID != "hs" || res.Action != ActionAlt {
		t.Fatalf("ctrl+h + s = %+v, want hs ActionAlt", res)
	}

	// Fallback to backspace when prefix+"h" matches no candidate.
	noH := NewSession([]Target{
		{ID: "p1", Label: "da", Rect: layout.Rect{X: 0, Y: 0, W: 2, H: 1}},
		{ID: "p2", Label: "ds", Rect: layout.Rect{X: 3, Y: 0, W: 2, H: 1}},
	})
	noH.HandleKey("d")
	res = noH.HandleKey("ctrl+h")
	if res.Status != StatusActive || noH.Prefix() != "" || len(noH.Remaining()) != 2 {
		t.Fatalf("ctrl+h backspace fallback = %+v prefix=%q rem=%d", res, noH.Prefix(), len(noH.Remaining()))
	}

	// Modifiers disabled: ctrl+h falls back to backspace even when 'h' is a candidate.
	noMod := NewSession([]Target{
		{ID: "h", Label: "h", Rect: layout.Rect{X: 0, Y: 0, W: 2, H: 1}},
		{ID: "dh", Label: "dh", Rect: layout.Rect{X: 3, Y: 0, W: 2, H: 1}},
		{ID: "da", Label: "da", Rect: layout.Rect{X: 6, Y: 0, W: 2, H: 1}},
	}, WithModifiers(false))
	noMod.HandleKey("d")
	res = noMod.HandleKey("ctrl+h")
	if res.Status != StatusActive || noMod.Prefix() != "" || !noMod.Active() {
		t.Fatalf("ctrl+h with modifiers disabled = %+v prefix=%q active=%v", res, noMod.Prefix(), noMod.Active())
	}
}

func TestActiveActionCyclingAndModifierOverride(t *testing.T) {
	targets := []Target{
		{ID: "t1", Label: "a", Rect: layout.Rect{X: 0, Y: 0, W: 4, H: 1}},
		{ID: "t2", Label: "da", Rect: layout.Rect{X: 5, Y: 0, W: 4, H: 1}},
		{ID: "t3", Label: "ds", Rect: layout.Rect{X: 10, Y: 0, W: 4, H: 1}},
	}
	s := NewSession(targets, WithDefaultAction(ActionSecondary))
	if s.ActiveAction() != ActionSecondary {
		t.Fatalf("ActiveAction = %q, want %q", s.ActiveAction(), ActionSecondary)
	}

	// Lowercase match resolves to configured default/active action.
	res := s.HandleKey("a")
	if res.Status != StatusMatched || res.Action != ActionSecondary {
		t.Fatalf("lowercase match = %+v, want ActionSecondary", res)
	}

	// Explicit modifiers still override active action when enabled.
	s.Reset()
	if s.ActiveAction() != ActionSecondary {
		t.Fatalf("Reset ActiveAction = %q, want %q", s.ActiveAction(), ActionSecondary)
	}
	res = s.HandleKey("alt+a")
	if res.Status != StatusMatched || res.Action != ActionMeta {
		t.Fatalf("alt+a override = %+v, want ActionMeta", res)
	}
	s.Reset()
	res = s.HandleKey("ctrl+a")
	if res.Status != StatusMatched || res.Action != ActionAlt {
		t.Fatalf("ctrl+a override = %+v, want ActionAlt", res)
	}

	// SetAction with custom host action + mid-prefix change.
	s.Reset()
	s.HandleKey("d")
	s.SetAction(Action("reveal"))
	if s.ActiveAction() != Action("reveal") {
		t.Fatalf("ActiveAction = %q, want reveal", s.ActiveAction())
	}
	res = s.HandleKey("s")
	if res.Status != StatusMatched || res.Target == nil || res.Target.ID != "t3" || res.Action != Action("reveal") {
		t.Fatalf("custom action match = %+v, want t3 reveal", res)
	}

	// CycleAction with explicit order (including unseen current action and wrap-around).
	s.Reset()
	order := []Action{ActionDefault, ActionSecondary, ActionAlt, ActionMeta, Action("extra")}
	if got := s.CycleAction(order); got != ActionAlt {
		t.Fatalf("CycleAction from secondary = %q, want alt", got)
	}
	if got := s.CycleAction(order); got != ActionMeta {
		t.Fatalf("CycleAction = %q, want meta", got)
	}
	if got := s.CycleAction(order); got != Action("extra") {
		t.Fatalf("CycleAction = %q, want extra", got)
	}
	if got := s.CycleAction(order); got != ActionDefault {
		t.Fatalf("CycleAction wrap = %q, want default", got)
	}
	s.SetAction(Action("unlisted"))
	if got := s.CycleAction(order); got != ActionDefault {
		t.Fatalf("CycleAction from unlisted = %q, want default", got)
	}

	// CycleAction(nil) uses legend order when configured, or default 4-action order.
	legendSession := NewSession(targets, WithActionLegend([]ActionLegend{
		{Key: "a-z", Label: "first", Action: Action("first")},
		{Key: "A-Z", Label: "second", Action: Action("second")},
		{Key: "esc", Label: "cancel"},
	}))
	if got := legendSession.CycleAction(nil); got != Action("first") {
		t.Fatalf("CycleAction(nil) from default = %q, want first", got)
	}
	if got := legendSession.CycleAction(nil); got != Action("second") {
		t.Fatalf("CycleAction(nil) = %q, want second", got)
	}
	if got := legendSession.CycleAction(nil); got != Action("first") {
		t.Fatalf("CycleAction(nil) wrap = %q, want first", got)
	}

	// HandleCommand supports hints.cycle_action and hints.set_action.
	cmdRes, ok := legendSession.HandleCommand(core.Command{ID: "hints.cycle_action"})
	if !ok || cmdRes.Status != StatusActive || legendSession.ActiveAction() != Action("second") {
		t.Fatalf("hints.cycle_action = %+v ok=%v act=%q", cmdRes, ok, legendSession.ActiveAction())
	}
	cmdRes, ok = legendSession.HandleCommand(core.Command{ID: "hints.set_action", Args: map[string]any{"action": "meta"}})
	if !ok || cmdRes.Status != StatusActive || legendSession.ActiveAction() != ActionMeta {
		t.Fatalf("hints.set_action = %+v ok=%v act=%q", cmdRes, ok, legendSession.ActiveAction())
	}

	// WithModifiers(false) disables Shift/ctrl/alt overrides so all resolve to ActiveAction.
	noMod := NewSession(targets, WithDefaultAction(ActionMeta), WithModifiers(false))
	if noMod.ModifiersEnabled() {
		t.Fatal("expected ModifiersEnabled() == false")
	}
	for _, key := range []string{"a", "A", "ctrl+a", "alt+a"} {
		noMod.Reset()
		r := noMod.HandleKey(key)
		if r.Status != StatusMatched || r.Action != ActionMeta {
			t.Fatalf("key %q with modifiers disabled = %+v, want ActionMeta", key, r)
		}
	}
}

func TestHostActionLegendAndFacts(t *testing.T) {
	targets := []Target{
		{ID: "t1", Label: "a", Rect: layout.Rect{X: 0, Y: 0, W: 4, H: 1}},
		{ID: "t2", Label: "da", Rect: layout.Rect{X: 5, Y: 0, W: 4, H: 1}},
		{ID: "t3", Label: "ds", Rect: layout.Rect{X: 10, Y: 0, W: 4, H: 1}},
	}

	// Default legend omitted: preserves backwards-compatible text and exposes truthful Facts.
	def := NewSession(targets)
	defNode := def.View("bar")
	defText, _ := defNode.Props["text"].(string)
	if defText != "HINTS _  (3)  [A-Z] secondary  [ctrl] alt  [esc] cancel" {
		t.Fatalf("default View text = %q", defText)
	}
	if defNode.Facts["active"] != true || defNode.Facts["prefix"] != "" ||
		defNode.Facts["remaining"] != 3 || defNode.Facts["total"] != 3 ||
		defNode.Facts["action"] != string(ActionDefault) {
		t.Fatalf("unexpected default Facts: %#v", defNode.Facts)
	}

	// Host-configured ActionLegend renders truthful items and highlights the active action.
	legend := []ActionLegend{
		{Key: "a-z", Label: "open", Action: ActionDefault},
		{Key: "A-Z", Label: "yank", Action: ActionSecondary},
		{Key: "ctrl", Label: "edit", Action: ActionAlt},
		{Key: "alt", Label: "inspect", Action: ActionMeta},
		{Key: "esc", Label: "cancel"},
	}
	s := NewSession(targets, WithActionLegend(legend))
	v1 := s.View("bar")
	text1, _ := v1.Props["text"].(string)
	if !strings.Contains(text1, "● [a-z] open") || !strings.Contains(text1, "[alt] inspect") || !strings.Contains(text1, "[esc] cancel") {
		t.Fatalf("host legend v1 = %q", text1)
	}
	if strings.Contains(text1, "[A-Z] secondary") {
		t.Fatalf("host legend should replace hardcoded default text: %q", text1)
	}

	// Cycling action updates both Facts()["action"], rendered highlight, and CacheKey.
	s.CycleAction(nil)
	v2 := s.View("bar")
	text2, _ := v2.Props["text"].(string)
	if !strings.Contains(text2, "● [A-Z] yank") || strings.Contains(text2, "● [a-z] open") {
		t.Fatalf("host legend after cycle = %q", text2)
	}
	if v2.CacheKey == v1.CacheKey {
		t.Fatalf("expected CacheKey to change after CycleAction (%d == %d)", v2.CacheKey, v1.CacheKey)
	}
	if v2.Facts["action"] != string(ActionSecondary) {
		t.Fatalf("v2 Facts[action] = %v, want secondary", v2.Facts["action"])
	}

	// Render through ui.Render to verify rune-safe ToneSpans with multi-byte '●'.
	fr, _ := ui.Render(v2, ui.DefaultDarkTheme(), "", 100, 1)
	if !strings.Contains(stripANSI(fr.ANSI), "● [A-Z] yank") {
		t.Fatalf("rendered legend strip = %q", stripANSI(fr.ANSI))
	}

	// Active action not present in legend still shows cleanly as a badge.
	s.SetAction(Action("custom"))
	v3 := s.View("bar")
	text3, _ := v3.Props["text"].(string)
	if !strings.Contains(text3, "[custom]") {
		t.Fatalf("expected [custom] pill in legend = %q", text3)
	}

	// Narrowing and matching update Facts truthfully.
	s.HandleKey("d")
	facts := s.Facts()
	if facts["active"] != true || facts["prefix"] != "d" || facts["remaining"] != 2 || facts["total"] != 3 || facts["action"] != "custom" {
		t.Fatalf("narrowed facts = %#v", facts)
	}
	ov := s.ViewOverlay("ov")
	if ov.Facts["prefix"] != "d" || ov.Facts["action"] != "custom" || ov.Facts["remaining"] != 2 {
		t.Fatalf("ViewOverlay facts = %#v", ov.Facts)
	}

	s.HandleKey("alt+a")
	matchedFacts := s.Facts()
	if matchedFacts["active"] != false || matchedFacts["remaining"] != 0 || matchedFacts["action"] != string(ActionMeta) {
		t.Fatalf("matched facts = %#v", matchedFacts)
	}

	// Nil session Facts is safe.
	var nilSession *Session
	nf := nilSession.Facts()
	if nf["active"] != false || nf["total"] != 0 || nf["action"] != string(ActionDefault) {
		t.Fatalf("nil session facts = %#v", nf)
	}
}
