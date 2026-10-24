package ochazuke

import (
	tea "charm.land/bubbletea/v2"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestVisualYankShapesAndAtomicUndo(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		keys       []string
		want       Yank
		deleted    string
	}{
		{"char", "猫abc\ndef", []string{"v", "l"}, Yank{Text: "猫a", Shape: "char"}, "bc\ndef"},
		{"line", "abc\ndef\nlast", []string{"V", "j"}, Yank{Text: "abc\ndef\n", Shape: "line"}, "last"},
		{"block", "a界z\nb\nc界x", []string{"l", "ctrl+v", "j", "j"}, Yank{Text: "界\n  \n界", Shape: "block", Width: 2}, "az\nb\ncx"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, in := editorTestRuntime(t, tc.text, true)
			editorTestKeys(t, r, tc.keys...)
			editorTestKeys(t, r, "y")
			if got := r.Registers["\""].Yank; got != tc.want {
				t.Fatalf("yank=%+v want %+v", got, tc.want)
			}
			in.Cursor = 0
			editorTestKeys(t, r, tc.keys...)
			editorTestKeys(t, r, "d")
			if string(in.Text) != tc.deleted {
				t.Fatalf("deleted=%q want %q", string(in.Text), tc.deleted)
			}
			editorTestKeys(t, r, "u")
			if string(in.Text) != tc.text {
				t.Fatalf("undo lost text: %q", string(in.Text))
			}
		})
	}
}
func TestLineOperatorsAndChangeUndo(t *testing.T) {
	r, in := editorTestRuntime(t, "one two\nthree\nfour", true)
	editorTestKeys(t, r, "d", "w")
	if string(in.Text) != "two\nthree\nfour" {
		t.Fatal(string(in.Text))
	}
	editorTestKeys(t, r, "u", "2", "d", "d")
	if string(in.Text) != "four" {
		t.Fatal(string(in.Text))
	}
	editorTestKeys(t, r, "u", "c", "c", "X", "esc")
	if string(in.Text) != "X\nthree\nfour" {
		t.Fatal(string(in.Text))
	}
	editorTestKeys(t, r, "u")
	if string(in.Text) != "one two\nthree\nfour" {
		t.Fatal("change was not atomic", string(in.Text))
	}
	editorTestKeys(t, r, "G", "d", "d")
	if string(in.Text) != "one two\nthree" {
		t.Fatal(string(in.Text))
	}
	editorTestKeys(t, r, "u")
	if string(in.Text) != "one two\nthree\nfour" {
		t.Fatal(string(in.Text))
	}
}
func TestRegisterAppendBlackHoleAndCrossFieldPaste(t *testing.T) {
	r, in := editorTestRuntime(t, "abc def", true)
	editorTestKeys(t, r, "\"", "a", "y", "w", "w", "\"", "A", "y", "e")
	if got := r.Registers["a"].Yank.Text; got != "abc def" {
		t.Fatalf("append=%q", got)
	}
	editorTestKeys(t, r, "\"", "_", "d", "d")
	if r.Registers["\""].Yank.Text != "abc def" {
		t.Fatal("black hole changed unnamed register")
	}
	second := NewInput("other", "", true)
	if err := r.AddInput(second); err != nil {
		t.Fatal(err)
	}
	if err := r.Focus(second.ID); err != nil {
		t.Fatal(err)
	}
	editorTestKeys(t, r, "\"", "a", "p")
	if string(second.Text) != "abc def" {
		t.Fatal(string(second.Text))
	}
	editorTestKeys(t, r, "u")
	if string(second.Text) != "" || string(in.Text) != "" {
		t.Fatal("cross-field history not isolated")
	}
}
func TestPathKillGlobalHistoryAndLiteralPaste(t *testing.T) {
	r, in := editorTestRuntime(t, "/foo/bar-file.txt", false)
	editorTestKeys(t, r, "A", "ctrl+w")
	if string(in.Text) != "/foo/" || r.Registers["\""].Yank.Text != "bar-file.txt" {
		t.Fatalf("path kill: %q %+v", string(in.Text), r.Registers["\""])
	}
	second := NewInput("second", "", false)
	if err := r.AddInput(second); err != nil {
		t.Fatal(err)
	}
	if err := r.Focus(second.ID); err != nil {
		t.Fatal(err)
	}
	editorTestKeys(t, r, "i", "ctrl+y")
	if string(second.Text) != "bar-file.txt" || len(r.YankHistory) != 1 {
		t.Fatal("global kill paste failed")
	}
}
func TestBlockPasteRaggedUnicodeAndUndo(t *testing.T) {
	r, in := editorTestRuntime(t, "a界z\nb\nc界x", true)
	editorTestKeys(t, r, "l", "ctrl+v", "j", "j", "y")
	second := NewInput("second", "xyz\nq\nuvw", true)
	if err := r.AddInput(second); err != nil {
		t.Fatal(err)
	}
	if err := r.Focus(second.ID); err != nil {
		t.Fatal(err)
	}
	editorTestKeys(t, r, "l", "P")
	if got := string(second.Text); got != "x界yz\nq  \nu界vw" {
		t.Fatalf("block paste=%q", got)
	}
	editorTestKeys(t, r, "u")
	if string(second.Text) != "xyz\nq\nuvw" {
		t.Fatal("block paste undo lost data")
	}
	if string(in.Text) != "a界z\nb\nc界x" {
		t.Fatal("yank changed source")
	}
}
func TestVisualPasteIsOneUndoAndLinePlacement(t *testing.T) {
	r, in := editorTestRuntime(t, "a\nb\nc", true)
	editorTestKeys(t, r, "y", "y", "G", "V", "p")
	if string(in.Text) != "a\nb\na\n" {
		t.Fatalf("line replacement=%q", string(in.Text))
	}
	editorTestKeys(t, r, "u")
	if string(in.Text) != "a\nb\nc" {
		t.Fatal("visual paste was not atomic")
	}
}
func modelEditorKeys(t *testing.T, m *Model, keys ...string) {
	t.Helper()
	for _, key := range keys {
		if _, err := m.Runtime.Dispatch(MacroKey{Key: key}); err != nil {
			t.Fatalf("%q: %v", key, err)
		}
	}
}
func TestMacrosDispatchModesCountsFocusAndLiteralPaste(t *testing.T) {
	r, in := editorTestRuntime(t, "", true)
	second := NewInput("second", "", true)
	if err := r.AddInput(second); err != nil {
		t.Fatal(err)
	}
	r.Define("app").Mode("normal").Bind("tab", Do("focus.input", Arguments{"id": "second"}))
	m := NewModel(r)
	modelEditorKeys(t, m, "q", "a", "i", "X", "esc", "q", "2", "@", "a", "@", "@")
	if string(in.Text) != "XXXX" || in.Mode != "normal" {
		t.Fatalf("macro count/transitions: %q %s", string(in.Text), in.Mode)
	}
	modelEditorKeys(t, m, "q", "b", "tab", "A")
	if _, err := r.Dispatch(MacroKey{Key: "[ctrl+a]", Paste: true}); err != nil {
		t.Fatal(err)
	}
	modelEditorKeys(t, m, "esc", "q")
	if err := r.Focus(in.ID); err != nil {
		t.Fatal(err)
	}
	modelEditorKeys(t, m, "@", "b")
	if r.Context().InputID != "second" || string(second.Text) != "ctrl+actrl+a" || string(in.Text) != "XXXX" {
		t.Fatalf("focus/literal macro: %s %q %q", r.Context().InputID, string(second.Text), string(in.Text))
	}
}
func TestMacroRecursiveSafeguardAndPrefixPriority(t *testing.T) {
	r, in := editorTestRuntime(t, "one two", false)
	m := NewModel(r)
	r.Define("app").Mode("normal").Bind("w", Do("quit.app")).Bind("a", Do("quit.app"))
	modelEditorKeys(t, m, "d", "w")
	if string(in.Text) != "two" || r.Quit {
		t.Fatal("operator prefix lost to binding")
	}
	r.Registers["a"] = Register{Keys: []MacroKey{{Key: "@"}, {Key: "a"}}}
	modelEditorKeys(t, m, "@")
	_, err := r.Dispatch(MacroKey{Key: "a"})
	if err == nil || !strings.Contains(err.Error(), "recursion") {
		t.Fatalf("recursive macro: %v", err)
	}
	if r.Quit || r.Macro.Depth != 0 || r.Mode(r.Context()) != "normal" || in.Pending() != "" || len(r.Frames) != 1 {
		t.Fatalf("recursive macro left context: %+v mode=%s", r.Macro, r.Mode(r.Context()))
	}
}
func TestYankHistoryModalRestoresFocusWithoutInsertion(t *testing.T) {
	r, in := editorTestRuntime(t, "one two", true)
	m := NewModel(r)
	modelEditorKeys(t, m, "y", "w", "w", "y", "e", "ctrl+y", "j")
	if len(r.Frames) != 2 {
		t.Fatal("no yank modal")
	}
	view := m.View().Content
	if !strings.Contains(ansi.Strip(view), "Yank history") {
		t.Fatal("history renderer absent")
	}
	modelEditorKeys(t, m, "enter")
	if len(r.Frames) != 1 || r.Context().InputID != in.ID || string(in.Text) != "one two" {
		t.Fatal("history selection inserted or lost origin")
	}
	modelEditorKeys(t, m, "p")
	if string(in.Text) != "one tone wo" {
		t.Fatalf("history paste=%q", string(in.Text))
	}
}
func TestPathsMetadataNormalizesAndEditorRemainsAvailable(t *testing.T) {
	r := NewRuntime()
	in := NewInput("paths", "one\ntwo", false)
	in.Kind = "paths"
	in.PathFilter = "file"
	if err := r.AddInput(in); err != nil {
		t.Fatal(err)
	}
	if !in.Multiline || in.Snapshot()["kind"] != "paths" {
		t.Fatal("paths not normalized")
	}
	if err := r.Focus(in.ID); err != nil {
		t.Fatal(err)
	}
	editorTestKeys(t, r, "o", "three", "esc")
	if string(in.Text) != "one\nthree\ntwo" {
		t.Fatal(string(in.Text))
	}
}
func TestSelectionViewEscapesControlsAndFollowsCursor(t *testing.T) {
	r, in := editorTestRuntime(t, "界abc\n\x1b[31m\nlast", true)
	editorTestKeys(t, r, "V", "j")
	view := InputView(in, 8, 2)
	if strings.Contains(ansi.Strip(view), "\x1b") {
		t.Fatal("control escaped into view")
	}
	if !strings.Contains(view, "\x1b[7m") {
		t.Fatal("selection not highlighted")
	}
	for _, line := range strings.Split(view, "\n") {
		if ansi.StringWidth(line) > 8 {
			t.Fatal("view exceeded width")
		}
	}
	m := NewModel(r)
	m.Update(tea.WindowSizeMsg{Width: 24, Height: 10})
	_ = m.View().Content
}

func TestBlockChangeRepeatsInsertionAtomically(t *testing.T) {
	r, in := editorTestRuntime(t, "abcd\nx\nwxyz", true)
	editorTestKeys(t, r, "l", "ctrl+v", "j", "j", "c", "猫", "esc")
	if string(in.Text) != "a猫cd\nx猫\nw猫yz" {
		t.Fatalf("block change=%q", string(in.Text))
	}
	editorTestKeys(t, r, "u")
	if string(in.Text) != "abcd\nx\nwxyz" {
		t.Fatal("block change was not one undo", string(in.Text))
	}
	editorTestKeys(t, r, "ctrl+r")
	if string(in.Text) != "a猫cd\nx猫\nw猫yz" {
		t.Fatal("block change redo lost text")
	}
}

func TestMacroStepLimitAndErrorModalCleanup(t *testing.T) {
	r, _ := editorTestRuntime(t, "abc", true)
	m := NewModel(r)
	r.Registers["a"] = Register{Keys: []MacroKey{{Key: "h"}, {Key: "l"}}}
	_, err := r.Execute(Request{Verb: "replay", Noun: "macro", Args: Arguments{"register": "a", "steps": 10000}})
	if err == nil || !strings.Contains(err.Error(), "step limit") {
		t.Fatalf("step bound: %v", err)
	}
	if r.Macro.Depth != 0 || r.Mode(r.Context()) != "normal" {
		t.Fatal("step bound left replay active")
	}
	r.Register(Operation{Verb: "fail", Noun: "editor-test", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		if err := r.Push(&Frame{ID: "macro-test-modal", Focus: "app"}); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("expected macro failure")
	}})
	r.Define("app").Mode("").Bind("ctrl+t", Do("fail.editor-test"))
	r.Registers["b"] = Register{Keys: []MacroKey{{Key: "ctrl+t"}}}
	modelEditorKeys(t, m, "@")
	_, err = r.Dispatch(MacroKey{Key: "b"})
	if err == nil || err.Error() != "expected macro failure" || len(r.Frames) != 1 {
		t.Fatalf("failed replay: frames=%d err=%v", len(r.Frames), err)
	}
}

func TestVisualPasteRejectsInvalidCountWithoutMutation(t *testing.T) {
	r, in := editorTestRuntime(t, "abc", true)
	editorTestKeys(t, r, "y", "y", "v")
	_, err := r.Execute(Request{Verb: "paste", Noun: "input", Args: Arguments{"steps": -1}})
	if err == nil || string(in.Text) != "abc" || in.Mode != "visual" {
		t.Fatal("invalid paste mutated selection")
	}
}

func TestBlockYankPreservesEmojiGraphemes(t *testing.T) {
	r, in := editorTestRuntime(t, "a👩‍💻z\nb👩‍💻x", true)
	editorTestKeys(t, r, "l", "ctrl+v", "j", "y")
	if got := r.Registers["\""].Yank; got.Text != "👩‍💻\n👩‍💻" || got.Width != 2 {
		t.Fatalf("grapheme yank=%+v", got)
	}
	in.Cursor = 0
	editorTestKeys(t, r, "l", "ctrl+v", "j", "d")
	if string(in.Text) != "az\nbx" {
		t.Fatalf("grapheme delete=%q", string(in.Text))
	}
	editorTestKeys(t, r, "u")
	if string(in.Text) != "a👩‍💻z\nb👩‍💻x" {
		t.Fatal("grapheme undo lost data")
	}
}

func TestAppMacroCountWithoutInputAndUppercaseAppend(t *testing.T) {
	r := NewRuntime()
	m := NewModel(r)
	calls := 0
	r.Register(Operation{Verb: "count", Noun: "editor-test", Run: func(r *Runtime, c Context, a Arguments) (any, error) { calls++; return calls, nil }})
	r.Define("app").Mode("").Bind("ctrl+t", Do("count.editor-test"))
	modelEditorKeys(t, m, "q", "a", "ctrl+t", "q", "q", "A", "ctrl+t", "q", "2", "@", "a", "@", "@")
	if calls != 8 {
		t.Fatalf("application macro calls=%d want 8", calls)
	}
}

func TestMotionOperatorsReachEOFAndChangeCurrentWord(t *testing.T) {
	r, in := editorTestRuntime(t, "abc", false)
	editorTestKeys(t, r, "y", "w")
	if r.Registers["\""].Yank.Text != "abc" {
		t.Fatal("yw dropped final rune")
	}
	editorTestKeys(t, r, "d", "w")
	if string(in.Text) != "" {
		t.Fatalf("dw left %q", string(in.Text))
	}
	editorTestKeys(t, r, "u", "l", "l", "d", "l")
	if string(in.Text) != "ab" {
		t.Fatalf("dl at EOL left %q", string(in.Text))
	}
	r, in = editorTestRuntime(t, "x y", false)
	editorTestKeys(t, r, "c", "w", "Z", "esc")
	if string(in.Text) != "Z y" {
		t.Fatalf("cw ate following word: %q", string(in.Text))
	}
	editorTestKeys(t, r, "u")
	if string(in.Text) != "x y" {
		t.Fatal("cw undo was not atomic")
	}
}
