package trie_test

import (
	"reflect"
	"sync"
	"testing"

	"ofam.dev/ochazuke/trie"
)

type testBinding struct {
	Command string
	Scope   string
	Mode    string
	Hint    string
}

func ptr[T any](v T) *T {
	return &v
}

func assertResult[T comparable](t *testing.T, label string, got trie.Result[T], wantKind trie.MatchKind, wantVal *T, wantNext map[string]T) {
	t.Helper()
	if got.Kind != wantKind {
		t.Fatalf("%s: Kind = %v (%d), want %v (%d)", label, got.Kind, got.Kind, wantKind, wantKind)
	}
	if wantVal == nil {
		if got.Value != nil {
			t.Fatalf("%s: Value = %v, want nil", label, *got.Value)
		}
	} else {
		if got.Value == nil {
			t.Fatalf("%s: Value = nil, want %v", label, *wantVal)
		}
		if *got.Value != *wantVal {
			t.Fatalf("%s: *Value = %v, want %v", label, *got.Value, *wantVal)
		}
	}
	if len(wantNext) == 0 {
		if got.NextKeys != nil {
			t.Fatalf("%s: NextKeys = %v, want nil", label, got.NextKeys)
		}
		return
	}
	if got.NextKeys == nil {
		t.Fatalf("%s: NextKeys = nil, want %v", label, wantNext)
	}
	if len(got.NextKeys) != len(wantNext) {
		t.Fatalf("%s: len(NextKeys) = %d (%v), want %d (%v)", label, len(got.NextKeys), got.NextKeys, len(wantNext), wantNext)
	}
	for k, wv := range wantNext {
		gv, ok := got.NextKeys[k]
		if !ok {
			t.Fatalf("%s: NextKeys missing key %q (got %v, want %v)", label, k, got.NextKeys, wantNext)
		}
		if gv != wv {
			t.Fatalf("%s: NextKeys[%q] = %v, want %v", label, k, gv, wv)
		}
	}
}

func TestAPISignaturesAndConstants(t *testing.T) {
	var (
		_ func([]trie.Entry[string]) *trie.Trie[string]                            = trie.Build[string]
		_ func(*trie.Trie[string], []string, string) trie.Result[string]           = (*trie.Trie[string]).Step
		_ func(*trie.Trie[string], []string) trie.Result[string]                   = (*trie.Trie[string]).Lookup
		_ func([]trie.Entry[testBinding]) *trie.Trie[testBinding]                  = trie.Build[testBinding]
		_ func(*trie.Trie[testBinding], []string, string) trie.Result[testBinding] = (*trie.Trie[testBinding]).Step
		_ func(*trie.Trie[testBinding], []string) trie.Result[testBinding]         = (*trie.Trie[testBinding]).Lookup
	)

	var e trie.Entry[string]
	e.Keys = []string{"q"}
	e.Value = "app.quit"

	var r trie.Result[string]
	r.Kind = trie.Exact
	r.Value = ptr("app.quit")
	r.NextKeys = map[string]string{"x": "other"}

	if got := reflect.TypeOf(trie.None).Kind(); got != reflect.Uint8 {
		t.Fatalf("MatchKind underlying kind = %v, want uint8", got)
	}
	if trie.None != 0 || trie.Prefix != 1 || trie.Exact != 2 || trie.Ambiguous != 3 {
		t.Fatalf("constants = (%d, %d, %d, %d), want (0, 1, 2, 3)", trie.None, trie.Prefix, trie.Exact, trie.Ambiguous)
	}
}

func TestExactMatches(t *testing.T) {
	tr := trie.Build([]trie.Entry[string]{
		{Keys: []string{"q"}, Value: "app.quit"},
		{Keys: []string{"?"}, Value: "help.show"},
		{Keys: []string{"ctrl+a"}, Value: "leader"},
		{Keys: []string{"g", "g"}, Value: "motion.top"},
		{Keys: []string{"ctrl+w", ">"}, Value: "pane.widen"},
		{Keys: []string{"<leader>", "f", "f"}, Value: "file.find"},
		{Keys: []string{"z"}, Value: ""},
	})

	cases := []struct {
		name string
		seq  []string
		want string
	}{
		{name: "single key", seq: []string{"q"}, want: "app.quit"},
		{name: "single symbol", seq: []string{"?"}, want: "help.show"},
		{name: "single modifier", seq: []string{"ctrl+a"}, want: "leader"},
		{name: "two key vim", seq: []string{"g", "g"}, want: "motion.top"},
		{name: "two key split", seq: []string{"ctrl+w", ">"}, want: "pane.widen"},
		{name: "three key leader", seq: []string{"<leader>", "f", "f"}, want: "file.find"},
		{name: "bound empty string value", seq: []string{"z"}, want: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertResult(t, "Lookup", tr.Lookup(tc.seq), trie.Exact, ptr(tc.want), nil)
			prefix := tc.seq[:len(tc.seq)-1]
			last := tc.seq[len(tc.seq)-1]
			assertResult(t, "Step", tr.Step(prefix, last), trie.Exact, ptr(tc.want), nil)
		})
	}

	t.Run("bound zero int value", func(t *testing.T) {
		intTrie := trie.Build([]trie.Entry[int]{
			{Keys: []string{"0"}, Value: 0},
			{Keys: []string{"1"}, Value: 1},
		})
		assertResult(t, "Lookup zero int", intTrie.Lookup([]string{"0"}), trie.Exact, ptr(0), nil)
		assertResult(t, "Step zero int", intTrie.Step(nil, "0"), trie.Exact, ptr(0), nil)
	})
}

func TestPrefixAndNextKeys(t *testing.T) {
	tr := trie.Build([]trie.Entry[string]{
		{Keys: []string{"g", "g"}, Value: "top"},
		{Keys: []string{"g", "t"}, Value: "tab.next"},
		{Keys: []string{"g", "a", "b"}, Value: "deep.ab"},
		{Keys: []string{"<leader>", "f", "f"}, Value: "find"},
		{Keys: []string{"<leader>", "b", "d"}, Value: "buf.del"},
	})

	cases := []struct {
		name     string
		seq      []string
		wantNext map[string]string
	}{
		{
			name:     "g prefix includes direct and intermediate children",
			seq:      []string{"g"},
			wantNext: map[string]string{"g": "top", "t": "tab.next", "a": ""},
		},
		{
			name:     "g a intermediate prefix",
			seq:      []string{"g", "a"},
			wantNext: map[string]string{"b": "deep.ab"},
		},
		{
			name:     "leader prefix with only intermediate children",
			seq:      []string{"<leader>"},
			wantNext: map[string]string{"f": "", "b": ""},
		},
		{
			name:     "leader f intermediate prefix",
			seq:      []string{"<leader>", "f"},
			wantNext: map[string]string{"f": "find"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertResult(t, "Lookup", tr.Lookup(tc.seq), trie.Prefix, nil, tc.wantNext)
			prefix := tc.seq[:len(tc.seq)-1]
			last := tc.seq[len(tc.seq)-1]
			assertResult(t, "Step", tr.Step(prefix, last), trie.Prefix, nil, tc.wantNext)
		})
	}
}

func TestAmbiguousAndStepPast(t *testing.T) {
	tr := trie.Build([]trie.Entry[string]{
		{Keys: []string{"d"}, Value: "op.del"},
		{Keys: []string{"d", "d"}, Value: "line.del"},
		{Keys: []string{"d", "i", "w"}, Value: "inner.word"},
		{Keys: []string{"g"}, Value: "g1"},
		{Keys: []string{"g", "f"}, Value: "gf"},
		{Keys: []string{"g", "f", "e"}, Value: "gfe"},
	})

	cases := []struct {
		name     string
		prefix   []string
		key      string
		wantKind trie.MatchKind
		wantVal  *string
		wantNext map[string]string
	}{
		{
			name:     "d is ambiguous with bound and intermediate children",
			prefix:   nil,
			key:      "d",
			wantKind: trie.Ambiguous,
			wantVal:  ptr("op.del"),
			wantNext: map[string]string{"d": "line.del", "i": ""},
		},
		{
			name:     "d d resolves exact",
			prefix:   []string{"d"},
			key:      "d",
			wantKind: trie.Exact,
			wantVal:  ptr("line.del"),
		},
		{
			name:     "d i resolves prefix",
			prefix:   []string{"d"},
			key:      "i",
			wantKind: trie.Prefix,
			wantNext: map[string]string{"w": "inner.word"},
		},
		{
			name:     "d i w resolves exact",
			prefix:   []string{"d", "i"},
			key:      "w",
			wantKind: trie.Exact,
			wantVal:  ptr("inner.word"),
		},
		{
			name:     "d x resolves none",
			prefix:   []string{"d"},
			key:      "x",
			wantKind: trie.None,
		},
		{
			name:     "g resolves ambiguous level 1",
			prefix:   nil,
			key:      "g",
			wantKind: trie.Ambiguous,
			wantVal:  ptr("g1"),
			wantNext: map[string]string{"f": "gf"},
		},
		{
			name:     "g f resolves ambiguous level 2",
			prefix:   []string{"g"},
			key:      "f",
			wantKind: trie.Ambiguous,
			wantVal:  ptr("gf"),
			wantNext: map[string]string{"e": "gfe"},
		},
		{
			name:     "g f e resolves exact level 3",
			prefix:   []string{"g", "f"},
			key:      "e",
			wantKind: trie.Exact,
			wantVal:  ptr("gfe"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertResult(t, "Step", tr.Step(tc.prefix, tc.key), tc.wantKind, tc.wantVal, tc.wantNext)
			seq := append(append([]string(nil), tc.prefix...), tc.key)
			assertResult(t, "Lookup", tr.Lookup(seq), tc.wantKind, tc.wantVal, tc.wantNext)
		})
	}
}

func TestDuplicateKeysFirstMatchWins(t *testing.T) {
	t.Run("two duplicates", func(t *testing.T) {
		tr := trie.Build([]trie.Entry[string]{
			{Keys: []string{"d", "d"}, Value: "first"},
			{Keys: []string{"d", "d"}, Value: "second"},
		})
		assertResult(t, "Lookup", tr.Lookup([]string{"d", "d"}), trie.Exact, ptr("first"), nil)
		assertResult(t, "Step", tr.Step([]string{"d"}, "d"), trie.Exact, ptr("first"), nil)
	})

	t.Run("three duplicates", func(t *testing.T) {
		tr := trie.Build([]trie.Entry[string]{
			{Keys: []string{"g", "g"}, Value: "p1"},
			{Keys: []string{"g", "g"}, Value: "p2"},
			{Keys: []string{"g", "g"}, Value: "p3"},
		})
		assertResult(t, "Lookup", tr.Lookup([]string{"g", "g"}), trie.Exact, ptr("p1"), nil)
	})

	t.Run("duplicate with longer prefix updates NextKeys with winner", func(t *testing.T) {
		tr := trie.Build([]trie.Entry[string]{
			{Keys: []string{"g"}, Value: "g1"},
			{Keys: []string{"g"}, Value: "g2"},
			{Keys: []string{"g", "g"}, Value: "gg1"},
			{Keys: []string{"g", "g"}, Value: "gg2"},
		})
		assertResult(t, "Lookup g", tr.Lookup([]string{"g"}), trie.Ambiguous, ptr("g1"), map[string]string{"g": "gg1"})
		assertResult(t, "Lookup g g", tr.Lookup([]string{"g", "g"}), trie.Exact, ptr("gg1"), nil)
	})

	t.Run("zero-value first duplicate not overwritten", func(t *testing.T) {
		tr := trie.Build([]trie.Entry[string]{
			{Keys: []string{"x"}, Value: ""},
			{Keys: []string{"x"}, Value: "nonempty"},
		})
		assertResult(t, "Lookup x", tr.Lookup([]string{"x"}), trie.Exact, ptr(""), nil)
	})
}

func TestNoneMatches(t *testing.T) {
	tr := trie.Build([]trie.Entry[string]{
		{Keys: []string{"g", "g"}, Value: "top"},
		{Keys: []string{"g", "a", "b"}, Value: "deep"},
		{Keys: []string{"q"}, Value: "quit"},
	})

	cases := []struct {
		name   string
		prefix []string
		key    string
		seq    []string
	}{
		{name: "unknown first key", prefix: nil, key: "z", seq: []string{"z"}},
		{name: "unknown continuation from prefix", prefix: []string{"g"}, key: "z", seq: []string{"g", "z"}},
		{name: "unknown continuation from single exact", prefix: []string{"q"}, key: "x", seq: []string{"q", "x"}},
		{name: "unknown continuation from two-key exact", prefix: []string{"g", "g"}, key: "g", seq: []string{"g", "g", "g"}},
		{name: "diverging from deeper prefix", prefix: []string{"g", "a"}, key: "z", seq: []string{"g", "a", "z"}},
		{name: "empty key on prefix", prefix: []string{"g"}, key: "", seq: []string{"g", ""}},
		{name: "empty key at root", prefix: nil, key: "", seq: []string{""}},
		{name: "empty token in prefix", prefix: []string{""}, key: "g", seq: []string{"", "g"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertResult(t, "Step", tr.Step(tc.prefix, tc.key), trie.None, nil, nil)
			assertResult(t, "Lookup", tr.Lookup(tc.seq), trie.None, nil, nil)
		})
	}
}

func TestStepDoesNotMutatePrefixBackingArray(t *testing.T) {
	tr := trie.Build([]trie.Entry[string]{
		{Keys: []string{"g", "g"}, Value: "top"},
		{Keys: []string{"<leader>", "f", "f"}, Value: "find"},
	})

	t.Run("len 1 cap 4 exact match", func(t *testing.T) {
		buf := make([]string, 1, 4)
		buf[0] = "g"
		sentinel := buf[:4]
		sentinel[1] = "SENTINEL1"
		sentinel[2] = "SENTINEL2"
		sentinel[3] = "SENTINEL3"

		res := tr.Step(buf, "g")
		assertResult(t, "Step", res, trie.Exact, ptr("top"), nil)

		wantSentinel := []string{"g", "SENTINEL1", "SENTINEL2", "SENTINEL3"}
		if !reflect.DeepEqual(sentinel, wantSentinel) {
			t.Fatalf("backing array mutated: got %v, want %v", sentinel, wantSentinel)
		}
		if len(buf) != 1 || cap(buf) != 4 || buf[0] != "g" {
			t.Fatalf("buf mutated: len=%d cap=%d val=%v", len(buf), cap(buf), buf)
		}
	})

	t.Run("len 1 cap 4 none match", func(t *testing.T) {
		buf := make([]string, 1, 4)
		buf[0] = "g"
		sentinel := buf[:4]
		sentinel[1] = "SENTINEL1"
		sentinel[2] = "SENTINEL2"
		sentinel[3] = "SENTINEL3"

		res := tr.Step(buf, "unknown")
		assertResult(t, "Step", res, trie.None, nil, nil)

		wantSentinel := []string{"g", "SENTINEL1", "SENTINEL2", "SENTINEL3"}
		if !reflect.DeepEqual(sentinel, wantSentinel) {
			t.Fatalf("backing array mutated: got %v, want %v", sentinel, wantSentinel)
		}
	})

	t.Run("len 0 cap 4 prefix match", func(t *testing.T) {
		buf := make([]string, 0, 4)
		sentinel := buf[:4]
		sentinel[0] = "SENTINEL0"
		sentinel[1] = "SENTINEL1"
		sentinel[2] = "SENTINEL2"
		sentinel[3] = "SENTINEL3"

		res := tr.Step(buf, "g")
		assertResult(t, "Step", res, trie.Prefix, nil, map[string]string{"g": "top"})

		wantSentinel := []string{"SENTINEL0", "SENTINEL1", "SENTINEL2", "SENTINEL3"}
		if !reflect.DeepEqual(sentinel, wantSentinel) {
			t.Fatalf("backing array mutated: got %v, want %v", sentinel, wantSentinel)
		}
	})

	t.Run("len 2 cap 6 multi-key exact match", func(t *testing.T) {
		buf := make([]string, 2, 6)
		buf[0] = "<leader>"
		buf[1] = "f"
		sentinel := buf[:6]
		sentinel[2] = "SENTINEL2"
		sentinel[3] = "SENTINEL3"
		sentinel[4] = "SENTINEL4"
		sentinel[5] = "SENTINEL5"

		res := tr.Step(buf, "f")
		assertResult(t, "Step", res, trie.Exact, ptr("find"), nil)

		wantSentinel := []string{"<leader>", "f", "SENTINEL2", "SENTINEL3", "SENTINEL4", "SENTINEL5"}
		if !reflect.DeepEqual(sentinel, wantSentinel) {
			t.Fatalf("backing array mutated: got %v, want %v", sentinel, wantSentinel)
		}
	})
}

func TestImmutabilityAfterBuild(t *testing.T) {
	t.Run("mutate entries and Keys after Build", func(t *testing.T) {
		keys1 := []string{"g", "g"}
		keys2 := []string{"g", "t"}
		entries := []trie.Entry[string]{
			{Keys: keys1, Value: "top"},
			{Keys: keys2, Value: "tab.next"},
		}
		tr := trie.Build(entries)

		keys1[0] = "x"
		keys1[1] = "y"
		entries[0].Value = "mutated"
		entries[1] = trie.Entry[string]{Keys: []string{"z"}, Value: "hijacked"}
		entries = append(entries, trie.Entry[string]{Keys: []string{"q"}, Value: "extra"})
		_ = entries

		assertResult(t, "g g intact", tr.Lookup([]string{"g", "g"}), trie.Exact, ptr("top"), nil)
		assertResult(t, "g t intact", tr.Lookup([]string{"g", "t"}), trie.Exact, ptr("tab.next"), nil)
		assertResult(t, "x y absent", tr.Lookup([]string{"x", "y"}), trie.None, nil, nil)
		assertResult(t, "q absent", tr.Lookup([]string{"q"}), trie.None, nil, nil)
	})

	t.Run("mutate Result.Value pointer does not affect trie", func(t *testing.T) {
		tr := trie.Build([]trie.Entry[string]{
			{Keys: []string{"q"}, Value: "quit"},
		})
		res1 := tr.Lookup([]string{"q"})
		if res1.Value == nil || *res1.Value != "quit" {
			t.Fatalf("unexpected initial result: %+v", res1)
		}
		*res1.Value = "hacked"

		res2 := tr.Lookup([]string{"q"})
		assertResult(t, "second Lookup", res2, trie.Exact, ptr("quit"), nil)
		if res1.Value == res2.Value {
			t.Fatalf("expected defensive copy with distinct pointer, got same pointer %p", res1.Value)
		}
	})

	t.Run("mutate Result.NextKeys does not affect trie", func(t *testing.T) {
		tr := trie.Build([]trie.Entry[string]{
			{Keys: []string{"g", "g"}, Value: "top"},
			{Keys: []string{"g", "t"}, Value: "tab.next"},
		})
		res1 := tr.Lookup([]string{"g"})
		res1.NextKeys["injected"] = "evil"
		res1.NextKeys["t"] = "overwritten"
		delete(res1.NextKeys, "g")

		res2 := tr.Step(nil, "g")
		assertResult(t, "Step g intact", res2, trie.Prefix, nil, map[string]string{"g": "top", "t": "tab.next"})
	})

	t.Run("Lookup does not mutate input seq", func(t *testing.T) {
		tr := trie.Build([]trie.Entry[string]{
			{Keys: []string{"g", "g"}, Value: "top"},
		})
		seq := []string{"g", "g"}
		snap := []string{"g", "g"}
		_ = tr.Lookup(seq)
		if !reflect.DeepEqual(seq, snap) {
			t.Fatalf("seq mutated: got %v, want %v", seq, snap)
		}
	})
}

func TestNilEmptyAndMalformedEntries(t *testing.T) {
	t.Run("nil receiver", func(t *testing.T) {
		var tr *trie.Trie[string]
		assertResult(t, "Lookup nil", tr.Lookup(nil), trie.None, nil, nil)
		assertResult(t, "Lookup empty", tr.Lookup([]string{}), trie.None, nil, nil)
		assertResult(t, "Lookup key", tr.Lookup([]string{"a"}), trie.None, nil, nil)
		assertResult(t, "Step nil prefix", tr.Step(nil, "a"), trie.None, nil, nil)
		assertResult(t, "Step with prefix", tr.Step([]string{"a"}, "b"), trie.None, nil, nil)
		assertResult(t, "Step empty key", tr.Step(nil, ""), trie.None, nil, nil)
	})

	t.Run("zero-value Trie struct", func(t *testing.T) {
		var tr trie.Trie[string]
		assertResult(t, "Lookup", tr.Lookup([]string{"a"}), trie.None, nil, nil)
		assertResult(t, "Step", tr.Step(nil, "a"), trie.None, nil, nil)
	})

	t.Run("empty Build inputs", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			entries []trie.Entry[string]
		}{
			{name: "nil entries", entries: nil},
			{name: "empty entries", entries: []trie.Entry[string]{}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				tr := trie.Build(tc.entries)
				if tr == nil {
					t.Fatalf("Build returned nil *Trie")
				}
				assertResult(t, "Lookup a", tr.Lookup([]string{"a"}), trie.None, nil, nil)
				assertResult(t, "Step a", tr.Step(nil, "a"), trie.None, nil, nil)
			})
		}
	})

	t.Run("empty Keys and empty tokens skipped cleanly without orphan nodes", func(t *testing.T) {
		tr := trie.Build([]trie.Entry[string]{
			{Keys: nil, Value: "nil-keys"},
			{Keys: []string{}, Value: "empty-keys"},
			{Keys: []string{""}, Value: "single-empty"},
			{Keys: []string{"orphan", ""}, Value: "trailing-empty"},
			{Keys: []string{"", "g"}, Value: "leading-empty"},
			{Keys: []string{"a"}, Value: "ok"},
			{Keys: []string{"g", "w", "n"}, Value: "win"},
			{Keys: []string{"g", "x", ""}, Value: "bad-branch"},
		})

		assertResult(t, "valid a", tr.Lookup([]string{"a"}), trie.Exact, ptr("ok"), nil)
		assertResult(t, "orphan skipped upfront", tr.Lookup([]string{"orphan"}), trie.None, nil, nil)
		assertResult(t, "empty key lookup", tr.Lookup([]string{""}), trie.None, nil, nil)
		assertResult(t, "leading empty skipped", tr.Lookup([]string{"", "g"}), trie.None, nil, nil)
		assertResult(t, "bad-branch did not leave x under g", tr.Lookup([]string{"g"}), trie.Prefix, nil, map[string]string{"w": ""})
	})

	t.Run("populated trie Lookup nil or empty seq returns None", func(t *testing.T) {
		tr := trie.Build([]trie.Entry[string]{
			{Keys: []string{"g", "g"}, Value: "top"},
		})
		assertResult(t, "Lookup(nil)", tr.Lookup(nil), trie.None, nil, nil)
		assertResult(t, "Lookup([]string{})", tr.Lookup([]string{}), trie.None, nil, nil)
	})
}

func TestGenericTypesAndConcurrency(t *testing.T) {
	t.Run("struct binding generic", func(t *testing.T) {
		bQuit := testBinding{Command: "app.quit", Scope: "app", Mode: "normal", Hint: "quit"}
		bDel := testBinding{Command: "op.delete", Scope: "editor", Mode: "normal", Hint: "delete"}
		bDelLine := testBinding{Command: "line.delete", Scope: "editor", Mode: "normal", Hint: "delete line"}
		bFileFind := testBinding{Command: "file.find", Scope: "app", Mode: "normal", Hint: "find file"}
		bZero := testBinding{}

		tr := trie.Build([]trie.Entry[testBinding]{
			{Keys: []string{"q"}, Value: bQuit},
			{Keys: []string{"q"}, Value: testBinding{Command: "shadowed"}},
			{Keys: []string{"d"}, Value: bDel},
			{Keys: []string{"d", "d"}, Value: bDelLine},
			{Keys: []string{"<leader>", "f", "f"}, Value: bFileFind},
			{Keys: []string{"z"}, Value: bZero},
		})

		assertResult(t, "exact struct", tr.Lookup([]string{"q"}), trie.Exact, ptr(bQuit), nil)
		assertResult(t, "bound zero struct", tr.Lookup([]string{"z"}), trie.Exact, ptr(bZero), nil)
		assertResult(t, "ambiguous struct", tr.Lookup([]string{"d"}), trie.Ambiguous, ptr(bDel), map[string]testBinding{"d": bDelLine})
		assertResult(t, "prefix intermediate zero struct", tr.Lookup([]string{"<leader>"}), trie.Prefix, nil, map[string]testBinding{"f": bZero})

		res := tr.Lookup([]string{"q"})
		res.Value.Command = "mutated"
		assertResult(t, "struct copy isolated", tr.Lookup([]string{"q"}), trie.Exact, ptr(bQuit), nil)
	})

	t.Run("int generic bound zero vs intermediate zero", func(t *testing.T) {
		tr := trie.Build([]trie.Entry[int]{
			{Keys: []string{"a"}, Value: 0},
			{Keys: []string{"b", "c"}, Value: 42},
		})
		assertResult(t, "bound zero int", tr.Lookup([]string{"a"}), trie.Exact, ptr(0), nil)
		assertResult(t, "intermediate prefix int", tr.Lookup([]string{"b"}), trie.Prefix, nil, map[string]int{"c": 42})
		assertResult(t, "prefix with intermediate child", tr.Step(nil, "b"), trie.Prefix, nil, map[string]int{"c": 42})

		tr2 := trie.Build([]trie.Entry[int]{
			{Keys: []string{"g", "w", "n"}, Value: 7},
		})
		assertResult(t, "intermediate zero int in NextKeys", tr2.Lookup([]string{"g"}), trie.Prefix, nil, map[string]int{"w": 0})
	})

	t.Run("concurrent reads", func(t *testing.T) {
		tr := trie.Build([]trie.Entry[string]{
			{Keys: []string{"q"}, Value: "quit"},
			{Keys: []string{"d"}, Value: "del"},
			{Keys: []string{"d", "d"}, Value: "del.line"},
			{Keys: []string{"g", "g"}, Value: "top"},
			{Keys: []string{"g", "a", "b"}, Value: "deep"},
		})

		const goroutines = 16
		const iterations = 100
		var wg sync.WaitGroup
		wg.Add(goroutines)
		for g := 0; g < goroutines; g++ {
			go func() {
				defer wg.Done()
				for i := 0; i < iterations; i++ {
					r1 := tr.Lookup([]string{"q"})
					if r1.Kind != trie.Exact || r1.Value == nil || *r1.Value != "quit" {
						t.Errorf("unexpected r1: %+v", r1)
						return
					}
					r2 := tr.Step(nil, "d")
					if r2.Kind != trie.Ambiguous || r2.Value == nil || *r2.Value != "del" || r2.NextKeys["d"] != "del.line" {
						t.Errorf("unexpected r2: %+v", r2)
						return
					}
					r3 := tr.Step([]string{"g"}, "a")
					if r3.Kind != trie.Prefix || r3.Value != nil || r3.NextKeys["b"] != "deep" {
						t.Errorf("unexpected r3: %+v", r3)
						return
					}
					r4 := tr.Lookup([]string{"missing"})
					if r4.Kind != trie.None || r4.Value != nil || r4.NextKeys != nil {
						t.Errorf("unexpected r4: %+v", r4)
						return
					}
				}
			}()
		}
		wg.Wait()
	})
}
