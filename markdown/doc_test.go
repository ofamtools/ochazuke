package markdown

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
)

// Every list form is a task; nested items are tasks of their own; a
// "- [ ]" in a fenced or indented code block, or outside a list item, is
// not; each Box points between the [ and ].
func TestTasksFindsEveryItemAndItsBox(t *testing.T) {
	src := strings.Join([]string{
		"# Plan",
		"",
		"- [ ] dash",
		"* [x] star",
		"+ [X] plus",
		"1. [ ] numbered",
		"2) [x] paren",
		"- [ ] outer",
		"  - [ ] inner",
		"    1. [x] innermost",
		"> - [ ] quoted",
		"",
		"```",
		"- [ ] fenced code",
		"```",
		"",
		"    - [ ] indented code",
		"",
		"[ ] not in a list",
		"- plain item",
		"- [y] not a box",
		"",
	}, "\n")
	tasks := Tasks(src)
	want := []struct {
		text    string
		checked bool
		line    int
	}{
		{"dash", false, 3}, {"star", true, 4}, {"plus", true, 5}, {"numbered", false, 6}, {"paren", true, 7},
		{"outer", false, 8}, {"inner", false, 9}, {"innermost", true, 10}, {"quoted", false, 11},
	}
	if len(tasks) != len(want) {
		t.Fatalf("got %d tasks: %+v", len(tasks), tasks)
	}
	for i, w := range want {
		got := tasks[i]
		if got.Text != w.text || got.Checked != w.checked || got.Line != w.line {
			t.Errorf("task %d: %+v, want %+v", i, got, w)
		}
		if src[got.Box-1] != '[' || src[got.Box+1] != ']' || checked(src[got.Box]) != w.checked {
			t.Errorf("task %d: box at %d reads %q", i, got.Box, src[got.Box-1:got.Box+2])
		}
	}
}

func writeFile(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "notes.md")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func newDoc(t *testing.T, src Source) (*ochazuke.Runtime, *Doc) {
	t.Helper()
	r := ochazuke.NewRuntime()
	ochazuke.NewModel(r)
	if _, err := r.AddPage("notes", "Notes", func(*ochazuke.Runtime, int, int) ochazuke.Block { return ochazuke.Text("") }); err != nil {
		t.Fatal(err)
	}
	d, err := New(r, "doc", "notes", src)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("notes"); err != nil {
		t.Fatal(err)
	}
	if err := d.Focus(); err != nil {
		t.Fatal(err)
	}
	return r, d
}

func press(t *testing.T, r *ochazuke.Runtime, keys ...string) error {
	t.Helper()
	var last error
	for _, k := range keys {
		if _, err := r.Dispatch(ochazuke.MacroKey{Key: k}); err != nil {
			last = err
		}
	}
	return last
}

// The awkward bytes stay as they are: CRLF line ends, trailing spaces,
// tabs, wide characters, an X box, no final newline.
const awkward = "Intro  \r\n\r\n- [ ] café 日本\t\r\n- [X] two  \r\n\t- [ ] tabbed\r\n\r\n```\r\n- [ ] code\r\n```"

// A toggle leaves the file byte-identical but for the one box character,
// and keeps its permissions.
func TestToggleRewritesOnlyTheBox(t *testing.T) {
	path := writeFile(t, awkward)
	r, d := newDoc(t, File(path))
	if got := len(d.Tasks()); got != 3 {
		t.Fatalf("%d tasks", got)
	}
	if err := press(t, r, "j", "x"); err != nil {
		t.Fatal(err)
	}
	after := readFile(t, path)
	box := d.Tasks()[1].Box
	if len(after) != len(awkward) || after[:box] != awkward[:box] || after[box+1:] != awkward[box+1:] || after[box] != ' ' {
		t.Fatalf("toggle changed more than the box:\n%q\n%q", awkward, after)
	}
	if d.Tasks()[1].Checked {
		t.Fatal("the view did not re-read the toggle")
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v %v", info.Mode(), err)
	}
	if err := press(t, r, "k", "x"); err != nil {
		t.Fatal(err)
	}
	first := d.Tasks()[0].Box
	if got := readFile(t, path); got[first] != 'x' || got[:first] != after[:first] || got[first+1:] != after[first+1:] {
		t.Fatalf("second toggle: %q", got)
	}
}

// u undoes a toggle and ctrl+r redoes it, each written; an undo restores
// the very character (X, not x).
func TestUndoRedoWrite(t *testing.T) {
	path := writeFile(t, awkward)
	r, _ := newDoc(t, File(path))
	if err := press(t, r, "j", "x"); err != nil {
		t.Fatal(err)
	}
	toggled := readFile(t, path)
	if err := press(t, r, "u"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); got != awkward {
		t.Fatalf("undo:\n%q\nwant\n%q", got, awkward)
	}
	if err := press(t, r, "ctrl+r"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); got != toggled {
		t.Fatalf("redo:\n%q", got)
	}
	if err := press(t, r, "ctrl+r"); err == nil {
		t.Fatal("redo with nothing to redo must say so")
	}
}

// A change on disk between load and toggle: the item is found again by
// its text, and the toggle lands on it, the other change kept.
func TestToggleRefindsAfterExternalChange(t *testing.T) {
	path := writeFile(t, "- [ ] milk\n- [ ] bread\n")
	r, d := newDoc(t, File(path))
	if err := press(t, r, "j"); err != nil {
		t.Fatal(err)
	}
	changed := "# Shopping\n\n- [ ] eggs\n- [x] milk\n- [ ] bread\n"
	if err := os.WriteFile(path, []byte(changed), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := press(t, r, "x"); err != nil {
		t.Fatal(err)
	}
	want := "# Shopping\n\n- [ ] eggs\n- [x] milk\n- [x] bread\n"
	if got := readFile(t, path); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if sel, _ := d.Selected(); sel.Text != "bread" {
		t.Fatalf("selected %+v", sel)
	}
}

// An item gone (or its text changed) on disk refuses the toggle with a
// message and writes nothing; the view shows the source as it is now.
func TestToggleRefusesWhenTheItemIsGone(t *testing.T) {
	for name, changed := range map[string]string{
		"gone":    "- [ ] milk\n",
		"renamed": "- [ ] milk\n- [ ] brown bread\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := writeFile(t, "- [ ] milk\n- [ ] bread\n")
			r, d := newDoc(t, File(path))
			if err := press(t, r, "j"); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(changed), 0o600); err != nil {
				t.Fatal(err)
			}
			err := press(t, r, "x")
			if err == nil || !strings.Contains(err.Error(), `"bread"`) {
				t.Fatalf("toggle: %v", err)
			}
			if got := readFile(t, path); got != changed {
				t.Fatalf("wrote %q", got)
			}
			if d.Text() != changed {
				t.Fatal("the view must show the source as it is now")
			}
		})
	}
}

// A custom Source (a database, say): the framework makes the one-character
// edit and hands Save the whole text; a failed save writes nothing and
// says so; no Save is read-only.
func TestCustomSource(t *testing.T) {
	stored := "notes\n\n- [ ] one\n- [ ] two\n"
	var saves []string
	fail := false
	src := Source{
		Load: func() (string, error) { return stored, nil },
		Save: func(text string) error {
			if fail {
				return errors.New("database is locked")
			}
			saves = append(saves, text)
			stored = text
			return nil
		},
	}
	r, _ := newDoc(t, src)
	if err := press(t, r, "j", "x"); err != nil {
		t.Fatal(err)
	}
	if len(saves) != 1 || saves[0] != "notes\n\n- [ ] one\n- [x] two\n" {
		t.Fatalf("saves %q", saves)
	}
	fail = true
	if err := press(t, r, "x"); err == nil || !strings.Contains(err.Error(), "database is locked") {
		t.Fatalf("failed save: %v", err)
	}
	if stored != saves[0] {
		t.Fatal("a failed save changed the store")
	}
	_, ro := newDoc(t, Source{Load: func() (string, error) { return "- [ ] a\n", nil }})
	if err := ro.Toggle(); err == nil {
		t.Fatal("a Source without Save must refuse")
	}
}

// View maps each item to its rendered lines: j and k move the one
// reverse-video highlight item by item, a wrapped item highlighted whole,
// nothing highlighted once the focus leaves.
func TestViewHighlightsTheSelectedItem(t *testing.T) {
	src := "# List\n\n- [ ] first item that is long enough to wrap onto a second line here\n- [x] second\n  - [ ] nested\n\n```\n- [ ] code\n```\n"
	r, d := newDoc(t, Source{Load: func() (string, error) { return src, nil }})
	mono := ochazuke.Appearance{Theme: "mono"}
	reversed := func(view string) []string {
		var out []string
		for _, line := range strings.Split(view, "\n") {
			if strings.Contains(line, "\x1b[7m") {
				out = append(out, strings.TrimSpace(ansi.Strip(line)))
			}
		}
		return out
	}
	view := d.View(40, 0, mono)
	if got := reversed(view); len(got) != 2 || !strings.HasPrefix(got[0], "[ ] first") {
		t.Fatalf("first item: %q\n%s", got, ansi.Strip(view))
	}
	if strings.ContainsRune(view, startMarker) || strings.ContainsRune(view, endMarker) {
		t.Fatal("a marker leaked into the view")
	}
	_ = press(t, r, "j")
	if got := reversed(d.View(40, 0, mono)); len(got) != 1 || got[0] != "[x] second" {
		t.Fatalf("second: %q", got)
	}
	_ = press(t, r, "j")
	if got := reversed(d.View(40, 0, mono)); len(got) != 1 || got[0] != "[ ] nested" {
		t.Fatalf("nested: %q", got)
	}
	_ = press(t, r, "j") // the last item: nothing to spill to on this page
	if got := reversed(d.View(40, 0, mono)); len(got) != 1 || got[0] != "[ ] nested" {
		t.Fatalf("past the end: %q", got)
	}
	if err := r.SetFocus("notes"); err != nil {
		t.Fatal(err)
	}
	if got := reversed(d.View(40, 0, mono)); len(got) != 0 {
		t.Fatalf("unfocused: %q", got)
	}
	for _, line := range strings.Split(d.View(40, 0, mono), "\n") {
		if w := ansi.StringWidth(line); w > 40 {
			t.Fatalf("line %d wide: %q", w, ansi.Strip(line))
		}
	}
}

// A windowed View keeps the selected item on screen and says where it is.
func TestViewWindowsToTheSelection(t *testing.T) {
	var b strings.Builder
	b.WriteString("# Long\n\n")
	for i := range 30 {
		b.WriteString("- [ ] item " + string(rune('a'+i%26)) + string(rune('a'+i/26)) + "\n")
	}
	r, d := newDoc(t, Source{Load: func() (string, error) { return b.String(), nil }})
	mono := ochazuke.Appearance{Theme: "mono"}
	_ = press(t, r, "2", "5", "j")
	view := d.View(40, 10, mono)
	lines := strings.Split(view, "\n")
	if len(lines) != 10 || !strings.Contains(lines[9], " / ") {
		t.Fatalf("window:\n%s", ansi.Strip(view))
	}
	if !strings.Contains(view, "\x1b[7m") {
		t.Fatalf("the selected item scrolled away:\n%s", ansi.Strip(view))
	}
}

// A document without task items is not a focus stop.
func TestNoTasksIsNoStop(t *testing.T) {
	r, d := newDoc(t, Source{Load: func() (string, error) { return "# Just text\n", nil }})
	if !r.Instances[d.ListID()].Unfocusable {
		t.Fatal("an empty task list must not be a stop")
	}
	if err := d.Toggle(); err == nil {
		t.Fatal("toggle with no task must say so")
	}
}

// The markdown rows are atomic, and the footer offers x, u and ctrl+r on
// a task item.
func TestRowsAreAtomicAndHinted(t *testing.T) {
	r, _ := newDoc(t, Source{Load: func() (string, error) { return "- [ ] a\n", nil }, Save: func(string) error { return nil }})
	if warnings, err := r.ValidateBindings(); err != nil || len(warnings) != 0 {
		t.Fatalf("%v %v", warnings, err)
	}
	hints := ochazuke.BindingHints(r, r.Focus)
	for _, want := range []string{"x markdown.toggle", "u markdown.undo", "ctrl+r markdown.redo"} {
		if !strings.Contains(hints, want) {
			t.Fatalf("hints lack %q: %s", want, hints)
		}
	}
}
