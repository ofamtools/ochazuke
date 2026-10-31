package markdown

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
)

var mono = ochazuke.Appearance{Theme: "mono"}

// plainLines is a rendering's lines without styles or trailing padding.
func plainLines(text string) []string {
	var out []string
	for _, line := range strings.Split(ansi.Strip(text), "\n") {
		out = append(out, strings.TrimRight(line, " "))
	}
	return out
}

// With PreserveNewlines every line of a paragraph is a line of its own;
// without it Glamour joins them. A long line still wraps.
func TestPreserveNewlinesKeepsParagraphLines(t *testing.T) {
	src := "line one\nline two\nline three\n\na line long enough that it has to wrap at thirty columns\n"
	joined := strings.Join(plainLines(Block(src, 30, mono).Text), "\n")
	if !strings.Contains(joined, "line one line two") {
		t.Fatalf("default rendering kept the newlines:\n%s", joined)
	}
	got := plainLines(Block(src, 30, mono, PreserveNewlines()).Text)
	want := []string{"  line one", "  line two", "  line three", ""}
	if len(got) < len(want)+2 {
		t.Fatalf("too few lines:\n%s", strings.Join(got, "\n"))
	}
	for i, w := range want {
		if got[i] != w {
			t.Fatalf("line %d = %q, want %q:\n%s", i, got[i], w, strings.Join(got, "\n"))
		}
	}
	for _, line := range got {
		if ansi.StringWidth(line) > 30 {
			t.Fatalf("line %q wider than 30", line)
		}
	}
	if len(got) < 6 {
		t.Fatalf("the long line did not wrap:\n%s", strings.Join(got, "\n"))
	}
}

// Code fences and tables draw the same with the option as without it.
func TestPreserveNewlinesLeavesCodeAndTables(t *testing.T) {
	for _, src := range []string{
		"```go\nfunc main() {\n\tprintln(1)\n}\n```\n",
		"| a | b |\n|---|---|\n| 1 | 2 |\n| 3 | 4 |\n",
	} {
		for _, a := range []ochazuke.Appearance{mono, {Theme: "adaptive", Dark: true}} {
			if plain, kept := Block(src, 40, a).Text, Block(src, 40, a, PreserveNewlines()).Text; plain != kept {
				t.Fatalf("%q in %s differs:\n%s\n---\n%s", src, a.Theme, plain, kept)
			}
		}
	}
}

// The option is part of the cache key: both renderings are kept and each
// is served for its own option.
func TestPreserveNewlinesIsCachedSeparately(t *testing.T) {
	src := "cached one\ncached two\n"
	plain := Block(src, 33, mono).Text
	kept := Block(src, 33, mono, PreserveNewlines()).Text
	if plain == kept {
		t.Fatal("the option did not change the rendering")
	}
	cache.Lock()
	_, a := cache.renderings[renderKey{src, 0, 33, StylePlain, options{}}]
	_, b := cache.renderings[renderKey{src, 0, 33, StylePlain, options{newlines: true}}]
	cache.Unlock()
	if !a || !b {
		t.Fatalf("cached: default %v, preserved %v", a, b)
	}
	if Block(src, 33, mono).Text != plain || Block(src, 33, mono, PreserveNewlines()).Text != kept {
		t.Fatal("a cached rendering was served for the other option")
	}
}

// A Doc with the option draws one line per source line, the highlight
// still covers exactly the selected item's lines, and toggles rewrite
// only the box byte.
func TestPreserveNewlinesDoc(t *testing.T) {
	src := "Plan  \r\nfirst line\r\nsecond line\r\n\r\n- [ ] café 日本\t\r\n  more of it\r\n- [X] two\r\n\r\nafter one\r\nafter two\r\n\r\n```\r\n- [ ] code\r\n```"
	path := writeFile(t, src)
	r, d := newDoc(t, File(path), PreserveNewlines())
	_ = r.Settings().Set(CursorSetting, "line")
	view := d.View(40, 0, mono)
	lines := plainLines(view)
	for _, want := range []string{"first line", "second line", "after one", "after two"} {
		found := false
		for _, line := range lines {
			if strings.TrimSpace(line) == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("%q is not a line of its own:\n%s", want, strings.Join(lines, "\n"))
		}
	}
	reversed := func(view string) []string {
		var out []string
		for _, line := range strings.Split(view, "\n") {
			if strings.Contains(line, "\x1b[7m") {
				out = append(out, strings.TrimSpace(ansi.Strip(line)))
			}
		}
		return out
	}
	if err := press(t, r, "]", "x"); err != nil {
		t.Fatal(err)
	}
	if got := reversed(d.View(40, 0, mono)); len(got) != 1 || got[0] != "[ ] café 日本" {
		t.Fatalf("first item highlight: %q\n%s", got, ansi.Strip(view))
	}
	if err := press(t, r, "j"); err != nil {
		t.Fatal(err)
	}
	if got := reversed(d.View(40, 0, mono)); len(got) != 1 || got[0] != "more of it" {
		t.Fatalf("its second line: %q", got)
	}
	if err := press(t, r, "]", "x"); err != nil {
		t.Fatal(err)
	}
	if got := reversed(d.View(40, 0, mono)); len(got) != 1 || got[0] != "[x] two" {
		t.Fatalf("second item highlight: %q", got)
	}
	before := readFile(t, path)
	if err := press(t, r, "x"); err != nil {
		t.Fatal(err)
	}
	after := readFile(t, path)
	box := d.Tasks()[1].Box
	if len(after) != len(before) || after[:box] != before[:box] || after[box+1:] != before[box+1:] || after[box] != ' ' {
		t.Fatalf("toggle changed more than the box:\n%q\n%q", before, after)
	}
	if err := press(t, r, "u"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); got != before {
		t.Fatalf("undo did not restore the bytes:\n%q\n%q", before, got)
	}
	if got := reversed(d.View(40, 0, mono)); len(got) != 1 || got[0] != "[x] two" {
		t.Fatalf("after undo: %q", got)
	}
}
