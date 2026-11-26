package bindings_test

import (
	"testing"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/traits"
	"ofam.dev/ochazuke/widgets/bindings"
	"ofam.dev/ochazuke/widgets/table"
)

func TestBindingsAndTableFilterableMultiTerm(t *testing.T) {
	tbl := table.New([]table.Column{{Key: "title", Title: "Title"}}, []table.Row{
		{ID: "r1", Cells: map[string]any{"title": "Alpha"}},
		{ID: "r2", Cells: map[string]any{"title": "Beta"}},
	})
	var tblf traits.Filterable = tbl
	if !tblf.Filter("bet", traits.MatchSubstring) || len(tbl.Filtered()) != 1 {
		t.Fatalf("expected table Filterable to keep Beta only, got %d", len(tbl.Filtered()))
	}

	bm := bindings.New(
		[]core.Mode{{Name: "normal"}},
		[]core.Binding{
			{Mode: "normal", Keys: []string{"g", "p"}, Command: "panes.group.toggle", Hint: "toggle rail grouping"},
			{Mode: "normal", Keys: []string{"space"}, Command: "table.group.toggle", Hint: "fold group"},
			{Mode: "normal", Keys: []string{"m"}, Command: "record.star", Hint: "star"},
		},
	)
	var bf traits.Filterable = bm
	if !bf.Filter("group toggle", traits.MatchSubstring) {
		t.Fatal("expected multi-word AND 'group toggle' to match *.group.toggle bindings")
	}
	if got := len(bm.Filtered()); got != 2 {
		t.Fatalf("bindings 'group toggle' matched %d rows, want 2", got)
	}
}
