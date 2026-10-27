package ochazuke

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// paneModel is a page drawing a list and a detail in Layout.SplitBlock
// through PaneLayout (when split is set), at 100x24 with stateDir as the
// state directory ("" for none).
func paneModel(t *testing.T, stateDir string, split bool) *Model {
	t.Helper()
	r := NewRuntime()
	r.Config().StateDir = stateDir
	r.Define("thing")
	r.Instance("p")
	list, err := r.AddList("p/list", []ListItem{{ID: "a", Label: "a", Noun: "thing"}, {ID: "b", Label: "b", Noun: "thing"}}, Parent("p"))
	if err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	render := func(r *Runtime, w, h int) Block {
		if !split {
			return Text(list.View(w, h))
		}
		l := r.PaneLayout("p", Layout{Width: m.Width, Height: m.Height, Styles: NewStyles(Appearance{Theme: "mono"})})
		return l.SplitBlock("list", Text(list.View(w/2, h)), "detail", Text("detail text"))
	}
	if _, err := r.AddPage("p", "P", render, Focus("p/list")); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("p"); err != nil {
		t.Fatal(err)
	}
	m.Width, m.Height = 100, 24
	m.View()
	return m
}

func paneKeys(m *Model, keys ...string) {
	for _, key := range keys {
		m.handleKey(MacroKey{Key: key})
		m.View()
	}
}

func TestPaneKeysGrowShrinkAndResetTheSplit(t *testing.T) {
	m := paneModel(t, "", true)
	r := m.Runtime
	paneKeys(m, ">")
	if got := r.PaneShift("p"); got != PaneStep {
		t.Fatalf("> shift = %d, want %d (status %q)", got, PaneStep, m.Status)
	}
	paneKeys(m, "3", ">")
	if got := r.PaneShift("p"); got != 4*PaneStep {
		t.Fatalf("3> shift = %d, want %d", got, 4*PaneStep)
	}
	paneKeys(m, "<", "<")
	if got := r.PaneShift("p"); got != 2*PaneStep {
		t.Fatalf("< < shift = %d", got)
	}
	paneKeys(m, "=")
	if got := r.PaneShift("p"); got != 0 {
		t.Fatalf("= shift = %d", got)
	}
	for range 20 {
		paneKeys(m, ">")
	}
	if got := r.PaneShift("p"); got != PaneShiftLimit {
		t.Fatalf("held > shift = %d, want the limit %d", got, PaneShiftLimit)
	}
	paneKeys(m, "<")
	if got := r.PaneShift("p"); got != PaneShiftLimit-PaneStep {
		t.Fatalf("< after the limit = %d: the next key must move at once", got)
	}
	if r.Focus != "a" || m.Status != "" {
		t.Fatalf("focus %q status %q", r.Focus, m.Status)
	}
	hints := BindingHints(r, r.Focus)
	if !strings.Contains(hints, ">/< resize") || strings.Contains(hints, "pane.") {
		t.Fatalf("hints = %q; want one >/< resize row", hints)
	}
}

func TestPaneKeysOnlyWhereASplitIsDrawn(t *testing.T) {
	m := paneModel(t, "", false)
	r := m.Runtime
	if hints := BindingHints(r, r.Focus); strings.Contains(hints, "resize") {
		t.Fatalf("hints without a split = %q", hints)
	}
	paneKeys(m, ">", "<")
	if got := r.PaneShift("p"); got != 0 {
		t.Fatalf("> without a split moved the divider: %d", got)
	}
	// The verbs themselves still run (the CLI, the palette).
	if _, err := r.Execute(Request{Noun: "app.pane", Verb: "grow"}); err != nil || r.PaneShift("p") != PaneStep {
		t.Fatalf("pane.grow direct: %v, shift %d", err, r.PaneShift("p"))
	}
}

func TestPaneDividerMovesTheDrawnSplit(t *testing.T) {
	m := paneModel(t, "", true)
	divider := func() int {
		for _, row := range strings.Split(ansi.Strip(m.View().Content), "\n") {
			if strings.Count(row, "╭") == 2 {
				return strings.LastIndex(row, "╭")
			}
		}
		t.Fatal("no split drawn")
		return 0
	}
	before := divider()
	paneKeys(m, ">")
	if after := divider(); after <= before {
		t.Fatalf("> did not move the divider right: %d then %d", before, after)
	}
}

func TestPaneDividerRememberedAcrossRestarts(t *testing.T) {
	dir := t.TempDir()
	m := paneModel(t, dir, true)
	paneKeys(m, "2", ">")
	data, err := os.ReadFile(filepath.Join(dir, PanesFile))
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]int
	if err := json.Unmarshal(data, &saved); err != nil || saved["p"] != 2*PaneStep {
		t.Fatalf("panes file %s: %v", data, err)
	}
	if again := paneModel(t, dir, true); again.Runtime.PaneShift("p") != 2*PaneStep {
		t.Fatalf("restart shift = %d", again.Runtime.PaneShift("p"))
	}
	paneKeys(m, "=")
	if data, _ := os.ReadFile(filepath.Join(dir, PanesFile)); strings.Contains(string(data), `"p"`) {
		t.Fatalf("reset left the page in the file: %s", data)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("state dir holds %d files; want the panes file alone (no temp left)", len(entries))
	}
}

func TestPaneDividerNotPersistedWithoutAStateDirectory(t *testing.T) {
	m := paneModel(t, "", true)
	m.Runtime.Config().Disable()
	paneKeys(m, ">")
	if m.Runtime.Config().PanesPath() != "" || m.Runtime.PaneShift("p") != PaneStep {
		t.Fatalf("disabled config: path %q shift %d", m.Runtime.Config().PanesPath(), m.Runtime.PaneShift("p"))
	}
}

func TestPanesFileThatDoesNotParseIsANotice(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, PanesFile), []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := paneModel(t, dir, true)
	if m.Runtime.PaneShift("p") != 0 || len(m.Notices) != 1 || !strings.Contains(m.Notices[0], PanesFile) {
		t.Fatalf("notices %q", m.Notices)
	}
}
