package ochazuke

import (
	"strings"
	"testing"
)

func editorTestRuntime(t *testing.T, text string, multiline bool) (*Runtime, *Input) {
	t.Helper()
	r := NewRuntime()
	in := NewInput("editor", text, multiline)
	if err := r.AddInput(in); err != nil {
		t.Fatal(err)
	}
	if err := r.Focus(in.ID); err != nil {
		t.Fatal(err)
	}
	return r, in
}

// editorTestKey builds the key event for a test key name; "[text]" is a paste.
func editorTestKey(key string) MacroKey {
	if strings.HasPrefix(key, "[") && strings.HasSuffix(key, "]") && len(key) >= 2 {
		return MacroKey{Key: key, Paste: true}
	}
	return MacroKey{Key: key}
}

func editorTestKeys(t *testing.T, r *Runtime, keys ...string) {
	t.Helper()
	for _, key := range keys {
		if _, err := r.Dispatch(editorTestKey(key)); err != nil {
			t.Fatalf("key %q: %v", key, err)
		}
	}
}

// editorTestDispatch reports whether a key was claimed and the error it produced.
func editorTestDispatch(r *Runtime, key string) (bool, error) { return r.Dispatch(editorTestKey(key)) }

func editorTestExecute(t *testing.T, r *Runtime, verb string, args Arguments) {
	t.Helper()
	if _, err := r.Execute(Request{Verb: verb, Noun: "input", Args: args}); err != nil {
		t.Fatalf("%s.input: %v", verb, err)
	}
}

func editorTestState(t *testing.T, in *Input, text string, cursor int, mode string) {
	t.Helper()
	if string(in.Text) != text || in.Cursor != cursor || in.Mode != mode {
		t.Fatalf("got text=%q cursor=%d mode=%q; want text=%q cursor=%d mode=%q", string(in.Text), in.Cursor, in.Mode, text, cursor, mode)
	}
}

func TestReadlineOnlyKeepsTypingAfterEscapeAndModeChanges(t *testing.T) {
	r := NewRuntime()
	in := NewInput("readline", "path", false)
	if err := r.AddInput(in, Kind("prompt")); err != nil {
		t.Fatal(err)
	}
	if err := r.Focus(in.ID); err != nil {
		t.Fatal(err)
	}
	editorTestKeys(t, r, "ctrl+e", "esc", "j", "k")
	editorTestState(t, in, "pathjk", 6, "insert")
	for _, mode := range []string{"normal", "visual", "visual-line", "visual-block"} {
		result, err := r.Execute(Request{Verb: "set", Noun: "mode", Args: Arguments{"mode": mode}})
		if err != nil || result != "insert" {
			t.Fatalf("set mode %s: result=%v err=%v", mode, result, err)
		}
		editorTestState(t, in, "pathjk", 6, "insert")
	}
	editorTestKeys(t, r, "[猫]")
	editorTestState(t, in, "pathjk猫", 7, "insert")
	editorTestExecute(t, r, "undo", nil)
	editorTestState(t, in, "path", 0, "insert")
	editorTestExecute(t, r, "redo", nil)
	editorTestState(t, in, "pathjk猫", 7, "insert")
}

func TestReadlineOnlyPathKillYankAndMultiline(t *testing.T) {
	r := NewRuntime()
	in := NewInput("readline", "/work/src/main.go", true)
	if err := r.AddInput(in, Kind("prompt")); err != nil {
		t.Fatal(err)
	}
	if err := r.Focus(in.ID); err != nil {
		t.Fatal(err)
	}
	editorTestKeys(t, r, "ctrl+e", "ctrl+w")
	editorTestState(t, in, "/work/src/", 10, "insert")
	editorTestKeys(t, r, "ctrl+y", "enter", "j", "esc", "k")
	editorTestState(t, in, "/work/src/main.go\njk", 20, "insert")
}

func TestEditorUnicodeModesAndGroupedUndo(t *testing.T) {
	r, in := editorTestRuntime(t, "猫", false)
	editorTestKeys(t, r, "z", "界", "[not inserted]")
	editorTestState(t, in, "猫", 0, "normal")
	editorTestKeys(t, r, "i", "é", "🙂", "esc")
	editorTestState(t, in, "é🙂猫", 1, "normal")
	editorTestKeys(t, r, "u")
	editorTestState(t, in, "猫", 0, "normal")
	editorTestKeys(t, r, "ctrl+r")
	editorTestState(t, in, "é🙂猫", 1, "normal")
	editorTestKeys(t, r, "a", "界", "backspace", "火", "esc")
	editorTestState(t, in, "é🙂火猫", 2, "normal")
	editorTestKeys(t, r, "u")
	editorTestState(t, in, "é🙂猫", 1, "normal")
}

func TestEditorWordMotionsAndCounts(t *testing.T) {
	r, in := editorTestRuntime(t, "é猫 one.two\nx\nlast\n", true)
	for _, step := range []struct {
		keys   []string
		cursor int
	}{
		{[]string{"w"}, 3}, {[]string{"e"}, 5}, {[]string{"w"}, 6},
		{[]string{"w"}, 7}, {[]string{"b"}, 6}, {[]string{"b"}, 3},
		{[]string{"0", "2", "w"}, 6}, {[]string{"0", "e"}, 1},
		{[]string{"0", "3", "l"}, 3}, {[]string{"2", "h"}, 1},
		{[]string{"$"}, 9}, {[]string{"0", "2", "e"}, 5},
	} {
		editorTestKeys(t, r, step.keys...)
		if in.Cursor != step.cursor {
			t.Fatalf("after %v: cursor=%d want %d", step.keys, in.Cursor, step.cursor)
		}
	}
	editorTestKeys(t, r, "0", "2", "x")
	editorTestState(t, in, " one.two\nx\nlast\n", 0, "normal")
	editorTestKeys(t, r, "u", "3", "x", "u", "ctrl+r")
	editorTestState(t, in, "one.two\nx\nlast\n", 0, "normal")
}

func TestEditorMultilineBoundariesAndDesiredColumn(t *testing.T) {
	r, in := editorTestRuntime(t, "abcdef\nx\nuvwxyz\n", true)
	for _, step := range []struct {
		keys   []string
		cursor int
	}{
		{[]string{"$"}, 5}, {[]string{"j"}, 7}, {[]string{"j"}, 14},
		{[]string{"j"}, 16}, {[]string{"k"}, 14},
		{[]string{"G", "h", "l", "x"}, 16},
		{[]string{"g", "g"}, 0}, {[]string{"2", "g", "g"}, 7},
		{[]string{"G"}, 16}, {[]string{"2", "G"}, 7},
		{[]string{"9", "9", "9", "j"}, 16},
	} {
		editorTestKeys(t, r, step.keys...)
		editorTestState(t, in, "abcdef\nx\nuvwxyz\n", step.cursor, "normal")
	}
	editorTestKeys(t, r, "i", "enter", "esc")
	editorTestState(t, in, "abcdef\nx\nuvwxyz\n\n", 17, "normal")
	editorTestKeys(t, r, "u")
	editorTestState(t, in, "abcdef\nx\nuvwxyz\n", 16, "normal")
}

func TestEditorEmptyAndSingleLineBoundaries(t *testing.T) {
	r, in := editorTestRuntime(t, "", false)
	editorTestKeys(t, r, "h", "j", "k", "l", "w", "b", "e", "$", "G", "x", "u", "ctrl+r")
	editorTestState(t, in, "", 0, "normal")
	editorTestKeys(t, r, "i", "backspace", "delete", "ctrl+a", "ctrl+e", "enter")
	editorTestState(t, in, "", 0, "insert")
	if handled, err := editorTestDispatch(r, "enter"); handled || err != nil {
		t.Fatalf("single-line enter: handled=%v err=%v", handled, err)
	}
	if _, err := editorTestDispatch(r, "[a\nb]"); err == nil {
		t.Fatal("single-line paste accepted a newline")
	}
	editorTestState(t, in, "", 0, "insert")
	editorTestKeys(t, r, "é", "esc", "x")
	editorTestState(t, in, "", 0, "normal")
	for _, key := range []string{"o", "O"} {
		if _, err := editorTestDispatch(r, key); err == nil {
			t.Fatalf("single-line %s accepted", key)
		}
		editorTestState(t, in, "", 0, "normal")
	}
}

func TestEditorInsertionPositionsAndOpenLineGrouping(t *testing.T) {
	for _, tc := range []struct {
		key, text string
		cursor    int
	}{
		{"i", "  Xcat\ndog", 2}, {"a", "  cXat\ndog", 3},
		{"I", "  Xcat\ndog", 2}, {"A", "  catX\ndog", 5},
		{"o", "  cat\nX\ndog", 6}, {"O", "X\n  cat\ndog", 0},
	} {
		t.Run(tc.key, func(t *testing.T) {
			r, in := editorTestRuntime(t, "  cat\ndog", true)
			editorTestKeys(t, r, "2", "l", tc.key, "X", "esc")
			editorTestState(t, in, tc.text, tc.cursor, "normal")
			editorTestKeys(t, r, "u")
			editorTestState(t, in, "  cat\ndog", 2, "normal")
		})
	}
}

func TestEditorReadlineMotionAndKillYank(t *testing.T) {
	r, in := editorTestRuntime(t, "hello 世界\nsecond", true)
	for _, step := range []struct {
		keys   []string
		cursor int
	}{
		{[]string{"A", "ctrl+b"}, 7}, {[]string{"ctrl+f"}, 8},
		{[]string{"ctrl+a"}, 0}, {[]string{"ctrl+e"}, 8},
		{[]string{"alt+b"}, 6}, {[]string{"alt+f"}, 8},
		{[]string{"ctrl+n"}, 15}, {[]string{"ctrl+p"}, 8},
	} {
		editorTestKeys(t, r, step.keys...)
		editorTestState(t, in, "hello 世界\nsecond", step.cursor, "insert")
	}
	r, in = editorTestRuntime(t, "one 世界 tail", false)
	editorTestKeys(t, r, "A", "ctrl+w")
	editorTestState(t, in, "one 世界 ", 7, "insert")
	if r.Registers().Named["\""].Yank.Text != "tail" {
		t.Fatalf("yank=%q want tail", r.Registers().Named["\""].Yank.Text)
	}
	editorTestKeys(t, r, "ctrl+y", "ctrl+u")
	editorTestState(t, in, "", 0, "insert")
	editorTestKeys(t, r, "ctrl+y", "ctrl+a", "ctrl+k")
	editorTestState(t, in, "", 0, "insert")
	editorTestKeys(t, r, "ctrl+y", "ctrl+b", "delete", "backspace")
	editorTestState(t, in, "one 世界 ta", 9, "insert")
	editorTestKeys(t, r, "esc", "u")
	editorTestState(t, in, "one 世界 tail", 0, "normal")
}

func TestEditorReadlineWordBoundariesDifferFromVim(t *testing.T) {
	r, in := editorTestRuntime(t, "one foo.bar", false)
	editorTestKeys(t, r, "i", "alt+f")
	editorTestState(t, in, "one foo.bar", 3, "insert")
	editorTestKeys(t, r, "alt+f")
	editorTestState(t, in, "one foo.bar", 7, "insert")
	editorTestKeys(t, r, "alt+f", "alt+b")
	editorTestState(t, in, "one foo.bar", 8, "insert")
	editorTestKeys(t, r, "ctrl+e", "ctrl+w")
	editorTestState(t, in, "one ", 4, "insert")
	if r.Registers().Named["\""].Yank.Text != "foo.bar" {
		t.Fatalf("yank=%q want foo.bar", r.Registers().Named["\""].Yank.Text)
	}
	editorTestKeys(t, r, "ctrl+y", "esc", "u")
	editorTestState(t, in, "one foo.bar", 0, "normal")
}

func TestEditorKillNewlineAndBackspaceJoin(t *testing.T) {
	r, in := editorTestRuntime(t, "ab\ncd", true)
	editorTestKeys(t, r, "A", "ctrl+k")
	editorTestState(t, in, "abcd", 2, "insert")
	if r.Registers().Named["\""].Yank.Text != "\n" {
		t.Fatalf("yank=%q want newline", r.Registers().Named["\""].Yank.Text)
	}
	editorTestKeys(t, r, "ctrl+y", "backspace")
	editorTestState(t, in, "abcd", 2, "insert")
	editorTestKeys(t, r, "esc", "u")
	editorTestState(t, in, "ab\ncd", 0, "normal")
}

func TestEditorPasteIsLiteralAndGrouped(t *testing.T) {
	r, in := editorTestRuntime(t, "", true)
	editorTestKeys(t, r, "i", "[]", "[ctrl+a]", "[\r\n猫\t🙂]", "esc")
	editorTestState(t, in, "ctrl+a\n猫\t🙂", 9, "normal")
	editorTestKeys(t, r, "u")
	editorTestState(t, in, "", 0, "normal")
	editorTestKeys(t, r, "ctrl+r")
	editorTestState(t, in, "ctrl+a\n猫\t🙂", 9, "normal")
}

func TestEditorStandaloneEditsAndAlternateUndoBranches(t *testing.T) {
	r, in := editorTestRuntime(t, "", false)
	editorTestExecute(t, r, "insert", Arguments{"text": "α"})
	editorTestExecute(t, r, "insert", Arguments{"text": "β"})
	editorTestState(t, in, "βα", 1, "normal")
	editorTestExecute(t, r, "undo", nil)
	editorTestState(t, in, "α", 0, "normal")
	editorTestExecute(t, r, "insert", Arguments{"text": "γ"})
	editorTestState(t, in, "γα", 1, "normal")
	editorTestExecute(t, r, "undo", nil)
	editorTestExecute(t, r, "redo", Arguments{"branch": 0})
	editorTestState(t, in, "βα", 1, "normal")
	editorTestExecute(t, r, "undo", nil)
	editorTestExecute(t, r, "redo", Arguments{"branch": 1})
	editorTestState(t, in, "γα", 1, "normal")
	editorTestExecute(t, r, "undo", Arguments{"steps": 2})
	editorTestState(t, in, "", 0, "normal")
	editorTestExecute(t, r, "redo", Arguments{"steps": 2})
	editorTestState(t, in, "γα", 1, "normal")
	before := in.Head
	if _, err := r.Execute(Request{Verb: "redo", Noun: "input", Args: Arguments{"branch": 99}}); err == nil {
		t.Fatal("invalid branch accepted")
	}
	if in.Head != before {
		t.Fatal("invalid branch moved history")
	}
	editorTestExecute(t, r, "replace", Arguments{"start": 0, "end": 1, "text": "🙂"})
	editorTestState(t, in, "🙂α", 1, "normal")
	editorTestExecute(t, r, "undo", nil)
	editorTestState(t, in, "γα", 1, "normal")
}

func TestEditorInputIsolationAndCapturedTarget(t *testing.T) {
	r, first := editorTestRuntime(t, "alpha beta", false)
	second := NewInput("second", "XYZ", false)
	if err := r.AddInput(second); err != nil {
		t.Fatal(err)
	}
	editorTestKeys(t, r, "A", "ctrl+w", "esc", "2", "g")
	if first.Count() != 2 || first.Pending() != "g" || r.Registers().Named["\""].Yank.Text != "beta" {
		t.Fatalf("first state=%v", first.Snapshot())
	}
	if err := r.Focus(second.ID); err != nil {
		t.Fatal(err)
	}
	editorTestKeys(t, r, "i", "ctrl+y", "!", "esc")
	editorTestState(t, second, "beta!XYZ", 4, "normal")
	editorTestState(t, first, "alpha ", 5, "normal")
	// The mode is global: focusing another input dropped first's pending g.
	if first.Count() != 0 || first.Pending() != "" || r.Mode.Name != "normal" {
		t.Fatalf("focus change must reset the mode: %+v", r.Mode)
	}
	if _, err := r.Execute(Request{Verb: "undo", Noun: "input", Target: "editor"}); err != nil {
		t.Fatal(err)
	}
	editorTestState(t, first, "alpha beta", 0, "normal")
	editorTestState(t, second, "beta!XYZ", 4, "normal")
	editorTestKeys(t, r, "u")
	editorTestState(t, second, "XYZ", 0, "normal")
	if r.Registers().Named["\""].Yank.Text != "beta" {
		t.Fatal("shared yank lost after cross-field paste and undo")
	}
}

// A count or prefix is the focused instance's pending mode; the runtime's
// key-sequence prefix is separate state and neither disturbs the other.
func TestEditorInputPrefixDoesNotUseRuntimePending(t *testing.T) {
	r, in := editorTestRuntime(t, "abcdef", false)
	editorTestKeys(t, r, "2")
	if r.Pending != "" || in.Count() != 2 || r.Mode.Name != "count_pending" {
		t.Fatalf("runtime pending=%q input count=%d state=%q", r.Pending, in.Count(), r.Mode.Name)
	}
	editorTestKeys(t, r, "l")
	editorTestState(t, in, "abcdef", 2, "normal")
	if r.Pending != "" || in.Count() != 0 || r.Mode.Name != "normal" {
		t.Fatalf("runtime pending=%q input count=%d state=%q", r.Pending, in.Count(), r.Mode.Name)
	}
	editorTestKeys(t, r, "g", "esc")
	if in.Pending() != "" || r.Pending != "" || in.Mode != "normal" {
		t.Fatalf("input prefix=%q runtime prefix=%q", in.Pending(), r.Pending)
	}
}

func TestEditorModeHookAndCustomMode(t *testing.T) {
	r, in := editorTestRuntime(t, "cat", false)
	if err := r.DefineMode("search", false); err != nil {
		t.Fatal(err)
	}
	setMode := func(mode string) {
		t.Helper()
		if _, err := r.Execute(Request{Verb: "set", Noun: "mode", Args: Arguments{"mode": mode}}); err != nil {
			t.Fatal(err)
		}
	}
	setMode("insert")
	editorTestKeys(t, r, "x", "y")
	setMode("normal")
	editorTestKeys(t, r, "u")
	editorTestState(t, in, "cat", 0, "normal")
	setMode("search")
	if handled, err := editorTestDispatch(r, "i"); handled || err != nil {
		t.Fatalf("custom mode key: handled=%v err=%v", handled, err)
	}
	editorTestState(t, in, "cat", 0, "search")
	if _, err := r.Execute(Request{Verb: "set", Noun: "mode", Args: Arguments{"mode": "unknown"}}); err == nil {
		t.Fatal("unregistered mode accepted")
	}
	setMode("normal")
	editorTestKeys(t, r, "i", "z", "esc", "u")
	editorTestState(t, in, "cat", 0, "normal")
}

func TestEditorBindingPrecedenceAndSnapshot(t *testing.T) {
	r, in := editorTestRuntime(t, "abc", false)
	r.Define("app").Mode("normal").Bind("i", Do("input.move", Arguments{"motion": "right"}))
	editorTestKeys(t, r, "i")
	editorTestState(t, in, "abc", 1, "normal")
	editorTestKeys(t, r, "2", "g")
	snapshot := in.Snapshot()
	if snapshot["count"] != 2 || snapshot["pending"] != "g" || snapshot["column"] != 1 || snapshot["row"] != 0 {
		t.Fatalf("snapshot=%v", snapshot)
	}
	editorTestExecute(t, r, "insert", Arguments{"text": "X"})
	editorTestState(t, in, "aXbc", 2, "normal")
}
