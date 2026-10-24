package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"ochazuke"
	"ochazuke/grid"
	"ochazuke/internal/keytest"
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
		t.Fatalf("cursor = %v, want %v (focus %s)", got, want, a.r.FocusID())
	}
}

func TestOpenAndNavigateWithCounts(t *testing.T) {
	a := open(t)
	if !reflect.DeepEqual(a.Header, []string{"name", "email", "role"}) || len(a.Rows) != 5 {
		t.Fatalf("loaded header=%v rows=%d", a.Header, len(a.Rows))
	}
	if a.r.FocusID() != "r0:c0" || a.r.Page != "sheet" {
		t.Fatalf("start focus=%s page=%s", a.r.FocusID(), a.r.Page)
	}
	if got := a.r.InstancePath("r0:c0"); !reflect.DeepEqual(got, []string{"app", "sheet", "grid", "c0", "r0", "r0:c0"}) {
		t.Fatalf("cell path = %v", got)
	}
	press(t, a, "3", "j")
	cursor(t, a, grid.Ref{Row: 3, Col: 0})
	if a.r.FocusID() != "r3:c0" || a.r.Instances["r0:c0"] != nil {
		t.Fatalf("focus=%s, old cell kept=%v", a.r.FocusID(), a.r.Instances["r0:c0"] != nil)
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
	if mode := a.r.Mode(a.r.Context()); mode != "normal" {
		t.Fatalf("mode after motions = %q", mode)
	}
	view := ansi.Strip(a.m.View().Content)
	for _, want := range []string{"people.csv", "5 rows × 3 columns", "r0:c2", " 1 ann", "i edit.cell"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view lacks %q:\n%s", want, view)
		}
	}
}

func TestEditCellThroughPrompt(t *testing.T) {
	a := open(t)
	press(t, a, "j", "enter")
	if len(a.r.Frames) != 2 || a.r.FocusID() != fieldID || string(a.field.Text) != "bo" {
		t.Fatalf("prompt: frames=%d focus=%s text=%q", len(a.r.Frames), a.r.FocusID(), string(a.field.Text))
	}
	if mode := a.r.Mode(a.r.Context()); mode != "insert" {
		t.Fatalf("prompt mode = %q", mode)
	}
	if view := ansi.Strip(a.m.View().Content); !strings.Contains(view, "Edit r1:c0") {
		t.Fatalf("modal title missing:\n%s", view)
	}
	keytest.Type(a.m, "b")
	press(t, a, "enter")
	if len(a.r.Frames) != 1 || a.Rows[1][0] != "bob" || !a.Dirty || len(a.Undo) != 1 {
		t.Fatalf("commit: frames=%d cell=%q dirty=%v undo=%d", len(a.r.Frames), a.Rows[1][0], a.Dirty, len(a.Undo))
	}
	if a.r.FocusID() != "r1:c0" {
		t.Fatalf("focus after commit = %s", a.r.FocusID())
	}
	press(t, a, "i")
	keytest.Type(a.m, "zzz")
	press(t, a, "esc")
	if len(a.r.Frames) != 1 || a.Rows[1][0] != "bob" || len(a.Undo) != 1 {
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
	if len(a.r.Frames) != 2 || a.r.FocusID() != "confirm" {
		t.Fatalf("confirm not open: frames=%d focus=%s", len(a.r.Frames), a.r.FocusID())
	}
	if view := ansi.Strip(a.m.View().Content); !strings.Contains(view, "Delete row r1:c0?") {
		t.Fatalf("confirm prompt missing:\n%s", view)
	}
	press(t, a, "n")
	if len(a.r.Frames) != 1 || len(a.Rows) != 5 {
		t.Fatalf("n: frames=%d rows=%d", len(a.r.Frames), len(a.Rows))
	}
	press(t, a, "d", "d", "y")
	if len(a.Rows) != 4 || a.Rows[1][0] != "cy" || a.r.FocusID() != "r1:c0" {
		t.Fatalf("y: rows=%d row1=%v focus=%s", len(a.Rows), a.Rows[1], a.r.FocusID())
	}
	press(t, a, "u")
	if len(a.Rows) != 5 || a.Rows[1][0] != "bo" {
		t.Fatalf("undo delete: rows=%d row1=%v", len(a.Rows), a.Rows[1])
	}
	// A script skips the prompt with Confirmed.
	if _, err := a.r.Execute(ochazuke.Request{Verb: "delete", Noun: "row", Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	if len(a.Rows) != 4 || len(a.r.Frames) != 1 {
		t.Fatalf("confirmed delete: rows=%d frames=%d", len(a.Rows), len(a.r.Frames))
	}
}

func TestVisualColumnYankAndPaste(t *testing.T) {
	a := open(t)
	press(t, a, "l", "ctrl+v")
	if mode := a.r.Mode(a.r.Context()); mode != grid.ShapeColumn {
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
	if mode := a.r.Mode(a.r.Context()); mode != "normal" {
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
	if err != nil || !reflect.DeepEqual(names, []string{"lower", "slug", "title", "trim", "upper", "fillDown"}) {
		t.Fatalf("list.transformations = %v, %v", names, err)
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
	if a.r.FocusID() != "confirm" {
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
	if len(a.Rows) != 6 || a.Rows[5][0] != "" || a.r.FocusID() != "r5:c1" {
		t.Fatalf("insertBelow: rows=%d focus=%s", len(a.Rows), a.r.FocusID())
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
	if a.r.FocusID() != "" && len(a.r.Frames) != 1 {
		t.Fatalf("save with a path opened a prompt: frames=%d", len(a.r.Frames))
	}
	if _, err := a.r.Execute(ochazuke.Request{Verb: "saveAs", Noun: "sheet"}); err != nil {
		t.Fatal(err)
	}
	if a.r.FocusID() != fieldID || string(a.field.Text) != out {
		t.Fatalf("save-as prompt: focus=%s text=%q", a.r.FocusID(), string(a.field.Text))
	}
	press(t, a, "ctrl+o")
	if a.r.Frames[len(a.r.Frames)-1].ID != "picker" {
		t.Fatalf("ctrl+o did not open the picker: %v", a.r.Frames[len(a.r.Frames)-1])
	}
	press(t, a, "esc", "esc")
	if len(a.r.Frames) != 1 || a.ask != nil {
		t.Fatalf("esc twice: frames=%d ask=%v", len(a.r.Frames), a.ask)
	}
}

func TestPaletteListsTheNounVerbs(t *testing.T) {
	a := open(t)
	verbs := map[string][]string{}
	for _, op := range a.r.Catalog() {
		verbs[op.Noun] = append(verbs[op.Noun], op.Verb)
	}
	for noun, want := range map[string][]string{
		"column":    {"delete", "filter", "rename", "sort", "transform"},
		"row":       {"delete", "duplicate", "insertBelow"},
		"cell":      {"clear", "edit", "paste", "transform", "yank"},
		"selection": {"all", "cancel", "clear", "delete", "mark", "paste", "shape", "swap", "transform", "unmark", "yank"},
		"sheet":     {"addColumn", "open", "redo", "save", "saveAs", "sort", "undo"},
	} {
		if got := verbs[noun]; !reflect.DeepEqual(got, want) {
			t.Fatalf("%s verbs = %v, want %v", noun, got, want)
		}
	}
	if op := a.r.Operations["delete.row"]; op.Confirm == "" {
		t.Fatal("delete.row has no confirm")
	}
	press(t, a, ":")
	keytest.Type(a.m, "transform")
	view := ansi.Strip(a.m.View().Content)
	if !a.m.Palette.IsActive() || !strings.Contains(view, "transform") {
		t.Fatalf("palette:\n%s", view)
	}
	press(t, a, "enter")
	view = ansi.Strip(a.m.View().Content)
	for _, noun := range []string{"cell", "column", "selection"} {
		if !strings.Contains(view, noun) {
			t.Fatalf("transform nouns missing %s:\n%s", noun, view)
		}
	}
	// Hints are the bound rows: a hint never names an unbound key.
	hints := ochazuke.BindingHints(a.r, "r0:c0")
	for _, want := range []string{"d d delete.row", "t u transform.selection", "ctrl+s save.sheet", "v shape.selection"} {
		if !strings.Contains(hints, want) {
			t.Fatalf("hints lack %q: %s", want, hints)
		}
	}
}

func TestEmptySheetStartsOnTheGrid(t *testing.T) {
	a, err := New("")
	if err != nil {
		t.Fatal(err)
	}
	a.m.Width, a.m.Height = 60, 16
	if a.r.FocusID() != "grid" {
		t.Fatalf("focus = %s", a.r.FocusID())
	}
	// No cell instance: the cell, row and column rows are not on the stack.
	keytest.Press(a.m, "j")
	keytest.Press(a.m, "i")
	if a.m.Status != "" || len(a.r.Frames) != 1 {
		t.Fatalf("keys on an empty sheet: status=%q frames=%d", a.m.Status, len(a.r.Frames))
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
	if a.r.FocusID() != "r0:c0" || len(a.Rows) != 1 {
		t.Fatalf("after first row: focus=%s rows=%d", a.r.FocusID(), len(a.Rows))
	}
	if view := ansi.Strip(a.m.View().Content); !strings.Contains(view, "(untitled) [+]") {
		t.Fatalf("view:\n%s", view)
	}
}
