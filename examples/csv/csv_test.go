package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
	"github.com/unixsuperhero/ochazuke/grid"
	"github.com/unixsuperhero/ochazuke/internal/keytest"
)

const fixture = "testdata/people.csv"

func open(t *testing.T) *App {
	t.Helper()
	a, err := New(fixture)
	if err != nil {
		t.Fatal(err)
	}
	a.m.Width, a.m.Height = 80, 24
	return a
}

// press sends keys one by one, failing on the first error the model shows.
func press(t *testing.T, a *App, keys ...string) {
	t.Helper()
	for _, key := range keys {
		keytest.Press(a.m, key)
		if a.m.Status != "" {
			t.Fatalf("key %q: %s", key, a.m.Status)
		}
	}
}

func cursor(t *testing.T, a *App, want grid.Ref) {
	t.Helper()
	if got := a.grid.Cursor(); got != want {
		t.Fatalf("cursor = %v, want %v (focus %s)", got, want, a.r.Focus)
	}
}

func TestOpenAndNavigateWithCounts(t *testing.T) {
	a := open(t)
	if !reflect.DeepEqual(a.Header, []string{"name", "email", "role"}) || len(a.Rows) != 5 {
		t.Fatalf("loaded header=%v rows=%d", a.Header, len(a.Rows))
	}
	if a.r.Focus != "r0:c0" || a.r.Page != "sheet" {
		t.Fatalf("start focus=%s page=%s", a.r.Focus, a.r.Page)
	}
	if got := a.r.InstancePath("r0:c0"); !reflect.DeepEqual(got, []string{"app", "sheet", "grid", "c0", "r0", "r0:c0"}) {
		t.Fatalf("cell path = %v", got)
	}
	press(t, a, "3", "j")
	cursor(t, a, grid.Ref{Row: 3, Col: 0})
	if a.r.Focus != "r3:c0" || a.r.Instances["r0:c0"] != nil {
		t.Fatalf("focus=%s, old cell kept=%v", a.r.Focus, a.r.Instances["r0:c0"] != nil)
	}
	press(t, a, "2", "l")
	cursor(t, a, grid.Ref{Row: 3, Col: 2})
	press(t, a, "0")
	cursor(t, a, grid.Ref{Row: 3, Col: 0})
	press(t, a, "$")
	cursor(t, a, grid.Ref{Row: 3, Col: 2})
	press(t, a, "g", "g")
	cursor(t, a, grid.Ref{Row: 0, Col: 2})
	press(t, a, "G")
	cursor(t, a, grid.Ref{Row: 4, Col: 2})
	press(t, a, "2", "G")
	cursor(t, a, grid.Ref{Row: 1, Col: 2})
	press(t, a, "9", "k")
	cursor(t, a, grid.Ref{Row: 0, Col: 2})
	press(t, a, "4", "g", "g")
	cursor(t, a, grid.Ref{Row: 3, Col: 2})
	// The sheet has two focus stops, the grid and the filter field: } lands
	// on the field and tab returns to the remembered cell.
	press(t, a, "}")
	if a.r.Focus != filterID {
		t.Fatalf("} from the grid: %s", a.r.Focus)
	}
	press(t, a, "tab")
	if a.r.Focus != "r3:c2" {
		t.Fatalf("tab from the filter field: %s", a.r.Focus)
	}
	press(t, a, "g", "g")
	if mode := a.r.Mode.Name; mode != "normal" {
		t.Fatalf("mode after motions = %q", mode)
	}
	view := ansi.Strip(a.m.View().Content)
	for _, want := range []string{"people.csv", "5 rows × 3 columns", "r0:c2", " 1 ann", "i cell.edit"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view lacks %q:\n%s", want, view)
		}
	}
}

func TestEditCellThroughPrompt(t *testing.T) {
	a := open(t)
	press(t, a, "j", "enter")
	if len(a.r.Modals()) != 1 || a.r.Focus != fieldID || string(a.field.Text) != "bo" {
		t.Fatalf("prompt: frames=%d focus=%s text=%q", len(a.r.Modals()), a.r.Focus, string(a.field.Text))
	}
	if mode := a.r.Mode.Name; mode != "insert" {
		t.Fatalf("prompt mode = %q", mode)
	}
	if view := ansi.Strip(a.m.View().Content); !strings.Contains(view, "Edit r1:c0") {
		t.Fatalf("modal title missing:\n%s", view)
	}
	keytest.Type(a.m, "b")
	press(t, a, "enter")
	if len(a.r.Modals()) != 0 || a.Rows[1][0] != "bob" || !a.Dirty || len(a.Undo) != 1 {
		t.Fatalf("commit: frames=%d cell=%q dirty=%v undo=%d", len(a.r.Modals()), a.Rows[1][0], a.Dirty, len(a.Undo))
	}
	if a.r.Focus != "r1:c0" {
		t.Fatalf("focus after commit = %s", a.r.Focus)
	}
	press(t, a, "i")
	keytest.Type(a.m, "zzz")
	press(t, a, "esc")
	if len(a.r.Modals()) != 0 || a.Rows[1][0] != "bob" || len(a.Undo) != 1 {
		t.Fatalf("cancel changed the sheet: %q undo=%d", a.Rows[1][0], len(a.Undo))
	}
	press(t, a, "u")
	if a.Rows[1][0] != "bo" || len(a.Undo) != 0 || len(a.Redo) != 1 {
		t.Fatalf("undo: cell=%q undo=%d redo=%d", a.Rows[1][0], len(a.Undo), len(a.Redo))
	}
	press(t, a, "ctrl+r")
	if a.Rows[1][0] != "bob" || len(a.Undo) != 1 {
		t.Fatalf("redo: cell=%q undo=%d", a.Rows[1][0], len(a.Undo))
	}
}

func TestDeleteRowAsksForConfirmation(t *testing.T) {
	a := open(t)
	press(t, a, "j", "d", "d")
	if len(a.r.Modals()) != 1 || a.r.Focus != "confirm" {
		t.Fatalf("confirm not open: frames=%d focus=%s", len(a.r.Modals()), a.r.Focus)
	}
	if view := ansi.Strip(a.m.View().Content); !strings.Contains(view, "Delete row r1?") {
		t.Fatalf("confirm prompt missing:\n%s", view)
	}
	press(t, a, "n")
	if len(a.r.Modals()) != 0 || len(a.Rows) != 5 {
		t.Fatalf("n: frames=%d rows=%d", len(a.r.Modals()), len(a.Rows))
	}
	press(t, a, "d", "d", "y")
	if len(a.Rows) != 4 || a.Rows[1][0] != "cy" || a.r.Focus != "r1:c0" {
		t.Fatalf("y: rows=%d row1=%v focus=%s", len(a.Rows), a.Rows[1], a.r.Focus)
	}
	press(t, a, "u")
	if len(a.Rows) != 5 || a.Rows[1][0] != "bo" {
		t.Fatalf("undo delete: rows=%d row1=%v", len(a.Rows), a.Rows[1])
	}
	// A script skips the prompt with Confirmed.
	if _, err := a.r.Execute(ochazuke.Request{Verb: "delete", Noun: "row", Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	if len(a.Rows) != 4 || len(a.r.Modals()) != 0 {
		t.Fatalf("confirmed delete: rows=%d frames=%d", len(a.Rows), len(a.r.Modals()))
	}
}

func TestVisualColumnYankAndPaste(t *testing.T) {
	a := open(t)
	press(t, a, "l", "ctrl+v")
	if mode := a.r.Mode.Name; mode != grid.ShapeColumn {
		t.Fatalf("mode = %q", mode)
	}
	press(t, a, "l")
	if sel := a.grid.Selected(); len(sel) != 10 {
		t.Fatalf("selected %d cells, want 10: %v", len(sel), sel)
	}
	press(t, a, "y")
	y, err := a.r.ReadYank("")
	if err != nil {
		t.Fatal(err)
	}
	if y.Shape != "column" || !strings.HasPrefix(y.Text, "email\trole\na@x.io\tdev\n") {
		t.Fatalf("yank = %+v", y)
	}
	if mode := a.r.Mode.Name; mode != "normal" {
		t.Fatalf("mode after yank = %q", mode)
	}
	press(t, a, "0", "p")
	if !reflect.DeepEqual(a.Header, []string{"name", "email", "role", "email", "role"}) {
		t.Fatalf("header after paste = %v", a.Header)
	}
	if !reflect.DeepEqual(a.Rows[1], []string{"bo", "b@x.io", "ops", "b@x.io", "ops"}) {
		t.Fatalf("row 1 after paste = %v", a.Rows[1])
	}
	press(t, a, "u")
	if len(a.Header) != 3 || len(a.Rows[1]) != 3 {
		t.Fatalf("undo paste: header=%v row1=%v", a.Header, a.Rows[1])
	}
	// Row yank pastes rows below; the editor's registers keep the text.
	press(t, a, "V", "j", "y", "G", "p")
	if len(a.Rows) != 7 || a.Rows[5][0] != "ann" || a.Rows[6][0] != "bo" {
		t.Fatalf("row paste: rows=%d tail=%v %v", len(a.Rows), a.Rows[5], a.Rows[6])
	}
	if regs := a.r.Registers(); regs.Named["0"].Yank.Shape != "line" || len(regs.History) != 2 {
		t.Fatalf("registers: %+v history=%d", regs.Named["0"], len(regs.History))
	}
}

func TestTransformUpperOnBlockPlusMark(t *testing.T) {
	a := open(t)
	// Range r1..r2 in column 0 plus a mark on r3: the mark sits outside the
	// range because the anchor was swapped away from it.
	press(t, a, "3", "j", "v", "m", "k", "o", "k", "k")
	if got := a.grid.Marks(); !reflect.DeepEqual(got, []grid.Ref{{Row: 3, Col: 0}}) {
		t.Fatalf("marks = %v", got)
	}
	want := []grid.Ref{{Row: 1, Col: 0}, {Row: 2, Col: 0}, {Row: 3, Col: 0}}
	if got := a.grid.Selected(); !reflect.DeepEqual(got, want) {
		t.Fatalf("selected = %v, want %v", got, want)
	}
	press(t, a, "t", "u")
	if a.Rows[0][0] != "ann" || a.Rows[1][0] != "BO" || a.Rows[2][0] != "CY" || a.Rows[3][0] != "DEE" || a.Rows[4][0] != "  eve " {
		t.Fatalf("after upper: %v", a.Rows)
	}
	if a.grid.Shape() != "" || len(a.grid.Marks()) != 0 || len(a.Undo) != 1 || len(a.Undo[0].Cells) != 3 {
		t.Fatalf("after transform: shape=%q marks=%d undo=%+v", a.grid.Shape(), len(a.grid.Marks()), a.Undo)
	}
	press(t, a, "u")
	if a.Rows[1][0] != "bo" || a.Rows[3][0] != "dee" {
		t.Fatalf("undo transform: %v", a.Rows)
	}
	// The same verb through Execute with a sheet transformation and through
	// the column noun.
	press(t, a, "G", "t", "t")
	if a.Rows[4][0] != "eve" {
		t.Fatalf("trim: %q", a.Rows[4][0])
	}
	if _, err := a.r.Execute(ochazuke.Request{Verb: "transform", Noun: "column", Args: ochazuke.Arguments{"name": "upper"}}); err != nil {
		t.Fatal(err)
	}
	if a.Rows[0][0] != "ANN" || a.Rows[4][0] != "EVE" || a.Rows[0][1] != "a@x.io" {
		t.Fatalf("column upper: %v", a.Rows)
	}
	press(t, a, "l", "g", "g", "V", "G", "f")
	for i, row := range a.Rows {
		if row[1] != "a@x.io" {
			t.Fatalf("fillDown row %d = %v", i, row)
		}
	}
	names, err := a.r.Execute(ochazuke.Request{Verb: "list", Noun: "transformations"})
	choices, _ := ochazuke.ParseChoices(names)
	var ids []string
	for _, c := range choices {
		ids = append(ids, c.ID)
		if c.Label == c.ID {
			t.Fatalf("choice %s has no label", c.ID)
		}
	}
	if err != nil || !reflect.DeepEqual(ids, []string{"lower", "slug", "title", "trim", "upper", "fillDown"}) {
		t.Fatalf("transformations.list = %v, %v", names, err)
	}
	if _, err := a.r.Execute(ochazuke.Request{Verb: "transform", Noun: "selection", Args: ochazuke.Arguments{"name": "nope"}}); err == nil {
		t.Fatal("unknown transformation accepted")
	}
}

func TestVisualBlockDeleteClearsAndRowDeleteRemoves(t *testing.T) {
	a := open(t)
	press(t, a, "v", "j", "l", "d")
	if a.Rows[0][0] != "" || a.Rows[1][1] != "" || a.Rows[0][2] != "dev" || len(a.Rows) != 5 {
		t.Fatalf("block delete: %v", a.Rows)
	}
	if y, _ := a.r.ReadYank(""); y.Shape != "block" || y.Text != "ann\ta@x.io\nbo\tb@x.io" || y.Width != 2 {
		t.Fatalf("block yank = %+v", y)
	}
	press(t, a, "u", "g", "g", "0", "V", "j", "d")
	if len(a.Rows) != 3 || a.Rows[0][0] != "cy" {
		t.Fatalf("row delete: %v", a.Rows)
	}
	press(t, a, "ctrl+v", "d")
	if !reflect.DeepEqual(a.Header, []string{"email", "role"}) || len(a.Rows[0]) != 2 {
		t.Fatalf("column delete: %v %v", a.Header, a.Rows[0])
	}
	press(t, a, "u", "u")
	if len(a.Rows) != 5 || len(a.Header) != 3 || a.Rows[0][0] != "ann" {
		t.Fatalf("undo twice: %v %v", a.Header, a.Rows)
	}
}

func TestColumnVerbsAndSave(t *testing.T) {
	a := open(t)
	press(t, a, "l", "l", "r")
	keytest.Type(a.m, "s")
	press(t, a, "enter")
	if a.Header[2] != "roles" || len(a.Undo) != 1 {
		t.Fatalf("rename: %v", a.Header)
	}
	press(t, a, "/")
	keytest.Type(a.m, "dev")
	press(t, a, "enter")
	if a.grid.VisibleCount() != 3 || a.Dirty != true || len(a.Undo) != 1 {
		t.Fatalf("filter: visible=%d", a.grid.VisibleCount())
	}
	press(t, a, "G")
	cursor(t, a, grid.Ref{Row: 4, Col: 2})
	if _, err := a.r.Execute(ochazuke.Request{Verb: "filter", Noun: "column", Args: ochazuke.Arguments{"query": ""}}); err != nil {
		t.Fatal(err)
	}
	press(t, a, "s")
	if a.Rows[0][2] != "dev" || a.Rows[4][2] != "qa" || a.Rows[0][0] != "ann" {
		t.Fatalf("sort: %v", a.Rows)
	}
	press(t, a, "D")
	if a.r.Focus != "confirm" {
		t.Fatal("column delete did not ask")
	}
	press(t, a, "y")
	if len(a.Header) != 2 {
		t.Fatalf("column delete: %v", a.Header)
	}
	press(t, a, "+")
	keytest.Type(a.m, "team")
	press(t, a, "enter")
	if !reflect.DeepEqual(a.Header, []string{"name", "email", "team"}) || a.Rows[0][2] != "" {
		t.Fatalf("addColumn: %v %v", a.Header, a.Rows[0])
	}
	// The cursor stayed on its row through the filter, the sort and the
	// column change: dee, the last row after sorting.
	cursor(t, a, grid.Ref{Row: 4, Col: 1})
	press(t, a, "o")
	if len(a.Rows) != 6 || a.Rows[5][0] != "" || a.r.Focus != "r5:c1" {
		t.Fatalf("insertBelow: rows=%d focus=%s", len(a.Rows), a.r.Focus)
	}
	press(t, a, "k", "y", "y")
	if len(a.Rows) != 7 || a.Rows[4][0] != "dee" || a.Rows[5][0] != "dee" {
		t.Fatalf("duplicate: %v", a.Rows)
	}
	out := filepath.Join(t.TempDir(), "out.csv")
	if _, err := a.r.Execute(ochazuke.Request{Verb: "saveAs", Noun: "sheet", Args: ochazuke.Arguments{"path": out}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "name,email,team\nann,a@x.io,\ncy,c@x.io,\n\"  eve \",e@x.io,\n") || a.Dirty || a.Path != out {
		t.Fatalf("saved:\n%s\ndirty=%v path=%s", data, a.Dirty, a.Path)
	}
	// Save-as through the prompt: ctrl+o inside opens the picker on the field.
	press(t, a, "ctrl+s")
	if a.r.Focus != "" && len(a.r.Modals()) != 0 {
		t.Fatalf("save with a path opened a prompt: frames=%d", len(a.r.Modals()))
	}
	if _, err := a.r.Execute(ochazuke.Request{Verb: "saveAs", Noun: "sheet"}); err != nil {
		t.Fatal(err)
	}
	if a.r.Focus != fieldID || string(a.field.Text) != out {
		t.Fatalf("save-as prompt: focus=%s text=%q", a.r.Focus, string(a.field.Text))
	}
	press(t, a, "ctrl+o")
	if a.r.TopModal() != "picker" {
		t.Fatalf("ctrl+o did not open the picker: %v", a.r.TopModal())
	}
	press(t, a, "esc", "esc")
	if len(a.r.Modals()) != 0 || a.ask != nil {
		t.Fatalf("esc twice: frames=%d ask=%v", len(a.r.Modals()), a.ask)
	}
}

func TestPaletteListsTheNounVerbs(t *testing.T) {
	a := open(t)
	verbs := map[string][]string{}
	for _, op := range a.r.Catalog() {
		noun := a.r.DisplayNoun(op.Noun) // the grid's selection is ochazuke.selection
		verbs[noun] = append(verbs[noun], op.Verb)
	}
	for noun, want := range map[string][]string{
		"column":    {"delete", "filter", "rename", "sort", "transform"},
		"row":       {"delete", "duplicate", "insertBelow", "transform"},
		"cell":      {"clear", "edit", "paste", "transform", "yank"},
		"selection": {"all", "clear", "delete", "mark", "paste", "swap", "transform", "unmark", "yank"},
		"sheet":     {"addColumn", "filter", "open", "redo", "save", "saveAs", "sort", "undo"},
	} {
		if got := verbs[noun]; !reflect.DeepEqual(got, want) {
			t.Fatalf("%s verbs = %v, want %v", noun, got, want)
		}
	}
	if op := a.r.Operations["row.delete"]; op.Confirm == "" {
		t.Fatal("row.delete has no confirm")
	}
	// Hints are the bound rows of the mode: a hint never names an unbound key.
	hints := ochazuke.BindingHints(a.r, "r0:c0")
	for _, want := range []string{"d d row.delete", "t u cell.transform", "ctrl+s sheet.save", "v mode.set"} {
		if !strings.Contains(hints, want) {
			t.Fatalf("hints lack %q: %s", want, hints)
		}
	}
	press(t, a, ":")
	keytest.Type(a.m, "transform")
	view := ansi.Strip(a.m.View().Content)
	if !a.m.Palette.IsActive() || !strings.Contains(view, "transform") {
		t.Fatalf("palette:\n%s", view)
	}
	// The list completes the next word: the nouns that have transform.
	keytest.Type(a.m, " ")
	view = ansi.Strip(a.m.View().Content)
	for _, noun := range []string{"cell", "row", "column"} {
		if !strings.Contains(view, "  "+noun) {
			t.Fatalf("transform nouns missing %s:\n%s", noun, view)
		}
	}
	if strings.Contains(view, "  selection") {
		t.Fatalf("selection offered without a selection:\n%s", view)
	}
}

func TestEmptySheetStartsOnTheGrid(t *testing.T) {
	a, err := New("")
	if err != nil {
		t.Fatal(err)
	}
	a.m.Width, a.m.Height = 60, 16
	if a.r.Focus != "grid" {
		t.Fatalf("focus = %s", a.r.Focus)
	}
	// No cell instance: the cell, row and column rows are not on the stack.
	keytest.Press(a.m, "j")
	keytest.Press(a.m, "i")
	if a.m.Status != "" || len(a.r.Modals()) != 0 {
		t.Fatalf("keys on an empty sheet: status=%q frames=%d", a.m.Status, len(a.r.Modals()))
	}
	if _, err := a.r.Execute(ochazuke.Request{Verb: "edit", Noun: "cell"}); err == nil {
		t.Fatal("editing an empty sheet reported no error")
	}
	if _, err := a.r.Execute(ochazuke.Request{Verb: "addColumn", Noun: "sheet", Args: ochazuke.Arguments{"name": "a"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.r.Execute(ochazuke.Request{Verb: "insertBelow", Noun: "row"}); err != nil {
		t.Fatal(err)
	}
	if a.r.Focus != "r0:c0" || len(a.Rows) != 1 {
		t.Fatalf("after first row: focus=%s rows=%d", a.r.Focus, len(a.Rows))
	}
	if view := ansi.Strip(a.m.View().Content); !strings.Contains(view, "(untitled) [+]") {
		t.Fatalf("view:\n%s", view)
	}
}

// Every row the CSV app and the grid install, on their nouns and on the
// cell instances, is a Request to a registered operation or an unbind; see
// TestBindingRowsAreAtomic in the framework package.
func TestCSVBindingRowsAreAtomic(t *testing.T) {
	if _, err := open(t).r.ValidateBindings(); err != nil {
		t.Fatal(err)
	}
}

// Every noun that resolves to a cell set has transform and the same t rows:
// cell (the cell), row (every cell of the row), column (every visible cell),
// selection (the selected cells). t i and t s run title and slug.
func TestTransformOnEveryCellSetNoun(t *testing.T) {
	a := open(t)
	for _, noun := range []string{"cell", "row", "column", "selection"} {
		if _, ok := a.r.Operation(noun + ".transform"); !ok {
			t.Fatalf("%s has no transform", noun)
		}
		if op, _ := a.r.Operation(noun + ".transform"); op.Parameters["name"].Source != "transformations.list" {
			t.Fatalf("%s.transform name source = %q", noun, op.Parameters["name"].Source)
		}
	}
	for _, focus := range []string{"r0:c0", "r0"} {
		hints := ochazuke.BindingHints(a.r, focus)
		noun := map[string]string{"r0:c0": "cell", "r0": "row"}[focus]
		for _, keys := range []string{"t u", "t l", "t t", "t i", "t s"} {
			if !strings.Contains(hints, keys+" "+noun+".transform") {
				t.Fatalf("%s lacks %q %s.transform: %s", focus, keys, noun, hints)
			}
		}
	}
	press(t, a, "j")
	if _, err := a.r.Execute(ochazuke.Request{Verb: "transform", Noun: "row", Target: "r1", Args: ochazuke.Arguments{"name": "upper"}}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a.Rows[1], []string{"BO", "B@X.IO", "OPS"}) || a.Rows[0][0] != "ann" || len(a.Undo[0].Cells) != 3 {
		t.Fatalf("row upper: %v undo=%+v", a.Rows[1], a.Undo)
	}
	press(t, a, "G")
	if err := a.r.SetFocus("r4:c0"); err != nil {
		t.Fatal(err)
	}
	a.Rows[4][0] = "  eve ADAMS-o'neil "
	press(t, a, "t", "i")
	if a.Rows[4][0] != "Eve Adams-o'neil" {
		t.Fatalf("title: %q", a.Rows[4][0])
	}
	press(t, a, "t", "s")
	if a.Rows[4][0] != "eve-adams-o-neil" {
		t.Fatalf("slug: %q", a.Rows[4][0])
	}
	if _, err := a.r.Execute(ochazuke.Request{Verb: "transform", Noun: "cell", Args: ochazuke.Arguments{"name": "nope"}}); err == nil || !strings.Contains(err.Error(), "must be one of") {
		t.Fatalf("a name outside the source accepted: %v", err)
	}
}

// The transform name completes from transformations.list: ctrl+n moves a
// highlight onto the list, typing narrows it, enter on the highlight
// appends the word and enter on the complete line runs it.
func TestTransformNameCompletes(t *testing.T) {
	a := open(t)
	press(t, a, ":")
	keytest.Type(a.m, "transform cell ")
	view := ansi.Strip(a.m.View().Content)
	for _, want := range []string{"name · string · required · from transformations.list", "upper  UPPER CASE", "slug  slug-case", "title  Title Case"} {
		if !strings.Contains(view, want) {
			t.Fatalf("completions lack %q:\n%s", want, view)
		}
	}
	keytest.Type(a.m, "u")
	press(t, a, "ctrl+n", "enter")
	if got := a.m.Palette.Text(); got != "transform cell upper " {
		t.Fatalf("enter on the highlight: %q", got)
	}
	press(t, a, "enter")
	if a.m.Palette.IsOpen() || a.Rows[0][0] != "ANN" || a.r.Focus != "r0:c0" {
		t.Fatalf("executed: open=%v cell=%q focus=%s", a.m.Palette.IsOpen(), a.Rows[0][0], a.r.Focus)
	}
}

// paletteLine types a command line after : and runs it with enter.
func paletteLine(t *testing.T, a *App, line string) {
	t.Helper()
	press(t, a, ":")
	keytest.Type(a.m, line)
	press(t, a, "enter")
	if a.m.Palette.IsOpen() {
		t.Fatalf("%q left the palette open", line)
	}
}

// A line without a noun transforms the cell; with row, column or
// selection, the instance of that noun on the focus path; the cursor
// never moves.
func TestTransformTargetsFromTheCommandLine(t *testing.T) {
	a := open(t)
	press(t, a, "j")
	paletteLine(t, a, "transform upper")
	if a.Rows[1][0] != "BO" || a.Rows[1][1] != "b@x.io" || a.Rows[0][0] != "ann" {
		t.Fatalf("cell: %v", a.Rows[:2])
	}
	paletteLine(t, a, "tr ro up")
	if !reflect.DeepEqual(a.Rows[1], []string{"BO", "B@X.IO", "OPS"}) || a.Rows[2][0] != "cy" {
		t.Fatalf("row: %v", a.Rows[:3])
	}
	press(t, a, "l")
	paletteLine(t, a, "column transform upper")
	if a.Rows[0][1] != "A@X.IO" || a.Rows[4][1] != "E@X.IO" || a.Rows[0][2] != "dev" {
		t.Fatalf("column: %v", a.Rows)
	}
	cursor(t, a, grid.Ref{Row: 1, Col: 1})
	press(t, a, "l", "v", "j")
	paletteLine(t, a, "transform selection upper")
	if a.Rows[1][2] != "OPS" || a.Rows[2][2] != "DEV" || a.Rows[0][2] != "dev" || a.Rows[3][2] != "qa" {
		t.Fatalf("selection: %v", a.Rows)
	}
	cursor(t, a, grid.Ref{Row: 2, Col: 2})
	// The selection ended with the transform: selection is not a noun now.
	press(t, a, ":")
	keytest.Type(a.m, "transform selection upper")
	keytest.Press(a.m, "enter")
	if !a.m.Palette.IsOpen() || a.m.Status != "selection.transform is not applicable now" {
		t.Fatalf("selection transform without a selection: open=%v status=%q", a.m.Palette.IsOpen(), a.m.Status)
	}
	keytest.Press(a.m, "ctrl+c")
}

// paletteFinds reports whether the palette shows line after query is typed
// as its first word, opening it with ctrl+space (: is text in the filter
// field) and closing it.
func paletteFinds(t *testing.T, a *App, query, line string) bool {
	t.Helper()
	press(t, a, "ctrl+space")
	keytest.Type(a.m, query)
	view := ansi.Strip(a.m.View().Content)
	a.m.Palette.Close()
	return strings.Contains(view, line)
}

// Guarded verbs: selection verbs are offered only during a visual
// selection, row and column verbs only while the focus is in the grid.
// A modal opened over a selection (the palette) keeps it.
func TestPaletteOffersGuardedVerbsOnlyWhenTheyApply(t *testing.T) {
	a := open(t)
	if paletteFinds(t, a, "selection", "selection") && !paletteFinds(t, a, "selection", "No completions") {
		t.Fatal("selection verbs offered without a selection")
	}
	if !paletteFinds(t, a, "insertBelow", "insertBelow") || !paletteFinds(t, a, "rename", "rename") {
		t.Fatal("row or column verbs hidden on a cell")
	}
	press(t, a, "}")
	if a.r.Focus != filterID {
		t.Fatalf("focus = %s", a.r.Focus)
	}
	if !paletteFinds(t, a, "insertBelow", "No completions") || !paletteFinds(t, a, "rename", "No completions") {
		t.Fatal("row or column verbs offered in the filter field")
	}
	if paletteFinds(t, a, "addColumn", "No completions") {
		t.Fatal("an unguarded sheet verb was hidden")
	}
	press(t, a, "tab", "v", "j")
	if !paletteFinds(t, a, "selection", "selection") || paletteFinds(t, a, "selection", "No completions") {
		t.Fatal("selection verbs hidden during a selection")
	}
	// : inside the selection opens the palette, and the verb it runs sees
	// the selection the palette opened over.
	if a.grid.Shape() != grid.ShapeBlock || len(a.grid.Selected()) != 2 {
		t.Fatalf("palette dropped the selection: shape=%q sel=%v", a.grid.Shape(), a.grid.Selected())
	}
	press(t, a, ":")
	keytest.Type(a.m, "transform selection upper")
	press(t, a, "enter")
	if a.Rows[0][0] != "ANN" || a.Rows[1][0] != "BO" || a.Rows[2][0] != "cy" || a.grid.Shape() != "" {
		t.Fatalf("selection transform from the palette: %v shape=%q", a.Rows[:3], a.grid.Shape())
	}
}

// The filter field is the sheet's second focus stop: } and tab land on it
// in insert, enter filters by the cursor column and returns to the cell,
// shift+tab and esc return without filtering, / focuses it with the
// current query, and a query set by script shows in it.
func TestFilterFieldIsASecondFocusStop(t *testing.T) {
	a := open(t)
	press(t, a, "l", "l", "j", "}")
	if a.r.Focus != filterID || a.r.Mode.Name != "insert" {
		t.Fatalf("} : focus=%s mode=%s", a.r.Focus, a.r.Mode.Name)
	}
	if view := ansi.Strip(a.m.View().Content); !strings.Contains(view, "filter role: ") {
		t.Fatalf("filter line missing:\n%s", view)
	}
	keytest.Type(a.m, "dev")
	press(t, a, "shift+tab")
	if a.r.Focus != "r1:c2" || a.grid.VisibleCount() != 5 {
		t.Fatalf("shift+tab: focus=%s visible=%d", a.r.Focus, a.grid.VisibleCount())
	}
	press(t, a, "{")
	if a.r.Focus != filterID || string(a.filterIn.Text) != "dev" {
		t.Fatalf("{ : focus=%s text=%q", a.r.Focus, string(a.filterIn.Text))
	}
	press(t, a, "enter")
	if a.grid.VisibleCount() != 3 || a.Filter.Col != 2 || a.Filter.Query != "dev" || a.r.Mode.Name != "normal" {
		t.Fatalf("enter: visible=%d filter=%+v mode=%s", a.grid.VisibleCount(), a.Filter, a.r.Mode.Name)
	}
	// The cursor's row r1 (ops) is hidden: the cursor moved to a shown row.
	if !strings.HasSuffix(a.r.Focus, ":c2") {
		t.Fatalf("enter returned to %s", a.r.Focus)
	}
	press(t, a, "/")
	if a.r.Focus != filterID || a.filterIn.Cursor != 3 {
		t.Fatalf("/ : focus=%s cursor=%d", a.r.Focus, a.filterIn.Cursor)
	}
	press(t, a, "esc")
	if !strings.HasSuffix(a.r.Focus, ":c2") {
		t.Fatalf("esc returned to %s", a.r.Focus)
	}
	if _, err := a.r.Execute(ochazuke.Request{Verb: "filter", Noun: "column", Args: ochazuke.Arguments{"query": ""}}); err != nil {
		t.Fatal(err)
	}
	if string(a.filterIn.Text) != "" || a.grid.VisibleCount() != 5 {
		t.Fatalf("cleared: text=%q visible=%d", string(a.filterIn.Text), a.grid.VisibleCount())
	}
}

// The column delete confirm names the column and its header; the D row on
// the column instance carries the header, so a rename shows the new name.
func TestDeleteColumnConfirmNamesTheHeader(t *testing.T) {
	a := open(t)
	press(t, a, "l", "D")
	if view := ansi.Strip(a.m.View().Content); !strings.Contains(view, "Delete column c1 (email)?") {
		t.Fatalf("confirm prompt:\n%s", view)
	}
	press(t, a, "n", "r")
	keytest.Type(a.m, "2")
	press(t, a, "enter", "D")
	if view := ansi.Strip(a.m.View().Content); !strings.Contains(view, "Delete column c1 (email2)?") {
		t.Fatalf("confirm after rename:\n%s", view)
	}
	press(t, a, "y")
	if !reflect.DeepEqual(a.Header, []string{"name", "role"}) {
		t.Fatalf("header = %v", a.Header)
	}
	if _, err := a.r.Execute(ochazuke.Request{Verb: "delete", Noun: "column", Confirmed: true, Args: ochazuke.Arguments{"name": "email"}}); err == nil {
		t.Fatal("a delete naming another header ran")
	}
}

// Under the palette the grid keeps its cursor cell and its selection,
// dimmed by the backdrop: a snapshot of the backdrop's left edge in the
// mono theme (faint, the selected cell underlined, the cursor cell
// reversed). Rendering changes no state: the focus stays on the line.
func TestGridHighlightsStayVisibleUnderThePalette(t *testing.T) {
	a := open(t)
	if _, err := a.r.Execute(ochazuke.Request{Verb: "set", Noun: "appearance", Args: ochazuke.Arguments{"theme": "mono"}}); err != nil {
		t.Fatal(err)
	}
	press(t, a, "v", "j", ":")
	lines := strings.Split(a.m.View().Content, "\n")
	var got []string
	for _, line := range lines[5:7] {
		got = append(got, line[:strings.Index(line, "│")])
	}
	// The palette's list names each verb's aliases (apply · also se,
	// setg, …), so its box is wide and starts near the left edge.
	want := []string{"\x1b[2m   1 \x1b[4mann\x1b[m", "\x1b[2m   2 \x1b[7mbo \x1b[m"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("backdrop rows = %q, want %q", got, want)
	}
	if a.r.Focus != "__palette/line" || a.r.Mode.Name != "insert" {
		t.Fatalf("rendering moved the focus: %s %s", a.r.Focus, a.r.Mode.Name)
	}
}
