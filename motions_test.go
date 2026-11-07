package ochazuke

import (
	"slices"
	"strings"
	"testing"
)

// The motion rows (W B E, } {, H M L, %) as keys: cursor moves, counts,
// operators and visual extension, through the editor's binding rows.
func TestMotionTableKeys(t *testing.T) {
	const words = "foo.bar baz-qux end"
	const paras = "one\ntwo\n\nthree\nfour\n\nfive"
	const screen = "  a\nb\nc\nd\n  e"
	const brackets = "a (b c) d"
	for _, tc := range []struct {
		name, text string
		cursor     int
		keys       []string
		want       string // text after
		wantCursor int
		mode       string
		register   string // the unnamed register, when set
	}{
		{"W", words, 0, []string{"W"}, words, 8, "normal", ""},
		{"2W", words, 0, []string{"2", "W"}, words, 16, "normal", ""},
		{"W at the end stays on the last character", words, 18, []string{"W"}, words, 18, "normal", ""},
		{"3W clamps to the last character", words, 0, []string{"3", "W"}, words, 18, "normal", ""},
		{"B", words, 18, []string{"B"}, words, 16, "normal", ""},
		{"2B", words, 18, []string{"2", "B"}, words, 8, "normal", ""},
		{"B at the start", words, 0, []string{"B"}, words, 0, "normal", ""},
		{"E", words, 0, []string{"E"}, words, 6, "normal", ""},
		{"2E", words, 0, []string{"2", "E"}, words, 14, "normal", ""},
		{"w keeps punctuation words", words, 0, []string{"w"}, words, 3, "normal", ""},
		{"dW", words, 0, []string{"d", "W"}, "baz-qux end", 0, "normal", "foo.bar "},
		{"2dW", words, 0, []string{"2", "d", "W"}, "end", 0, "normal", "foo.bar baz-qux "},
		{"d2W", words, 0, []string{"d", "2", "W"}, "end", 0, "normal", "foo.bar baz-qux "},
		{"dW on the last character", words, 18, []string{"d", "W"}, "foo.bar baz-qux en", 17, "normal", "d"},
		{"dE", words, 0, []string{"d", "E"}, " baz-qux end", 0, "normal", "foo.bar"},
		{"dB", words, 8, []string{"d", "B"}, "baz-qux end", 0, "normal", "foo.bar "},
		{"cW changes to the WORD's end", words, 0, []string{"c", "W"}, " baz-qux end", 0, "insert", "foo.bar"},
		{"yW", words, 8, []string{"y", "W"}, words, 8, "normal", "baz-qux "},
		{"vE extends", words, 0, []string{"v", "E", "y"}, words, 0, "normal", "foo.bar"},

		{"}", paras, 0, []string{"}"}, paras, 8, "normal", ""},
		{"} twice", paras, 0, []string{"}", "}"}, paras, 20, "normal", ""},
		{"} past the last paragraph", paras, 20, []string{"}"}, paras, 24, "normal", ""},
		{"2}", paras, 0, []string{"2", "}"}, paras, 20, "normal", ""},
		{"{", paras, 24, []string{"{"}, paras, 20, "normal", ""},
		{"2{", paras, 24, []string{"2", "{"}, paras, 8, "normal", ""},
		{"{ to the start", paras, 6, []string{"{"}, paras, 0, "normal", ""},
		{"d} from the line start", paras, 0, []string{"d", "}"}, "\nthree\nfour\n\nfive", 0, "normal", "one\ntwo\n"},
		{"d} inside a line keeps the line break", paras, 1, []string{"d", "}"}, "o\n\nthree\nfour\n\nfive", 0, "normal", "ne\ntwo"},
		{"y}", paras, 9, []string{"y", "}"}, paras, 9, "normal", "three\nfour\n"},
		{"d{", paras, 24, []string{"d", "{"}, "one\ntwo\n\nthree\nfour\ne", 20, "normal", "\nfiv"},
		{"v} extends", paras, 0, []string{"v", "}"}, paras, 8, "visual", ""},
		{"V} extends by lines", paras, 0, []string{"V", "}", "y"}, paras, 0, "normal", "one\ntwo\n\n"},

		{"H", screen, 12, []string{"H"}, screen, 2, "normal", ""},
		{"L", screen, 0, []string{"L"}, screen, 12, "normal", ""},
		{"M", screen, 0, []string{"M"}, screen, 6, "normal", ""},
		{"2H", screen, 12, []string{"2", "H"}, screen, 4, "normal", ""},
		{"2L", screen, 0, []string{"2", "L"}, screen, 8, "normal", ""},
		{"yH is linewise", screen, 6, []string{"y", "H"}, screen, 2, "normal", "  a\nb\nc\n"},
		{"dH on the top line deletes it", screen, 2, []string{"d", "H"}, "b\nc\nd\n  e", 0, "normal", "  a\n"},
		{"dL", screen, 6, []string{"d", "L"}, "  a\nb", 4, "normal", "c\nd\n  e\n"},
		{"vL extends", screen, 0, []string{"v", "L"}, screen, 12, "visual", ""},

		{"%", brackets, 0, []string{"%"}, brackets, 6, "normal", ""},
		{"% back", brackets, 6, []string{"%"}, brackets, 2, "normal", ""},
		{"% with no bracket after stays", brackets, 8, []string{"%"}, brackets, 8, "normal", ""},
		{"y%", brackets, 0, []string{"y", "%"}, brackets, 0, "normal", "a (b c)"},
		{"d% backward is inclusive", brackets, 6, []string{"d", "%"}, "a  d", 2, "normal", "(b c)"},
		{"d% with no bracket does nothing", brackets, 8, []string{"d", "%"}, brackets, 8, "normal", ""},
		{"v% extends", brackets, 0, []string{"v", "%", "y"}, brackets, 0, "normal", "a (b c)"},
		{"% across lines and nesting", "f(a,\n  (b))\nx", 1, []string{"%"}, "f(a,\n  (b))\nx", 10, "normal", ""},
		{"% unmatched fails", "(a", 0, []string{"%"}, "(a", 0, "normal", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, in := editorTestRuntime(t, tc.text, true)
			in.Cursor = tc.cursor
			r.Registers().Named["\""] = Register{}
			editorTestKeys(t, r, tc.keys...)
			editorTestState(t, in, tc.want, tc.wantCursor, tc.mode)
			if got := r.Registers().Named["\""].Yank.Text; got != tc.register {
				t.Fatalf("register=%q want %q", got, tc.register)
			}
		})
	}
}

// Every new motion is a no-op on an empty buffer and on a single line
// where it has nowhere to go, and never an error.
func TestMotionTableEdges(t *testing.T) {
	for _, text := range []string{"", "x", "abc"} {
		for _, keys := range [][]string{{"W"}, {"B"}, {"E"}, {"}"}, {"{"}, {"H"}, {"M"}, {"L"}, {"%"}, {"d", "W"}, {"d", "}"}, {"d", "%"}, {"y", "{"}, {"c", "W", "esc"}} {
			t.Run(text+"/"+strings.Join(keys, ""), func(t *testing.T) {
				r, in := editorTestRuntime(t, text, true)
				editorTestKeys(t, r, keys...)
				if in.Cursor < 0 || in.Cursor > len(in.Text) {
					t.Fatalf("cursor %d out of %q", in.Cursor, string(in.Text))
				}
			})
		}
	}
}

// A single-line input has no paragraphs: } and { are multiline rows, so
// they leave the cursor alone (and reach the outside keys).
func TestParagraphKeysSkipSingleLine(t *testing.T) {
	r, in := editorTestRuntime(t, "one two", false)
	in.Cursor = 2
	_, _ = editorTestDispatch(r, "}")
	_, _ = editorTestDispatch(r, "{")
	if in.Cursor != 2 || string(in.Text) != "one two" {
		t.Fatalf("cursor=%d text=%q", in.Cursor, string(in.Text))
	}
}

func TestMoveRefusesUnknownMotion(t *testing.T) {
	r, _ := editorTestRuntime(t, "abc", false)
	if _, err := r.Execute(Request{Verb: "move", Noun: "ochazuke.input", Args: Arguments{"motion": "nowhere"}}); err == nil {
		t.Fatal("unknown motion moved")
	}
	if _, err := r.Execute(Request{Verb: "operator", Noun: "ochazuke.input", Args: Arguments{"operator": "d", "motion": "nowhere"}}); err == nil {
		t.Fatal("unknown operator motion acted")
	}
}

// screenBuffer is a TextBuffer with a window, as an application's copy
// mode would implement it.
type screenBuffer struct {
	text         []rune
	first, count int
}

func (b screenBuffer) Runes() []rune                   { return b.text }
func (b screenBuffer) ScreenLines() (first, count int) { return b.first, b.count }

// H, M and L move within the lines on screen, from TextMotion over any
// TextBuffer.
func TestScreenMotionsOverTextBuffer(t *testing.T) {
	var lines []string
	for i := range 10 {
		lines = append(lines, strings.Repeat(" ", i%2)+string(rune('a'+i)))
	}
	b := screenBuffer{text: []rune(strings.Join(lines, "\n")), first: 3, count: 4} // lines 3..6
	lineOf := func(pos int) int { return lineNumber(b.text, pos) }
	for _, tc := range []struct {
		motion string
		steps  int
		line   int
	}{
		{"screen-top", 1, 3}, {"screen-top", 2, 4}, {"screen-top", 9, 6},
		{"screen-middle", 1, 4},
		{"screen-bottom", 1, 6}, {"screen-bottom", 3, 4}, {"screen-bottom", 9, 3},
	} {
		m, ok := TextMotion(tc.motion)
		if !ok {
			t.Fatalf("no motion %q", tc.motion)
		}
		pos, moved := m.To(b, MotionArgs{Pos: 0, Steps: tc.steps})
		if !moved || lineOf(pos) != tc.line {
			t.Fatalf("%s %d: line %d moved=%v, want line %d", tc.motion, tc.steps, lineOf(pos), moved, tc.line)
		}
		if start := lineStartAt(b.text, tc.line); pos != firstNonBlank(b.text, start) {
			t.Fatalf("%s %d: pos %d not the first non-blank", tc.motion, tc.steps, pos)
		}
	}
	// A screen past the text's end clamps to its last line.
	b.first, b.count = 8, 5
	m, _ := TextMotion("screen-bottom")
	if pos, _ := m.To(b, MotionArgs{Steps: 1}); lineOf(pos) != 9 {
		t.Fatalf("L past the end: line %d", lineOf(pos))
	}
}

// InputBlock records the lines it showed on its placement, so H and L in
// a buffer taller than its box stay within what the person saw.
func TestInputScreenFromItsDraw(t *testing.T) {
	var lines []string
	for i := range 30 {
		lines = append(lines, "line"+string(rune('A'+i)))
	}
	r := NewRuntime()
	m := NewModel(r)
	m.Width, m.Height = 40, 12
	in, err := r.OpenTextView("tall", strings.Join(lines, "\n"))
	if err != nil {
		t.Fatal(err)
	}
	editorTestKeys(t, r, "G")
	_ = m.View()
	first, count := in.ScreenLines()
	if first == 0 || count <= 1 || first+count != 30 {
		t.Fatalf("screen first=%d count=%d: want a window of the text", first, count)
	}
	editorTestKeys(t, r, "L")
	if got := lineNumber(in.Text, in.Cursor); got != first+count-1 {
		t.Fatalf("L: line %d want %d", got, first+count-1)
	}
	editorTestKeys(t, r, "H")
	if got := lineNumber(in.Text, in.Cursor); got != first {
		t.Fatalf("H: line %d want %d", got, first)
	}
	// The view keeps its lines while the cursor stays among them (the
	// text view's top), so M lands in their middle.
	_ = m.View()
	editorTestKeys(t, r, "M")
	if got := lineNumber(in.Text, in.Cursor); got != first+(count-1)/2 {
		t.Fatalf("M: line %d want %d", got, first+(count-1)/2)
	}
}

// The table declares every motion the editor's rows name, and the
// motions vocabulary namecheck reads includes the table's names.
func TestMotionRowsNameTableMotions(t *testing.T) {
	r := NewRuntime()
	for _, b := range r.DefaultBindings() {
		if b.Noun != EditorNoun {
			continue
		}
		name, ok := b.Request.Args["motion"].(string)
		if !ok || b.Request.Verb == "kill" {
			continue
		}
		if _, ok := TextMotion(name); !ok {
			t.Errorf("%s/%s %q names motion %q the table lacks", b.Mode, b.Keys, b.Request.Verb, name)
		}
	}
	vocab := r.NameVocab()
	for _, name := range MotionNames() {
		if !slices.Contains(vocab[name], "motion") {
			t.Errorf("vocab lacks motion %q", name)
		}
	}
}
