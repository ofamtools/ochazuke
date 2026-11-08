// Package readertest is the one table of lines the reader reads, shared by
// the palette's test (package ochazuke) and the CLI's (package cli), so the
// same words read the same in both. It is plain data: each test builds the
// operations below on its own runtime, runs every row, and checks the
// operation that ran (or would run) and its arguments, or the error.
package readertest

// Param is one parameter of an operation.
type Param struct {
	Name, Type string
	Enum       []string
	Positional bool
}

// Op is one operation of the fixture: a noun's action, its parameters and
// an alias of it on the same noun.
type Op struct {
	Noun, Action string
	Params       []Param
	Alias        string
}

// MainNoun is the fixture's main noun (Noun.Main).
const MainNoun = "lane"

// Ops are the fixture's operations, in the program's own namespace (no
// framework word shares their first letters, so the palette, which also
// offers the framework's operations, reads them as the CLI does).
var Ops = []Op{
	{Noun: "quad", Action: "warp", Params: []Param{{Name: "name", Type: "string", Enum: []string{"up", "down"}, Positional: true}}},
	{Noun: "quill", Action: "wipe"},
	{Noun: "lane", Action: "state"},
	{Noun: "lane", Action: "list", Params: []Param{{Name: "state", Type: "string"}}},
	{Noun: "lane", Action: "define", Params: []Param{{Name: "project", Type: "string"}}},
	{Noun: "plan", Action: "define"},
	{Noun: "question", Action: "ask", Params: []Param{{Name: "text", Type: "string", Positional: true}}, Alias: "pose"},
	{Noun: "json.doc", Action: "rows"},
	{Noun: "md.doc", Action: "rows"},
}

// Row is one line and what it reads as: the operation (its two-part key)
// and its arguments (name=value, sorted, comma-joined), or text the error
// holds.
type Row struct {
	Line string
	Op   string
	Args string
	Err  string
}

// Rows are the lines, the owner's examples among them: a pair of
// prefixes settled by the rest of the line (t r up), a pair two readings
// fit, a verb alone on the main noun (o-lane state, o-lane list --state
// running), a verb with its noun named after it, an alias read verb first
// (o-questions add), namespaced and punctuated spellings.
var Rows = []Row{
	{Line: "q w up", Op: "quad.warp", Args: "name=up"},
	{Line: "quad warp up", Op: "quad.warp", Args: "name=up"},
	{Line: "warp quad up", Op: "quad.warp", Args: "name=up"},
	{Line: "q w", Err: "is ambiguous"},
	{Line: "state", Op: "lane.state"},
	{Line: "list --state running", Op: "lane.list", Args: "state=running"},
	{Line: "define", Op: "lane.define"},
	{Line: "define plan", Op: "plan.define"},
	{Line: "define --project p1", Op: "lane.define", Args: "project=p1"},
	{Line: "pose hello", Op: "question.ask", Args: "text=hello"},
	{Line: "json.doc.rows", Op: "json.doc.rows"},
	{Line: "json.doc rows", Op: "json.doc.rows"},
	{Line: "rows, json.doc", Op: "json.doc.rows"},
	{Line: "rows , json.doc", Op: "json.doc.rows"},
	{Line: "md.doc.rows", Op: "md.doc.rows"},
	{Line: "doc rows", Err: `"doc rows" is ambiguous: json.doc rows, md.doc rows`},
}

// Completions are words being completed (the last one, "" after a space)
// and the names the reader offers for it, which the palette's list and the
// CLI's complete both narrow to.
var Completions = []struct {
	Words []string
	Want  []string
}{
	{Words: []string{"qua"}, Want: []string{"quad"}},
	{Words: []string{"wip"}, Want: []string{"wipe"}},
	{Words: []string{"warp", ""}, Want: []string{"quad"}},
	{Words: []string{"lane", "de"}, Want: []string{"define"}},
	{Words: []string{"define", "pl"}, Want: []string{"plan"}},
}
