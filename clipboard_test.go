package ochazuke

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// The clipboard setting says which yanks also reach the system clipboard
// (Registers.PendingCommand: OSC 52 through tea.SetClipboard, or the
// app's Registers.Copy); the clipboard register "+ (and "*) always does.
func TestYanksReachTheClipboard(t *testing.T) {
	for _, tc := range []struct {
		setting       string
		yank, deleted bool // whether a yank and a delete are copied
	}{
		{"yanks", true, false},
		{"unnamed", true, true},
		{"none", false, false},
	} {
		t.Run(tc.setting, func(t *testing.T) {
			r, _ := editorTestRuntime(t, "one two", false)
			if err := r.Settings().Set(ClipboardSetting, tc.setting); err != nil {
				t.Fatal(err)
			}
			var copied []string
			r.Registers().Copy = func(text string) tea.Cmd {
				copied = append(copied, text)
				return func() tea.Msg { return nil }
			}
			drain := func() []string {
				copied = nil
				r.PendingCommands()
				return copied
			}
			editorTestKeys(t, r, "y", "w")
			if got := drain(); (len(got) == 1 && got[0] == "one ") != tc.yank {
				t.Fatalf("yank copied %q", got)
			}
			editorTestKeys(t, r, "x")
			if got := drain(); (len(got) == 1 && got[0] == "o") != tc.deleted {
				t.Fatalf("delete copied %q", got)
			}
			editorTestKeys(t, r, "\"", "+", "y", "w")
			if got := drain(); len(got) != 1 || got[0] != "ne " {
				t.Fatalf("\"+yw copied %q", got)
			}
			editorTestKeys(t, r, "\"", "*", "p")
			if got := r.Registers().Named["+"].Yank.Text; got != "ne " {
				t.Fatalf("\"* reads %q", got)
			}
		})
	}
}

// With no Copy the text goes out as OSC 52.
func TestClipboardDefaultsToOSC52(t *testing.T) {
	r, _ := editorTestRuntime(t, "abc", false)
	editorTestKeys(t, r, "y", "y")
	if r.Registers().PendingCommand() == nil {
		t.Fatal("a yank scheduled no clipboard command")
	}
	if r.Registers().PendingCommand() != nil {
		t.Fatal("the clipboard command repeats")
	}
}
