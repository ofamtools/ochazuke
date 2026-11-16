package ochazuke

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ofam.dev/ochazuke/keytest"
)

// The help page's editing keys, wired to the binding verbs and the user's
// bindings.json in a temp config dir: r captures a key with the preview
// shown before enter applies it, the old key's default is unbound and
// listed as removed, u undoes through history-entry.undo (the file goes
// back as it was), d d and U remove and reset, W shows the file, the
// binding verbs run alone (the palette's and the CLI's route), and with
// help.edit off the keys stand aside.
func TestHelpPageEdits(t *testing.T) {
	r, m := helpPageFixture(t)
	dir := t.TempDir()
	r.Config().Dir = dir
	path := filepath.Join(dir, BindingsFile)
	file := func() string {
		data, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		return string(data)
	}
	keytest.Press(m, "?")
	p, list := r.helpPage(), r.List(helpRowsID)
	cut := "row:noun tile|normal|x|||tile.cut"
	selectRow := func(id string) {
		t.Helper()
		if err := list.Select(id); err != nil {
			t.Fatalf("select %s: %v", id, err)
		}
	}
	selectRow(cut)
	keytest.Press(m, "r")
	keytest.Press(m, "ctrl+t")
	detail := strings.Join(p.Detail, "\n")
	if !strings.Contains(detail, "After this change, ctrl+t runs: tile.cut (at tile)") || file() != "" || r.Mode.Name != helpPageCaptureMode {
		t.Fatalf("before enter: mode %s, file %q, detail:\n%s", r.Mode.Name, file(), detail)
	}
	keytest.Press(m, "enter")
	if f := file(); !strings.Contains(f, `{"key": "ctrl+t", "action": "tile.cut"}`) || !strings.Contains(f, `{"key": "x", "unbind": true}`) {
		t.Fatalf("after enter the file is:\n%s\n(status %q)", f, m.Status)
	}
	if e := helpEntry(t, p.Walk, "ctrl+t", "tile.cut"); e.State != StatusAnswers {
		t.Fatalf("ctrl+t tile.cut is %s", e.State)
	}
	removed := false
	for _, v := range p.Rows {
		removed = removed || v.State == StatusRemoved && v.Row.Request.Key() == "tile.cut" && v.Changed
	}
	if !removed || !strings.Contains(m.View().Content, "✎") {
		t.Fatal("the unbound default is not listed as removed and changed")
	}
	keytest.Press(m, "u")
	if f := file(); f != "" {
		t.Fatalf("u left the file:\n%s", f)
	}
	if e := helpEntry(t, p.Walk, "x", "tile.cut"); e.State != StatusAnswers {
		t.Fatalf("after u, x tile.cut is %s", e.State)
	}
	// d d unbinds the default; U on the removed row puts it back.
	selectRow(cut)
	keytest.Press(m, "d")
	keytest.Press(m, "d")
	if !strings.Contains(file(), `{"key": "x", "unbind": true}`) || !strings.Contains(strings.Join(p.Detail, "\n"), "(unbound)") {
		t.Fatalf("d d: file %q detail %q status %q info %q mode %s focus %s", file(), p.Detail, m.Status, m.Info, r.Mode.Name, r.Focus)
	}
	selectRow("removed:noun tile|normal|x|||tile.cut")
	keytest.Press(m, "U")
	if strings.Contains(file(), `"unbind": true`) {
		t.Fatalf("U left %q", file())
	}
	keytest.Press(m, "W")
	if r.TopModal() == "" {
		t.Fatal("W opened no view of the file")
	}
	keytest.Press(m, "esc")
	// The verbs alone, as the palette and the CLI run them.
	if _, err := r.Execute(Request{Noun: "ochazuke.binding", Verb: "unify", Args: Arguments{"verb": "fold", "key": "ctrl+o"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.binding", Verb: "rebind", Args: Arguments{"noun": "tile", "mode": "normal", "key": "ctrl+o", "hint": "fold it"}}); err != nil {
		t.Fatal(err)
	}
	if f := file(); !strings.Contains(f, `{"key": "ctrl+o", "action": "tile.fold", "hint": "fold it"}`) {
		t.Fatalf("unify then rebind wrote:\n%s", f)
	}
	// help.edit off: the page is read-only again.
	if err := r.Settings().Set(HelpEditSetting, false); err != nil {
		t.Fatal(err)
	}
	selectRow(cut)
	keytest.Press(m, "r")
	if p.Edit != nil || r.Mode.Name == helpPageCaptureMode {
		t.Fatalf("help.edit off: r started a capture (mode %s)", r.Mode.Name)
	}
}
