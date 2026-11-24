package traits_test

import (
	"encoding/json"
	"slices"
	"testing"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/traits"
)

func TestFilterCompileAndMatch(t *testing.T) {
	t.Run("empty query matches everything", func(t *testing.T) {
		for _, mode := range []traits.MatchMode{"", traits.MatchSubstring, traits.MatchFuzzy, traits.MatchRegex} {
			m, err := traits.CompileFilter("", mode, traits.FilterConfig{})
			if err != "" {
				t.Fatalf("mode %q empty query err = %q", mode, err)
			}
			if !m.Match("anything") || !m.Match("") || !m.MatchAny() {
				t.Fatalf("mode %q empty query should match all", mode)
			}
			if hit := m.MatchRunes("hello"); slices.Contains(hit, true) || len(hit) != 5 {
				t.Fatalf("mode %q empty MatchRunes = %v", mode, hit)
			}
		}
	})

	t.Run("substring case insensitive and sensitive", func(t *testing.T) {
		ci, err := traits.CompileFilter("THEME", traits.MatchSubstring, traits.FilterConfig{})
		if err != "" || !ci.Match("myTheme.go") || ci.Match("other.go") {
			t.Fatalf("case-insensitive substring failed: err=%q", err)
		}
		if !ci.MatchAny("foo", "myTheme.go") || ci.MatchAny("foo", "bar") {
			t.Fatal("MatchAny substring failed")
		}

		cs, err := traits.CompileFilter("Theme", traits.MatchSubstring, traits.FilterConfig{CaseSensitive: true})
		if err != "" || !cs.Match("myTheme.go") || cs.Match("mytheme.go") {
			t.Fatalf("case-sensitive substring failed: err=%q", err)
		}
	})

	t.Run("fuzzy subsequence", func(t *testing.T) {
		m, err := traits.CompileFilter("rdr", traits.MatchFuzzy, traits.FilterConfig{})
		if err != "" || !m.Match("render.go") || m.Match("readme.md") {
			t.Fatalf("fuzzy failed: err=%q", err)
		}
		cs, _ := traits.CompileFilter("RG", traits.MatchFuzzy, traits.FilterConfig{CaseSensitive: true})
		if !cs.Match("RenderGo") || cs.Match("render.go") {
			t.Fatal("case-sensitive fuzzy failed")
		}
	})

	t.Run("regex valid and invalid", func(t *testing.T) {
		m, err := traits.CompileFilter(`^m.*\.go$`, traits.MatchRegex, traits.FilterConfig{})
		if err != "" || !m.Match("Main.go") || m.Match("render.go") {
			t.Fatalf("case-insensitive regex failed: err=%q", err)
		}
		cs, err := traits.CompileFilter(`^m.*\.go$`, traits.MatchRegex, traits.FilterConfig{CaseSensitive: true})
		if err != "" || !cs.Match("main.go") || cs.Match("Main.go") {
			t.Fatalf("case-sensitive regex failed: err=%q", err)
		}

		bad, err := traits.CompileFilter("([", traits.MatchRegex, traits.FilterConfig{})
		if err == "" {
			t.Fatal("expected error for invalid regex")
		}
		if bad.Match("render.go") {
			t.Fatal("invalid regex should not match unrelated text")
		}
		if !bad.Match("literal ([ here") {
			t.Fatal("invalid regex should fall back to literal substring matching")
		}
		if hit := bad.MatchRunes("literal ([ here"); slices.Contains(hit, true) {
			t.Fatalf("invalid regex MatchRunes should be all false, got %v", hit)
		}
	})
}

func TestFilterMatchRunesUnicode(t *testing.T) {
	t.Run("substring unicode", func(t *testing.T) {
		m, _ := traits.CompileFilter("茶", traits.MatchSubstring, traits.FilterConfig{})
		got := m.MatchRunes("お茶漬け")
		want := []bool{false, true, false, false}
		if !slices.Equal(got, want) {
			t.Fatalf("substring unicode = %v, want %v", got, want)
		}
	})

	t.Run("fuzzy unicode", func(t *testing.T) {
		m, _ := traits.CompileFilter("αδ", traits.MatchFuzzy, traits.FilterConfig{})
		got := m.MatchRunes("ΑβγΔε")
		want := []bool{true, false, false, true, false}
		if !slices.Equal(got, want) {
			t.Fatalf("fuzzy unicode = %v, want %v", got, want)
		}
		noHit := m.MatchRunes("Αβγ")
		if slices.Contains(noHit, true) {
			t.Fatalf("partial fuzzy match should highlight nothing: %v", noHit)
		}
	})

	t.Run("regex unicode", func(t *testing.T) {
		m, _ := traits.CompileFilter("β.Δ", traits.MatchRegex, traits.FilterConfig{})
		got := m.MatchRunes("αβγδε")
		want := []bool{false, true, true, true, false}
		if !slices.Equal(got, want) {
			t.Fatalf("regex unicode = %v, want %v", got, want)
		}
	})

	t.Run("case-folding length change does not panic", func(t *testing.T) {
		m, _ := traits.CompileFilter("ab", traits.MatchSubstring, traits.FilterConfig{})
		got := m.MatchRunes("İab") // U+0130 expands to 2 runes under strings.ToLower
		want := []bool{false, true, true}
		if !slices.Equal(got, want) {
			t.Fatalf("expanding rune MatchRunes = %v, want %v", got, want)
		}
	})
}

func TestFilterCycleAndFactsAndJSON(t *testing.T) {
	// Default cycle: substring -> fuzzy -> regex -> substring.
	if got := traits.CycleMatchMode("", traits.FilterConfig{}); got != traits.MatchFuzzy {
		t.Fatalf("cycle from empty = %q, want fuzzy", got)
	}
	if got := traits.CycleMatchMode(traits.MatchFuzzy, traits.FilterConfig{}); got != traits.MatchRegex {
		t.Fatalf("cycle from fuzzy = %q, want regex", got)
	}
	if got := traits.CycleMatchMode(traits.MatchRegex, traits.FilterConfig{}); got != traits.MatchSubstring {
		t.Fatalf("cycle from regex = %q, want substring", got)
	}

	// Custom mode list.
	custom := traits.FilterConfig{
		DefaultMode: traits.MatchFuzzy,
		Modes:       []traits.MatchMode{traits.MatchFuzzy, traits.MatchRegex},
	}
	if got := traits.CycleMatchMode(traits.MatchFuzzy, custom); got != traits.MatchRegex {
		t.Fatalf("custom cycle 1 = %q, want regex", got)
	}
	if got := traits.CycleMatchMode(traits.MatchRegex, custom); got != traits.MatchFuzzy {
		t.Fatalf("custom cycle 2 = %q, want fuzzy", got)
	}
	if got := traits.CycleMatchMode(traits.MatchSubstring, custom); got != traits.MatchFuzzy {
		t.Fatalf("unknown current mode falls back to first = %q, want fuzzy", got)
	}

	// Facts.
	facts := traits.FilterFacts(traits.FilterState{
		Query:  "abc",
		Active: true,
		Mode:   traits.MatchRegex,
		Err:    "bad",
	}, traits.FilterConfig{})
	if facts["filtering"] != true || facts["query"] != "abc" || facts["queryEmpty"] != false ||
		facts["hasFilter"] != true || facts["matchMode"] != "regex" || facts["filterError"] != "bad" {
		t.Fatalf("FilterFacts = %+v", facts)
	}
	emptyFacts := traits.FilterFacts(traits.FilterState{}, traits.FilterConfig{DefaultMode: traits.MatchFuzzy})
	if emptyFacts["filtering"] != false || emptyFacts["queryEmpty"] != true ||
		emptyFacts["hasFilter"] != false || emptyFacts["matchMode"] != "fuzzy" || emptyFacts["filterError"] != "" {
		t.Fatalf("empty FilterFacts = %+v", emptyFacts)
	}

	// JSON round-trip.
	in := traits.FilterConfig{
		DefaultMode:   traits.MatchFuzzy,
		Modes:         []traits.MatchMode{traits.MatchSubstring, traits.MatchFuzzy},
		CaseSensitive: true,
		StartActive:   true,
	}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal FilterConfig: %v", err)
	}
	var out traits.FilterConfig
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal FilterConfig: %v", err)
	}
	if out.DefaultMode != in.DefaultMode || !slices.Equal(out.Modes, in.Modes) ||
		out.CaseSensitive != in.CaseSensitive || out.StartActive != in.StartActive {
		t.Fatalf("FilterConfig round-trip mismatch: got %+v, want %+v", out, in)
	}
}

func TestFoldHandlePruneFactsAndJSON(t *testing.T) {
	cfg := traits.FoldConfig{LeafFoldsParent: true}
	all := []string{"a", "a2", "b"}
	parentTarget := traits.FoldTarget{ID: "a", HasChildren: true}
	leafTarget := traits.FoldTarget{ID: "a1", ParentID: "a", HasChildren: false}
	rootLeaf := traits.FoldTarget{ID: "c", HasChildren: false}

	// Unknown command is unhandled.
	st := traits.FoldState{}
	if _, _, changed, handled := traits.FoldHandle(st, cfg, parentTarget, all, core.Command{ID: "nope"}); changed || handled {
		t.Fatalf("unknown cmd: changed=%v handled=%v", changed, handled)
	}

	// Toggle parent folds and unfolds without mutating prior state.
	st1, focus, changed, handled := traits.FoldHandle(st, cfg, parentTarget, all, core.Command{ID: traits.FoldCmdToggle})
	if !handled || !changed || focus != "" || !st1.Collapsed["a"] {
		t.Fatalf("toggle fold: st=%+v focus=%q changed=%v handled=%v", st1, focus, changed, handled)
	}
	if len(st.Collapsed) != 0 {
		t.Fatal("FoldHandle mutated input state")
	}

	// Close on already-collapsed parent is a no-op.
	stSame, _, changed, handled := traits.FoldHandle(st1, cfg, parentTarget, all, core.Command{ID: traits.FoldCmdClose})
	if !handled || changed || !stSame.Collapsed["a"] {
		t.Fatalf("close already folded: st=%+v changed=%v handled=%v", stSame, changed, handled)
	}

	// Open unfolds parent.
	st2, focus, changed, handled := traits.FoldHandle(st1, cfg, parentTarget, all, core.Command{ID: traits.FoldCmdOpen})
	if !handled || !changed || focus != "" || st2.Collapsed["a"] {
		t.Fatalf("open: st=%+v focus=%q changed=%v handled=%v", st2, focus, changed, handled)
	}

	// LeafFoldsParent folds parent and returns parent focusID.
	stLeaf, focus, changed, handled := traits.FoldHandle(st2, cfg, leafTarget, all, core.Command{ID: traits.FoldCmdToggle})
	if !handled || !changed || focus != "a" || !stLeaf.Collapsed["a"] {
		t.Fatalf("leaf toggle: st=%+v focus=%q changed=%v handled=%v", stLeaf, focus, changed, handled)
	}

	// LeafFoldsParent=false ignores leaf toggle/close.
	stNoLeaf, focus, changed, handled := traits.FoldHandle(st2, traits.FoldConfig{}, leafTarget, all, core.Command{ID: traits.FoldCmdClose})
	if !handled || changed || focus != "" || len(stNoLeaf.Collapsed) != 0 {
		t.Fatalf("leaf with LeafFoldsParent=false: st=%+v focus=%q changed=%v handled=%v", stNoLeaf, focus, changed, handled)
	}

	// Root leaf has no parent to fold.
	if _, focus, changed, handled := traits.FoldHandle(st2, cfg, rootLeaf, all, core.Command{ID: traits.FoldCmdClose}); !handled || changed || focus != "" {
		t.Fatalf("root leaf close: focus=%q changed=%v handled=%v", focus, changed, handled)
	}

	// CloseAll and OpenAll.
	stAll, _, changed, handled := traits.FoldHandle(st2, cfg, parentTarget, all, core.Command{ID: traits.FoldCmdCloseAll})
	if !handled || !changed || len(stAll.Collapsed) != 3 || !stAll.Collapsed["a2"] || !stAll.Collapsed["b"] {
		t.Fatalf("close all: st=%+v changed=%v handled=%v", stAll, changed, handled)
	}
	// Repeating CloseAll reports changed=false.
	if _, _, changed, handled := traits.FoldHandle(stAll, cfg, parentTarget, all, core.Command{ID: traits.FoldCmdCloseAll}); !handled || changed {
		t.Fatalf("repeat close all: changed=%v handled=%v", changed, handled)
	}

	// FoldPrune drops vanished IDs without mutating input.
	pruned := traits.FoldPrune(stAll, []string{"a", "c"})
	if len(pruned.Collapsed) != 1 || !pruned.Collapsed["a"] || pruned.Collapsed["b"] {
		t.Fatalf("FoldPrune: %+v", pruned)
	}
	if len(stAll.Collapsed) != 3 {
		t.Fatal("FoldPrune mutated input state")
	}

	// FoldFacts.
	fParent := traits.FoldFacts(stAll, parentTarget)
	if fParent["folded"] != true || fParent["hasChildren"] != true || fParent["foldCount"] != 3 {
		t.Fatalf("FoldFacts parent: %+v", fParent)
	}
	fLeaf := traits.FoldFacts(stAll, leafTarget)
	if fLeaf["folded"] != false || fLeaf["hasChildren"] != false || fLeaf["foldCount"] != 3 {
		t.Fatalf("FoldFacts leaf: %+v", fLeaf)
	}

	// OpenAll clears all collapsed entries.
	stOpen, _, changed, handled := traits.FoldHandle(stAll, cfg, parentTarget, all, core.Command{ID: traits.FoldCmdOpenAll})
	if !handled || !changed || len(stOpen.Collapsed) != 0 {
		t.Fatalf("open all: st=%+v changed=%v handled=%v", stOpen, changed, handled)
	}
	if _, _, changed, handled := traits.FoldHandle(stOpen, cfg, parentTarget, all, core.Command{ID: traits.FoldCmdOpenAll}); !handled || changed {
		t.Fatalf("repeat open all: changed=%v handled=%v", changed, handled)
	}

	// JSON round-trip.
	in := traits.FoldConfig{LeafFoldsParent: true, DefaultFolded: true}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal FoldConfig: %v", err)
	}
	var out traits.FoldConfig
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal FoldConfig: %v", err)
	}
	if out != in {
		t.Fatalf("FoldConfig round-trip mismatch: got %+v, want %+v", out, in)
	}
}
