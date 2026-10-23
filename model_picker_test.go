package ochazuke

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/ntt")})
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
