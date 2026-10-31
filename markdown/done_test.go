package markdown

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
)

const doneSource = "# Plan\n\n- [x] finished\n  - [ ] unfinished child\n  - [X] finished child\n  > **owner** today: concealed note\n\n- [ ] first\n- [x] other finished\n- [ ] last\n"

func TestDoneVisibilityAndMotions(t *testing.T) {
	for _, unit := range []string{"item", "line"} {
		t.Run(unit, func(t *testing.T) {
			text := doneSource
			r, d := newDoc(t, Source{Load: func() (string, error) { return text, nil }, Save: func(s string) error { text = s; return nil }})
			if err := r.Settings().Set(CursorSetting, unit); err != nil {
				t.Fatal(err)
			}
			if err := r.Settings().Set(DoneSetting, "hide"); err != nil {
				t.Fatal(err)
			}
			view := ansi.Strip(d.View(70, 0, mono))
			for _, hidden := range []string{"finished", "concealed note"} {
				if strings.Contains(view, hidden) {
					t.Fatalf("leaked %s:\n%s", hidden, view)
				}
			}
			if !strings.Contains(view, "3 done hidden") {
				t.Fatal(view)
			}
			if d.Text() != doneSource || len(d.Tasks()) != 6 {
				t.Fatalf("source/model changed: %q / %d", d.Text(), len(d.Tasks()))
			}
			if err := d.Select(0); err == nil {
				t.Fatal("selected hidden task")
			}
			for _, keys := range [][]string{{"]", "x"}, {"j"}, {"k"}, {"2", "j"}, {"g", "g"}, {"G"}, {"ctrl+u"}, {"ctrl+d"}} {
				if err := press(t, r, keys...); err != nil {
					t.Fatal(err)
				}
				e, _ := d.Cursor()
				if e < 0 || !d.drawn(e) || d.elems[e].Checked {
					t.Fatalf("%v selected concealed element %d", keys, e)
				}
			}
			if err := d.Select(3); err != nil {
				t.Fatal(err)
			}
			if err := press(t, r, "x"); err != nil {
				t.Fatal(err)
			}
			selected, ok := d.Selected()
			if !ok || selected.Text != "last" {
				t.Fatalf("toggle fallback: %+v", selected)
			}
			if err := press(t, r, "x"); err != nil {
				t.Fatal(err)
			}
			e, _ := d.Cursor()
			if e != 0 {
				t.Fatalf("last task should fall back to heading, got %d", e)
			}
			if !strings.Contains(text, "- [x] last") {
				t.Fatal(text)
			}
			if err := d.SetDone("show"); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(ansi.Strip(d.View(70, 0, mono)), "unfinished child") {
				t.Fatal("show did not restore subtree")
			}
			if err := r.Settings().Set(DoneSetting, "invalid"); err == nil {
				t.Fatal("accepted bad visibility")
			}
		})
	}
}

func TestDoneDocumentOverrideAndIndicator(t *testing.T) {
	r, d := newDoc(t, Source{Load: func() (string, error) { return doneSource, nil }})
	other, err := New(r, "other", "notes", Source{Load: func() (string, error) { return doneSource, nil }})
	if err != nil {
		t.Fatal(err)
	}
	for _, verb := range []string{"done-hide", "done-show", "done-toggle"} {
		if _, err := r.Execute(ochazuke.Request{Noun: DocNoun, Verb: verb, Args: ochazuke.Arguments{"id": d.ID()}}); err != nil {
			t.Fatal(err)
		}
	}
	if d.Done() != "hide" || other.Done() != "show" || r.Settings().String(DoneSetting) != "show" {
		t.Fatal("document override escaped its document")
	}
	if err := r.Settings().Set(DoneIndicatorSetting, "false"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ansi.Strip(d.View(60, 0, mono)), "done hidden") {
		t.Fatal("indicator opt-out ignored")
	}
	if err := d.SetDone(""); err != nil {
		t.Fatal(err)
	}
	if d.Done() != "show" {
		t.Fatal("did not reset override")
	}
	if err := r.Settings().Set(DoneSetting, "hide"); err != nil {
		t.Fatal(err)
	}
	if d.Done() != "hide" || other.Done() != "hide" {
		t.Fatal("live setting not inherited")
	}
	if err := d.Select(0); err == nil {
		t.Fatal("live setting left hidden cursor rows")
	}
	if err := d.SetDone("invalid"); err == nil {
		t.Fatal("invalid override accepted")
	}
}

func TestDoneAllHiddenAndEditing(t *testing.T) {
	for _, save := range []bool{true, false} {
		t.Run(fmt.Sprint(save), func(t *testing.T) {
			text := "- [ ] only\n  - [ ] child\n"
			src := Source{Load: func() (string, error) { return text, nil }}
			if save {
				src.Save = func(s string) error { text = s; return nil }
			}
			r, d := newDoc(t, src)
			if err := d.SetDone("hide"); err != nil {
				t.Fatal(err)
			}
			if save {
				if err := press(t, r, "x"); err != nil {
					t.Fatal(err)
				}
			} else {
				text = strings.Replace(text, "[ ]", "[x]", 1)
				if err := d.Reload(); err != nil {
					t.Fatal(err)
				}
			}
			if e, _ := d.Cursor(); e != -1 {
				t.Fatalf("hidden cursor: %d", e)
			}
			if d.list.Len() != 1 || r.Instances[d.ListID()].Unfocusable {
				t.Fatal("no focusable empty-view cursor")
			}
			for _, key := range []string{"j", "k", "G", "ctrl+d"} {
				if err := press(t, r, key); err != nil {
					t.Fatal(err)
				}
			}
			if err := d.SetDone("show"); err != nil {
				t.Fatal(err)
			}
			if e, _ := d.Cursor(); e < 0 {
				t.Fatal("show did not recover cursor")
			}
		})
	}
	text := "- [x] concealed\n- [ ] editable\n- [ ] next\n"
	r, d := newDoc(t, Source{Load: func() (string, error) { return text, nil }, Save: func(s string) error { text = s; return nil }})
	if err := d.SetDone("hide"); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"i", "a", "I", "A"} {
		if err := press(t, r, key); err != nil {
			t.Fatal(err)
		}
		if d.edit == nil || strings.Contains(string(d.edit.input.Text), "concealed") {
			t.Fatalf("%s opened hidden region", key)
		}
		if err := press(t, r, "esc"); err != nil {
			t.Fatal(err)
		}
	}
	if err := press(t, r, "o"); err != nil {
		t.Fatal(err)
	}
	if !d.Editing() {
		t.Fatal("open stopped working")
	}
	d.edit.input.Text = []rune("- [ ] new task")
	if err := press(t, r, "esc"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "new task") || !strings.Contains(text, "concealed") {
		t.Fatal(text)
	}
}

func TestDoneHidesWholeSourceSubtrees(t *testing.T) {
	for _, src := range []string{
		"- [x] done\n\n  paragraph of hidden continuation\n\n  ```\n  hidden code\n  ```\n\n  | a | b |\n  |---|---|\n  | hidden table | x |\n\n  > **owner** today: hidden note\n\n- [ ] visible\n",
		"1. [X] done\n   - [ ] hidden child\n2. [ ] visible\n",
		"- [x] done\n- [x] done\n- [ ] visible\n",
		"- [x] done\r\n  - [ ] hidden child\r\n- [ ] visible\r\n",
	} {
		t.Run(src, func(t *testing.T) {
			_, d := newDoc(t, Source{Load: func() (string, error) { return src, nil }})
			if err := d.SetDone("hide"); err != nil {
				t.Fatal(err)
			}
			view := ansi.Strip(d.View(45, 0, mono))
			if strings.Contains(view, "hidden child") || strings.Contains(view, "hidden continuation") || strings.Contains(view, "hidden note") || strings.Contains(view, "hidden table") || strings.Contains(view, "hidden code") || strings.Contains(view, "[x]") || strings.Contains(view, "[X]") {
				t.Fatal(view)
			}
			if !strings.Contains(view, "visible") {
				t.Fatal(view)
			}
			for i, e := range d.elems {
				if e.Checked && d.drawn(i) {
					t.Fatalf("drawn hidden element %d", i)
				}
			}
		})
	}
}

func TestDoneIndicatorStaysVisibleWhileScrolling(t *testing.T) {
	text := "- [x] done\n"
	for i := 0; i < 30; i++ {
		text += fmt.Sprintf("- [ ] visible %d\n", i)
	}
	r, d := newDoc(t, Source{Load: func() (string, error) { return text, nil }})
	if err := d.SetDone("hide"); err != nil {
		t.Fatal(err)
	}
	d.View(40, 7, mono)
	if err := press(t, r, "G"); err != nil {
		t.Fatal(err)
	}
	view := ansi.Strip(d.View(40, 7, mono))
	if d.offset == 0 || !strings.HasPrefix(view, "1 done hidden\n") || len(strings.Split(view, "\n")) > 7 {
		t.Fatalf("unpinned indicator:\n%s", view)
	}
	if task, ok := d.Selected(); !ok || task.Text != "visible 29" {
		t.Fatal("scroll lost cursor")
	}
	if view := d.View(8, 1, mono); len(strings.Split(view, "\n")) > 1 || ansi.StringWidth(view) > 8 {
		t.Fatalf("small view: %q", view)
	}
}

func TestDoneSourceProjectionBeyondMarkerLimit(t *testing.T) {
	prefix := strings.Repeat("plain\n", maxMarkers)
	src := prefix + "concealed\n"
	els := make([]Element, maxMarkers+1)
	hidden := make([]bool, len(els))
	for i := 0; i < maxMarkers; i++ {
		els[i] = Element{Start: i * 6, End: i*6 + 5, ms: -1, me: -1}
	}
	els[maxMarkers] = Element{Start: len(prefix), End: len(src) - 1, ms: -1, me: -1}
	hidden[maxMarkers] = true
	if got := markedSource(src, els, hidden); got != prefix {
		t.Fatal("element beyond marker limit escaped visibility projection")
	}
}

func TestDoneExternalChangeCannotOpenHiddenChild(t *testing.T) {
	text := "- [ ] parent\n  - child\n- next\n"
	r, d := newDoc(t, Source{Load: func() (string, error) { return text, nil }, Save: func(s string) error { text = s; return nil }})
	if err := d.SetDone("hide"); err != nil {
		t.Fatal(err)
	}
	if err := d.SelectElement(1); err != nil {
		t.Fatal(err)
	}
	text = strings.Replace(text, "[ ]", "[x]", 1)
	if err := press(t, r, "i"); err == nil || d.Editing() {
		t.Fatal("external change opened a hidden child")
	}
	if e, _ := d.Cursor(); e != 2 {
		t.Fatalf("external change fallback: %d", e)
	}
}
