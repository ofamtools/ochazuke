package ochazuke

import (
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/unixsuperhero/ochazuke/internal/keytest"
)

// keysText is the line the editing-key tests edit: words split by a
// space, a dot and a space, so the readline word (alt+b) and the WORD
// (ctrl+w) end in different places.
const keysText = "foo bar.baz qux"

// insertAt is a focused single-line input holding keysText in insert mode
// with the cursor at cursor.
func insertAt(t *testing.T, cursor int) (*Runtime, *Input) {
	t.Helper()
	r, in := editorTestRuntime(t, keysText, false)
	if err := r.SetMode("insert"); err != nil {
		t.Fatal(err)
	}
	in.Cursor = cursor
	return r, in
}

// The default insert rows: every input answers the standard deleting and
// moving keys (bindings/editor.json), each as the readline or macOS key it
// names.
func TestInputEditingKeys(t *testing.T) {
	cases := []struct {
		key    string
		cursor int
		text   string
		want   int
	}{
		// Delete the word before the cursor: what alt+b moves over.
		{"alt+backspace", 11, "foo bar. qux", 8},
		{"ctrl+backspace", 11, "foo bar. qux", 8},
		// ctrl+w stays the WORD (whitespace) kill.
		{"ctrl+w", 11, "foo  qux", 4},
		// Delete to the line's start.
		{"super+backspace", 11, " qux", 0},
		{"ctrl+u", 11, " qux", 0},
		// Delete the word after the cursor: what alt+f moves over.
		{"alt+d", 4, "foo .baz qux", 4},
		{"ctrl+delete", 4, "foo .baz qux", 4},
		// Delete to the line's end.
		{"ctrl+k", 4, "foo ", 4},
		// Word motions.
		{"alt+left", 11, keysText, 8},
		{"ctrl+left", 11, keysText, 8},
		{"alt+right", 4, keysText, 7},
		{"ctrl+right", 4, keysText, 7},
		{"alt+b", 11, keysText, 8},
		{"alt+f", 4, keysText, 7},
		// Line start and end.
		{"super+left", 11, keysText, 0},
		{"super+right", 4, keysText, len(keysText)},
		{"meta+left", 11, keysText, 0},
		{"meta+right", 4, keysText, len(keysText)},
		{"ctrl+a", 11, keysText, 0},
		{"ctrl+e", 4, keysText, len(keysText)},
	}
	for _, c := range cases {
		t.Run(c.key, func(t *testing.T) {
			r, in := insertAt(t, c.cursor)
			editorTestKeys(t, r, c.key)
			editorTestState(t, in, c.text, c.want, "insert")
		})
	}
}

// A kill goes to the yank history: ctrl+y puts the word alt+backspace
// took back.
func TestInputWordKillYanks(t *testing.T) {
	r, in := insertAt(t, 11)
	editorTestKeys(t, r, "alt+backspace", "ctrl+y")
	editorTestState(t, in, keysText, 11, "insert")
	editorTestKeys(t, r, "alt+d")
	editorTestState(t, in, "foo bar.baz", 11, "insert")
	editorTestKeys(t, r, "ctrl+y")
	editorTestState(t, in, keysText, 15, "insert")
}

// With a select range open (a rename), every deleting key removes the
// range, whatever its motion, and every moving key closes it, keeps the
// text and moves as it would (UX-RULES R9.6).
func TestInputEditingKeysOverSelection(t *testing.T) {
	kills := []string{"alt+backspace", "ctrl+backspace", "ctrl+w", "super+backspace", "ctrl+u", "alt+d", "ctrl+delete", "ctrl+k", "backspace", "delete"}
	for _, key := range kills {
		t.Run(key, func(t *testing.T) {
			r, in := insertAt(t, 0)
			if err := in.SelectText(4, 11); err != nil {
				t.Fatal(err)
			}
			editorTestKeys(t, r, key)
			editorTestState(t, in, "foo  qux", 4, "insert")
			if _, _, ok := in.SelectedText(); ok {
				t.Fatal("the range stayed open")
			}
		})
	}
	moves := []struct {
		key  string
		want int
	}{
		{"left", 4}, {"ctrl+b", 4}, {"right", 11}, {"ctrl+f", 11},
		{"alt+left", 8}, {"ctrl+left", 8}, {"alt+b", 8},
		{"alt+right", 15}, {"ctrl+right", 15}, {"alt+f", 15},
		{"home", 0}, {"ctrl+a", 0}, {"super+left", 0},
		{"end", 15}, {"ctrl+e", 15}, {"super+right", 15},
	}
	for _, c := range moves {
		t.Run(c.key, func(t *testing.T) {
			r, in := insertAt(t, 0)
			if err := in.SelectText(4, 11); err != nil {
				t.Fatal(err)
			}
			editorTestKeys(t, r, c.key)
			editorTestState(t, in, keysText, c.want, "insert")
			if _, _, ok := in.SelectedText(); ok {
				t.Fatal("the range stayed open")
			}
		})
	}
}

// A prompt (the palette's line, a modal question) is an input like any
// other: it answers the same rows.
func TestPromptEditingKeys(t *testing.T) {
	r := NewRuntime()
	in := NewInput("ask", keysText, false)
	if err := r.AddInput(in, Kind("ochazuke.prompt")); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFocus(in.ID); err != nil {
		t.Fatal(err)
	}
	in.Cursor = 11
	editorTestKeys(t, r, "ctrl+backspace")
	editorTestState(t, in, "foo bar. qux", 8, "insert")
	editorTestKeys(t, r, "super+backspace")
	editorTestState(t, in, " qux", 0, "insert")
	editorTestKeys(t, r, "alt+d")
	editorTestState(t, in, "", 0, "insert")
}

// What the terminal sends for each key, recorded, as Bubble Tea decodes it
// (the reader ochazuke's Model gets keys from): each lands on the key name
// a row binds. Bubble Tea v2 pushes the kitty keyboard protocol's
// disambiguate flag (1) and modifyOtherKeys 2 on start, so a terminal that
// speaks kitty (Ghostty; tuios for its panes) sends the CSI u forms; tuios
// encodes a pane's keys from the flags the pane pushed (vt.EncodeKeyCSIu).
// Ghostty's default macOS keybinds turn some keys into text before any
// encoding: super+backspace into 0x15 (ctrl+u), super+left 0x01 (ctrl+a),
// super+right 0x05 (ctrl+e), alt+left ESC b (alt+b), alt+right ESC f
// (alt+f).
func TestTerminalSequencesReachTheRows(t *testing.T) {
	cases := []struct {
		name, bytes, key string
	}{
		{"kitty ctrl+backspace", "\x1b[127;5u", "ctrl+backspace"},
		{"kitty alt+backspace", "\x1b[127;3u", "alt+backspace"},
		{"kitty super+backspace", "\x1b[127;9u", "super+backspace"},
		{"legacy alt+backspace", "\x1b\x7f", "alt+backspace"},
		{"modifyOtherKeys ctrl+backspace", "\x1b[27;5;127~", "ctrl+backspace"},
		{"legacy ctrl+backspace (BS)", "\x08", "ctrl+h"},
		{"ghostty super+backspace", "\x15", "ctrl+u"},
		{"ghostty super+left", "\x01", "ctrl+a"},
		{"ghostty super+right", "\x05", "ctrl+e"},
		{"ghostty alt+left", "\x1bb", "alt+b"},
		{"ghostty alt+right", "\x1bf", "alt+f"},
		{"ctrl+w", "\x17", "ctrl+w"},
		{"legacy alt+d", "\x1bd", "alt+d"},
		{"kitty alt+d", "\x1b[100;3u", "alt+d"},
		{"ctrl+left", "\x1b[1;5D", "ctrl+left"},
		{"ctrl+right", "\x1b[1;5C", "ctrl+right"},
		{"alt+left", "\x1b[1;3D", "alt+left"},
		// A kitty terminal spells cmd+arrow CSI 1;9D (kitty's super bit,
		// 8), which the decoder reads with xterm's bits as meta; tuios
		// decodes it so and re-encodes meta as kitty's 32, CSI 1;33D,
		// which reads as super. Both spellings are bound.
		{"kitty super+left", "\x1b[1;9D", "meta+left"},
		{"kitty super+right", "\x1b[1;9C", "meta+right"},
		{"tuios super+left", "\x1b[1;33D", "super+left"},
		{"tuios super+right", "\x1b[1;33C", "super+right"},
		{"ctrl+delete", "\x1b[3;5~", "ctrl+delete"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var d uv.EventDecoder
			n, ev := d.Decode([]byte(c.bytes))
			press, ok := ev.(uv.KeyPressEvent)
			if !ok || n != len(c.bytes) {
				t.Fatalf("decoded %T %v from %d of %d bytes", ev, ev, n, len(c.bytes))
			}
			msg := tea.KeyPressMsg(press)
			if got := msg.String(); got != c.key {
				t.Fatalf("decoded %q, want %q", got, c.key)
			}
		})
	}
	// End to end: the decoded message, through the Model, edits the input.
	r, in := insertAt(t, 11)
	m := NewModel(r)
	var d uv.EventDecoder
	_, ev := d.Decode([]byte("\x1b[127;5u"))
	m.Update(tea.KeyPressMsg(ev.(uv.KeyPressEvent)))
	editorTestState(t, in, "foo bar. qux", 8, "insert")
}

// rename.select is a framework setting, on by default.
func TestRenameSelectSettingDefault(t *testing.T) {
	r := NewRuntime()
	if !r.RenameSelect() {
		t.Fatal("rename.select is off before it is declared")
	}
	r.EnsureConfigSettings()
	if !r.RenameSelect() {
		t.Fatal("rename.select is off by default")
	}
	if err := r.Settings().Set(RenameSelectSetting, false); err != nil {
		t.Fatal(err)
	}
	if r.RenameSelect() {
		t.Fatal("rename.select stays on after set false")
	}
}

// i (outline.edit with no select) renames the line: it opens with the
// whole text selected, typing replaces it.
func TestOutlineEditRenameSelects(t *testing.T) {
	r, m, _, writes := treeFixture(t)
	keytest.Press(m, "i")
	in := r.Input(r.Focus)
	if lo, hi, ok := in.SelectedText(); !ok || lo != 0 || hi != 5 || r.Mode.Name != "insert" {
		t.Fatalf("selected %d %d %v mode %s", lo, hi, ok, r.Mode.Name)
	}
	m.Update(keytest.Msg("z", true, false))
	keytest.Press(m, "esc")
	if want := []string{"rename a: z"}; !slices.Equal(*writes, want) {
		t.Fatalf("writes %v, want %v", *writes, want)
	}
}

// Every motion closes the rename's selection, keeps the name and moves
// as it would; esc keeps the name and leaves, writing nothing.
func TestOutlineRenameMotionsKeepTheName(t *testing.T) {
	moves := []struct {
		key  string
		want int
	}{
		{"end", 5}, {"ctrl+e", 5}, {"super+right", 5},
		{"home", 0}, {"ctrl+a", 0}, {"super+left", 0},
		{"left", 0}, {"ctrl+b", 0}, {"right", 5}, {"ctrl+f", 5},
		{"alt+b", 0}, {"alt+left", 0}, {"ctrl+left", 0},
		{"alt+f", 5}, {"alt+right", 5}, {"ctrl+right", 5},
	}
	for _, c := range moves {
		t.Run(c.key, func(t *testing.T) {
			r, m, _, writes := treeFixture(t)
			keytest.Press(m, "i")
			in := r.Input(r.Focus)
			keytest.Press(m, c.key)
			if string(in.Text) != "alpha" || in.Cursor != c.want {
				t.Fatalf("text %q cursor %d, want alpha at %d", string(in.Text), in.Cursor, c.want)
			}
			if _, _, ok := in.SelectedText(); ok {
				t.Fatal("the selection stayed open")
			}
			m.Update(keytest.Msg("!", true, false))
			keytest.Press(m, "esc")
			want := "alpha"[:c.want] + "!" + "alpha"[c.want:]
			if got := []string{"rename a: " + want}; !slices.Equal(*writes, got) {
				t.Fatalf("writes %v, want %v", *writes, got)
			}
		})
	}
	r, m, tree, writes := treeFixture(t)
	keytest.Press(m, "i")
	keytest.Press(m, "esc")
	if len(*writes) != 0 || r.Mode.Name != "normal" || tree.Nodes()[0].Label != "alpha" {
		t.Fatalf("esc: writes %v mode %s", *writes, r.Mode.Name)
	}
}

// rename.select off: i opens the line with the cursor at its end, and an
// explicit select still selects.
func TestOutlineEditRenameSelectOff(t *testing.T) {
	r, m, _, writes := treeFixture(t)
	r.EnsureConfigSettings()
	if err := r.Settings().Set(RenameSelectSetting, false); err != nil {
		t.Fatal(err)
	}
	keytest.Press(m, "i")
	in := r.Input(r.Focus)
	if _, _, ok := in.SelectedText(); ok || in.Cursor != 5 {
		t.Fatalf("off opened selected (cursor %d)", in.Cursor)
	}
	m.Update(keytest.Msg("!", true, false))
	keytest.Press(m, "esc")
	if want := []string{"rename a: alpha!"}; !slices.Equal(*writes, want) {
		t.Fatalf("writes %v, want %v", *writes, want)
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.outline", Verb: "edit", Args: Arguments{"select": true}}); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := r.Input(r.Focus).SelectedText(); !ok {
		t.Fatal("select: true did not select with the setting off")
	}
}

// A record editor with Rename opens its field selected while rename.select
// is on, and with the cursor at the end when it is off; Insert alone never
// selects.
func TestRecordEditorRenameSelects(t *testing.T) {
	open := func(t *testing.T, rename, insert, on bool) (*Runtime, *Model, *Input, *[]string) {
		t.Helper()
		r, m, ed, _ := recordFixture(t, nil)
		keytest.Press(m, "q")
		if !ed.Closed() {
			t.Fatal("placed editor still open")
		}
		r.EnsureConfigSettings()
		if err := r.Settings().Set(RenameSelectSetting, on); err != nil {
			t.Fatal(err)
		}
		var writes []string
		_, err := r.OpenRecordEditor("rename", RecordSpec{
			Title:  "Rename",
			Fields: []RecordField{{Name: "name", Parameter: Parameter{Type: "string"}}},
			Rename: rename, Insert: insert,
			Load: func() (Record, error) { return Record{"name": "old"}, nil },
			Save: func(w RecordWrite) Result { writes = append(writes, w.String()); return Done(nil) },
		})
		if err != nil {
			t.Fatal(err)
		}
		return r, m, r.Input(r.Focus), &writes
	}
	r, m, in, writes := open(t, true, false, true)
	if lo, hi, ok := in.SelectedText(); !ok || lo != 0 || hi != 3 || r.Mode.Name != "insert" {
		t.Fatalf("selected %d %d %v mode %s", lo, hi, ok, r.Mode.Name)
	}
	m.Update(keytest.Msg("n", true, false))
	keytest.Press(m, "esc")
	keytest.Press(m, "esc")
	if want := []string{"write name=n"}; !slices.Equal(*writes, want) {
		t.Fatalf("writes %v, want %v", *writes, want)
	}
	r, _, in, _ = open(t, true, false, false)
	if _, _, ok := in.SelectedText(); ok || in.Cursor != 3 || r.Mode.Name != "insert" {
		t.Fatalf("off: cursor %d mode %s", in.Cursor, r.Mode.Name)
	}
	_, _, in, _ = open(t, false, true, true)
	if _, _, ok := in.SelectedText(); ok {
		t.Fatal("Insert alone selected")
	}
}
