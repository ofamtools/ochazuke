package ochazuke

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"ofam.dev/ochazuke/keytest"
)

// The shell renders through Layout.Shell: every viewport is exactly filled,
// the mode badge sits on the title row and the footer is the focus's own
// binding rows, so rebinding a key changes the hint.
func TestModelChromeIsLayoutShellWithHintsFromBindings(t *testing.T) {
	r := NewRuntime()
	page := pageTestRegister(t, r, "alpha")
	pageTestRegister(t, r, "beta")
	page.Render = func(_ *Runtime, width, height int) Block {
		return Text(strings.TrimSuffix(strings.Repeat(strings.Repeat("░", width)+"\n", height), "\n"))
	}
	m := NewModel(r)
	if _, err := r.Execute(Request{Verb: "set", Noun: "ochazuke.appearance", Args: Arguments{"theme": "mono"}}); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("alpha"); err != nil {
		t.Fatal(err)
	}
	for _, sidebar := range []bool{false, true} {
		m.Sidebar = sidebar
		for _, size := range [][2]int{{80, 24}, {56, 18}, {48, 15}} {
			t.Run(fmt.Sprintf("sidebar=%t/%dx%d", sidebar, size[0], size[1]), func(t *testing.T) {
				m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				view := ansi.Strip(m.View().Content)
				rows := strings.Split(view, "\n")
				if len(rows) != size[1] {
					t.Fatalf("got %d rows, want %d", len(rows), size[1])
				}
				for i, row := range rows {
					if ansi.StringWidth(row) != size[0] {
						t.Fatalf("row %d has %d cells, want %d: %q", i, ansi.StringWidth(row), size[0], row)
					}
				}
				layout := Layout{Width: size[0], Height: size[1], Styles: NewStyles(*m.Appearance)}
				bw, bh := layout.BodySize()
				if !sidebar && strings.Count(view, "░") != bw*bh {
					t.Fatalf("page body is not Layout.BodySize %dx%d:\n%s", bw, bh, view)
				}
				if !strings.HasPrefix(strings.TrimSpace(rows[0]), "◆ Ochazuke") || !strings.HasSuffix(strings.TrimSpace(rows[0]), "NORMAL") {
					t.Fatalf("title row lacks the title and mode badge: %q", rows[0])
				}
				if sidebar && !strings.Contains(view, "▸ ALPHA") || !sidebar && !strings.Contains(rows[1], "› ALPHA") {
					t.Fatalf("page navigation missing (sidebar=%t):\n%s", sidebar, view)
				}
				if footer := rows[size[1]-2]; !hintPrefix(footer, BindingHints(r, r.Focus)) || !strings.Contains(BindingHints(r, r.Focus), "? bindings.help") || strings.Contains(BindingHints(r, r.Focus), "ochazuke.page.next") {
					t.Fatalf("footer is not the focus's binding hints: %q", footer)
				}
			})
		}
	}
	m.Sidebar = false
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	// The hint is the same data bindings.list prints: a rebound key moves with it.
	if err := r.Bind(Binding{Noun: "app", Mode: "normal", Keys: "?", Request: Request{Verb: "open", Noun: "ochazuke.yank-history"}}); err != nil {
		t.Fatal(err)
	}
	footer := strings.Split(ansi.Strip(m.View().Content), "\n")[22]
	hints := BindingHints(r, r.Focus)
	if !hintPrefix(footer, hints) || !strings.Contains(hints, "? yank-history.open") || strings.Contains(hints, "? bindings.help") {
		t.Fatalf("footer did not follow the rebinding: %q (%q)", footer, hints)
	}
	// An input in insert mode shows the mode badge; the editor's insert
	// rows are hidden (? help lists them) unless help.hints-hidden.
	if err := r.SetFocus("alpha-input"); err != nil {
		t.Fatal(err)
	}
	keytest.Press(m, "i")
	rows := strings.Split(ansi.Strip(m.View().Content), "\n")
	if insert := BindingHints(r, r.Focus); !strings.HasSuffix(strings.TrimSpace(rows[0]), "INSERT") || !(insert == "" && strings.TrimSpace(rows[22]) == "" || hintPrefix(rows[22], insert)) || strings.Contains(insert, "ctrl+w") || strings.Contains(insert, "ochazuke.input.move") {
		t.Fatalf("insert mode chrome: title %q footer %q", rows[0], rows[22])
	}
	if err := r.Settings().Set(HintsShowHiddenSetting, true); err != nil {
		t.Fatal(err)
	}
	if insert := BindingHints(r, r.Focus); !strings.Contains(insert, "ctrl+w input.kill") || strings.Contains(insert, "ochazuke.input.move") {
		t.Fatalf("help.hints-hidden: %q", insert)
	}
	if err := r.Settings().Set(HintsShowHiddenSetting, false); err != nil {
		t.Fatal(err)
	}
	// An error replaces the hints until the next key.
	keytest.Press(m, "esc")
	m.Status = "unknown target"
	if footer := strings.Split(ansi.Strip(m.View().Content), "\n")[22]; !strings.Contains(footer, "Error: unknown target") {
		t.Fatalf("error footer: %q", footer)
	}
	// A modal's hint is its focus's rows: the modal noun's esc among them.
	m.Status = ""
	if err := r.Push("note", "app"); err != nil {
		t.Fatal(err)
	}
	m.ModalViews["note"] = ModalView{Title: "Note", Render: func(*Runtime, *Instance, int, int) Block { return Text("read me") }}
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "esc modal.pop") {
		t.Fatalf("modal hint is not derived from the modal rows:\n%s", view)
	}
}

// hintPrefix reports whether a rendered footer row is the (possibly
// truncated) hint line.
func hintPrefix(row, hints string) bool {
	text := strings.TrimSuffix(strings.TrimSpace(row), "…")
	return text != "" && strings.HasPrefix(hints, text)
}

// A notice stays in the footer across keys and errors until notice.clear;
// the framework binds no key to it.
func TestModelNoticePersistsUntilCleared(t *testing.T) {
	r := NewRuntime()
	pageTestRegister(t, r, "alpha")
	m := NewModel(r)
	if err := r.OpenPage("alpha"); err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: 200, Height: 12})
	m.Notify("ext: noun x skipped")
	footer := func() string { return m.footer().Text }
	if !strings.HasPrefix(footer(), "Notice: ext: noun x skipped · ") {
		t.Fatalf("footer = %q", footer())
	}
	m.Status = "boom" // the last key's error
	if f := m.footer(); !f.Error || f.Text != "Error: boom · Notice: ext: noun x skipped" {
		t.Fatalf("with an error: %+v", f)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.Status != "" || !strings.Contains(footer(), "Notice: ext: noun x skipped") || !strings.Contains(ansi.Strip(m.View().Content), "Notice: ext: noun x skipped") {
		t.Fatalf("after the next key: footer %q", footer())
	}
	if got, err := r.Execute(Request{Verb: "list", Noun: "ochazuke.notice"}); err != nil || fmt.Sprint(got) != "[ext: noun x skipped]" {
		t.Fatalf("notice.list = %v, %v", got, err)
	}
	if got, err := r.Execute(Request{Verb: "clear", Noun: "ochazuke.notice"}); err != nil || got != 1 || len(m.Notices) != 0 || strings.Contains(footer(), "Notice") {
		t.Fatalf("notice.clear = %v, %v; footer %q", got, err, footer())
	}
	for _, n := range r.Nouns {
		for _, rows := range n.Bindings {
			for _, row := range rows {
				if row.Request.Noun == "ochazuke.notice" {
					t.Fatalf("%s binds %s", n.Name, row.Request.Key())
				}
			}
		}
	}
}
