package ochazuke

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func pickerFixture(t *testing.T, text, kind, filter string, cursor int) (*Runtime, *Picker, *Input) {
	t.Helper()
	pickerGitEnvironment(t)
	r := NewRuntime()
	in := NewInput("origin", text, kind == "paths")
	in.Kind, in.PathFilter, in.Cursor = kind, filter, cursor
	if err := r.AddInput(in, []string{"app"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Focus(in.ID); err != nil {
		t.Fatal(err)
	}
	p := NewPicker(r)
	t.Cleanup(func() { p.stop() })
	return r, p, in
}

func pickerDo(t *testing.T, r *Runtime, verb, noun string, args Arguments) any {
	t.Helper()
	result, err := r.Execute(Request{Verb: verb, Noun: noun, Args: args})
	if err != nil {
		t.Fatalf("%s.%s: %v", verb, noun, err)
	}
	return result
}

func pickerKey(t *testing.T, r *Runtime, key string) {
	t.Helper()
	handled, err := r.HandleBinding(key)
	if !handled && err == nil {
		handled, err = HandleInputKey(r, key)
	}
	if err != nil || !handled {
		t.Fatalf("key %q: handled=%v error=%v", key, handled, err)
	}
}

func pickerCommand(t *testing.T, p *Picker, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	message := make(chan tea.Msg, 1)
	go func() { message <- cmd() }()
	select {
	case msg := <-message:
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, child := range batch {
				pickerCommand(t, p, child)
			}
		} else if !p.HandleMessage(msg) {
			t.Fatalf("unhandled picker message %T", msg)
		}
	case <-time.After(10 * time.Second):
		p.stop()
		t.Fatal("picker command exceeded deadline")
	}
}

func pickerDrain(t *testing.T, p *Picker) {
	t.Helper()
	for cmd := p.PendingCommand(); cmd != nil; cmd = p.PendingCommand() {
		pickerCommand(t, p, cmd)
	}
	if p.Loading {
		t.Fatal("picker stayed loading without queued work")
	}
	if p.Error != "" {
		t.Fatalf("picker error: %s", p.Error)
	}
}

func pickerHighlight(t *testing.T, r *Runtime, p *Picker, path string) {
	t.Helper()
	for i, entry := range p.Entries {
		if entry.Path == path {
			pickerDo(t, r, "move", "picker", Arguments{"delta": i - p.Selected})
			return
		}
	}
	t.Fatalf("missing result %q in %v", path, p.Entries)
}

func TestPickerInsertsAtCapturedRuneCursorAndRestoresOrigin(t *testing.T) {
	root := t.TempDir()
	chosen := pickerFile(t, root, "picked file.txt", "")
	canonical, err := filepath.EvalSymlinks(chosen)
	if err != nil {
		t.Fatal(err)
	}
	r, p, origin := pickerFixture(t, "préSUFFIX", "text", "", 3)
	pickerDo(t, r, "open", "picker", Arguments{"path": chosen, "filter": "file"})
	pickerDrain(t, p)
	if _, err := r.Execute(Request{Verb: "move", Noun: "input", Target: origin.ID, Args: Arguments{"motion": "line-end"}}); err != nil {
		t.Fatal(err)
	}
	pickerKey(t, r, "enter")
	if got, want := string(origin.Text), "pré"+canonical+"SUFFIX"; got != want {
		t.Fatalf("insert got %q want %q", got, want)
	}
	if p.IsOpen() || r.Context().InputID != origin.ID || r.Inputs[origin.ID] != origin {
		t.Fatalf("origin context not restored: %#v", r.Context())
	}
	pickerDo(t, r, "undo", "input", nil)
	if got := string(origin.Text); got != "préSUFFIX" {
		t.Fatalf("origin undo got %q", got)
	}
}

func TestPickerPathValueAndPathsCurrentLineReplacement(t *testing.T) {
	root := t.TempDir()
	chosen := pickerFile(t, root, "nested/target file.go", "")
	canonical, err := filepath.EvalSymlinks(chosen)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"path", "paths"} {
		t.Run(kind, func(t *testing.T) {
			line := filepath.Join(root, "nested", "tar")
			text, cursor := line, 2
			if kind == "paths" {
				text, cursor = "/keep/first\n"+line+"\n/keep/last", len([]rune("/keep/first\n"))+2
			}
			r, p, origin := pickerFixture(t, text, kind, "file", cursor)
			pickerDo(t, r, "open", "picker", nil)
			if got := string(p.input.Text); got != line {
				t.Fatalf("initial relevant path got %q want %q", got, line)
			}
			pickerDrain(t, p)
			if p.Filter != "file" || !reflect.DeepEqual(pickerEntryPaths(p.Entries), []string{"target file.go"}) {
				t.Fatalf("derived filter/results %q %v", p.Filter, p.Entries)
			}
			pickerDo(t, r, "choose", "picker", nil)
			want := canonical
			if kind == "paths" {
				want = "/keep/first\n" + canonical + "\n/keep/last"
			}
			if got := string(origin.Text); got != want {
				t.Fatalf("%s replacement got %q want %q", kind, got, want)
			}
		})
	}
}

func TestPickerRejectsStaleOriginAndNewlinePathSetEntry(t *testing.T) {
	root := t.TempDir()
	chosen := pickerFile(t, root, "chosen", "")
	r, p, origin := pickerFixture(t, "before", "text", "", 2)
	pickerDo(t, r, "open", "picker", Arguments{"path": chosen, "filter": "file"})
	pickerDrain(t, p)
	if _, err := r.Execute(Request{Verb: "replace", Noun: "input", Target: origin.ID, Args: Arguments{"start": 0, "end": len(origin.Text), "text": "external change"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(Request{Verb: "choose", Noun: "picker"}); err == nil || !strings.Contains(err.Error(), "originating input changed") {
		t.Fatalf("stale origin: %v", err)
	}
	if got := string(origin.Text); got != "external change" || !p.IsOpen() {
		t.Fatalf("stale selection changed input/closed picker: %q %v", got, p.IsOpen())
	}
	pickerDo(t, r, "close", "picker", nil)

	pickerFile(t, root, "bad\nname", "")
	origin.Kind, origin.Multiline = "paths", true
	pickerDo(t, r, "open", "picker", Arguments{"path": root, "filter": "file"})
	pickerDrain(t, p)
	pickerHighlight(t, r, p, "bad\nname")
	if _, err := r.Execute(Request{Verb: "choose", Noun: "picker"}); err == nil || !strings.Contains(err.Error(), "newline") {
		t.Fatalf("newline filename: %v", err)
	}
	if string(origin.Text) != "external change" || !p.IsOpen() {
		t.Fatal("newline filename corrupted path set")
	}
}

func TestPickerInsertSelectionReadlineAndEscapeCancellation(t *testing.T) {
	root := t.TempDir()
	pickerFile(t, root, "alpha", "")
	pickerFile(t, root, "beta", "")
	r, p, origin := pickerFixture(t, "untouched", "text", "", 4)
	pickerDo(t, r, "open", "picker", Arguments{"path": root + string(filepath.Separator)})
	pickerDrain(t, p)
	if p.input.Mode != "insert" || r.Context().InputID != p.input.ID {
		t.Fatal("picker did not focus a native insert editor")
	}
	before, cursor := string(p.input.Text), p.input.Cursor
	pickerKey(t, r, "ctrl+n")
	if p.Selected != 1 || string(p.input.Text) != before || p.input.Cursor != cursor {
		t.Fatal("Ctrl-N edited path/cursor instead of moving selection")
	}
	pickerKey(t, r, "ctrl+p")
	if p.Selected != 0 || p.input.Cursor != cursor {
		t.Fatal("Ctrl-P failed to restore selection without editing")
	}
	pickerKey(t, r, "ctrl+a")
	if p.input.Cursor != 0 {
		t.Fatal("Ctrl-A did not use native editor")
	}
	pickerKey(t, r, "ctrl+e")
	pickerKey(t, r, "ctrl+u")
	if string(p.input.Text) != "" {
		t.Fatalf("Ctrl-U got %q", string(p.input.Text))
	}
	pickerKey(t, r, "ctrl+y")
	if string(p.input.Text) != before {
		t.Fatalf("Ctrl-Y got %q want %q", string(p.input.Text), before)
	}
	pickerDrain(t, p)
	pickerDo(t, r, "insert", "input", Arguments{"text": "child/"})
	pickerKey(t, r, "ctrl+w")
	if string(p.input.Text) != before {
		t.Fatalf("Ctrl-W did not stop at slash boundary: %q", string(p.input.Text))
	}
	pickerKey(t, r, "ctrl+y")
	if string(p.input.Text) != before+"child/" {
		t.Fatalf("Ctrl-Y did not restore killed component: %q", string(p.input.Text))
	}
	pickerKey(t, r, "ctrl+w")
	pickerKey(t, r, "j")
	if got := string(p.input.Text); got != before+"j" {
		t.Fatalf("insert j was stolen by navigation: %q", got)
	}
	pickerKey(t, r, "backspace")
	if string(p.input.Text) != before {
		t.Fatal("insert Backspace navigated instead of editing")
	}
	pickerKey(t, r, "esc")
	if !p.IsOpen() || p.input.Mode != "normal" {
		t.Fatal("first Escape must only leave insert mode")
	}
	pickerKey(t, r, "esc")
	if p.IsOpen() || string(origin.Text) != "untouched" || origin.Cursor != 4 || r.Context().InputID != origin.ID {
		t.Fatal("cancel did not preserve originating editor/context")
	}
}

func TestPickerNormalDirectionalKeysBrowseAndConfirmHighlightedDirectory(t *testing.T) {
	root := t.TempDir()
	pickerFile(t, root, "child/grandchild/file", "")
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	r, p, origin := pickerFixture(t, "", "path", "dir", 0)
	pickerDo(t, r, "open", "picker", Arguments{"path": root})
	pickerDrain(t, p)
	pickerKey(t, r, "esc")
	pickerKey(t, r, "j")
	if p.Entries[p.Selected].Path != "child" {
		t.Fatalf("j got %v", p.Entries[p.Selected])
	}
	pickerKey(t, r, "l")
	pickerDrain(t, p)
	if p.Directory != filepath.Join(canonical, "child") {
		t.Fatalf("l directory %q", p.Directory)
	}
	pickerKey(t, r, "h")
	pickerDrain(t, p)
	if p.Directory != canonical {
		t.Fatalf("h directory %q", p.Directory)
	}
	pickerKey(t, r, "down")
	pickerKey(t, r, "right")
	pickerDrain(t, p)
	if p.Directory != filepath.Join(canonical, "child") {
		t.Fatalf("right directory %q", p.Directory)
	}
	pickerKey(t, r, "left")
	pickerDrain(t, p)
	pickerKey(t, r, "j")
	pickerKey(t, r, "k")
	pickerKey(t, r, "down")
	pickerKey(t, r, "up")
	if p.Selected != 0 {
		t.Fatalf("k/up selection %d", p.Selected)
	}
	pickerHighlight(t, r, p, "child")
	pickerKey(t, r, "ctrl+s")
	if string(origin.Text) != filepath.Join(canonical, "child") || p.IsOpen() {
		t.Fatalf("confirmed highlight got %q, open=%v", string(origin.Text), p.IsOpen())
	}

	pickerDo(t, r, "open", "picker", Arguments{"path": root})
	pickerDrain(t, p)
	if !strings.Contains(p.View(100, 20), "current directory") {
		t.Fatal("current root is not a clearly labeled selection")
	}
	pickerDo(t, r, "choose", "picker", Arguments{"current": true})
	if string(origin.Text) != canonical {
		t.Fatalf("current root got %q", string(origin.Text))
	}
}

func TestPickerCLIPathEditsRefreshResultsWithoutRendering(t *testing.T) {
	root := t.TempDir()
	pickerFile(t, root, "alpha", "")
	pickerFile(t, root, "beta", "")
	r, p, _ := pickerFixture(t, "", "text", "", 0)
	pickerDo(t, r, "open", "picker", Arguments{"path": root, "filter": "file"})
	pickerDrain(t, p)
	t.Setenv("PATH", "") // Query edits reuse the index; no Git process per keystroke.
	reply := make(chan Response, 1)
	r.HandleCall(CallMsg{Request: Request{Verb: "replace", Noun: "input", Target: p.input.ID, Args: Arguments{"start": 0, "end": len(p.input.Text), "text": filepath.Join(root, "bt")}}, Reply: reply})
	if response := <-reply; response.Error != "" {
		t.Fatal(response.Error)
	}
	pickerDrain(t, p)
	if got, want := pickerEntryPaths(p.Entries), []string{"beta"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("CLI fuzzy edit got %q want %q", got, want)
	}
	pickerDo(t, r, "undo", "input", nil)
	pickerDrain(t, p)
	if got, want := pickerEntryPaths(p.Entries), []string{"alpha", "beta"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("CLI undo got %q want %q", got, want)
	}
}

func TestPickerIgnoresLateIndexAndSearchAfterEditsAndCancellation(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	pickerFile(t, first, "old-result", "")
	pickerFile(t, second, "new-result", "")
	r, p, origin := pickerFixture(t, "preserve", "text", "", 0)
	pickerDo(t, r, "open", "picker", Arguments{"path": first, "filter": "file"})
	oldIndex := p.PendingCommand()
	pickerDo(t, r, "replace", "input", Arguments{"start": 0, "end": len(p.input.Text), "text": second})
	pickerDrain(t, p)
	pickerCommand(t, p, oldIndex)
	if got := pickerEntryPaths(p.Entries); !reflect.DeepEqual(got, []string{"new-result"}) {
		t.Fatalf("late old root replaced results: %q", got)
	}
	pickerDo(t, r, "insert", "input", Arguments{"text": "/nw"})
	oldSearch := p.PendingCommand()
	pickerDo(t, r, "replace", "input", Arguments{"start": 0, "end": len(p.input.Text), "text": filepath.Join(second, "new-r")})
	pickerDrain(t, p)
	pickerCommand(t, p, oldSearch)
	if got := pickerEntryPaths(p.Entries); !reflect.DeepEqual(got, []string{"new-result"}) {
		t.Fatalf("late search replaced results: %q", got)
	}
	pickerDo(t, r, "insert", "input", Arguments{"text": "x"})
	canceled := p.PendingCommand()
	pickerDo(t, r, "close", "picker", nil)
	pickerCommand(t, p, canceled)
	if p.IsOpen() || string(origin.Text) != "preserve" || p.PendingCommand() != nil {
		t.Fatal("late canceled work revived modal or changed origin")
	}
}

func TestPickerFiltersValidateBeforeOpeningAndExplicitOverrideWins(t *testing.T) {
	root := t.TempDir()
	pickerFile(t, root, "file", "")
	r, p, origin := pickerFixture(t, root, "path", "dir", 0)
	if _, err := r.Execute(Request{Verb: "open", Noun: "picker", Args: Arguments{"filter": "directory"}}); err == nil || p.IsOpen() {
		t.Fatalf("invalid filter: %v, open=%v", err, p.IsOpen())
	}
	pickerDo(t, r, "open", "picker", Arguments{"filter": "file"})
	pickerDrain(t, p)
	if got := pickerEntryPaths(p.Entries); !reflect.DeepEqual(got, []string{"file"}) {
		t.Fatalf("explicit file filter got %v", got)
	}
	if _, err := r.Execute(Request{Verb: "choose", Noun: "picker", Args: Arguments{"current": true}}); err == nil || !p.IsOpen() {
		t.Fatalf("file accepted root: %v", err)
	}
	pickerDo(t, r, "close", "picker", nil)
	if string(origin.Text) != root {
		t.Fatal("filter error changed origin")
	}
	pickerDo(t, r, "open", "picker", nil)
	pickerDrain(t, p)
	if p.Filter != "dir" || !reflect.DeepEqual(pickerEntryPaths(p.Entries), []string{"."}) {
		t.Fatalf("derived directory filter got %q %v", p.Filter, p.Entries)
	}
}

func TestPickerIndexFailureIsVisibleAndNoPartialResultsSelectable(t *testing.T) {
	root := t.TempDir()
	pickerFile(t, root, "visible", "")
	r, p, origin := pickerFixture(t, "safe", "text", "", 0)
	pickerDo(t, r, "open", "picker", Arguments{"path": root})
	p.HandleMessage(pickerIndexMsg{picker: p, id: p.indexID, entries: []PickerEntry{{Path: "visible"}}, err: errors.New("permission denied: private")})
	if !strings.Contains(p.View(100, 20), "Index incomplete") || p.Loading {
		t.Fatalf("index error not visible: %s", p.View(100, 20))
	}
	if _, err := r.Execute(Request{Verb: "choose", Noun: "picker"}); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("incomplete index selection: %v", err)
	}
	if string(origin.Text) != "safe" || !p.IsOpen() {
		t.Fatal("index error changed origin or closed modal")
	}
	pickerDo(t, r, "insert", "input", Arguments{"text": string(filepath.Separator)})
	pickerDrain(t, p)
	if !reflect.DeepEqual(pickerEntryPaths(p.Entries), []string{".", "visible"}) {
		t.Fatalf("retry did not recover: %v", p.Entries)
	}
}

func TestPickerOrdinaryTextDefaultsToCwdNotInputContents(t *testing.T) {
	r, p, origin := pickerFixture(t, "this is ordinary prose, not a path", "", "file", 5)
	cwd, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatal(err)
	}
	pickerDo(t, r, "open", "picker", nil)
	if p.Directory != canonical || p.Filter != "any" || string(p.input.Text) != cwd+string(filepath.Separator) {
		t.Fatalf("ordinary input root/filter/text: %q %q %q", p.Directory, p.Filter, string(p.input.Text))
	}
	pickerDo(t, r, "close", "picker", nil)
	if string(origin.Text) != "this is ordinary prose, not a path" || origin.Cursor != 5 {
		t.Fatal("cancel changed ordinary input")
	}
}
