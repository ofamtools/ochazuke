package ochazuke

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// historyModel is a Model whose config and state live in temp dirs; its
// history writer is flushed when the test ends.
func historyModel(t *testing.T, configDir, stateDir string) *Model {
	t.Helper()
	r := NewRuntime()
	c := r.Config()
	c.Dir, c.StateDir = configDir, stateDir
	m := NewModel(r)
	m.Init()
	t.Cleanup(func() { _ = c.Flush() })
	return m
}

func historyLines(t *testing.T, path string) [][]byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.Split(bytes.TrimSpace(data), []byte("\n"))
}

// runPaletteLine records a request as the palette does.
func runPaletteLine(t *testing.T, r *Runtime, q Request) {
	t.Helper()
	restore := r.Sourced("palette")
	defer restore()
	if _, err := r.Execute(q); err != nil {
		t.Fatal(err)
	}
}

// The ring survives a restart: entries are appended to the state dir's
// history.jsonl, a new session reads them back (restored, before its own),
// palette recall offers them and dot-repeat does not run them.
func TestHistoryPersistsAcrossSessions(t *testing.T) {
	config, state := t.TempDir(), t.TempDir()
	m := historyModel(t, config, state)
	r := m.Runtime
	runPaletteLine(t, r, Request{Verb: "list", Noun: "ochazuke.modes"})
	runPaletteLine(t, r, Request{Verb: "list", Noun: "ochazuke.themes"})
	if err := r.Config().Flush(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(state, HistoryFile)
	if got := r.Config().HistoryPath(); got != path {
		t.Fatalf("path %q", got)
	}
	if lines := historyLines(t, path); len(lines) != 2 || !bytes.Contains(lines[0], []byte(`"verb":"list"`)) {
		t.Fatalf("file: %q", lines)
	}

	next := historyModel(t, config, state)
	nr := next.Runtime
	entries := nr.History(0)
	if len(entries) < 2 || !entries[0].Restored || entries[0].Request.Key() != "ochazuke.modes.list" || entries[1].Request.Key() != "ochazuke.themes.list" {
		t.Fatalf("restored: %+v", entries)
	}
	for i := 1; i < len(entries); i++ {
		if entries[i].Seq <= entries[i-1].Seq {
			t.Fatalf("seq does not rise: %+v", entries)
		}
	}
	p := next.Palette
	if _, err := nr.Execute(Request{Verb: "open", Noun: "ochazuke.palette"}); err != nil {
		t.Fatal(err)
	}
	if err := p.recallLine(1); err != nil || p.Text() != "list themes" {
		t.Fatalf("recall: %q %v", p.Text(), err)
	}
	if err := p.recallLine(1); err != nil || p.Text() != "list modes" {
		t.Fatalf("recall 2: %q %v", p.Text(), err)
	}
	if _, err := nr.repeatRequest(0); err == nil {
		t.Fatal("dot-repeat ran a restored entry")
	}
}

// A line that does not parse is skipped with one notice, and the file is
// rewritten without it.
func TestHistorySkipsCorruptLines(t *testing.T) {
	state := t.TempDir()
	path := filepath.Join(state, HistoryFile)
	good := `{"seq":1,"request":{"verb":"list","noun":"modes"},"context":{"focus":"app","mode":"normal","source":"palette"},"result":{"ok":true}}`
	if err := os.WriteFile(path, []byte(good+"\n{not json\n"+strings.Replace(good, `"seq":1`, `"seq":2`, 1)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := historyModel(t, t.TempDir(), state)
	restored := 0
	for _, e := range m.Runtime.History(0) {
		if e.Restored {
			restored++
		}
	}
	if restored != 2 {
		t.Fatalf("restored %d: %+v", restored, m.Runtime.History(0))
	}
	if len(m.Notices) != 1 || !strings.Contains(m.Notices[0], "1 line(s) do not parse, first at line 2 (skipped)") {
		t.Fatalf("notices: %q", m.Notices)
	}
	if lines := historyLines(t, path); len(lines) != 2 {
		t.Fatalf("not compacted: %q", lines)
	}
}

// history-limit bounds the file and the ring: the writer compacts at twice
// the limit (temp file + rename) and a new session keeps the last limit.
func TestHistoryIsBounded(t *testing.T) {
	config, state := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(config, SettingsFile), []byte(`{"history-limit": 3}`), 0o600); err != nil {
		t.Fatal(err)
	}
	m := historyModel(t, config, state)
	r := m.Runtime
	for range 10 {
		if _, err := r.Execute(Request{Verb: "clear", Noun: "ochazuke.notice"}); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(r.History(0)); n != 3 {
		t.Fatalf("ring holds %d", n)
	}
	if err := r.Config().Flush(); err != nil {
		t.Fatal(err)
	}
	if lines := historyLines(t, filepath.Join(state, HistoryFile)); len(lines) < 3 || len(lines) >= 6 {
		t.Fatalf("file holds %d lines", len(lines))
	}
	next := historyModel(t, config, state)
	entries := next.Runtime.History(0)
	if len(entries) != 3 || !entries[0].Restored {
		t.Fatalf("next session: %d entries %+v", len(entries), entries)
	}
	// Leftover temp files would mean a compaction did not rename.
	if matches, _ := filepath.Glob(filepath.Join(state, ".history-*")); len(matches) != 0 {
		t.Fatalf("temp files left: %v", matches)
	}
}

// No file without a state directory (a disabled config, a test binary) or
// when the application opts out; history-limit is declared only when the
// history is persisted.
func TestHistoryNotPersistedWhenDisabledOrOptedOut(t *testing.T) {
	state := t.TempDir()
	r := NewRuntime()
	r.Config().StateDir = state
	r.Config().Disable()
	m := NewModel(r)
	m.Init()
	if _, err := r.Execute(Request{Verb: "list", Noun: "ochazuke.modes"}); err != nil {
		t.Fatal(err)
	}
	if r.Settings().find(HistorySetting) != nil || r.Config().HistoryPath() != "" {
		t.Fatal("a disabled config declares history-limit or has a history path")
	}
	if _, err := r.Execute(Request{Verb: "path", Noun: "ochazuke.history"}); err == nil {
		t.Fatal("history.path without a file")
	}

	r = NewRuntime()
	r.Config().StateDir, r.Config().NoHistoryFile = state, true
	m = NewModel(r)
	m.Init()
	if _, err := r.Execute(Request{Verb: "list", Noun: "ochazuke.modes"}); err != nil {
		t.Fatal(err)
	}
	_ = r.Config().Flush()
	if _, err := os.Stat(filepath.Join(state, HistoryFile)); !os.IsNotExist(err) {
		t.Fatalf("history file written: %v", err)
	}
}

// history-limit 0 writes no file and reads none.
func TestHistoryLimitZeroKeepsNone(t *testing.T) {
	config, state := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(config, SettingsFile), []byte(`{"history-limit": 0}`), 0o600); err != nil {
		t.Fatal(err)
	}
	m := historyModel(t, config, state)
	if _, err := m.Runtime.Execute(Request{Verb: "list", Noun: "ochazuke.modes"}); err != nil {
		t.Fatal(err)
	}
	_ = m.Runtime.Config().Flush()
	if _, err := os.Stat(filepath.Join(state, HistoryFile)); !os.IsNotExist(err) {
		t.Fatalf("history file written: %v", err)
	}
}

// fileText is the history file after the writer drained, "" when absent.
func fileText(t *testing.T, r *Runtime) string {
	t.Helper()
	if err := r.Config().Flush(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(r.Config().HistoryPath())
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(data)
}

// Typing into an input and into a prompt, pasting included, writes
// nothing of it to the file: editing operations are never persisted.
func TestHistoryFileKeepsNoTyping(t *testing.T) {
	state := t.TempDir()
	r := NewRuntime()
	r.Config().Dir, r.Config().StateDir = t.TempDir(), state
	notes := NewInput("notes", "", false)
	if err := r.AddInput(notes); err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	m.Init()
	t.Cleanup(func() { _ = r.Config().Flush() })
	if err := r.SetFocus("notes"); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"i", "h", "u", "n", "t", "e", "r", "2", "[pasted-secret]", "backspace", "esc", "x", "u"} {
		m.handleKey(editorTestKey(key))
	}
	field := NewInput("answer", "", false)
	if _, err := r.OpenModal("ask", Payload{Prompt: "Password?"}, WithInput(field, Kind("ochazuke.prompt"))); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"s", "3", "c", "r", "[et]", "left", "ctrl+w"} {
		m.handleKey(editorTestKey(key))
	}
	if len(notes.Text) == 0 || len(r.History(0)) == 0 {
		t.Fatalf("nothing was typed: %q", string(notes.Text))
	}
	text := fileText(t, r)
	for _, typed := range []string{"hunter", "pasted-secret", "s3cr", `"noun":"input"`, `"noun":"key"`, `"noun":"register"`} {
		if strings.Contains(text, typed) {
			t.Fatalf("the file holds %q:\n%s", typed, text)
		}
	}
}

// A line run from the palette is written (its text-like arguments only
// when the operation opts in) and recalled after a restart.
func TestHistoryFileKeepsPaletteLines(t *testing.T) {
	config, state := t.TempDir(), t.TempDir()
	m := historyModel(t, config, state)
	r := m.Runtime
	if err := r.DeclareSetting(Setting{Name: "wrap", Type: SettingBoolean, Default: false}); err != nil {
		t.Fatal(err)
	}
	runPaletteLine(t, r, Request{Verb: "set", Noun: "ochazuke.settings", Args: Arguments{"name": "wrap", "value": "true"}})
	text := fileText(t, r)
	if !strings.Contains(text, `"value":"true"`) || strings.Contains(text, `"result":{"ok":true,"value"`) {
		t.Fatalf("file: %s", text)
	}
	next := historyModel(t, config, state)
	if _, err := next.Runtime.Execute(Request{Verb: "open", Noun: "ochazuke.palette"}); err != nil {
		t.Fatal(err)
	}
	if err := next.Palette.recallLine(1); err != nil || next.Palette.Text() != "set settings wrap true" {
		t.Fatalf("recall %q %v", next.Palette.Text(), err)
	}
}

// Arguments named like text are dropped from the file unless the
// operation sets PersistArgs; result values are never written.
func TestHistoryFileDropsTextArgsUnlessOptedIn(t *testing.T) {
	m := historyModel(t, t.TempDir(), t.TempDir())
	r := m.Runtime
	str := Parameter{Type: "string"}
	r.Register(Operation{Verb: "save", Noun: "note", Parameters: map[string]Parameter{"id": str, "text": str, "api_token": str}, Run: func(*Runtime, Context, Arguments) (any, error) { return "echoed", nil }})
	r.Define("tag").Verb("add", func(Ctx) Result { return Done("echoed") }).PersistArgs()
	op := r.Operations["tag.add"]
	op.Parameters = map[string]Parameter{"text": str}
	r.Operations["tag.add"] = op
	if _, err := r.Execute(Request{Verb: "save", Noun: "note", Args: Arguments{"id": "n1", "text": "my diary", "api_token": "abc123"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(Request{Verb: "add", Noun: "tag", Args: Arguments{"text": "groceries"}}); err != nil {
		t.Fatal(err)
	}
	text := fileText(t, r)
	if !strings.Contains(text, `"id":"n1"`) || strings.Contains(text, "my diary") || strings.Contains(text, "abc123") || !strings.Contains(text, `"text":"groceries"`) || strings.Contains(text, "echoed") {
		t.Fatalf("file: %s", text)
	}
}
