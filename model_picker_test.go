package ochazuke

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"ochazuke/internal/keytest"
)

func TestModelPickerProcessesCoalescedTypingAndCalls(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "target.txt"), []byte("target"), 0600); err != nil {
		t.Fatal(err)
	}
	m := NewDemo()
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
	reply := make(chan Response, 1)
	_, cmd := m.Update(CallMsg{Request: Request{Verb: "open", Noun: "picker", Target: "directory", Args: Arguments{"path": root, "filter": "file"}}, Reply: reply})
	response := <-reply
	if response.Error != "" {
		t.Fatal(response.Error)
	}
	drain(cmd)
	keytest.Press(m, "ctrl+e")
	cmd = keytest.Type(m, "/ntt")
	drain(cmd)
	if got := string(m.Runtime.Inputs["picker-path"].Text); got != root+"/ntt" {
		t.Fatalf("fast typing lost characters: %q", got)
	}
	frame := m.Runtime.Frames[len(m.Runtime.Frames)-1]
	entries, ok := frame.State["entries"].([]PickerEntry)
	if !ok || len(entries) != 1 || entries[0].Path != "nested/target.txt" {
		t.Fatalf("recursive fuzzy results: %#v", frame.State)
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
	m := NewDemo()
	t.Cleanup(func() {
		for _, p := range m.Pickers {
			p.stop()
		}
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
	reply := make(chan Response, 1)
	_, cmd := m.Update(CallMsg{Request: Request{Verb: "open", Noun: "picker", Target: "directory", Args: Arguments{"path": "~/proj", "filter": "dir"}}, Reply: reply})
	if response := <-reply; response.Error != "" {
		t.Fatal(response.Error)
	}
	search := stage(cmd)
	stage(search) // Leave recursion pending.
	frame := m.Runtime.Frames[len(m.Runtime.Frames)-1]
	entries, _ := frame.State["entries"].([]PickerEntry)
	if !reflect.DeepEqual(pickerEntryPaths(entries), []string{".", "project"}) || frame.State["loading"] != true {
		t.Fatalf("home projects shallow state: %#v", frame.State)
	}
	if view := m.View(); !strings.Contains(view, "project") || !strings.Contains(view, "Indexing/searching") {
		t.Fatalf("shallow project missing from modal:\n%s", view)
	}
	keytest.Press(m, "ctrl+n")
	keytest.Press(m, "ctrl+s")
	if got, want := string(m.Runtime.Inputs["directory"].Text), filepath.Join(canonical, "proj", "project"); got != want {
		t.Fatalf("shallow project selection = %q, want %q", got, want)
	}
	if frame == m.Runtime.Frames[len(m.Runtime.Frames)-1] {
		t.Fatal("project confirmation did not close picker")
	}
}
