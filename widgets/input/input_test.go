package input

import (
	"strings"
	"testing"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
)

func keys(t *testing.T, m *Model, sequence string) {
	t.Helper()
	for _, key := range strings.Fields(sequence) {
		if !m.HandleKey(key) {
			t.Fatalf("unhandled %q in %q (mode %s)", key, sequence, m.Mode())
		}
	}
}

func TestOperators(t *testing.T) {
	for _, tc := range []struct{ name, initial, keys, want string }{
		{"delete word", "one two", "d w", "two"},
		{"change word", "one two", "c w X esc", "X two"},
		{"change singleton WORD", "a two", "c W X esc", "X two"},
		{"inner word", "one two", "w l c i w X esc", "one X"},
		{"counted inner word", "one two", "d 2 i w", "two"},
		{"around word from blanks", "a  two  three", "l d a w", "a  three"},
		{"inside next pair", "pre (abc)", "d i (", "pre ()"},
		{"around word ending line", "one two", "w d a w", "one"},
		{"around quotes", `one "two" three`, `w d a "`, "one three"},
		{"escaped single quote", `one 'it\'s' ok`, "w c i ' X esc", "one 'X' ok"},
		{"nested parentheses", "((inner))", "l l d a )", "()"},
		{"inside brackets", "[xy]", "d i [", "[]"},
		{"around braces", "{xy}", "d a }", ""},
		{"end of empty line", "a\n\nb", "j d $", "a\n\nb"},
		{"vertical boundary", "a\nb", "G d j", "a\nb"},
		{"first paragraph", "first\nsecond\n\nthird", "d i p", "\nthird"},
		{"last paragraph", "first\n\nsecond\nthird", "G d i p", "first\n"},
		{"delete line", "one\ntwo\nthree", "j d d", "one\nthree"},
		{"last line", "a\nb", "G d d", "a"},
		{"change line", "one\ntwo\nthree", "j c c X esc", "one\nX\nthree"},
		{"counted vertical deletion", "a\nb\nc\nd", "d 2 j", "d"},
		{"delete to first line", "a\nb\nc\nd", "2 G d g g", "c\nd"},
		{"delete to last line", "a\nb\nc\nd", "j d G", "a"},
		{"operator motion counts", "1 2 3 4 5 6 7", "2 d 3 w", "7"},
		{"word at line ending", "one two\nthree", "w d w", "one \nthree"},
		{"yank and paste lines", "one\ntwo", "y y j p", "one\ntwo\none"},
		{"yank before first line", "one\ntwo", "j y y g g P", "two\none\ntwo"},
		{"named register", "one two three", `" a y i w w d w " a P`, "one onethree"},
		{"visual characters", "abcde", "l v l d", "ade"},
		{"visual reverse", "abcde", "$ v h h c X esc", "abX"},
		{"visual lines", "a\nb\nc", "V j d", "c"},
		{"visual object", "foo bar", "l v i w c X esc", "X bar"},
		{"matching delimiter delete", "pre(x(y)z)end", "3 l d %", "preend"},
		{"open below", "one\ntwo", "o X esc", "one\nX\ntwo"},
		{"open above", "one\ntwo", "j O X esc", "one\nX\ntwo"},
		{"insert at first nonblank", "  abc", "I X esc", "  Xabc"},
		{"append to line", "one\ntwo", "A X esc", "oneX\ntwo"},
		{"append to whitespace", "  ", "I X esc", "  X"},
		{"counted insert", "abc", "3 a X esc", "aXXXbc"},
		{"no-op preserves register", "one two", "y i w d h $ p", "one twoone"},
		{"change empty pair", "()", "c i ( X esc", "(X)"},
		{"cancel partial edit", "one two", "d esc l", "one two"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := New(tc.initial, true)
			keys(t, m, tc.keys)
			if m.Value() != tc.want || m.Mode() != ModeNormal {
				t.Fatalf("got %q, mode %q; want %q in normal", m.Value(), m.Mode(), tc.want)
			}
		})
	}
}

func TestMotions(t *testing.T) {
	for _, tc := range []struct {
		initial, keys string
		cursor        int
	}{
		{"界xy", "2 l h", 1},
		{"ab.cd  ef", "w", 2}, {"ab.cd  ef", "2 w", 3}, {"ab.cd  ef", "W", 7},
		{"ab.cd  ef", "W B", 0}, {"ab.cd  ef", "W b", 3},
		{"ab.cd  ef", "e", 1}, {"ab.cd  ef", "E", 4},
		{"  xy", "$ 0", 0}, {"  xy", "^", 2}, {"  xy", "$", 3},
		{"a\n\ntwo", "w", 2}, {"a\n\ntwo", "G b", 2},
		{"a\n\ntwo", "2 w", 3}, {"a\n   \ntwo", "w", 6},
		{"a\nb\n  c\nd", "7 5 %", 6},
		{"abcd\nx\nwxyz", "$ j j", 10}, {"abcd\nx\nwxyz", "G 2 l 2 k", 2},
		{"one\ntwo\n  three", "G", 10}, {"one\ntwo\n  three", "2 g g", 4}, {"one\ntwo\n  three", "2 G", 4},
		{"one\n\ntwo\n\nthree", "}", 4}, {"one\n\ntwo\n\nthree", "2 } {", 4},
		{"x(a[b])", "%", 6}, {"x(a[b])", "% %", 1},
		{"a\n\nc", "j", 2}, {"a\n", "G", 2}, {"", "l j b e %", 0},
	} {
		m := New(tc.initial, true)
		keys(t, m, tc.keys)
		if m.Cursor() != tc.cursor {
			t.Errorf("%q on %q: cursor %d, want %d", tc.keys, tc.initial, m.Cursor(), tc.cursor)
		}
	}
}

func TestUndoRedoAndRepeat(t *testing.T) {
	m := New("one two three", false)
	keys(t, m, "c i w X Y backspace esc w .")
	if m.Value() != "X X three" {
		t.Fatalf("repeat change: %q", m.Value())
	}
	keys(t, m, "u")
	if m.Value() != "X two three" {
		t.Fatalf("undo repeated edit: %q", m.Value())
	}
	keys(t, m, "u")
	if m.Value() != "one two three" || m.View("in").Facts["dirty"] != false {
		t.Fatalf("undo original edit: %q", m.Value())
	}
	keys(t, m, "2 ctrl+r")
	if m.Value() != "X X three" {
		t.Fatalf("redo: %q", m.Value())
	}
	keys(t, m, "u d w ctrl+r")
	if m.Value() != "X three" {
		t.Fatalf("branched redo: %q", m.Value())
	}
	keys(t, m, ".")
	if m.Value() != "X " {
		t.Fatalf("repeat deletion: %q", m.Value())
	}
}

func TestResolvedCommands(t *testing.T) {
	m := New("one\ntwo\nthree", true)
	run := func(id string, count int) {
		t.Helper()
		if !m.HandleCommand(core.Command{ID: id, Count: count}) {
			t.Fatalf("unknown %q", id)
		}
	}
	run("motion.bottom", 1)
	if m.Cursor() != 0 {
		t.Fatal("explicit 1G must select first line")
	}
	run("motion.top", 2)
	run(CmdDelete, 0)
	run("motion.end", 0)
	if m.Value() != "one\n\nthree" {
		t.Fatalf("resolved edit: %q", m.Value())
	}

	if m.HandleCommand(core.Command{ID: "modal.close"}) {
		t.Fatal("claimed unrelated command")
	}
}

func TestCorePassthroughAndRendering(t *testing.T) {
	m := New("a", true)
	keymap, err := core.NewKeymap([]core.Mode{{Name: "input", Passthrough: true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	engine := core.NewEngine(keymap)
	state := core.NewState(core.Frame{ID: "page"})
	state.Modals = []core.Frame{{ID: "dialog", Kind: core.FrameModal, Mode: "input", FocusID: "in"}}
	for _, event := range []core.Event{
		core.KeyEvent{Key: "A"}, core.KeyEvent{Key: "enter"}, core.PasteEvent{Text: "界é"},
		core.KeyEvent{Key: "backspace"}, core.KeyEvent{Key: "esc"}, core.KeyEvent{Key: "esc"},
	} {
		step := engine.Step(state, ui.ContextChain(m.View("in"), "in", nil), event)
		state = step.State
		if len(step.Effects) != 1 {
			t.Fatalf("%T effects: %+v", event, step.Effects)
		}
		pass, ok := step.Effects[0].(core.PassToWidgetEffect)
		if !ok || pass.NodeID != "in" || !m.HandleInput(pass.Key, pass.Text) {
			t.Fatalf("undelivered %+v", pass)
		}
	}
	if len(state.Modals) != 1 || m.Value() != "a\n界" || m.Cursor() != 2 {
		t.Fatalf("model after escape: %q at %d", m.Value(), m.Cursor())
	}
	keys(t, m, "A")
	node := m.View("in")
	if node.Kind != ui.KindInput || !node.Focusable || node.Scope != "input" || node.Facts["mode"] != "insert" || node.Facts["dirty"] != true || node.Facts["multiline"] != true || node.Props["cursor"] != 3 {
		t.Fatalf("view %+v", node)
	}
	frame, _ := ui.Render(node, ui.DefaultDarkTheme(), "in", 20, 3)
	if frame.Cursor.X != 3 || frame.Cursor.Y != 1 || !strings.Contains(frame.ANSI, "界") {
		t.Fatalf("rendered frame: %+v", frame)
	}
	if node.CacheKey == 0 || node.CacheKey != m.View("in").CacheKey {
		t.Fatal("unstable cache key")
	}
	keys(t, m, "esc")
	if node.CacheKey == m.View("in").CacheKey {
		t.Fatal("cache did not capture mode/cursor")
	}
}

func TestSingleLine(t *testing.T) {
	m := New("", false)
	keys(t, m, "i")
	m.HandleCommand(core.Command{ID: CmdInsert, Args: map[string]any{"text": "a\r\nb"}})
	keys(t, m, "enter esc o O")
	if m.Value() != "a b" || m.Mode() != ModeNormal {
		t.Fatalf("single-line input: %q %s", m.Value(), m.Mode())
	}
}

func TestZeroValueAndExplicitUnnamedRegister(t *testing.T) {
	var m Model
	if m.Mode() != ModeNormal {
		t.Fatal(m.Mode())
	}
	keys(t, &m, "i a b esc 0")
	keys(t, &m, `" " y y $`)
	keys(t, &m, `" " p`)
	if m.Value() != "abab" {
		t.Fatalf("single-line paste: %q", m.Value())
	}
}

func TestWordScopes(t *testing.T) {
	// Path scope: a/b/c.go steps by segment and separator.
	m := New("a/b/c.go", false)
	m.SetWordScope("path")
	var stops []int
	for range 6 {
		m.HandleKey("w")
		stops = append(stops, m.Cursor())
	}
	wantPath := []int{1, 2, 3, 4, 5, 6}
	for i, w := range wantPath {
		if stops[i] != w {
			t.Fatalf("path w stops = %v, want %v", stops, wantPath)
		}
	}
	m.HandleKey("b")
	if m.Cursor() != 5 {
		t.Fatalf("path b cursor = %d, want 5", m.Cursor())
	}
	// Path scope on a/.hidden separates / from .
	m2 := New("a/.hidden", false)
	m2.SetWordScope("path")
	m2.HandleKey("w") // /
	m2.HandleKey("w") // .
	if m2.Cursor() != 2 {
		t.Fatalf("path a/.hidden 2w cursor = %d, want 2", m2.Cursor())
	}

	// Code scope: identifiers stay together, brackets separate from operators.
	mc := New("foo_1(!bar)", false)
	mc.SetWordScope("code")
	mc.HandleKey("w") // (
	if mc.Cursor() != 5 {
		t.Fatalf("code w1 = %d, want 5", mc.Cursor())
	}
	mc.HandleKey("w") // !
	if mc.Cursor() != 6 {
		t.Fatalf("code w2 = %d, want 6", mc.Cursor())
	}

	// JSON scope: {}[],:" are distinct delimiters.
	mj := New(`{"a":1}`, false)
	mj.SetWordScope("json")
	var jstops []int
	for range 6 {
		mj.HandleKey("w")
		jstops = append(jstops, mj.Cursor())
	}
	wantJSON := []int{1, 2, 3, 4, 5, 6}
	for i, w := range wantJSON {
		if jstops[i] != w {
			t.Fatalf("json w stops = %v, want %v", jstops, wantJSON)
		}
	}

	// CSV scope: comma is its own separator class.
	mcsv := New(`"a","b"`, false)
	mcsv.SetWordScope("csv")
	mcsv.HandleKey("3")
	mcsv.HandleKey("w") // lands on ',' at index 3
	if mcsv.Cursor() != 3 {
		t.Fatalf("csv 3w cursor = %d, want 3", mcsv.Cursor())
	}

	// Markdown scope: foo-bar stays one word; # * [ ] ( ) are delimiters.
	mmd := New("## foo-bar [baz](qux)", false)
	mmd.SetWordScope("markdown")
	mmd.HandleKey("w") // foo-bar at 3
	if mmd.Cursor() != 3 {
		t.Fatalf("md w1 = %d, want 3", mmd.Cursor())
	}
	mmd.HandleKey("w") // [ at 11 (skipped whole foo-bar!)
	if mmd.Cursor() != 11 {
		t.Fatalf("md w2 = %d, want 11", mmd.Cursor())
	}

	// Readline on Line and Model honors WordScope (path ctrl+w / alt+b).
	l := NewLine("src/pkg/file.go")
	l.SetWordScope("path")
	l.HandleCommand(core.Command{ID: CmdKillWordBack})
	if l.Value() != "src/pkg/file." || l.Kill != "go" {
		t.Fatalf("line path ctrl+w: val=%q kill=%q", l.Value(), l.Kill)
	}
	l.HandleCommand(core.Command{ID: CmdWordLeft}) // before '.'
	if l.Cursor != 12 {
		t.Fatalf("line path alt+b: cursor=%d, want 12", l.Cursor)
	}
	mi := New("foo-bar baz", false)
	mi.SetWordScope("markdown")
	mi.HandleKey("A") // insert at end
	mi.HandleCommand(core.Command{ID: CmdWordLeft})
	mi.HandleCommand(core.Command{ID: CmdKillWordBack})
	if mi.Value() != "baz" {
		t.Fatalf("model markdown insert ctrl+w: val=%q, want %q", mi.Value(), "baz")
	}
}
