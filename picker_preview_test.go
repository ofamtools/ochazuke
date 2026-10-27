package ochazuke

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestPickerPreviewRejectsOldSelectionAndSessionResults(t *testing.T) {
	root := t.TempDir()
	pickerFile(t, root, "alpha.txt", "ALPHA CONTENT")
	pickerFile(t, root, "beta.txt", "BETA CONTENT")
	r, p, _ := pickerFixture(t, "", "path", "any", 0)
	pickerDo(t, r, "open", "picker", Arguments{"path": root})
	pickerDrain(t, p)
	pickerHighlight(t, r, p, "alpha.txt")
	var old []tea.Msg
	var collect func(tea.Cmd)
	collect = func(cmd tea.Cmd) {
		if cmd == nil {
			return
		}
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, child := range batch {
				collect(child)
			}
		} else {
			old = append(old, msg)
		}
	}
	collect(p.PendingCommand())
	pickerHighlight(t, r, p, "beta.txt")
	pickerDrain(t, p)
	for _, msg := range old {
		p.HandleMessage(msg)
	}
	view := ansi.Strip(p.View(100, 20))
	if !strings.Contains(view, "BETA CONTENT") || strings.Contains(view, "ALPHA CONTENT") {
		t.Fatalf("old preview replaced the selected file:\n%s", view)
	}
	pickerKey(t, r, "esc")
	if err := os.WriteFile(filepath.Join(root, "alpha.txt"), []byte("UPDATED ALPHA"), 0600); err != nil {
		t.Fatal(err)
	}
	pickerDo(t, r, "open", "picker", Arguments{"path": root})
	pickerDrain(t, p)
	pickerHighlight(t, r, p, "alpha.txt")
	pickerDrain(t, p)
	for _, msg := range old {
		p.HandleMessage(msg)
	}
	view = ansi.Strip(p.View(100, 20))
	if !strings.Contains(view, "UPDATED ALPHA") || strings.Contains(view, "ALPHA CONTENT") {
		t.Fatalf("previous session overwrote reopened preview:\n%s", view)
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

func TestPickerBindingHelpTracksRemappingAndViewport(t *testing.T) {
	root := t.TempDir()
	pickerFile(t, root, "alpha.txt", "a visible preview")
	r, p, origin := pickerFixture(t, "preserved", "text", "any", 0)
	pickerDo(t, r, "open", "picker", Arguments{"path": root})
	pickerDrain(t, p)
	r.Noun("ochazuke.picker").Bindings["insert"] = []Binding{
		{Mode: "insert", Keys: "alt+n", Request: Request{Verb: "move", Noun: "ochazuke.picker", Args: Arguments{"delta": 1}}},
		{Mode: "insert", Keys: "ctrl+g", Request: Request{Verb: "close", Noun: "ochazuke.picker"}},
	}
	pickerKey(t, r, "alt+n")
	pickerDrain(t, p)
	if p.Entries[p.Selected].Path != "alpha.txt" {
		t.Fatal("remapped navigation did not select the file")
	}
	view := ansi.Strip(p.View(100, 20))
	if !strings.Contains(view, "alt+n") || !strings.Contains(view, "ctrl+g") || strings.Contains(view, "ctrl+n") || strings.Contains(view, "esc") {
		t.Fatalf("help did not reflect effective remapped bindings:\n%s", view)
	}
	for _, size := range [][2]int{{1, 1}, {2, 2}, {24, 5}, {49, 12}, {50, 12}, {80, 24}, {120, 32}} {
		view := p.View(size[0], size[1])
		rows := strings.Split(view, "\n")
		if len(rows) != size[1] {
			t.Fatalf("%v: got %d rows", size, len(rows))
		}
		for _, row := range rows {
			if width := ansi.StringWidth(row); width != size[0] {
				t.Fatalf("%v: row width %d: %q", size, width, row)
			}
		}
	}
	pickerKey(t, r, "esc")
	pickerKey(t, r, "j")
	if !p.IsOpen() || p.input.Mode != "insert" || !strings.HasSuffix(string(p.input.Text), "j") {
		t.Fatal("unbound Escape enabled Vim or canceled the picker")
	}
	pickerKey(t, r, "ctrl+g")
	if p.IsOpen() || string(origin.Text) != "preserved" {
		t.Fatal("remapped cancellation changed the origin")
	}
}

func TestPickerOpensWithAbsoluteEditablePath(t *testing.T) {
	t.Chdir(t.TempDir())
	pickerFile(t, ".", "nested/target.txt", "contents")
	r, p, _ := pickerFixture(t, "", "path", "any", 0)
	pickerDo(t, r, "open", "picker", Arguments{"path": "nested/"})
	pickerDrain(t, p)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(cwd, "nested") + string(filepath.Separator)
	if string(p.input.Text) != want {
		t.Fatalf("relative opening path was not made absolute: %q, want %q", p.input.Text, want)
	}
	pickerKey(t, r, "ctrl+w")
	if got := string(p.input.Text); got != cwd+string(filepath.Separator) {
		t.Fatalf("absolute path could not be edited by component: %q", got)
	}
	pickerKey(t, r, "ctrl+y")
	if string(p.input.Text) != want {
		t.Fatal("yank did not restore absolute path component")
	}
}
