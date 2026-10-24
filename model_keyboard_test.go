package ochazuke

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"ochazuke/internal/keytest"
)

func modelType(m *Model, text string, batched bool) {
	if batched {
		keytest.Type(m, text)
		return
	}
	for _, ch := range text {
		keytest.Type(m, string(ch))
	}
}

func TestModelMacroRegisterTransitionsAndPalette(t *testing.T) {
	for _, batched := range []bool{false, true} {
		for _, focused := range []bool{false, true} {
			t.Run(fmt.Sprintf("batched=%t/focused=%t", batched, focused), func(t *testing.T) {
				r := NewRuntime()
				if focused {
					r, _ = editorTestRuntime(t, "", true)
				}
				m := NewModel(r)
				modelType(m, "q", batched)
				if !strings.Contains(ansi.Strip(m.View()), "choose a register") {
					t.Fatal("recording prefix did not prompt for one register character")
				}
				modelType(m, "q", batched)
				if r.Macro.Recording != "q" || !strings.Contains(ansi.Strip(m.View()), "REC @q") {
					t.Fatalf("second q did not immediately start visible recording: %+v", r.Macro)
				}
				modelType(m, ":", batched)
				if m.Status != "" || !m.Palette.IsActive() || r.Macro.Recording != "q" {
					t.Fatalf("qq: status=%q palette=%t macro=%+v", m.Status, m.Palette.IsActive(), r.Macro)
				}
				if _, err := r.Execute(Request{Verb: "close", Noun: "palette"}); err != nil {
					t.Fatal(err)
				}
				modelType(m, "q", batched)
				if got := r.Registers["q"].Yank.Text; got != ":" || r.Macro.Recording != "" {
					t.Fatalf("stop saved %q; want only colon and no recording", got)
				}

				freshRuntime := NewRuntime()
				if focused {
					freshRuntime, _ = editorTestRuntime(t, "", true)
				}
				fresh := NewModel(freshRuntime)
				modelType(fresh, "qq:", batched)
				if fresh.Status != "" || !fresh.Palette.IsActive() || fresh.Runtime.Macro.Recording != "q" {
					t.Fatalf("uninterrupted qq: status=%q palette=%t recording=%q", fresh.Status, fresh.Palette.IsActive(), fresh.Runtime.Macro.Recording)
				}
			})
		}
	}
}

func TestModelMacroConsumesOnlyOneRegisterCharacter(t *testing.T) {
	for _, batched := range []bool{false, true} {
		t.Run(fmt.Sprintf("batched=%t", batched), func(t *testing.T) {
			r, in := editorTestRuntime(t, "one two", true)
			m := NewModel(r)
			modelType(m, "qabc", batched)
			if m.Status != "" || r.Macro.Recording != "a" || in.Pending != "c" || !strings.Contains(ansi.Strip(m.View()), "REC @a") {
				t.Fatalf("qabc status=%q recording=%q pending=%q", m.Status, r.Macro.Recording, in.Pending)
			}
			keytest.Press(m, "esc")
			modelType(m, "q", batched)
			if got := r.Registers["a"].Yank.Text; got != "bc\x1b" {
				t.Fatalf("recorded register=%q, want b,c,Escape (not part of register name)", got)
			}
		})
	}
}

func TestModelMacroRecordingReplayAndLiteralPaste(t *testing.T) {
	for _, batched := range []bool{false, true} {
		t.Run(fmt.Sprintf("batched=%t", batched), func(t *testing.T) {
			r, in := editorTestRuntime(t, "", true)
			m := NewModel(r)
			modelType(m, "qqi", batched)
			keytest.Paste(m, "qabc:@")
			keytest.Press(m, "esc")
			modelType(m, "q2@q", batched)
			if got := string(in.Text); got != "qabc:qabc:qabc:@@@" || in.Mode != "normal" || m.Status != "" || r.Macro.Recording != "" {
				t.Fatalf("replay text=%q mode=%q status=%q recording=%q", got, in.Mode, m.Status, r.Macro.Recording)
			}
			if got := r.Registers["q"].Keys; !reflect.DeepEqual(got, []MacroKey{{Key: "i"}, {Key: "[qabc:@]", Paste: true}, {Key: "esc"}}) {
				t.Fatalf("recorded keys=%+v; expected literal paste once without stop q", got)
			}
		})
	}
}

func TestModelInvalidRegisterRecoversAtNextCommand(t *testing.T) {
	for _, batched := range []bool{false, true} {
		for _, prefix := range []string{"q", "@", "\""} {
			t.Run(fmt.Sprintf("batched=%t/prefix=%s", batched, prefix), func(t *testing.T) {
				r, in := editorTestRuntime(t, "preserve", true)
				m := NewModel(r)
				modelType(m, prefix+"!", batched)
				if m.Status == "" {
					t.Fatalf("invalid register error=%q", m.Status)
				}
				modelType(m, ":", batched)
				if !m.Palette.IsActive() || m.Status != "" || string(in.Text) != "preserve" {
					t.Fatalf("invalid register trapped next command: status=%q palette=%t text=%q", m.Status, m.Palette.IsActive(), string(in.Text))
				}
			})
		}
	}
}

func TestModelNonCharacterRegisterErrorDoesNotTrapCommands(t *testing.T) {
	for _, prefix := range []string{"q", "@", "\""} {
		t.Run(prefix, func(t *testing.T) {
			r, in := editorTestRuntime(t, "preserve", true)
			m := NewModel(r)
			modelType(m, prefix, true)
			keytest.Press(m, "tab")
			if m.Status == "" {
				t.Fatalf("non-character register error=%q", m.Status)
			}
			modelType(m, "iX", true)
			if m.Status != "" || string(in.Text) != "Xpreserve" || in.Mode != "insert" {
				t.Fatalf("register error trapped editing: status=%q text=%q mode=%q", m.Status, string(in.Text), in.Mode)
			}
		})
	}
}

func TestModelPendingMacroDoesNotInterpretPasteAsRegister(t *testing.T) {
	r, in := editorTestRuntime(t, "preserve", true)
	m := NewModel(r)
	modelType(m, "q", true)
	keytest.Paste(m, "qabc:")
	if m.Status != "" || r.Macro.Pending != "q" || string(in.Text) != "preserve" {
		t.Fatalf("paste consumed register prefix: status=%q pending=%q text=%q", m.Status, r.Macro.Pending, string(in.Text))
	}
	modelType(m, "q", true)
	if r.Macro.Recording != "q" || !strings.Contains(ansi.Strip(m.View()), "REC @q") {
		t.Fatalf("next typed register did not start recording: %+v", r.Macro)
	}
}

func TestModelMacroRecordsPaletteTypingWithoutStopping(t *testing.T) {
	r, in := editorTestRuntime(t, "preserve", true)
	m := NewModel(r)
	modelType(m, "qq:q", true)
	if !m.Palette.IsActive() || m.Palette.query != "q" || r.Macro.Recording != "q" || m.Status != "" {
		t.Fatalf("palette typing stopped recording: query=%q recording=%q status=%q", m.Palette.query, r.Macro.Recording, m.Status)
	}
	keytest.Press(m, "ctrl+c")
	modelType(m, "q", true)
	want := []MacroKey{{Key: ":"}, {Key: "q"}, {Key: "ctrl+c"}}
	if got := r.Registers["q"].Keys; !reflect.DeepEqual(got, want) {
		t.Fatalf("palette macro=%+v; want %+v", got, want)
	}
	modelType(m, "@q", true)
	if r.Macro.Last != "q" || m.Palette.IsOpen() || r.Quit || m.Status != "" || string(in.Text) != "preserve" {
		t.Fatalf("palette replay last=%q open=%t quit=%t status=%q text=%q", r.Macro.Last, m.Palette.IsOpen(), r.Quit, m.Status, string(in.Text))
	}
}

func TestNewModelNavigationDefaultsAndRemovedAliases(t *testing.T) {
	r := NewRuntime()
	pageTestRegister(t, r, "alpha")
	pageTestRegister(t, r, "beta")
	if err := r.OpenPage("alpha"); err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	keytest.Press(m, "ctrl+j")
	if r.ActivePage != "beta" {
		t.Fatalf("Ctrl-J page=%q, want beta", r.ActivePage)
	}
	keytest.Press(m, "ctrl+k")
	if r.ActivePage != "alpha" {
		t.Fatalf("Ctrl-K page=%q, want alpha", r.ActivePage)
	}
	for _, key := range []string{"ctrl+p", "alt+right", "alt+left"} {
		keytest.Press(m, key)
		if r.ActivePage != "alpha" || m.Palette.IsActive() {
			t.Fatalf("obsolete key %q navigated: page=%q palette=%t", key, r.ActivePage, m.Palette.IsActive())
		}
	}
	modelType(m, ":", true)
	if !m.Palette.IsActive() {
		t.Fatal("normal colon did not open palette")
	}
	if _, err := r.Execute(Request{Verb: "close", Noun: "palette"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Focus("alpha-input"); err != nil {
		t.Fatal(err)
	}
	modelType(m, "i:", true)
	keytest.Press(m, "ctrl+k")
	if got := string(r.Inputs["alpha-input"].Text); got != ":" || r.ActivePage != "alpha" || m.Palette.IsActive() {
		t.Fatalf("insert keys text=%q page=%q palette=%t", got, r.ActivePage, m.Palette.IsActive())
	}
	keytest.Press(m, "ctrl+@")
	if !m.Palette.IsActive() {
		t.Fatal("Ctrl-Space did not open palette from insert mode")
	}
}

func TestModelPageChromeKeepsActiveTabVisibleWithManyPages(t *testing.T) {
	r := NewRuntime()
	for i := range 65 {
		id := fmt.Sprintf("section-%c%c", 'a'+rune(i/26), 'a'+rune(i%26))
		pageTestRegister(t, r, id)
	}
	m := NewModel(r)
	for _, active := range []string{"section-aa", "section-bg", "section-cm"} {
		if err := r.OpenPage(active); err != nil {
			t.Fatal(err)
		}
		for _, width := range []int{32, 80, 120} {
			m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
			lines := strings.Split(ansi.Strip(m.View()), "\n")
			tabs := lines[1]
			if !strings.Contains(tabs, "› "+strings.ToUpper(active)) {
				t.Fatalf("active page missing from %d-column tabs: %q", width, tabs)
			}
			if strings.ContainsAny(tabs, "0123456789") || ansi.StringWidth(tabs) > width || len(lines) != 24 {
				t.Fatalf("unbounded or numbered chrome: %q (%d lines)", tabs, len(lines))
			}
		}
	}
}
