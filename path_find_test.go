package ochazuke

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"ofam.dev/ochazuke/keytest"
)

// The path picker's find source (path_find.go): the tests that were the
// path finder's (picker_test.go, picker_table_test.go,
// picker_preview_test.go before v0.96.0), moved onto the source.

// findLand runs the commands queued and feeds the job completions back to
// the model, again for what those queue, waiting up to two seconds for
// each (an index runs Git). A command that has not answered by then is
// kept and fed in once it does, by a later findLand (an index a test
// holds); a timer that never fires is left alone.
func findLand(m *Model, cmd tea.Cmd) {
	for range 50 {
		msgs := append(findMsgs(m, cmd), findReady(m)...)
		if len(msgs) == 0 {
			return
		}
		var next []tea.Cmd
		for _, msg := range msgs {
			_, out := m.Update(msg)
			next = append(next, out)
		}
		cmd = tea.Batch(next...)
	}
}

var (
	findLateMu sync.Mutex
	findLate   = map[*Model][]chan tea.Msg{}
)

// findMsgs runs cmd (a batch: each child) and keeps the completions.
func findMsgs(m *Model, cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	select {
	case msg := <-ch:
		return findOf(m, msg)
	case <-time.After(2 * time.Second):
		findLateMu.Lock()
		findLate[m] = append(findLate[m], ch)
		findLateMu.Unlock()
		return nil
	}
}

func findOf(m *Model, msg tea.Msg) []tea.Msg {
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, child := range batch {
			out = append(out, findMsgs(m, child)...)
		}
		return out
	}
	if _, ok := msg.(Completion); ok {
		return []tea.Msg{msg}
	}
	return nil
}

// findReady takes the kept commands that have answered since.
func findReady(m *Model) []tea.Msg {
	findLateMu.Lock()
	var ready []tea.Msg
	waiting := findLate[m][:0]
	for _, ch := range findLate[m] {
		select {
		case msg := <-ch:
			ready = append(ready, msg)
		default:
			waiting = append(waiting, ch)
		}
	}
	findLate[m] = waiting
	findLateMu.Unlock()
	var out []tea.Msg
	for _, msg := range ready {
		out = append(out, findOf(m, msg)...)
	}
	return out
}

// findFixture is a runtime with a model and one input in focus, the field
// picker.open with no name fills.
func findFixture(t *testing.T, text, kind, filter string, cursor int) (*Runtime, *Model, *Input) {
	t.Helper()
	pickerGitEnvironment(t)
	r := NewRuntime()
	m := NewModel(r)
	m.Width, m.Height = 100, 30
	m.Appearance.Theme = "mono"
	r.EnsureConfigSettings()
	in := NewInput("origin", text, kind == "paths")
	in.Kind, in.PathFilter, in.Cursor = kind, filter, cursor
	if err := r.AddInput(in); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFocus(in.ID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if c := r.Chooser(PathChooserKind); c != nil {
			c.closeSources()
		}
	})
	return r, m, in
}

// findOpen runs picker.open with no name and lands its loads.
func findOpen(t *testing.T, r *Runtime, m *Model, args Arguments) *Chooser {
	t.Helper()
	if _, err := r.Execute(Request{Verb: "open", Noun: PickerNoun, Args: args}); err != nil {
		t.Fatal(err)
	}
	findLand(m, r.PendingCommands())
	c := r.Chooser(PathChooserKind)
	if c == nil || c.Source() != PathSourceFind {
		t.Fatalf("picker.open with no name opened %v", c)
	}
	return c
}

func findPress(m *Model, keys ...string) {
	for _, k := range keys {
		findLand(m, keytest.Press(m, k))
	}
}

func findType(m *Model, text string) { findLand(m, keytest.Type(m, text)) }

func findDo(t *testing.T, r *Runtime, m *Model, verb string, args Arguments) error {
	t.Helper()
	_, err := r.Execute(Request{Verb: verb, Noun: PickerNoun, Args: args})
	findLand(m, r.PendingCommands())
	return err
}

// findHighlight moves the highlight onto the row labelled label.
func findHighlight(t *testing.T, r *Runtime, m *Model, c *Chooser, label string) {
	t.Helper()
	rows := c.Matches()
	at := slices.IndexFunc(rows, func(row ChooserRow) bool { return row.Label == label })
	if at < 0 {
		t.Fatalf("no row %q in %q", label, labels(rows))
	}
	row, _ := c.Highlighted()
	from := slices.IndexFunc(rows, func(one ChooserRow) bool { return one.ID == row.ID })
	if err := findDo(t, r, m, "move", Arguments{"delta": at - from}); err != nil {
		t.Fatal(err)
	}
	if row, _ := c.Highlighted(); row.Label != label {
		t.Fatalf("highlight on %q, want %q", row.Label, label)
	}
}

func canonicalPath(t *testing.T, path string) string {
	t.Helper()
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}

// gatedIndex holds the find source's recursive index until release is
// closed (or fails it with fail), so a test sees the shallow rows first.
type gatedIndex struct {
	release   chan struct{}
	fail      error
	mu        sync.Mutex
	cancelled bool
}

func (g *gatedIndex) index(ctx context.Context, root string, recursive bool) ([]PickerEntry, error) {
	if recursive {
		select {
		case <-g.release:
		case <-ctx.Done():
			g.mu.Lock()
			g.cancelled = true
			g.mu.Unlock()
			return nil, ctx.Err()
		}
		if g.fail != nil {
			return nil, g.fail
		}
	}
	return indexPickerEntries(ctx, root, recursive)
}

// picker.open with no name opens the path picker on its find source at
// the working folder, taking a file or a folder; the title names it.
func TestFindPickerOpenWithNoName(t *testing.T) {
	root := t.TempDir()
	pickerFile(t, root, "nested/deep.txt", "")
	pickerFile(t, root, "top.txt", "")
	t.Chdir(root)
	r, m, origin := findFixture(t, "this is ordinary prose, not a path", "", "file", 5)
	c := findOpen(t, r, m, nil)
	canonical := canonicalPath(t, root)
	if !strings.Contains(c.Title(), "[find "+displayPath(canonical)+"]") || !strings.HasPrefix(c.Title(), "Choose path") {
		t.Fatalf("title %q", c.Title())
	}
	if got, want := labels(c.Matches()), []string{". (current directory)", "nested/", "nested/deep.txt", "top.txt"}; !slices.Equal(got, want) {
		t.Fatalf("rows %q, want %q", got, want)
	}
	findPress(m, "esc")
	if r.Chooser(PathChooserKind) != nil || string(origin.Text) != "this is ordinary prose, not a path" || origin.Cursor != 5 || r.Context().InputID != origin.ID {
		t.Fatal("cancel changed the ordinary input or its focus")
	}
}

// tab fills the highlighted path into the query, nothing picked: a file's
// path, a folder's with a trailing / (find then lists it).
func TestFindTabCompletesHighlightedPathWithoutChoosing(t *testing.T) {
	for _, directory := range []bool{false, true} {
		t.Run(fmt.Sprintf("directory=%v", directory), func(t *testing.T) {
			root := t.TempDir()
			pickerFile(t, root, "nested/target.txt", "")
			r, m, origin := findFixture(t, "unchanged", "text", "any", 0)
			c := findOpen(t, r, m, Arguments{"path": root})
			label, want := "nested/target.txt", "nested/target.txt"
			if directory {
				label, want = "nested/", "nested/"
			}
			findHighlight(t, r, m, c, label)
			findPress(m, "tab")
			if c.Query() != want || r.Chooser(PathChooserKind) != c || string(origin.Text) != "unchanged" {
				t.Fatalf("query %q (want %q), open %v, origin %q", c.Query(), want, r.Chooser(PathChooserKind) == c, origin.Text)
			}
			if directory && !slices.Equal(labels(c.Matches()), []string{". (current directory)", "nested/target.txt"}) {
				t.Fatalf("a completed folder lists its paths: %q", labels(c.Matches()))
			}
		})
	}
}

// A pick replaces the text at the cursor the input had when the picker
// opened, and undo on the input takes it back.
func TestFindInsertsAtCapturedCursorAndUndo(t *testing.T) {
	root := t.TempDir()
	chosen := canonicalPath(t, pickerFile(t, root, "picked file.txt", ""))
	r, m, origin := findFixture(t, "préSUFFIX", "text", "", 3)
	c := findOpen(t, r, m, Arguments{"path": chosen, "filter": "file"})
	if c.Query() != "picked file.txt" {
		t.Fatalf("query %q", c.Query())
	}
	findPress(m, "enter")
	if got, want := string(origin.Text), "pré"+chosen+"SUFFIX"; got != want {
		t.Fatalf("insert got %q want %q", got, want)
	}
	if r.Chooser(PathChooserKind) != nil || r.Context().InputID != origin.ID {
		t.Fatalf("origin context not restored: %#v", r.Context())
	}
	if _, err := r.Execute(Request{Verb: "undo", Noun: "ochazuke.input"}); err != nil {
		t.Fatal(err)
	}
	if got := string(origin.Text); got != "préSUFFIX" {
		t.Fatalf("origin undo got %q", got)
	}
}

// A path field opens at its value with its filter and is replaced whole;
// a paths field at the line the cursor is on, and only that line changes.
func TestFindPathValueAndPathsCurrentLineReplacement(t *testing.T) {
	root := t.TempDir()
	chosen := canonicalPath(t, pickerFile(t, root, "nested/target file.go", ""))
	pickerFile(t, root, "nested/sub/other.go", "")
	for _, kind := range []string{"path", "paths"} {
		t.Run(kind, func(t *testing.T) {
			line := filepath.Join(root, "nested", "tar")
			text, cursor := line, 2
			if kind == "paths" {
				text, cursor = "/keep/first\n"+line+"\n/keep/last", len([]rune("/keep/first\n"))+2
			}
			r, m, origin := findFixture(t, text, kind, "file", cursor)
			c := findOpen(t, r, m, nil)
			if c.Query() != "tar" || !slices.Equal(labels(c.Matches()), []string{"target file.go"}) {
				t.Fatalf("query %q rows %q", c.Query(), labels(c.Matches()))
			}
			if err := findDo(t, r, m, "choose", nil); err != nil {
				t.Fatal(err)
			}
			want := chosen
			if kind == "paths" {
				want = "/keep/first\n" + chosen + "\n/keep/last"
			}
			if got := string(origin.Text); got != want {
				t.Fatalf("%s replacement got %q want %q", kind, got, want)
			}
		})
	}
}

// A field edited while the picker was open is not overwritten, and a file
// name with a newline is never put in a paths field.
func TestFindRefusesStaleOriginAndNewlinePathSetEntry(t *testing.T) {
	root := t.TempDir()
	chosen := pickerFile(t, root, "chosen", "")
	r, m, origin := findFixture(t, "before", "text", "", 2)
	findOpen(t, r, m, Arguments{"path": chosen, "filter": "file"})
	origin.Text = []rune("external change")
	if err := findDo(t, r, m, "choose", nil); err == nil || !strings.Contains(err.Error(), "originating input changed") {
		t.Fatalf("stale origin: %v", err)
	}
	if got := string(origin.Text); got != "external change" {
		t.Fatalf("stale pick changed the input: %q", got)
	}

	pickerFile(t, root, "bad\nname", "")
	origin.Kind, origin.Multiline = "paths", true
	c := findOpen(t, r, m, Arguments{"path": root, "filter": "file"})
	findHighlight(t, r, m, c, `bad\nname`)
	if err := findDo(t, r, m, "choose", nil); err == nil || !strings.Contains(err.Error(), "newline") {
		t.Fatalf("newline filename: %v", err)
	}
	if string(origin.Text) != "external change" {
		t.Fatal("newline filename corrupted the path set")
	}
}

// ctrl+n and ctrl+p move the highlight without touching the query; the
// query is an ordinary editor (ctrl+u, ctrl+y); esc cancels.
func TestFindKeysMoveEditAndCancel(t *testing.T) {
	root := t.TempDir()
	pickerFile(t, root, "alpha", "")
	pickerFile(t, root, "beta", "")
	r, m, origin := findFixture(t, "untouched", "text", "", 4)
	c := findOpen(t, r, m, Arguments{"path": root + string(filepath.Separator), "query": "a"})
	if c.input.Mode != "insert" || r.Context().InputID != c.input.ID {
		t.Fatal("the picker did not focus its search line in insert")
	}
	first, _ := c.Highlighted()
	findPress(m, "ctrl+n")
	second, _ := c.Highlighted()
	if second.ID == first.ID || c.Query() != "a" {
		t.Fatalf("ctrl+n: %q → %q, query %q", first.Label, second.Label, c.Query())
	}
	findPress(m, "ctrl+p")
	if row, _ := c.Highlighted(); row.ID != first.ID {
		t.Fatal("ctrl+p did not move back")
	}
	findPress(m, "ctrl+u")
	if c.Query() != "" {
		t.Fatalf("ctrl+u left %q", c.Query())
	}
	findPress(m, "ctrl+y")
	if c.Query() != "a" {
		t.Fatalf("ctrl+y got %q", c.Query())
	}
	findPress(m, "j")
	if c.Query() != "aj" {
		t.Fatalf("insert j was stolen by navigation: %q", c.Query())
	}
	findPress(m, "esc")
	if r.Chooser(PathChooserKind) != nil || string(origin.Text) != "untouched" || origin.Cursor != 4 || r.Context().InputID != origin.ID {
		t.Fatal("cancel did not keep the originating input and focus")
	}
}

// enter on a folder searches it, alt+up goes back to the parent; ctrl+s
// picks the highlighted folder; pick {current: true} the folder searched.
func TestFindBrowsesAndConfirmsHighlightedDirectory(t *testing.T) {
	root := t.TempDir()
	pickerFile(t, root, "child/grandchild/file", "")
	canonical := canonicalPath(t, root)
	r, m, origin := findFixture(t, "", "path", "dir", 0)
	c := findOpen(t, r, m, Arguments{"path": root})
	if got, want := labels(c.Matches()), []string{". (current directory)", "child/", "child/grandchild/"}; !slices.Equal(got, want) {
		t.Fatalf("dir rows %q", got)
	}
	findPress(m, "ctrl+n")
	findPress(m, "enter")
	if !strings.Contains(c.Title(), "[find "+displayPath(filepath.Join(canonical, "child"))+"]") {
		t.Fatalf("enter on a folder: %q", c.Title())
	}
	findPress(m, "alt+up")
	if !strings.Contains(c.Title(), "[find "+displayPath(canonical)+"]") {
		t.Fatalf("alt+up: %q", c.Title())
	}
	findHighlight(t, r, m, c, "child/")
	findPress(m, "ctrl+s")
	if string(origin.Text) != filepath.Join(canonical, "child") || r.Chooser(PathChooserKind) != nil {
		t.Fatalf("confirmed highlight got %q", origin.Text)
	}
	findOpen(t, r, m, Arguments{"path": root})
	if err := findDo(t, r, m, "choose", Arguments{"current": true}); err != nil {
		t.Fatal(err)
	}
	if string(origin.Text) != canonical {
		t.Fatalf("current folder got %q", origin.Text)
	}
}

// Editing the query searches the index already made: no Git per
// keystroke, and an edit from the CLI refreshes the rows as typing does.
func TestFindQueryEditsReuseTheIndex(t *testing.T) {
	root := t.TempDir()
	pickerFile(t, root, "alpha", "")
	pickerFile(t, root, "beta", "")
	r, m, _ := findFixture(t, "", "text", "", 0)
	c := findOpen(t, r, m, Arguments{"path": root, "filter": "file"})
	t.Setenv("PATH", "")
	if _, err := r.Execute(Request{Verb: "insert", Noun: "ochazuke.input", Target: c.input.ID, Args: Arguments{"text": "bt"}}); err != nil {
		t.Fatal(err)
	}
	findLand(m, r.PendingCommands())
	if got := labels(c.Matches()); !slices.Equal(got, []string{"beta"}) {
		t.Fatalf("CLI fuzzy edit got %q", got)
	}
	findPress(m, "ctrl+u")
	if got := labels(c.Matches()); !slices.Equal(got, []string{"alpha", "beta"}) {
		t.Fatalf("cleared query got %q", got)
	}
}

// filter is checked before anything opens; an explicit filter wins over
// the field's; a folder field's filter lists folders only.
func TestFindFiltersValidateBeforeOpeningAndExplicitOverrideWins(t *testing.T) {
	root := t.TempDir()
	pickerFile(t, root, "file", "")
	r, m, origin := findFixture(t, root, "path", "dir", 0)
	if _, err := r.Execute(Request{Verb: "open", Noun: PickerNoun, Args: Arguments{"filter": "directory"}}); err == nil || r.Chooser(PathChooserKind) != nil {
		t.Fatalf("invalid filter: %v", err)
	}
	c := findOpen(t, r, m, Arguments{"filter": "file"})
	if got := labels(c.Matches()); !slices.Equal(got, []string{"file"}) {
		t.Fatalf("explicit file filter got %q", got)
	}
	if err := findDo(t, r, m, "choose", Arguments{"current": true}); err == nil || r.Chooser(PathChooserKind) == nil {
		t.Fatalf("a file pick took the folder: %v", err)
	}
	findPress(m, "esc")
	if string(origin.Text) != root {
		t.Fatal("a refused pick changed the origin")
	}
	c = findOpen(t, r, m, nil)
	if got := labels(c.Matches()); !slices.Equal(got, []string{". (current directory)"}) {
		t.Fatalf("derived folder filter got %q", got)
	}
}

// While the recursive index runs, the shallow rows show and can be
// picked or opened, and the title says it is indexing; its rows then
// replace them, the moved highlight staying where it was.
func TestFindShallowRowsWhileIndexingThenFull(t *testing.T) {
	root := t.TempDir()
	pickerFile(t, root, ".gitignore", "/ignored/\n*.tmp\n")
	pickerFile(t, root, "ignored/deep", "")
	pickerFile(t, root, "hidden.tmp", "")
	pickerFile(t, root, "nested/deep", "")
	pickerFile(t, root, "visible", "")
	gate := &gatedIndex{release: make(chan struct{})}
	r, m, origin := findFixture(t, "unchanged", "path", "", 0)
	r.chooserMemory().findIndex = gate.index
	c := findOpen(t, r, m, Arguments{"path": root})
	if got, want := labels(c.Matches()), []string{". (current directory)", "nested/", ".gitignore", "visible"}; !slices.Equal(got, want) {
		t.Fatalf("shallow rows %q", got)
	}
	if !strings.Contains(c.Title(), "indexing…") || !c.Loading() {
		t.Fatalf("the recursive index is not shown: %q loading=%v", c.Title(), c.Loading())
	}
	findHighlight(t, r, m, c, "visible")
	close(gate.release)
	waitFor(t, func() bool { findLand(m, r.PendingCommands()); return !c.Loading() })
	if got, want := labels(c.Matches()), []string{". (current directory)", "nested/", ".gitignore", "nested/deep", "visible"}; !slices.Equal(got, want) {
		t.Fatalf("full rows %q", got)
	}
	if row, _ := c.Highlighted(); row.Label != "visible" || strings.Contains(c.Title(), "indexing") {
		t.Fatalf("the full index moved the highlight to %q (title %q)", row.Label, c.Title())
	}
	findPress(m, "enter")
	if string(origin.Text) != filepath.Join(canonicalPath(t, root), "visible") {
		t.Fatalf("picked %q", origin.Text)
	}
}

// waitFor polls cond for up to five seconds.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in time")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A failed recursive index keeps the shallow rows, usable, and the title
// says the index is incomplete; a query still searches them.
func TestFindIndexFailureKeepsShallowRows(t *testing.T) {
	root := t.TempDir()
	pickerFile(t, root, "alpha", "")
	pickerFile(t, root, "beta", "")
	gate := &gatedIndex{release: make(chan struct{}), fail: errors.New("permission denied: private")}
	close(gate.release)
	r, m, origin := findFixture(t, "safe", "path", "", 0)
	r.chooserMemory().findIndex = gate.index
	c := findOpen(t, r, m, Arguments{"path": root, "filter": "file"})
	waitFor(t, func() bool { findLand(m, r.PendingCommands()); return !c.Loading() })
	if !strings.Contains(c.Title(), "index incomplete: permission denied") {
		t.Fatalf("the failure is not shown: %q", c.Title())
	}
	findType(m, "bt")
	if got := labels(c.Matches()); !slices.Equal(got, []string{"beta"}) {
		t.Fatalf("query over the shallow rows: %q", got)
	}
	findPress(m, "enter")
	if string(origin.Text) != filepath.Join(canonicalPath(t, root), "beta") {
		t.Fatalf("picked %q", origin.Text)
	}
}

// Closing the picker cancels the index still running.
func TestFindCloseCancelsTheIndex(t *testing.T) {
	root := t.TempDir()
	pickerFile(t, root, "alpha", "")
	gate := &gatedIndex{release: make(chan struct{})}
	r, m, _ := findFixture(t, "", "text", "", 0)
	r.chooserMemory().findIndex = gate.index
	findOpen(t, r, m, Arguments{"path": root})
	findPress(m, "esc")
	waitFor(t, func() bool { gate.mu.Lock(); defer gate.mu.Unlock(); return gate.cancelled })
}

// Each row draws its path, size and modified time (statted off the loop,
// once: the cache answers again); the highlighted row is previewed beside
// the rows, following the highlight.
func TestFindRowsStatAndPreview(t *testing.T) {
	root := t.TempDir()
	pickerFile(t, root, "alpha.txt", "ALPHA CONTENT")
	pickerFile(t, root, "beta.txt", strings.Repeat("b", 2048))
	var mu sync.Mutex
	statted := map[string]int{}
	r, m, _ := findFixture(t, "", "text", "", 0)
	r.chooserMemory().findStat = func(path string) (fs.FileInfo, error) {
		mu.Lock()
		statted[filepath.Base(path)]++
		mu.Unlock()
		return os.Lstat(path)
	}
	c := findOpen(t, r, m, Arguments{"path": root, "filter": "file"})
	row := c.Matches()[1]
	if row.Label != "beta.txt" || strings.TrimSpace(row.Cells[1]) != "2.0K" || !strings.HasPrefix(row.Cells[2], time.Now().Format("2006-")) {
		t.Fatalf("beta's cells %q", row.Cells)
	}
	findType(m, "a")
	findPress(m, "ctrl+u")
	mu.Lock()
	if statted["beta.txt"] != 1 {
		t.Fatalf("beta statted %d times", statted["beta.txt"])
	}
	mu.Unlock()
	findLand(m, r.PendingCommands())
	view := ansi.Strip(c.View(100, 20).Text)
	if !strings.Contains(view, "ALPHA CONTENT") || !strings.Contains(view, "file · 13 bytes") {
		t.Fatalf("alpha's preview:\n%s", view)
	}
	findPress(m, "ctrl+n")
	view = ansi.Strip(c.View(100, 20).Text)
	if !strings.Contains(view, "bbbb") || strings.Contains(view, "ALPHA CONTENT") {
		t.Fatalf("the preview did not follow the highlight:\n%s", view)
	}
}

// ctrl+t steps the match mode and find ranks again in it: regex drops a
// scattered fuzzy match.
func TestFindMatchModeCycles(t *testing.T) {
	root := t.TempDir()
	pickerFile(t, root, "abc", "")
	pickerFile(t, root, "axbxc", "")
	r, m, _ := findFixture(t, "", "text", "", 0)
	c := findOpen(t, r, m, Arguments{"path": root, "filter": "file", "query": "abc"})
	if got := labels(c.Matches()); !slices.Equal(got, []string{"abc", "axbxc"}) || c.MatchMode() != MatchFuzzy {
		t.Fatalf("fuzzy rows %q mode %q", got, c.MatchMode())
	}
	findPress(m, "ctrl+t")
	if got := labels(c.Matches()); c.MatchMode() != MatchRegex || !slices.Equal(got, []string{"abc"}) {
		t.Fatalf("mode %q rows %q", c.MatchMode(), got)
	}
}

// path.find off leaves find out of a field's path picker; picker.open
// with no name still opens on it.
func TestPathFindSettingOff(t *testing.T) {
	root := t.TempDir()
	pickerFile(t, root, "a.txt", "")
	r, m, _ := findFixture(t, "", "text", "", 0)
	if err := r.Settings().Set(PathFindSetting, false); err != nil {
		t.Fatal(err)
	}
	c, err := r.OpenPathChooser(PathChooserSpec{Kind: "field-path", Pick: PathPick{Name: "path", Parameter: Parameter{Type: PathParameterType, Base: root}}})
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(c.Sources(), PathSourceFind) {
		t.Fatalf("path.find off: sources %q", c.Sources())
	}
	findPress(m, "esc")
	if err := r.Settings().Set(PathFindSetting, true); err != nil {
		t.Fatal(err)
	}
	c, err = r.OpenPathChooser(PathChooserSpec{Kind: "field-path", Pick: PathPick{Name: "path", Parameter: Parameter{Type: PathParameterType, Base: root}}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(c.Sources(), PathSourceFind) {
		t.Fatalf("path.find on: sources %q", c.Sources())
	}
	c.closeSources()
	findPress(m, "esc")
	if err := r.Settings().Set(PathFindSetting, false); err != nil {
		t.Fatal(err)
	}
	findOpen(t, r, m, Arguments{"path": root})
}

// NewPicker is a shim: picker.open opens the path picker without it, and
// its ViewBlock draws that picker.
func TestNewPickerShimDrawsThePathPicker(t *testing.T) {
	root := t.TempDir()
	pickerFile(t, root, "alpha.txt", "")
	r, m, _ := findFixture(t, "", "text", "", 0)
	p := NewPicker(r)
	if p.IsOpen() || p.ViewBlock(80, 10).Text != "" {
		t.Fatal("a closed picker drew")
	}
	findOpen(t, r, m, Arguments{"path": root, "filter": "file"})
	if !p.IsOpen() || !strings.Contains(ansi.Strip(p.View(80, 10)), "alpha.txt") {
		t.Fatalf("the shim did not draw the path picker:\n%s", p.View(80, 10))
	}
	// The finder's exported fields stay readable, filled from the find.
	if p.Directory != canonicalPath(t, root) || p.Filter != "file" || p.Selected != 0 || p.Loading || p.Error != "" ||
		len(p.Entries) != 1 || p.Entries[0] != (PickerEntry{Path: "alpha.txt"}) || p.Origin.InputID != "origin" || p.MatchMode() != MatchFuzzy {
		t.Fatalf("shim fields: %+v", *p)
	}
	findPress(m, "esc")
	if p.IsOpen() || p.Entries != nil || p.Directory != "" {
		t.Fatalf("closed shim fields: %+v", *p)
	}
}

func TestPickerStatsLRUIsBounded(t *testing.T) {
	s := newPickerStats()
	for i := range pickerStatLimit + 10 {
		s.put(fmt.Sprint(i), pickerStat{ok: true})
	}
	if s.len() != pickerStatLimit {
		t.Fatalf("%d cached, want %d", s.len(), pickerStatLimit)
	}
	if _, ok := s.get("0"); ok {
		t.Fatal("the oldest entry was kept")
	}
	if _, ok := s.get(fmt.Sprint(pickerStatLimit + 9)); !ok {
		t.Fatal("the newest entry was dropped")
	}
}

func TestPickerSize(t *testing.T) {
	for n, want := range map[int64]string{0: "0", 1023: "1023", 1536: "1.5K", 34 << 10: "34K", 2 << 20: "2.0M", 5 << 30: "5.0G"} {
		if got := pickerSize(n); got != want {
			t.Errorf("pickerSize(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestPickerPreviewBoundsAndFilesystemTypes(t *testing.T) {
	root := t.TempDir()
	large := pickerFile(t, root, "large.txt", strings.Repeat("x", 64*1024)+"NOT PREVIEWED")
	_, text, diagnostic := readPickerPreview(large)
	if diagnostic != "" || strings.Contains(text, "NOT PREVIEWED") || !strings.HasPrefix(text, strings.Repeat("x", 64*1024)) {
		t.Fatalf("large file preview was not bounded: bytes=%d error=%q", len(text), diagnostic)
	}
	binary := pickerFile(t, root, "binary", "\x00PRIVATE BINARY PAYLOAD")
	_, text, diagnostic = readPickerPreview(binary)
	if diagnostic != "" || strings.Contains(text, "PRIVATE BINARY PAYLOAD") {
		t.Fatalf("binary contents displayed: %q, %q", text, diagnostic)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(binary, link); err != nil {
		t.Fatal(err)
	}
	_, text, diagnostic = readPickerPreview(link)
	if diagnostic != "" || !strings.Contains(text, binary) || strings.Contains(text, "PRIVATE BINARY PAYLOAD") {
		t.Fatalf("symlink preview followed target: %q, %q", text, diagnostic)
	}
	_, _, diagnostic = readPickerPreview(filepath.Join(root, "missing"))
	if diagnostic == "" {
		t.Fatal("missing file did not report a preview error")
	}
}
