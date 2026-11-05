package ochazuke

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestGlyphChangesKeepModalCursor(t *testing.T) {
	t.Parallel()
	r := NewRuntime()
	m := NewModel(r)
	m.Width, m.Height = 60, 20
	p, err := r.AddPage("test", "Test", func(*Runtime, int, int) Block { return Text("日本語") })
	if err != nil {
		t.Fatal(err)
	}
	in := NewInput("name", "日本語", false)
	if err = r.AddInput(in, Parent(p.ID)); err != nil {
		t.Fatal(err)
	}
	if err = r.SetFocus("name"); err != nil {
		t.Fatal(err)
	}
	if _, err = r.OpenModal("prompt-test", Payload{Prompt: "日本語"}, WithInput(NewInput("prompt-line", "é界", false), Kind("prompt"))); err != nil {
		t.Fatal(err)
	}
	unicode := m.View()
	focus, mode := r.Focus, r.Mode
	if unicode.Cursor == nil {
		t.Fatal("modal has no cursor")
	}
	if err = r.Settings().Set(ChromeGlyphsSetting, "ascii"); err != nil {
		t.Fatal(err)
	}
	ascii := m.View()
	if !reflect.DeepEqual(unicode.Cursor, ascii.Cursor) || r.Focus != focus || !reflect.DeepEqual(r.Mode, mode) {
		t.Fatal("ASCII conversion changed cursor, focus or mode")
	}
	old, new := strings.Split(unicode.Content, "\n"), strings.Split(ascii.Content, "\n")
	if len(old) != len(new) {
		t.Fatal("frame height changed")
	}
	for i, line := range new {
		for _, r := range line {
			if r > 127 {
				t.Fatalf("row %d contains %U", i, r)
			}
		}
		if ansi.StringWidth(line) != ansi.StringWidth(old[i]) {
			t.Fatalf("row %d changed width", i)
		}
	}
}

func TestGlyphChangesRefreshTreeAndCardFolds(t *testing.T) {
	t.Parallel()
	r := NewRuntime()
	m := NewModel(r)
	tree, err := r.AddTree("tree", []TreeNode{{ID: "root", Label: "Root"}, {ID: "child", Parent: "root", Label: "Child"}})
	if err != nil {
		t.Fatal(err)
	}
	cards, err := r.AddTimeline("cards", []Card{{ID: "card", Header: "Card", Open: true, Sections: []CardSection{{Title: "Section", Body: TextBody("Body")}}}})
	if err != nil {
		t.Fatal(err)
	}
	if text := tree.View(40, 10); !strings.Contains(text, "▾") {
		t.Fatal(text)
	}
	if text := cards.View(40, 10); !strings.Contains(text, "▾") {
		t.Fatal(text)
	}
	if err = r.Settings().Set(ChromeGlyphsSetting, "nerd"); err != nil {
		t.Fatal(err)
	}
	_ = m.Styles()
	if text := tree.View(40, 10); !strings.Contains(text, "\uf078") {
		t.Fatalf("tree retained old fold mark: %s", text)
	}
	if text := cards.View(40, 10); !strings.Contains(text, "\uf078") || strings.Contains(text, "▾") {
		t.Fatalf("card body retained old fold mark: %s", text)
	}
}

func TestDirectStylesFollowChromeSettings(t *testing.T) {
	t.Parallel()
	r := NewRuntime()
	m := NewModel(r)
	if err := r.Settings().Set(ChromeBorderSetting, "classic"); err != nil {
		t.Fatal(err)
	}
	if err := r.Settings().Set(ChromeGlyphsSetting, "nerd"); err != nil {
		t.Fatal(err)
	}
	s := NewStyles(*m.Appearance)
	if got := s.FocusedPanel.GetBorderStyle().TopLeft; got != "╭" {
		t.Fatalf("direct styles missed classic: %q", got)
	}
	if got := s.Glyph("title.mark"); got != "\uf219" {
		t.Fatalf("direct styles missed Nerd Font: %q", got)
	}
	if !strings.Contains(s.ActiveTab.Render("Tab"), "\uf105") {
		t.Fatal("direct active tab missed glyph setting")
	}
}
