package ochazuke

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tailscale/hujson"
)

// fileFixtureRows mix every kind of row: plain, a hint, guarded
// alternatives on one key, an unbind (plain and guarded), args, a pattern
// with from, flags, an instance table.
func fileFixtureRows() []Binding {
	return []Binding{
		{Mode: "normal", Noun: "md-doc", Keys: "l", Request: Request{Noun: "ochazuke.focus", Verb: "next"}},
		{Mode: "normal", Noun: "md-doc", Keys: "enter", Request: Request{Noun: "ochazuke.focus", Verb: "next"}, Hint: "edit"},
		{Mode: "leader_pending", Noun: "terminal", Keys: ":", Request: Request{Noun: "ochazuke.palette", Verb: "open"}},
		{Mode: "normal", Noun: "pane", Keys: "]", Request: Request{Noun: "panes", Verb: "tab", Args: Arguments{"delta": 1}}},
		{Mode: "normal", Noun: "pane", Keys: "x", Unbind: true},
		{Mode: "normal", Noun: "pane", Keys: "d d", Request: Request{Noun: "pane", Verb: "kill"}, When: "selected", Hidden: true},
		{Mode: "normal", Noun: "pane", Keys: "d d", Request: Request{Noun: "pane", Verb: "close"}},
		{Mode: "normal", Noun: "pane", Keys: "u", When: "busy", Unbind: true},
		{Mode: "insert", Noun: "editor", Pattern: "any", Request: Request{Noun: "editor", Verb: "type"}, From: map[string]string{"text": "key"}, Capture: true},
		{Noun: "app", Keys: "ctrl+a v", Request: Request{Noun: "app", Verb: "split"}, Help: "Split the view"},
		{Instance: "work/agent", Mode: "normal", Keys: "ctrl+r", Request: Request{Noun: "pane", Verb: "restart", Target: "work/agent"}, Replay: true},
	}
}

const fileGolden = BindingsHeader + `
{
  "modes": {
    "leader_pending": {"pending": true}
  },
  "bindings": [
    {"context": "app", "bindings": [
      {"key": "ctrl+a v", "action": "app.split", "help": "Split the view"}
    ]},
    {"context": "editor/insert", "bindings": [
      {"key": "any", "action": "editor.type", "from": {"text": "key"}, "capture": true}
    ]},
    {"context": "md-doc/normal", "bindings": [
      {"key": "l", "action": "ochazuke.focus.next"},
      {"key": "enter", "action": "ochazuke.focus.next", "hint": "edit"}
    ]},
    {"context": "pane/normal", "bindings": [
      {"key": "]", "action": "panes.tab", "args": {"delta": 1}},
      {"key": "x", "unbind": true},
      {"key": "d d", "action": "pane.kill", "when": "selected", "hidden": true},
      {"key": "d d", "action": "pane.close"},
      {"key": "u", "when": "busy", "unbind": true}
    ]},
    {"context": "terminal/leader_pending", "bindings": [
      {"key": ":", "action": "ochazuke.palette.open"}
    ]},
    {"instance": "work/agent", "mode": "normal", "bindings": [
      {"key": "ctrl+r", "action": "pane.restart", "replay": true, "target": "work/agent"}
    ]}
  ]
}
`

// fileGoldenV033 is the fixture in the v0.33 form of a group (a key map,
// a key's rows an array), which the reader still takes.
const fileGoldenV033 = `// the v0.33 header comment
{
  "modes": {
    "leader_pending": {"pending": true}
  },
  "bindings": [
    {"context": "app", "bindings": {
      "ctrl+a v": {"action": "app.split", "help": "Split the view"}
    }},
    {"context": "editor/insert", "bindings": {
      "any": {"action": "editor.type", "from": {"text": "key"}, "capture": true}
    }},
    {"context": "md-doc/normal", "bindings": {
      "l": "ochazuke.focus.next",
      "enter": {"action": "ochazuke.focus.next", "hint": "edit"}
    }},
    {"context": "pane/normal", "bindings": {
      "]": {"action": "panes.tab", "args": {"delta": 1}},
      "x": null,
      "d d": [{"action": "pane.kill", "when": "selected", "hidden": true}, {"action": "pane.close"}],
      "u": {"when": "busy", "unbind": true}
    }},
    {"context": "terminal/leader_pending", "bindings": {
      ":": "ochazuke.palette.open"
    }},
    {"instance": "work/agent", "mode": "normal", "bindings": {
      "ctrl+r": {"action": "pane.restart", "replay": true, "target": "work/agent"}
    }}
  ]
}
`

func fileFixtureDoc() BindingsDoc {
	return BindingsDoc{Modes: map[string]ModeSpec{"leader_pending": {Pending: true}}, Groups: GroupBindings(fileFixtureRows())}
}

func TestFormatBindingsGolden(t *testing.T) {
	got, err := FormatBindings(fileFixtureDoc())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != fileGolden {
		t.Fatalf("got:\n%s\nwant:\n%s", got, fileGolden)
	}
	empty, _ := FormatBindings(BindingsDoc{})
	if string(empty) != BindingsHeader+"\n{\n  \"bindings\": []\n}\n" {
		t.Fatalf("empty: %q", empty)
	}
}

// rowsText is rows as compact JSON per row, for comparing two row lists.
func rowsText(t *testing.T, rows []Binding) []string {
	t.Helper()
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = canonical(t, row)
	}
	return out
}

// Written and read back: the same groups, the same rows in the same order,
// and written again the same bytes.
func TestFormatBindingsRoundTrip(t *testing.T) {
	data, err := FormatBindings(fileFixtureDoc())
	if err != nil {
		t.Fatal(err)
	}
	doc, format, err := ParseBindings(data)
	if err != nil || format != BindingsGrouped {
		t.Fatalf("%q %v", format, err)
	}
	want := rowsText(t, fileFixtureDoc().Rows())
	if got := rowsText(t, doc.Rows()); !slices.Equal(got, want) {
		t.Fatalf("rows:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	again, err := FormatBindings(doc)
	if err != nil || string(again) != string(data) {
		t.Fatalf("written twice differs (%v):\n%q\n%q", err, again, data)
	}
}

// All three formats read as the same rows in the same order: the
// one-row-per-line groups, the v0.33 key map, and the older array of rows
// (below); the line form may repeat a key with no guard, the map may not.
func TestParseBindingsReadsEveryFormat(t *testing.T) {
	want := rowsText(t, fileFixtureDoc().Rows())
	for name, text := range map[string]string{"lines": fileGolden, "v0.33 map": fileGoldenV033} {
		doc, format, err := ParseBindings([]byte(text))
		if err != nil || format != BindingsGrouped {
			t.Fatalf("%s: %q %v", name, format, err)
		}
		if got := rowsText(t, doc.Rows()); !slices.Equal(got, want) {
			t.Fatalf("%s rows:\n%s\nwant:\n%s", name, strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
		again, err := FormatBindings(doc)
		if err != nil || string(again) != fileGolden {
			t.Fatalf("%s written back is not the line form (%v):\n%s", name, err, again)
		}
	}
	doc, _, err := ParseBindings([]byte(`{"bindings": [{"context": "x/normal", "bindings": [
		{"key": "l", "action": "x.open"},
		{"key": "l", "action": "x.enter"},
		{"key": "digit", "action": "x.count", "from": {"n": "digit"}}
	]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	rows := doc.Rows()
	if len(rows) != 3 || rows[0].Keys != "l" || rows[1].Keys != "l" || rows[1].Request.Verb != "enter" || rows[2].Pattern != "digit" {
		t.Fatalf("rows %+v", rows)
	}
	for _, tc := range []struct{ text, want string }{
		{`{"bindings": [{"context": "app", "bindings": [{"action": "test.alpha"}]}]}`, `needs "key"`},
		{`{"bindings": [{"context": "app", "bindings": ["test.alpha"]}]}`, `want a row`},
		{`{"bindings": [{"context": "app", "bindings": [{"key": "a", "hint": "x"}]}]}`, `app a: no "action"`},
		{`{"bindings": [{"context": "app", "bindings": [{"key": "a", "action": "test.alpha", "nope": 1}]}]}`, `unknown field "nope"`},
	} {
		if _, _, err := ParseBindings([]byte(tc.text)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want %q", tc.text, err, tc.want)
		}
	}
}

// The older array of rows reads as the same rows, any field order.
func TestParseBindingsReadsTheRowsFormat(t *testing.T) {
	rows := fileFixtureRows()
	var b bytes.Buffer
	b.WriteString("[\n")
	for i, row := range rows {
		// Fields reversed: reading takes any order.
		var m map[string]json.RawMessage
		data, _ := json.Marshal(row)
		_ = json.Unmarshal(data, &m)
		keys := slices.Sorted(func(yield func(string) bool) {
			for k := range m {
				if !yield(k) {
					return
				}
			}
		})
		slices.Reverse(keys)
		b.WriteString("  {")
		for j, k := range keys {
			if j > 0 {
				b.WriteString(" ,\n   ")
			}
			fmt.Fprintf(&b, "%q :%s", k, m[k])
		}
		b.WriteString("}")
		if i < len(rows)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("]")
	doc, format, err := ParseBindings(b.Bytes())
	if err != nil || format != BindingsRows {
		t.Fatalf("%q %v\n%s", format, err, b.String())
	}
	if got, want := rowsText(t, doc.Rows()), rowsText(t, fileFixtureDoc().Rows()); !slices.Equal(got, want) {
		t.Fatalf("rows:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// One key with several rows in a table is an array; a key twice is an
// error that says so; a group needs a context or an instance.
func TestParseBindingsGroupedErrors(t *testing.T) {
	for _, tc := range []struct{ text, want string }{
		{`{"bindings": [{"context": "app", "bindings": {"a": "test.alpha", "a": "test.beta"}}]}`, `key "a" twice`},
		{`{"bindings": [{"bindings": {}}]}`, `needs "context"`},
		{`{"bindings": [{"context": "app", "bindings": {"a": 3}}]}`, `app a: want an action string`},
		{`{"bindings": [{"context": "app", "bindings": {"a": "alpha"}}]}`, `not noun.verb`},
		{`{"bindings": [{"context": "app", "bindings": {"a": {"hint": "x"}}}]}`, `no "action"`},
		{`{"bindings": [{"instance": "w", "extends": "visual", "bindings": {}}]}`, `extends applies to a noun`},
		{`{"rows": []}`, `unknown field "rows"`},
		{`{}`, `not a bindings file`},
	} {
		if _, _, err := ParseBindings([]byte(tc.text)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want %q", tc.text, err, tc.want)
		}
	}
	doc, _, err := ParseBindings([]byte(`{"bindings": [
		{"context": "ochazuke.editor/visual-line", "extends": "visual", "bindings": {"j": "ochazuke.input.down"}},
		{"context": "a/b/normal", "bindings": {"one-of:h j": "test.alpha"}}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	g := doc.Groups
	if g[0].Noun != "ochazuke.editor" || g[0].Mode != "visual-line" || g[0].Extends != "visual" || g[1].Noun != "a/b" || g[1].Mode != "normal" || g[1].Rows[0].Pattern != "one-of:h j" {
		t.Fatalf("groups %+v", g)
	}
}

// JSONC: line comments, block comments, a trailing comma and an inline
// comment after a row read as the plain file does, in both formats.
func TestBindingsFileJSONC(t *testing.T) {
	plain := `{"bindings": [{"context": "app/normal", "bindings": {"a": "test.beta", "x": null}}]}`
	commented := `// my keys
{
  /* the app's normal mode */
  "bindings": [
    {"context": "app/normal", "bindings": {
      "a": "test.beta", // was alpha
      "x": null, /* unbind */
    }},
  ],
}
`
	rowsPlain := `[{"noun": "app", "mode": "normal", "keys": "a", "request": {"noun": "test", "verb": "beta"}}]`
	rowsCommented := "[\n  // one row\n  {\"noun\": \"app\", \"mode\": \"normal\", \"keys\": \"a\", /* why */ \"request\": {\"noun\": \"test\", \"verb\": \"beta\"},}, // trailing\n]\n"
	for _, pair := range [][2]string{{plain, commented}, {rowsPlain, rowsCommented}} {
		a, fa, err := ParseBindings([]byte(pair[0]))
		if err != nil {
			t.Fatal(err)
		}
		b, fb, err := ParseBindings([]byte(pair[1]))
		if err != nil {
			t.Fatal(err)
		}
		if fa != fb || !slices.Equal(rowsText(t, a.Rows()), rowsText(t, b.Rows())) {
			t.Fatalf("commented reads differently: %v vs %v", a.Rows(), b.Rows())
		}
	}
	// And as the user's bindings.json.
	r, dir := exportFixture(t)
	writeConfig(t, dir, BindingsFile, commented)
	if problems := r.ReloadUserBindings(); len(problems) > 0 {
		t.Fatalf("problems: %v", problems)
	}
	got, err := r.Execute(Request{Noun: "ochazuke.bindings", Verb: "list", Args: Arguments{"all": true}})
	if err != nil {
		t.Fatal(err)
	}
	var a, x bool
	for _, row := range got.([]Binding) {
		if row.Noun == "app" && row.Mode == "normal" {
			a = a || row.Keys == "a" && row.Request.Key() == "test.beta"
			x = x || row.Keys == "x"
		}
	}
	if !a || x {
		t.Fatalf("a rebound %v, x present %v", a, x)
	}
	if r.Config().BindingsFormat() != BindingsGrouped {
		t.Fatalf("format %q", r.Config().BindingsFormat())
	}
}

// A malformed file names the line and column of the original text, with
// the comments counted.
func TestBindingsFileMalformedPosition(t *testing.T) {
	text := "// header\n{\n  /* c */ \"bindings\": [\n    {\"context\": \"app\" \"bindings\": {}}\n  ]\n}\n"
	_, _, err := ParseBindings([]byte(text))
	if err == nil || !strings.Contains(err.Error(), "line 4, column 23") {
		t.Fatalf("got %v", err)
	}
	// A type error after a comment is placed too.
	_, _, err = ParseBindings([]byte("// rows\n[\n  {\"keys\": 7}\n]"))
	if err == nil || !strings.Contains(err.Error(), "line 3, column") {
		t.Fatalf("got %v", err)
	}
	r, dir := exportFixture(t)
	writeConfig(t, dir, BindingsFile, text)
	if problems := r.ReloadUserBindings(); len(problems) != 1 || !strings.Contains(problems[0], "bindings.json: not a bindings file: line 4, column 23") {
		t.Fatalf("problems %v", problems)
	}
}

// Old rows, rewritten in the grouped format, install the same effective
// tables as the rows did.
func TestBindingsRowsToGroupedSameEffectiveRows(t *testing.T) {
	rowsFile := `[
  {"noun": "app", "mode": "normal", "keys": "a", "request": {"noun": "test", "verb": "beta"}},
  {"noun": "app", "mode": "normal", "keys": "g b", "request": null},
  {"noun": "app", "mode": "normal", "keys": "z", "when": "input", "request": {"noun": "test", "verb": "alpha"}},
  {"noun": "app", "mode": "normal", "keys": "z", "request": {"noun": "test", "verb": "beta"}, "hint": "bee"},
  {"noun": "app", "mode": "count_pending", "keys": "5", "request": {"noun": "ochazuke.count", "verb": "append", "args": {"digit": 5}}},
  {"noun": "ochazuke.editor", "mode": "insert", "pattern": "one-of:q w", "request": {"noun": "ochazuke.input", "verb": "insert"}, "from": {"text": "key"}}
]`
	effective := func(text string) ([]string, string) {
		r, dir := exportFixture(t)
		writeConfig(t, dir, BindingsFile, text)
		// Notices (the guarded z row answers first) are the same in both
		// formats; only the rows are compared.
		r.ReloadUserBindings()
		return rowsText(t, r.allBindings()), r.Config().BindingsFormat()
	}
	before, f1 := effective(rowsFile)
	doc, _, err := ParseBindings([]byte(rowsFile))
	if err != nil {
		t.Fatal(err)
	}
	grouped, err := FormatBindings(doc)
	if err != nil {
		t.Fatal(err)
	}
	after, f2 := effective(string(grouped))
	if f1 != BindingsRows || f2 != BindingsGrouped {
		t.Fatalf("formats %q %q", f1, f2)
	}
	if !slices.Equal(before, after) {
		t.Fatalf("effective rows differ:\n%s\n---\n%s\n%s", strings.Join(before, "\n"), strings.Join(after, "\n"), grouped)
	}
}

// The export loads back as bindings.json and leaves every table as it was.
func TestBindingsExportVerbLoadsBack(t *testing.T) {
	r, dir := exportFixture(t)
	path := filepath.Join(t.TempDir(), "keys.json")
	if _, err := r.Execute(Request{Noun: "ochazuke.bindings", Verb: "export", Args: Arguments{"path": path}}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.bindings", Verb: "export", Args: Arguments{"path": filepath.Join(dir, BindingsFile)}}); err == nil {
		t.Fatal("export over the live bindings.json without force")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	before := rowsText(t, r.allBindings())
	writeConfig(t, dir, BindingsFile, string(data))
	if problems := r.ReloadUserBindings(); len(problems) > 0 {
		t.Fatalf("problems: %v", problems)
	}
	if after := rowsText(t, r.allBindings()); !slices.Equal(before, after) {
		t.Fatal("tables changed after loading the export")
	}
}

// bindings check names the format it read on the status line.
func TestBindingsCheckReportsFormat(t *testing.T) {
	for _, tc := range []struct{ text, want string }{
		{`[{"noun": "app", "mode": "normal", "keys": "a", "request": {"noun": "test", "verb": "beta"}}]`, "bindings.json: rows format"},
		{`{"bindings": [{"context": "app/normal", "bindings": {"a": "test.beta"}}]}`, "bindings.json: grouped format"},
	} {
		r, dir := exportFixture(t)
		sink := &RecordingSink{}
		r.UI().Attach(sink)
		writeConfig(t, dir, BindingsFile, tc.text)
		got, err := r.Execute(Request{Noun: "ochazuke.bindings", Verb: "check"})
		if err != nil || len(got.([]string)) != 0 {
			t.Fatalf("check %v %v", got, err)
		}
		if sink.LastInfo() != tc.want {
			t.Fatalf("info %q, want %q", sink.LastInfo(), tc.want)
		}
	}
}

// A user file may repeat the application's modes and extends (the export
// does); one that differs is reported and ignored.
func TestBindingsFileModesMustAgree(t *testing.T) {
	r, dir := exportFixture(t)
	writeConfig(t, dir, BindingsFile, `{"modes": {"count_pending": {"pending": true}, "made_up": {"pending": true}, "normal": {"pending": true}},
		"bindings": [{"context": "app/normal", "extends": "insert", "bindings": {"a": "test.beta"}}]}`)
	problems := r.ReloadUserBindings()
	joined := strings.Join(problems, "\n")
	for _, want := range []string{"modes made_up: unknown mode", "modes normal: pending false", `app/normal: extends "insert" differs`} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in:\n%s", want, joined)
		}
	}
	if len(problems) != 3 {
		t.Fatalf("problems %v", problems)
	}
}

// A bad key in a grouped file is skipped and reported; the rest loads.
func TestBindingsFileBadKeySkipped(t *testing.T) {
	r, dir := exportFixture(t)
	writeConfig(t, dir, BindingsFile, `{"bindings": [{"context": "app/normal", "bindings": {"a": "test.beta", "q": 7, "w": "test.nope"}}]}`)
	problems := r.ReloadUserBindings()
	if len(problems) != 2 || !strings.Contains(problems[0], "bindings.json: app/normal q: want an action string") || !strings.Contains(problems[1], `bindings.json: app/normal w: unknown operation "test.nope" (skipped)`) {
		t.Fatalf("problems %q", problems)
	}
}

// settings.set edits settings.json in place: the user's comments, layout
// and trailing comma stay; a changed value is replaced where it stands, a
// new one appended at the file's indent.
func TestSettingsWriteKeepsComments(t *testing.T) {
	r, dir := exportFixture(t)
	writeConfig(t, dir, SettingsFile, "// my settings\n{\n    // which pager\n    \"pager\": \"more\", // less is more\n    /* rows */\n    \"lines\": 30,\n}\n")
	r.Settings().Reload()
	if _, err := r.Execute(Request{Noun: "ochazuke.settings", Verb: "set", Args: Arguments{"name": "pager", "value": "less"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.settings", Verb: "set", Args: Arguments{"name": "delay", "value": "1s"}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, SettingsFile))
	if err != nil {
		t.Fatal(err)
	}
	want := "// my settings\n{\n    // which pager\n    \"pager\": \"less\", // less is more\n    /* rows */\n    \"lines\": 30,\n    \"delay\": \"1s\"\n}\n"
	if string(data) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", data, want)
	}
	if got, _ := r.Settings().Value("lines"); fmt.Sprint(got) != "30" {
		t.Fatalf("lines %v", got)
	}
}

// BenchmarkStandardizeJSONC is the cost of reading a 100 KB config file:
// plain (the fast path, json.Valid only), commented (hujson.Standardize),
// and hujson.Standardize on the plain file for comparison.
func BenchmarkStandardizeJSONC(b *testing.B) {
	var plain bytes.Buffer
	plain.WriteString("[\n")
	for i := 0; plain.Len() < 100<<10; i++ {
		if i > 0 {
			plain.WriteString(",\n")
		}
		fmt.Fprintf(&plain, `{"mode": "normal", "noun": "n%d", "keys": "ctrl+%d", "request": {"noun": "ochazuke.focus", "verb": "next", "args": {"delta": %d}}, "hint": "row %d"}`, i, i, i, i)
	}
	plain.WriteString("\n]\n")
	commented := bytes.ReplaceAll(plain.Bytes(), []byte(",\n"), []byte(", // a comment\n"))
	for _, tc := range []struct {
		name string
		data []byte
		run  func([]byte) error
	}{
		{"plain", plain.Bytes(), func(d []byte) error { _, err := StandardizeJSONC(d); return err }},
		{"commented", commented, func(d []byte) error { _, err := StandardizeJSONC(d); return err }},
		{"hujson-on-plain", plain.Bytes(), func(d []byte) error { _, err := standardizeAlways(d); return err }},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.SetBytes(int64(len(tc.data)))
			for b.Loop() {
				if err := tc.run(tc.data); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func standardizeAlways(data []byte) ([]byte, error) { return hujson.Standardize(data) }
