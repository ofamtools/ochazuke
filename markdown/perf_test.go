package markdown

import (
	tea "charm.land/bubbletea/v2"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

// benchDocText is a ~500-line document of headings, paragraphs, lists,
// tasks, code and a table, the shape of a working notes file.
func benchDocText() string {
	var b strings.Builder
	for s := 0; len(strings.Split(b.String(), "\n")) < 500; s++ {
		fmt.Fprintf(&b, "## Section %d\n\nSome paragraph text for section %d with **bold**, `code` and a [link](https://example.com/%d).\nIt runs over a second line so wrapping has work to do at narrow widths.\n\n", s, s, s)
		fmt.Fprintf(&b, "- [ ] task one in %d\n- [x] task two in %d\n- plain bullet\n  - nested bullet\n\n", s, s)
		fmt.Fprintf(&b, "1. first\n2. second\n\n```go\nfunc f%d() int { return %d }\n```\n\n", s, s)
		if s%4 == 0 {
			b.WriteString("| a | b |\n|---|---|\n| 1 | 2 |\n\n")
		}
	}
	return b.String()
}

func benchDoc(b *testing.B) (*ochazuke.Runtime, *Doc) { return benchDocOf(b, benchDocText()) }

func benchDocOf(b *testing.B, text string) (*ochazuke.Runtime, *Doc) {
	b.Helper()
	path := filepath.Join(b.TempDir(), "bench.md")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		b.Fatal(err)
	}
	r := ochazuke.NewRuntime()
	ochazuke.NewModel(r)
	if _, err := r.AddPage("notes", "Notes", func(*ochazuke.Runtime, int, int) ochazuke.Block { return ochazuke.Text("") }); err != nil {
		b.Fatal(err)
	}
	d, err := New(r, "doc", "notes", File(path))
	if err != nil {
		b.Fatal(err)
	}
	if err := r.OpenPage("notes"); err != nil {
		b.Fatal(err)
	}
	if err := d.Focus(); err != nil {
		b.Fatal(err)
	}
	d.View(100, 40, ochazuke.Appearance{Theme: "adaptive", Dark: true})
	return r, d
}

func benchPress(b *testing.B, r *ochazuke.Runtime, k string) {
	if _, err := r.Dispatch(ochazuke.MacroKey{Key: k}); err != nil {
		b.Fatalf("press %q: %v", k, err)
	}
}

// BenchmarkInsertKeystroke is one keystroke in insert mode as a host
// runs it: the key, the pending command check and a frame.
func BenchmarkInsertKeystroke(b *testing.B) {
	r, d := benchDoc(b)
	a := ochazuke.Appearance{Theme: "adaptive", Dark: true}
	for range 20 {
		benchPress(b, r, "j")
	}
	benchPress(b, r, "A")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Type and erase in turn, so the block keeps its length.
		if i%2 == 0 {
			benchPress(b, r, "x")
		} else {
			benchPress(b, r, "backspace")
		}
		d.PendingCommand()
		d.View(100, 40, a)
	}
}

// BenchmarkInsertFlush is one debounce write while typing: the pause
// timer fires, the element is written and the source parsed again.
func BenchmarkInsertFlush(b *testing.B) {
	r, d := benchDoc(b)
	a := ochazuke.Appearance{Theme: "adaptive", Dark: true}
	for range 20 {
		benchPress(b, r, "j")
	}
	benchPress(b, r, "A")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchPress(b, r, "x")
		d.PendingCommand()
		msg, _ := d.DebounceTick()
		d.HandleMessage(msg)
		d.View(100, 40, a)
	}
}

// BenchmarkIdleFrame is a frame with insert mode open and nothing typed.
func BenchmarkIdleFrame(b *testing.B) {
	r, d := benchDoc(b)
	a := ochazuke.Appearance{Theme: "adaptive", Dark: true}
	benchPress(b, r, "A")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.PendingCommand()
		d.View(100, 40, a)
	}
}

// BenchmarkEditLeave is a whole edit: open, one key, leave (write, tidy,
// lay the document out again).
func BenchmarkEditLeave(b *testing.B) { benchEditLeave(b, benchDoc) }

// BenchmarkEditLeaveLarge is BenchmarkEditLeave in a ~3000-line document.
func BenchmarkEditLeaveLarge(b *testing.B) {
	benchEditLeave(b, func(b *testing.B) (*ochazuke.Runtime, *Doc) {
		return benchDocOf(b, strings.Repeat(benchDocText(), 6))
	})
}

func benchEditLeave(b *testing.B, open func(*testing.B) (*ochazuke.Runtime, *Doc)) {
	r, d := open(b)
	a := ochazuke.Appearance{Theme: "adaptive", Dark: true}
	for range 20 {
		benchPress(b, r, "j")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchPress(b, r, "A")
		benchPress(b, r, "x")
		benchPress(b, r, "esc")
		d.View(100, 40, a)
	}
}

// BenchmarkRenderSize draws the bench document repeated n times, its
// last run changed each time (as an edit changes one block), to show how
// a drawing after an edit scales with the document.
func BenchmarkRenderSize(b *testing.B) {
	for _, n := range []int{1, 2, 4} {
		for _, nl := range []bool{false, true} {
			b.Run(fmt.Sprintf("x%d/newlines=%v", n, nl), func(b *testing.B) {
				text := strings.Repeat(benchDocText(), n)
				els := Elements(text)
				opts := options{newlines: nl}
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					src := markedSource(text, els, nil) + strings.Repeat(" ", i%2)
					_ = renderUncached(src, 100, StyleDark, opts)
				}
			})
		}
	}
}

// benchModel is the bench document on a page drawn by a Model, the way
// o-md hosts it.
func benchModel(b *testing.B) (*ochazuke.Model, *Doc) {
	b.Helper()
	path := filepath.Join(b.TempDir(), "bench.md")
	if err := os.WriteFile(path, []byte(benchDocText()), 0o600); err != nil {
		b.Fatal(err)
	}
	r := ochazuke.NewRuntime()
	m := ochazuke.NewModel(r)
	var d *Doc
	if _, err := r.AddPage("notes", "Notes", func(_ *ochazuke.Runtime, w, h int) ochazuke.Block {
		return d.Block(w, h, *m.Appearance)
	}); err != nil {
		b.Fatal(err)
	}
	var err error
	d, err = New(r, "doc", "notes", File(path), PreserveNewlines())
	if err != nil {
		b.Fatal(err)
	}
	if err := r.OpenPage("notes"); err != nil {
		b.Fatal(err)
	}
	if err := d.Focus(); err != nil {
		b.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.View()
	return m, d
}

func modelKey(m *ochazuke.Model, code rune, text string) {
	m.Update(tea.KeyPressMsg{Code: code, Text: text})
	m.View()
}

// BenchmarkModelInsertKeystroke is a keystroke in insert mode through
// the Model: update and frame.
func BenchmarkModelInsertKeystroke(b *testing.B) {
	m, d := benchModel(b)
	for range 20 {
		modelKey(m, 'j', "j")
	}
	modelKey(m, 'A', "A")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i%2 == 0 {
			modelKey(m, 'x', "x")
		} else {
			modelKey(m, tea.KeyBackspace, "")
		}
	}
	b.StopTimer()
	if !d.Editing() {
		b.Fatal("insert mode was left")
	}
}

// BenchmarkModelNormalMove is j/k in normal mode through the Model.
func BenchmarkModelNormalMove(b *testing.B) {
	m, _ := benchModel(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i%40 < 20 {
			modelKey(m, 'j', "j")
		} else {
			modelKey(m, 'k', "k")
		}
	}
}
