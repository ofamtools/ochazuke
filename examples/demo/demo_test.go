package demo

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"ochazuke"
	"ochazuke/internal/keytest"
)

func TestModelCoalescedTypingPreservesModeTransitions(t *testing.T) {
	m := New()
	_, err := m.Runtime.Execute(ochazuke.Request{Verb: "open", Noun: "page", Args: ochazuke.Arguments{"id": "playground"}})
	if err != nil {
		t.Fatal(err)
	}
	keytest.Type(m, "iabc")
	in := m.Runtime.Input("directory")
	if string(in.Text) != "abc/tmp" || in.Mode != "insert" {
		t.Fatalf("coalesced iabc: text=%q mode=%q; want abc/tmp in insert mode", string(in.Text), in.Mode)
	}
	keytest.Paste(m, "ctrl+a")
	if string(in.Text) != "abcctrl+a/tmp" {
		t.Fatalf("paste interpreted as command: %q", string(in.Text))
	}
}

func TestModelCoalescedPaletteSearchNavigatesPage(t *testing.T) {
	m := New()
	if err := m.Palette.Open(); err != nil {
		t.Fatal(err)
	}
	typed := func(s string) { keytest.Type(m, s) }
	enter := func() { keytest.Press(m, "enter") }
	typed("open")
	enter()
	typed("page")
	enter()
	typed("playground")
	enter()
	enter()
	if m.Runtime.Page != "playground" || len(m.Runtime.Modals()) != 0 {
		t.Fatalf("coalesced palette navigation: page=%q frames=%d; want playground with palette closed", m.Runtime.Page, len(m.Runtime.Modals()))
	}
}

func TestDemoPageKeysDoNotStealInsertEditing(t *testing.T) {
	m := New()
	keytest.Press(m, "ctrl+j")
	if m.Runtime.Page != "playground" {
		t.Fatalf("Ctrl-J page = %q", m.Runtime.Page)
	}
	keytest.Press(m, "alt+1")
	if m.Runtime.Page != "playground" {
		t.Fatal("Alt-1 still switches pages")
	}
	keytest.Type(m, "iabc")
	keytest.Press(m, "ctrl+k")
	if got := string(m.Runtime.Input("directory").Text); got != "abc" {
		t.Fatalf("insert Ctrl-K text = %q, want abc", got)
	}
	if m.Runtime.Page != "playground" {
		t.Fatal("insert Ctrl-K switched page")
	}
	keytest.Press(m, "esc")
	keytest.Press(m, "ctrl+k")
	if m.Runtime.Page != "overview" {
		t.Fatalf("normal Ctrl-K page = %q", m.Runtime.Page)
	}
	keytest.Type(m, ":open")
	if !m.Palette.IsActive() || !strings.Contains(ansi.Strip(m.View().Content), "open") {
		t.Fatalf("colon search: active=%v view=%q", m.Palette.IsActive(), ansi.Strip(m.View().Content))
	}
}

func TestModelPickerProcessesCoalescedTypingAndCalls(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "target.txt"), []byte("target"), 0600); err != nil {
		t.Fatal(err)
	}
	m := New()
	drain := func(cmd tea.Cmd) {
		queue := []tea.Cmd{cmd}
		for steps := 0; len(queue) > 0; steps++ {
			if steps > 100 {
				t.Fatal("picker commands did not settle")
			}
			next := queue[0]
			queue = queue[1:]
			if next == nil {
				continue
			}
			msg := next()
			if batch, ok := msg.(tea.BatchMsg); ok {
				queue = append(queue, batch...)
				continue
			}
			_, pending := m.Update(msg)
			queue = append(queue, pending)
		}
	}
	reply := make(chan ochazuke.Response, 1)
	_, cmd := m.Update(ochazuke.CallMsg{Request: ochazuke.Request{Verb: "open", Noun: "picker", Target: "directory", Args: ochazuke.Arguments{"path": root, "filter": "file"}}, Reply: reply})
	response := <-reply
	if response.Error != "" {
		t.Fatal(response.Error)
	}
	drain(cmd)
	keytest.Press(m, "ctrl+e")
	cmd = keytest.Type(m, "/ntt")
	drain(cmd)
	if got := string(m.Runtime.Input("picker-path").Text); got != root+"/ntt" {
		t.Fatalf("fast typing lost characters: %q", got)
	}
	entries := m.Runtime.Instances["picker"].Component.(*ochazuke.Picker).Entries
	if len(entries) != 1 || entries[0].Path != "nested/target.txt" {
		t.Fatalf("recursive fuzzy results: %#v", entries)
	}
}

func TestModelPickerHomeProjectsSelectableBeforeRecursiveScan(t *testing.T) {
	pickerGitEnvironment(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	pickerFile(t, home, "proj/project/deep/file", "")
	canonical, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	m := New()
	t.Cleanup(func() {
		m.Runtime.Instances["picker"].Component.(*ochazuke.Picker).Close()
	})
	// Execute one delivery stage, retaining the next commands instead of
	// racing the full scan against assertions about the visible shallow list.
	stage := func(cmd tea.Cmd) tea.Cmd {
		queue := []tea.Cmd{cmd}
		var pending []tea.Cmd
		for len(queue) > 0 {
			next := queue[0]
			queue = queue[1:]
			if next == nil {
				continue
			}
			msg := next()
			if batch, ok := msg.(tea.BatchMsg); ok {
				queue = append(queue, batch...)
			} else {
				_, cmd := m.Update(msg)
				pending = append(pending, cmd)
			}
		}
		return tea.Batch(pending...)
	}
	reply := make(chan ochazuke.Response, 1)
	_, cmd := m.Update(ochazuke.CallMsg{Request: ochazuke.Request{Verb: "open", Noun: "picker", Target: "directory", Args: ochazuke.Arguments{"path": "~/proj", "filter": "dir"}}, Reply: reply})
	if response := <-reply; response.Error != "" {
		t.Fatal(response.Error)
	}
	search := stage(cmd)
	stage(search) // ochazuke.Leave recursion pending.
	top := m.Runtime.TopModal()
	picker := m.Runtime.Instances["picker"].Component.(*ochazuke.Picker)
	if !reflect.DeepEqual(pickerEntryPaths(picker.Entries), []string{".", "project"}) || !picker.Loading {
		t.Fatalf("home projects shallow state: %#v loading=%v", picker.Entries, picker.Loading)
	}
	if view := m.View().Content; !strings.Contains(view, "project") || !strings.Contains(view, "Indexing/searching") {
		t.Fatalf("shallow project missing from modal:\n%s", view)
	}
	keytest.Press(m, "ctrl+n")
	keytest.Press(m, "ctrl+s")
	if got, want := string(m.Runtime.Input("directory").Text), filepath.Join(canonical, "proj", "project"); got != want {
		t.Fatalf("shallow project selection = %q, want %q", got, want)
	}
	if top == m.Runtime.TopModal() {
		t.Fatal("project confirmation did not close picker")
	}
}

func TestSnapshotExposesNounsAndInstances(t *testing.T) {
	r := New().Runtime
	rows, err := ochazuke.JSONPointer(r.Snapshot(), "/nouns/playground/bindings/normal")
	if err != nil {
		t.Fatal(err)
	}
	if list, ok := rows.([]any); !ok || len(list) == 0 {
		t.Fatalf("playground normal rows: %#v", rows)
	}
	parent, err := ochazuke.JSONPointer(r.Snapshot(), "/instances/directory/parent")
	if err != nil || parent != "agent" {
		t.Fatalf("instance parent %v, %v", parent, err)
	}
	if page, _ := ochazuke.JSONPointer(r.Snapshot(), "/page"); page != "overview" {
		t.Fatalf("/page = %v", page)
	}
}

// assertPageInvariant: Runtime.Page is an ancestor of the base frame's focus.
func assertPageInvariant(t *testing.T, r *ochazuke.Runtime, step string) {
	t.Helper()
	if r.Page == "" {
		return
	}
	if !slices.Contains(r.Ancestry(r.BaseFocus()), r.Page) {
		t.Fatalf("%s: page %q is not an ancestor of base focus %q", step, r.Page, r.BaseFocus())
	}
}

// The demo session the phase 3 equivalence test drove: pages, fields, nested
// modals, yank history, palette, picker and sequences, every step green and
// the page invariant holding.
func TestDemoKeyboardSessionStaysGreen(t *testing.T) {
	m := New()
	r := m.Runtime
	r.BindingTimeout = time.Hour
	step := func(label string, run func()) {
		t.Helper()
		run()
		if m.Status != "" {
			t.Fatalf("%s: %s", label, m.Status)
		}
		assertPageInvariant(t, r, label)
	}
	press := func(keys ...string) {
		t.Helper()
		for _, key := range keys {
			step("key "+key, func() { keytest.Press(m, key) })
		}
	}
	execute := func(verb, noun string, args ochazuke.Arguments) {
		t.Helper()
		step(noun+"."+verb, func() {
			if _, err := r.Execute(ochazuke.Request{Verb: verb, Noun: noun, Args: args}); err != nil {
				t.Fatalf("%s.%s: %v", noun, verb, err)
			}
		})
	}
	press("ctrl+j", "i", "h", "i", "esc", "y", "y", "tab", "tab", "shift+tab", "ctrl+x")
	press("ctrl+n", "i", "x", "esc", "ctrl+n", "ctrl+g", "ctrl+g")
	press("ctrl+y", "esc")
	execute("open", "palette", nil)
	press("p", "a")
	execute("close", "palette", nil)
	press("?", "ctrl+g")
	execute("focus", "input", ochazuke.Arguments{"id": "directory"})
	press("ctrl+o")
	execute("close", "picker", nil)
	press("g")
	execute("select", "context", ochazuke.Arguments{"path": `["app","playground","agent"]`})
	press("ctrl+j", "j", "k", "ctrl+j", "t", "d", "ctrl+j", "ctrl+k", "ctrl+k")
	execute("open", "page", ochazuke.Arguments{"id": "playground"})
	execute("push", "modal", ochazuke.Arguments{"id": "draft", "input": "dialog"})
	execute("pop", "modal", nil)
	if string(r.Input("directory").Text) != "hi/tmp" || r.Registers().Named["\""].Yank.Text != "hi/tmp\n" {
		t.Fatalf("session state: text=%q yank=%q", r.Input("directory").Text, r.Registers().Named["\""].Yank.Text)
	}
}
func TestFocusedInputFollowsGlobalModeInDemo(t *testing.T) {
	m := New()
	r := m.Runtime
	for _, key := range []string{"ctrl+j", "i", "h", "esc", "v", "l", "esc", "ctrl+n", "i", "x", "esc", "ctrl+g", "2", "d"} {
		keytest.Press(m, key)
		if in := r.Input(r.Focus); in != nil && r.BaseMode() != in.Mode {
			t.Fatalf("after %s: input %s mode %q, global %+v", key, in.ID, in.Mode, r.Mode)
		}
	}
}

// Picker test helpers, as in the framework's own picker tests.

func pickerGitEnvironment(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	for _, key := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_INDEX_FILE", "GIT_CONFIG_COUNT"} {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}
}

func pickerFile(t *testing.T, root, path, text string) string {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	return full
}

func pickerEntryPaths(entries []ochazuke.PickerEntry) []string {
	paths := make([]string, len(entries))
	for i, entry := range entries {
		paths[i] = entry.Path
	}
	return paths
}

// Every row of the demo's tables is a Request to a registered operation or
// an unbind; see TestBindingRowsAreAtomic in the framework package.
func TestDemoBindingRowsAreAtomic(t *testing.T) {
	if _, err := New().Runtime.ValidateBindings(); err != nil {
		t.Fatal(err)
	}
}
