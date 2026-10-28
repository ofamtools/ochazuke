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
		r.DefineOwn("pane").Columns(
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

// TestCatalogListColumns: a host's list of a noun's rows takes the columns
// of the noun its row source's rows answer to; a noun without a row source
// or without columns gets a plain list.
func TestCatalogListColumns(t *testing.T) {
	r := ochazuke.NewRuntime()
	noop := func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) }
	cols := []ochazuke.Column{{Title: "pane"}, {Title: "state", Priority: 1}}
	r.DefineOwn("pane").Columns(cols...).Verb("list", noop, "List panes")
	r.Define("worktree").Columns(ochazuke.Column{Title: "ignored"}).Verb("panes", noop, "Panes of a worktree").Rows("pane")
	r.Define("tool").Columns(ochazuke.Column{Title: "verb rows"}).Verb("run", noop, "Run")
	r.Define("plain").Verb("list", noop, "List plain rows")
	data, err := Catalog(r, "tool", "v1")
	if err != nil {
		t.Fatal(err)
	}
	var doc CatalogDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	for noun, want := range map[string][]ochazuke.Column{"pane": cols, "worktree": cols, "tool": nil, "plain": nil, "missing": nil} {
		if got := doc.ListColumns(noun); !slices.Equal(got, want) {
			t.Errorf("ListColumns(%s) = %+v, want %+v", noun, got, want)
		}
	}
	if verb, rows := doc.RowSource("worktree"); verb != "panes" || rows != "pane" {
		t.Errorf("RowSource(worktree) = %s, %s", verb, rows)
	}
	// Mounted with the columns, a list draws the header; none, a plain list.
	host := ochazuke.NewRuntime()
	host.DefineOwn("pane")
	list, err := host.AddList("panes", []ochazuke.ListItem{{ID: "p1", Label: "one", Noun: "pane", Cells: []string{"one", "idle"}}}, ochazuke.Columns(doc.ListColumns("worktree")...))
	if err != nil {
		t.Fatal(err)
	}
	if got := list.Columns(); !slices.Equal(got, cols) {
		t.Fatalf("mounted list columns = %+v", got)
	}
	plain, err := host.AddList("tools", nil, ochazuke.Columns(doc.ListColumns("tool")...))
	if err != nil {
		t.Fatal(err)
	}
	if plain.Columns() != nil {
		t.Fatalf("a verb-row list got columns: %+v", plain.Columns())
	}
}

// TestCatalogRowSourcePrefersTheNounsOwnRows: a noun's row source is its
// list when list declares the noun's own rows, else another verb declaring
// the noun's own rows, else a list declaring none, and only then a verb
// declaring another noun's rows; inside a step, catalog (sorted) order. So
// slack-channel's history (rows of messages, sorted before list) and
// pr.ignored (the noun's rows too, sorted before list) never beat list.
func TestCatalogRowSourcePrefersTheNounsOwnRows(t *testing.T) {
	r := ochazuke.NewRuntime()
	noop := func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) }
	r.DefineOwn("message").Verb("list", noop, "List messages").Rows("message")
	// The o-slack case: history (h < l) declares another noun's rows.
	r.DefineOwn("channel").
		Verb("history", noop, "The channel's messages").Rows("message").
		Verb("list", noop, "List channels").Rows("channel")
	// The o-gh case: ignored (i < l) declares the noun's own rows too.
	r.DefineOwn("pr").
		Verb("ignored", noop, "Ignored PRs").Rows("pr").
		Verb("list", noop, "List PRs").Rows("pr")
	// The o-mcp case: list declares nothing, calls declares another noun's.
	r.DefineOwn("call").Verb("list", noop, "List calls").Rows("call")
	r.DefineOwn("server").
		Verb("calls", noop, "The server's calls").Rows("call").
		Verb("list", noop, "List servers")
	// No list: a verb with the noun's own rows beats one with another's.
	r.DefineOwn("thread").
		Verb("messages", noop, "The thread's messages").Rows("message").
		Verb("replies", noop, "Replies as threads").Rows("thread")
	// Only another noun's rows (mtm's store tables): that verb.
	r.DefineOwn("store").Verb("tables", noop, "The store's tables").Rows("message")
	r.DefineOwn("tool").Verb("run", noop, "Run")
	data, err := Catalog(r, "tool", "v1")
	if err != nil {
		t.Fatal(err)
	}
	var doc CatalogDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ noun, verb, rows string }{
		{"message", "list", "message"},
		{"channel", "list", "channel"},
		{"pr", "list", "pr"},
		{"server", "list", "server"},
		{"thread", "replies", "thread"},
		{"store", "tables", "message"},
		{"tool", "", ""},
		{"missing", "", ""},
	} {
		if verb, rows := doc.RowSource(c.noun); verb != c.verb || rows != c.rows {
			t.Errorf("RowSource(%s) = %q, %q, want %q, %q", c.noun, verb, rows, c.verb, c.rows)
		}
	}
}
