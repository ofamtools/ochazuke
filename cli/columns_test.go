package cli

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/unixsuperhero/ochazuke"
)

// TestCatalogCarriesColumns: a noun's declared columns reach the catalog
// in order, the bytes are the same on every build, and a noun without
// columns writes no columns key.
func TestCatalogCarriesColumns(t *testing.T) {
	build := func() []byte {
		r := ochazuke.NewRuntime()
		r.Define("pane").Columns(
			ochazuke.Column{Title: "pane"},
			ochazuke.Column{Title: "state", Priority: 1},
			ochazuke.Column{Title: "cwd", Min: 8, Priority: 2},
		).Verb("list", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) }, "List panes")
		r.Define("other").Verb("run", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) }, "Run")
		data, err := Catalog(r, "tool", "v1")
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	first := build()
	for range 5 {
		if again := build(); !bytes.Equal(first, again) {
			t.Fatal("catalog bytes differ between builds")
		}
	}
	var doc CatalogDoc
	if err := json.Unmarshal(first, &doc); err != nil {
		t.Fatal(err)
	}
	var pane, other *CatalogNoun
	for i := range doc.Nouns {
		switch doc.Nouns[i].Name {
		case "pane":
			pane = &doc.Nouns[i]
		case "other":
			other = &doc.Nouns[i]
		}
	}
	if pane == nil || other == nil {
		t.Fatalf("nouns %+v", doc.Nouns)
	}
	titles := []string{}
	for _, c := range pane.Columns {
		titles = append(titles, c.Title)
	}
	if !slices.Equal(titles, []string{"pane", "state", "cwd"}) || pane.Columns[2].Min != 8 || pane.Columns[2].Priority != 2 {
		t.Fatalf("pane columns %+v", pane.Columns)
	}
	if other.Columns != nil {
		t.Fatalf("other has columns %+v", other.Columns)
	}
	if strings.Count(string(first), `"columns"`) != 1 {
		t.Fatalf("want one columns key:\n%s", first)
	}
}

// TestRowCellsRoundTrip: cells survive JSON Lines and the row reader; a
// row without cells writes none.
func TestRowCellsRoundTrip(t *testing.T) {
	var out bytes.Buffer
	for _, row := range []Row{{Noun: "pane", ID: "w1", Label: "work/a", Cells: []string{"work/a", "idle", "~/proj"}}, {Noun: "pane", ID: "w2", Label: "b"}} {
		data, _ := json.Marshal(row)
		out.Write(append(data, '\n'))
	}
	if strings.Count(out.String(), `"cells"`) != 1 {
		t.Fatalf("cells key count:\n%s", out.String())
	}
	rows, _, err := NewRowReader(&out).Next(10, time.Second)
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows %v err %v", rows, err)
	}
	if !slices.Equal(rows[0].Cells, []string{"work/a", "idle", "~/proj"}) || rows[1].Cells != nil {
		t.Fatalf("cells %v %v", rows[0].Cells, rows[1].Cells)
	}
}
