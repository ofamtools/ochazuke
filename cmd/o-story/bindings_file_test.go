package main

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	story "github.com/unixsuperhero/monogatari"
	"github.com/unixsuperhero/ochazuke"
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

// Every story app's stored tables, exported in the grouped format and read
// back, are the same rows in the same order per table (guards and the
// h/l table-column, table-scrolls and table-spills fallthroughs
// included); installed into a fresh build of the story they leave every
// table as it was.
func TestBindingsExportRoundTripsEveryStoryApp(t *testing.T) {
	core, err := story.LoadFS(embedded, "stories")
	if err != nil {
		t.Fatal(err)
	}
	built := 0
	for _, s := range core {
		if s.Base != "" {
			continue // a trimmed story replays its base's app
		}
		ss, err := story.Library{Apps: hostApps}.Build(s, story.Options{Mono: true})
		if err != nil {
			continue // a story that does not build is its own test's failure
		}
		built++
		doc := ss.R.BindingsDoc()
		data, err := ochazuke.FormatBindings(doc)
		if err != nil {
			t.Fatalf("%s: %v", s.Path, err)
		}
		back, _, err := ochazuke.ParseBindings(data)
		if err != nil {
			t.Fatalf("%s: %v", s.Path, err)
		}
		want, got := tablesOf(t, doc.Rows(), true), tablesOf(t, back.Rows(), false)
		if len(want) != len(got) {
			t.Fatalf("%s: %d tables, read back %d", s.Path, len(want), len(got))
		}
		for id, rows := range want {
			if !slices.Equal(rows, got[id]) {
				t.Fatalf("%s: table %s differs:\n%v\n%v", s.Path, id, rows, got[id])
			}
		}
		fresh, err := story.Library{Apps: hostApps}.Build(s, story.Options{Mono: true})
		if err != nil {
			t.Fatalf("%s: %v", s.Path, err)
		}
		if _, err := fresh.R.LoadBindings(data); err != nil {
			t.Fatalf("%s: %v", s.Path, err)
		}
		stored := tablesOf(t, doc.Rows(), false)
		after := tablesOf(t, fresh.R.BindingsDoc().Rows(), false)
		for id, rows := range stored {
			if !slices.Equal(rows, after[id]) {
				t.Fatalf("%s: table %s changed after loading the export:\n%v\n%v", s.Path, id, rows, after[id])
			}
		}
		ss.Close()
		fresh.Close()
	}
	if built < 100 {
		t.Fatalf("only %d story apps built", built)
	}
}
