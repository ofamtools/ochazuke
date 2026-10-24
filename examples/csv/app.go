// Package main is the CSV reader and editor from docs/CSV-APP-SKETCH.md: the
// framework's lightweight second consumer. One file in memory, file I/O on
// open and save only, nothing off the loop. The application owns the sheet
// (path, header, rows, dirty flag, undo and redo stacks of patches); the
// framework owns focus, modes, modals, the editor text and the registers;
// the grid component owns the cursor, viewport and selection.
package main

import (
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/unixsuperhero/ochazuke"
	"github.com/unixsuperhero/ochazuke/grid"
)

// App is the state the application owns. Domain data never lives on an
// instance: instances carry only ids (r12, c2, r12:c2).
type App struct {
	Path   string
	Header []string
	Rows   [][]string
	Dirty  bool
	Undo   []Patch
	Redo   []Patch
	Filter FilterState

	r     *ochazuke.Runtime
	m     *ochazuke.Model
	grid  *grid.Grid
	field *ochazuke.Input
	// filterIn is the filter field below the grid (filter.go).
	filterIn *ochazuke.Input
	ask      *question
}

// FilterState is the row filter the sheet shows through: view state, not an edit.
type FilterState struct {
	Col   int
	Query string
}

// A Patch is the one edit shape: every change to the sheet is a patch, and
// the same patch, inverted, is its undo record. Cells replace values; Rows
// and Columns insert (Before nil), delete (After nil) or replace by index.
type Patch struct {
	Cells   []CellChange   `json:"cells,omitempty"`
	Rows    []RowChange    `json:"rows,omitempty"`
	Columns []ColumnChange `json:"columns,omitempty"`
}

type CellChange struct {
	At     grid.Ref `json:"at"`
	Before string   `json:"before"`
	After  string   `json:"after"`
}

type RowChange struct {
	Index  int      `json:"index"`
	Before []string `json:"before,omitempty"`
	After  []string `json:"after,omitempty"`
}

// Column is a header name with the column's values, one per row.
type Column struct {
	Name   string   `json:"name"`
	Values []string `json:"values"`
}

type ColumnChange struct {
	Index  int     `json:"index"`
	Before *Column `json:"before,omitempty"`
	After  *Column `json:"after,omitempty"`
}

// Empty reports a patch that changes nothing.
func (p Patch) Empty() bool { return len(p.Cells) == 0 && len(p.Rows) == 0 && len(p.Columns) == 0 }

// Inverse swaps before and after in every change.
func (p Patch) Inverse() Patch {
	out := Patch{}
	for _, c := range p.Cells {
		out.Cells = append(out.Cells, CellChange{At: c.At, Before: c.After, After: c.Before})
	}
	for _, r := range p.Rows {
		out.Rows = append(out.Rows, RowChange{Index: r.Index, Before: r.After, After: r.Before})
	}
	for _, c := range p.Columns {
		out.Columns = append(out.Columns, ColumnChange{Index: c.Index, Before: c.After, After: c.Before})
	}
	return out
}

// Apply records a patch: write it, mark the sheet dirty, push it on the undo
// stack, drop the redo stack and refresh the grid. An empty patch is a no-op.
func (a *App) Apply(p Patch) {
	if p.Empty() {
		return
	}
	a.write(p)
	a.Dirty = true
	a.Undo = append(a.Undo, p)
	a.Redo = nil
}

// UndoPatch reverts the last patch; RedoPatch replays the last undone one.
func (a *App) UndoPatch() error {
	if len(a.Undo) == 0 {
		return errors.New("nothing to undo")
	}
	p := a.Undo[len(a.Undo)-1]
	a.Undo = a.Undo[:len(a.Undo)-1]
	a.write(p.Inverse())
	a.Redo = append(a.Redo, p)
	a.Dirty = true
	return nil
}

func (a *App) RedoPatch() error {
	if len(a.Redo) == 0 {
		return errors.New("nothing to redo")
	}
	p := a.Redo[len(a.Redo)-1]
	a.Redo = a.Redo[:len(a.Redo)-1]
	a.write(p)
	a.Undo = append(a.Undo, p)
	a.Dirty = true
	return nil
}

// write applies a patch to the sheet: column deletions high to low, column
// replacements, column insertions low to high; rows the same way; then
// cells. Every patch the verbs build is homogeneous, so the order between
// kinds only matters for the inverse, which uses the same one.
func (a *App) write(p Patch) {
	cols := append([]ColumnChange(nil), p.Columns...)
	sort.SliceStable(cols, func(i, j int) bool { return cols[i].Index > cols[j].Index })
	for _, c := range cols {
		if c.After == nil && c.Before != nil {
			a.deleteColumn(c.Index)
		}
	}
	for _, c := range cols {
		if c.After != nil && c.Before != nil {
			a.setColumn(c.Index, *c.After)
		}
	}
	for i := len(cols) - 1; i >= 0; i-- {
		if c := cols[i]; c.After != nil && c.Before == nil {
			a.insertColumn(c.Index, *c.After)
		}
	}
	rows := append([]RowChange(nil), p.Rows...)
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Index > rows[j].Index })
	for _, r := range rows {
		if r.After == nil && r.Before != nil && r.Index < len(a.Rows) {
			a.Rows = append(a.Rows[:r.Index], a.Rows[r.Index+1:]...)
		}
	}
	for _, r := range rows {
		if r.After != nil && r.Before != nil && r.Index < len(a.Rows) {
			a.Rows[r.Index] = a.pad(append([]string(nil), r.After...))
		}
	}
	for i := len(rows) - 1; i >= 0; i-- {
		if r := rows[i]; r.After != nil && r.Before == nil {
			at := min(r.Index, len(a.Rows))
			a.Rows = append(a.Rows[:at], append([][]string{a.pad(append([]string(nil), r.After...))}, a.Rows[at:]...)...)
		}
	}
	for _, c := range p.Cells {
		if c.At.Row >= 0 && c.At.Row < len(a.Rows) && c.At.Col >= 0 && c.At.Col < len(a.Header) {
			a.Rows[c.At.Row][c.At.Col] = c.After
		}
	}
	a.grid.SetData(a.Header, a.Rows)
}

func (a *App) deleteColumn(i int) {
	if i < 0 || i >= len(a.Header) {
		return
	}
	a.Header = append(a.Header[:i], a.Header[i+1:]...)
	for r := range a.Rows {
		a.Rows[r] = append(a.Rows[r][:i], a.Rows[r][i+1:]...)
	}
}

func (a *App) setColumn(i int, col Column) {
	if i < 0 || i >= len(a.Header) {
		return
	}
	a.Header[i] = col.Name
	for r := range a.Rows {
		if r < len(col.Values) {
			a.Rows[r][i] = col.Values[r]
		}
	}
}

func (a *App) insertColumn(i int, col Column) {
	i = min(max(i, 0), len(a.Header))
	a.Header = append(a.Header[:i], append([]string{col.Name}, a.Header[i:]...)...)
	for r := range a.Rows {
		value := ""
		if r < len(col.Values) {
			value = col.Values[r]
		}
		a.Rows[r] = append(a.Rows[r][:i], append([]string{value}, a.Rows[r][i:]...)...)
	}
}

// pad fits a row to the header width.
func (a *App) pad(row []string) []string {
	for len(row) < len(a.Header) {
		row = append(row, "")
	}
	return row[:len(a.Header)]
}

// column reads a column as a value.
func (a *App) column(i int) Column {
	col := Column{Name: a.Header[i], Values: make([]string, len(a.Rows))}
	for r, row := range a.Rows {
		col.Values[r] = row[i]
	}
	return col
}

// Cell reads one value, "" outside the sheet.
func (a *App) Cell(ref grid.Ref) string {
	if ref.Row < 0 || ref.Row >= len(a.Rows) || ref.Col < 0 || ref.Col >= len(a.Header) {
		return ""
	}
	return a.Rows[ref.Row][ref.Col]
}

// File I/O through encoding/csv; the first record is the header.

// Load replaces the sheet with a file's contents and clears history.
func (a *App) Load(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	reader := csv.NewReader(f)
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		return err
	}
	a.Path = path
	a.Header, a.Rows = nil, nil
	if len(records) > 0 {
		a.Header = records[0]
		for _, record := range records[1:] {
			a.Rows = append(a.Rows, a.pad(record))
		}
	}
	a.Dirty, a.Undo, a.Redo, a.Filter = false, nil, nil, FilterState{}
	a.syncFilterField()
	a.grid.SetFilter(nil)
	a.grid.SetData(a.Header, a.Rows)
	return nil
}

// Save writes the sheet to path and makes it the sheet's path.
func (a *App) Save(path string) error {
	if path == "" {
		return errors.New("the sheet has no path; use save-as")
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := csv.NewWriter(f)
	if err := w.Write(a.Header); err != nil {
		f.Close()
		return err
	}
	if err := w.WriteAll(a.Rows); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	a.Path, a.Dirty = path, false
	return nil
}

// Transformations are data behind one verb. A text transformation sees the
// value; a sheet transformation sees a read view of the sheet and the cells
// being transformed. Both produce the same cell patch.

var textTransformations = map[string]func(string) string{
	"upper": strings.ToUpper,
	"lower": strings.ToLower,
	"trim":  strings.TrimSpace,
	"title": func(v string) string {
		words := strings.Fields(strings.ToLower(v))
		for i, w := range words {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
		return strings.Join(words, " ")
	},
	"slug": func(v string) string {
		return strings.Trim(strings.Join(strings.FieldsFunc(strings.ToLower(v), func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
		}), "-"), "-")
	},
}

var sheetTransformations = map[string]func(a *App, at grid.Ref, selected []grid.Ref) string{
	// fillDown gives every selected cell the value of the topmost selected
	// cell in its column, as a spreadsheet's fill-down does.
	"fillDown": func(a *App, at grid.Ref, selected []grid.Ref) string {
		top := at
		for _, ref := range selected {
			if ref.Col == at.Col && ref.Row < top.Row {
				top = ref
			}
		}
		return a.Cell(top)
	},
}

// TransformationNames lists every transformation, text ones first.
func TransformationNames() []string {
	var text, sheet []string
	for name := range textTransformations {
		text = append(text, name)
	}
	for name := range sheetTransformations {
		sheet = append(sheet, name)
	}
	sort.Strings(text)
	sort.Strings(sheet)
	return append(text, sheet...)
}

// transformationLabels describe each transformation in the pick list.
var transformationLabels = map[string]string{
	"upper":    "UPPER CASE",
	"lower":    "lower case",
	"trim":     "trim surrounding spaces",
	"title":    "Title Case",
	"slug":     "slug-case",
	"fillDown": "fill down from the top selected cell",
}

// TransformationChoices is transformations.list: every name with a label,
// in TransformationNames order, the shape a parameter Source returns.
func TransformationChoices() []ochazuke.Choice {
	names := TransformationNames()
	out := make([]ochazuke.Choice, len(names))
	for i, name := range names {
		out[i] = ochazuke.Choice{ID: name, Label: transformationLabels[name]}
	}
	return out
}

// Transform builds the patch that applies a named transformation to cells.
func (a *App) Transform(name string, cells []grid.Ref) (Patch, error) {
	text, isText := textTransformations[name]
	sheet, isSheet := sheetTransformations[name]
	if !isText && !isSheet {
		return Patch{}, fmt.Errorf("unknown transformation %q; transformations.list names them", name)
	}
	var p Patch
	for _, ref := range cells {
		before := a.Cell(ref)
		after := before
		if isText {
			after = text(before)
		} else {
			after = sheet(a, ref, cells)
		}
		if after != before {
			p.Cells = append(p.Cells, CellChange{At: ref, Before: before, After: after})
		}
	}
	return p, nil
}

// Yanks: the shared registers keep the shape so a row yank pastes rows and
// a column yank pastes columns. Text is rows of tab-separated cells, one
// row per line; a column yank's first line is the header names.

func (a *App) yankOf(cells []grid.Ref, shape string) ochazuke.Yank {
	top, bottom, left, right := grid.Bounds(cells)
	set := map[grid.Ref]bool{}
	for _, ref := range cells {
		set[ref] = true
	}
	var lines []string
	if shape == grid.ShapeColumn {
		var names []string
		for c := left; c <= right; c++ {
			if set[grid.Ref{Row: top, Col: c}] {
				names = append(names, a.Header[c])
			}
		}
		lines = append(lines, strings.Join(names, "\t"))
	}
	for r := top; r <= bottom; r++ {
		var row []string
		for c := left; c <= right; c++ {
			if shape == grid.ShapeColumn && !set[grid.Ref{Row: top, Col: c}] {
				continue
			}
			if set[grid.Ref{Row: r, Col: c}] || shape == grid.ShapeColumn {
				row = append(row, a.Cell(grid.Ref{Row: r, Col: c}))
			} else {
				row = append(row, "")
			}
		}
		if shape == grid.ShapeRow && !set[grid.Ref{Row: r, Col: left}] {
			continue
		}
		lines = append(lines, strings.Join(row, "\t"))
	}
	switch shape {
	case grid.ShapeRow:
		return ochazuke.Yank{Text: strings.Join(lines, "\n") + "\n", Shape: "line"}
	case grid.ShapeColumn:
		return ochazuke.Yank{Text: strings.Join(lines, "\n"), Shape: "column", Width: right - left + 1}
	}
	return ochazuke.Yank{Text: strings.Join(lines, "\n"), Shape: "block", Width: right - left + 1}
}

// pastePatch turns a yank into the patch that pastes it at the cursor: rows
// below the cursor row, columns after the cursor column, a block filling a
// rectangle from the cursor (clipped to the sheet). Any other text pastes as
// a block of tab-separated lines.
func (a *App) pastePatch(y ochazuke.Yank, at grid.Ref) Patch {
	if y.Text == "" {
		return Patch{}
	}
	lines := strings.Split(strings.TrimSuffix(y.Text, "\n"), "\n")
	var p Patch
	switch y.Shape {
	case "line":
		for i, line := range lines {
			p.Rows = append(p.Rows, RowChange{Index: at.Row + 1 + i, After: a.pad(strings.Split(line, "\t"))})
		}
	case "column":
		names := strings.Split(lines[0], "\t")
		for j, name := range names {
			col := Column{Name: name, Values: make([]string, len(a.Rows))}
			for r := range a.Rows {
				if r+1 < len(lines) {
					if cells := strings.Split(lines[r+1], "\t"); j < len(cells) {
						col.Values[r] = cells[j]
					}
				}
			}
			p.Columns = append(p.Columns, ColumnChange{Index: at.Col + 1 + j, After: &col})
		}
	default:
		for i, line := range lines {
			for j, value := range strings.Split(line, "\t") {
				ref := grid.Ref{Row: at.Row + i, Col: at.Col + j}
				if ref.Row >= len(a.Rows) || ref.Col >= len(a.Header) {
					continue
				}
				if before := a.Cell(ref); before != value {
					p.Cells = append(p.Cells, CellChange{At: ref, Before: before, After: value})
				}
			}
		}
	}
	return p
}
