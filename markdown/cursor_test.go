package markdown

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"ofam.dev/ochazuke"
)

func TestItemCursorMovesAcrossElements(t *testing.T) {
	text := "# Heading\n\nA paragraph long enough to wrap over several drawn lines in a small pane.\n\n- [ ] a task that also wraps over several drawn lines in this pane\n  - nested\n\n| a | b |\n|---|---|\n| 日本 | row |\n\n```\ncode one\ncode two\n```\n"
	r, d := newDoc(t, Source{Load: func() (string, error) { return text, nil }, Save: func(s string) error { text = s; return nil }})
	d.View(32, 0, mono)
	for e := range d.elems {
		if !d.drawn(e) {
			continue
		}
		got, off := d.Cursor()
		if got != e || off != 0 {
			t.Fatalf("cursor %d/%d, want element %d first line", got, off, e)
		}
		if e < len(d.elems)-1 {
			if err := press(t, r, "j"); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := press(t, r, "2", "k"); err != nil {
		t.Fatal(err)
	}
	if got, _ := d.Cursor(); got != len(d.elems)-3 {
		t.Fatalf("2k: element %d", got)
	}
	if err := d.Select(0); err != nil {
		t.Fatal(err)
	}
	before := text
	if err := press(t, r, "x"); err != nil {
		t.Fatal(err)
	}
	if text != strings.Replace(before, "[ ]", "[x]", 1) {
		t.Fatalf("item toggle: %q", text)
	}
	for _, key := range []string{"i", "a", "I", "A"} {
		if err := press(t, r, key); err != nil {
			t.Fatal(err)
		}
		if !d.Editing() || !strings.Contains(string(d.edit.input.Text), "- [x] a task") {
			t.Fatalf("%s did not edit the whole task", key)
		}
		if err := press(t, r, "esc"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestItemCursorFullWidth(t *testing.T) {
	for _, text := range []string{
		"- [x] 日本 café é 👩‍💻 finish a long task with several wrapped lines in a narrow pane\n- next\n",
		"- parent\n  - [x] 日本 nested task with long text wrapping across several drawn lines here\n  - sibling\n",
		"| name | detail |\n|---|---|\n| 日本 | a very long table row that wraps across several lines |\n| next | done |\n",
	} {
		r, d := newDoc(t, Source{Load: func() (string, error) { return text, nil }})
		d.View(36, 0, mono)
		e := 0
		if d.elems[0].Kind == KindTable || strings.Contains(text, "parent") {
			e = 1
		}
		if err := d.SelectElement(e); err != nil {
			t.Fatal(err)
		}
		check := func(focused bool) {
			view := strings.Split(d.View(36, 0, mono), "\n")
			span := d.lay.spans[e]
			if span[1] <= span[0] {
				t.Fatalf("fixture did not wrap: %q", text)
			}
			st := lipgloss.NewStyle().Reverse(true)
			for l, line := range view {
				if l < span[0] || l > span[1] {
					continue
				}
				plain := ansi.Truncate(ansi.Strip(d.lay.lines[l]), 36, "")
				want := st.Render(plain + strings.Repeat(" ", 36-ansi.StringWidth(plain)))
				if !focused {
					want = ochazuke.NewStyles(mono).Fade(r.Settings().Int(ochazuke.DimUnfocusedSetting)).Render(want)
				}
				if line != want || ansi.StringWidth(line) != 36 {
					t.Fatalf("full-width cursor on line %d focused=%v:\ngot  %q\nwant %q", l, focused, line, want)
				}
			}
		}
		check(true)
		if err := r.SetFocus("notes"); err != nil {
			t.Fatal(err)
		}
		check(false)
	}
}

func TestItemCursorLineSettingChangesLive(t *testing.T) {
	r, d := newDoc(t, Source{Load: func() (string, error) {
		return "- [ ] first task with enough text to wrap over several drawn lines in this pane\n- [ ] second\n", nil
	}})
	d.View(32, 0, mono)
	if err := r.Settings().Set(CursorSetting, "line"); err != nil {
		t.Fatal(err)
	}
	if err := press(t, r, "j"); err != nil {
		t.Fatal(err)
	}
	if e, off := d.Cursor(); e != 0 || off != 1 {
		t.Fatalf("line mode: %d/%d", e, off)
	}
	if err := r.Settings().Set(CursorSetting, "item"); err != nil {
		t.Fatal(err)
	}
	if err := press(t, r, "j"); err != nil {
		t.Fatal(err)
	}
	if e, off := d.Cursor(); e != 1 || off != 0 {
		t.Fatalf("item mode: %d/%d", e, off)
	}
	if err := press(t, r, "k"); err != nil {
		t.Fatal(err)
	}
	if e, off := d.Cursor(); e != 0 || off != 0 {
		t.Fatalf("k: %d/%d", e, off)
	}
	if err := r.Settings().Set(CursorSetting, "invalid"); err == nil {
		t.Fatal("invalid cursor setting accepted")
	}
}

func TestItemCursorLongElementPacingAndScrolling(t *testing.T) {
	text := "# Start\n\n```\n" + strings.Repeat("code line\n", 35) + "```\n\n# End\n"
	r, d := newDoc(t, Source{Load: func() (string, error) { return text, nil }})
	d.View(36, 12, mono)
	if err := press(t, r, "j"); err != nil {
		t.Fatal(err)
	}
	d.View(36, 12, mono)
	if e, _ := d.Cursor(); e != 1 {
		t.Fatalf("j did not enter code: %d", e)
	}
	for n := 0; n < 20; n++ {
		from := d.offset
		if err := press(t, r, "j"); err != nil {
			t.Fatal(err)
		}
		d.View(36, 12, mono)
		if d.offset-from > 5 {
			t.Fatalf("j scrolled %d lines, max 5", d.offset-from)
		}
		if e, _ := d.Cursor(); e == 2 {
			break
		}
	}
	if e, _ := d.Cursor(); e != 2 {
		t.Fatal("never reached End")
	}
	if err := d.SelectElement(1); err != nil {
		t.Fatal(err)
	}
	d.View(36, 12, mono)
	from := d.offset
	if err := press(t, r, "5", "ctrl+e"); err != nil {
		t.Fatal(err)
	}
	d.View(36, 12, mono)
	if d.offset != from+5 {
		t.Fatalf("ctrl+e in code: offset %d want %d", d.offset, from+5)
	}
	if e, _ := d.Cursor(); e != 1 {
		t.Fatal("line scroll left code item")
	}
}
