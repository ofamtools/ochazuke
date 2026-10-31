package main

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

// tablesOf is every table's rows in table order, keyed by noun or
// instance and mode, each row as JSON (guards included).
//
// spelled writes a row spelled by an alias as the alias (what a file
// holds before the runtime resolves it).
func tablesOf(t *testing.T, rows []ochazuke.Binding, spelled bool) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for _, row := range rows {
		row.Aliases = nil
		if spelled && row.Request.Via != "" {
			if i := strings.LastIndex(row.Request.Via, "."); i > 0 {
				row.Request.Noun, row.Request.Verb, row.Request.Via = row.Request.Via[:i], row.Request.Via[i+1:], ""
			}
		}
		data, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		var v any
		_ = json.Unmarshal(data, &v) // numbers read back as float64, as from a file
		data, _ = json.Marshal(v)
		id := row.Noun + "|" + row.Instance + "|" + row.Mode
		out[id] = append(out[id], string(data))
	}
	return out
}

// The project's every stored table, exported in the grouped format and
// read back, is the same rows in the same order per table; installed into
// a fresh build it leaves every table as it was.
func TestBindingsExportRoundTripsEveryTable(t *testing.T) {
	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatal(err)
	}
	m, err := Build(cfg)
	if err != nil {
		t.Fatal(err)
	}
	doc := m.Runtime.BindingsDoc()
	data, err := ochazuke.FormatBindings(doc)
	if err != nil {
		t.Fatal(err)
	}
	back, _, err := ochazuke.ParseBindings(data)
	if err != nil {
		t.Fatal(err)
	}
	want, got := tablesOf(t, doc.Rows(), true), tablesOf(t, back.Rows(), false)
	if len(want) != len(got) {
		t.Fatalf("%d tables, read back %d", len(want), len(got))
	}
	for id, rows := range want {
		if !slices.Equal(rows, got[id]) {
			t.Fatalf("table %s differs:\n%v\n%v", id, rows, got[id])
		}
	}
	fresh, err := Build(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fresh.Runtime.LoadBindings(data); err != nil {
		t.Fatal(err)
	}
	stored := tablesOf(t, doc.Rows(), false)
	after := tablesOf(t, fresh.Runtime.BindingsDoc().Rows(), false)
	for id, rows := range stored {
		if !slices.Equal(rows, after[id]) {
			t.Fatalf("table %s changed after loading the export:\n%v\n%v", id, rows, after[id])
		}
	}
}
