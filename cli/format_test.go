package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/unixsuperhero/ochazuke"
)

// formatRuntime is a tool with a noun that declares columns (pane), one
// whose rows are plain records (item), a state verb answering both as
// Lines, and a verb whose result is not rows.
func formatRuntime() *ochazuke.Runtime {
	r := ochazuke.NewRuntime()
	done := func(v any) ochazuke.AppHandler { return func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(v) } }
	panes := []map[string]any{
		{"id": "w1", "label": "work/claude", "cells": []string{"work/claude", "idle", "~/proj/ochazuke"}},
		{"id": "w2", "label": "notes", "cells": []string{"notes", "needs_input", "~/hub/o-plans/plans"}},
		{"id": "w3", "label": "bare row"},
	}
	r.DefineOwn("pane").Columns(
		ochazuke.Column{Title: "pane"},
		ochazuke.Column{Title: "state", Priority: 1},
		ochazuke.Column{Title: "cwd", Min: 8, Priority: 2},
	).Verb("list", done(panes), "List panes").Rows("pane").
		Verb("show", done(map[string]any{"id": "w1", "state": "idle"}), "Show a pane")
	items := []map[string]any{
		{"id": "a", "label": "alpha", "data": map[string]any{"size": 12, "path": "/a, b", "tags": []string{"x"}}},
		{"id": "b", "label": "beta", "data": map[string]any{"size": 3456, "path": "/b|c", "id": "dup"}},
	}
	r.Define("item").Verb("list", done(items), "List items").Rows("item").
		Verb("none", done([]map[string]any{}), "No items").Rows("item")
	r.Define("thing").Verb("state", done(Lines{
		Row{Noun: "pane", ID: "w1", Label: "work/claude", Cells: []string{"work/claude", "idle", "~/proj"}},
		Row{Noun: "item", ID: "a", Label: "alpha", Data: map[string]any{"note": "line one\nline \"two\""}},
		Row{Noun: "pane", ID: "w2", Label: "notes", Cells: []string{"notes", "done", "~"}},
	}), "Every list's rows").State()
	return r
}

// runFormat runs one command of the format runtime at width.
func runFormat(t *testing.T, width int, args ...string) (string, string, error) {
	t.Helper()
	var out, errs bytes.Buffer
	sh := New(formatRuntime(), "tool", strings.NewReader(""), &out, &errs)
	sh.Width = width
	err := sh.Run(context.Background(), args)
	return out.String(), errs.String(), err
}

// checkGolden compares got with testdata/name, rewriting it under -update.
func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (go test ./cli -run %s -update writes it)", err, t.Name())
	}
	if got != string(want) {
		t.Fatalf("%s differs:\n--- got\n%s--- want\n%s", name, got, want)
	}
}

// goldenFormat prints each command's output under its command line.
func goldenFormat(t *testing.T, format string, width int, commands ...[]string) string {
	t.Helper()
	var b strings.Builder
	for _, args := range commands {
		out, errs, err := runFormat(t, width, append(args, "--format", format)...)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		b.WriteString("$ tool " + strings.Join(args, " ") + " --format " + format + "\n" + out)
		if errs != "" {
			b.WriteString("[stderr] " + errs)
		}
	}
	return b.String()
}

var formatCommands = [][]string{{"pane", "list"}, {"item", "list"}, {"item", "none"}, {"thing", "state"}}

// TestFormatTable: declared columns over cells (a row without cells shows
// its label), record fields when the noun declares none (data's fields
// stand in for data, a clash is data.KEY, numbers align right), an empty
// result with columns is the header, a state's Lines are one table per
// noun under its name. No width (a pipe) cuts nothing.
func TestFormatTable(t *testing.T) {
	checkGolden(t, "format-table.golden", goldenFormat(t, "table", -1, formatCommands...))
}

// TestFormatMarkdown: the same tables as GitHub tables; a state's Lines
// are one table per noun under "## NOUN".
func TestFormatMarkdown(t *testing.T) {
	checkGolden(t, "format-markdown.golden", goldenFormat(t, "markdown", 20, formatCommands...))
}

// TestFormatMarkdownEscapes: a backslash, a pipe and line breaks in a
// cell cannot break the row; other control characters are spaces.
func TestFormatMarkdownEscapes(t *testing.T) {
	var b strings.Builder
	writeMarkdown(&b, []table{{cols: []ochazuke.Column{{Title: "a|b"}, {Title: "n", Align: ochazuke.AlignRight}}, rows: [][]string{{`x\|y`, "1\r\n2\n3\r4\t5"}}}})
	want := "| a\\|b | n |\n| --- | ---: |\n| x\\\\\\|y | 1<br>2<br>3<br>4 5 |\n"
	if b.String() != want {
		t.Fatalf("got  %q\nwant %q", b.String(), want)
	}
}

// TestFormatTOON: the same tables as TOON tabular arrays, one header per
// noun.
func TestFormatTOON(t *testing.T) {
	checkGolden(t, "format-toon.golden", goldenFormat(t, "toon", 20, formatCommands...))
}

// TestFormatTOONEscapes: a comma, a quote or a line break quotes a value
// (titles and the noun too) with \\ \" \n \r inside; a backslash alone, a
// number-like string and an empty value stay bare.
func TestFormatTOONEscapes(t *testing.T) {
	var b strings.Builder
	writeTOON(&b, []table{
		{noun: "n,1", cols: []ochazuke.Column{{Title: "a,b"}, {Title: "c"}}, rows: [][]string{{`say "hi"\`, "1\r\n2"}, {`C:\x`, ""}, {"007", "x y"}}},
		{cols: []ochazuke.Column{{Title: "only"}}},
	})
	want := `"n,1"[3]{"a,b",c}:` + "\n" +
		`  "say \"hi\"\\","1\r\n2"` + "\n" +
		`  C:\x,` + "\n" +
		`  007,x y` + "\n" +
		`[0]{only}:` + "\n"
	if b.String() != want {
		t.Fatalf("got\n%s\nwant\n%s", b.String(), want)
	}
}

// TestFormatTableWidth: at a terminal's width cells cut with "…" and the
// columns drop by Priority (cwd first, then state); without one nothing
// is cut however long the line.
func TestFormatTableWidth(t *testing.T) {
	var b strings.Builder
	for _, width := range []int{40, 30, 16} {
		out, _, err := runFormat(t, width, "pane", "list", "--format", "table")
		if err != nil {
			t.Fatal(err)
		}
		b.WriteString(fmt.Sprintf("-- width %d\n", width) + out)
	}
	checkGolden(t, "format-table-width.golden", b.String())
	for _, line := range strings.Split(b.String(), "\n") {
		if strings.HasPrefix(line, "--") {
			continue
		}
		if n := len([]rune(line)); n > 40 {
			t.Fatalf("line over the widest width: %q", line)
		}
	}
	// No terminal: Stdout is a buffer, Width 0 detects none and cuts nothing.
	out, _, err := runFormat(t, 0, "pane", "list", "--format", "table")
	if err != nil || !strings.Contains(out, "~/hub/o-plans/plans") || strings.Contains(out, "…") {
		t.Fatalf("no tty cut: %v\n%s", err, out)
	}
}

// TestFormatFallsBackToJSON: a result that is not rows prints as JSON
// with a note on stderr.
func TestFormatFallsBackToJSON(t *testing.T) {
	out, errs, err := runFormat(t, -1, "pane", "show", "--format", "table")
	if err != nil {
		t.Fatal(err)
	}
	if out != "{\n  \"id\": \"w1\",\n  \"state\": \"idle\"\n}\n" {
		t.Fatalf("stdout %q", out)
	}
	if errs != "note: pane.show does not answer rows; printing JSON instead of --format table\n" {
		t.Fatalf("stderr %q", errs)
	}
}

// TestFormatFlags: json is the default and --format json prints the same
// bytes; --format=VALUE works; an unknown format is an error; a bare
// --json agrees only with json; --json ARGS passes arguments next to any
// --format; an operation with its own format parameter keeps --format.
func TestFormatFlags(t *testing.T) {
	plain, _, err := runFormat(t, -1, "pane", "list")
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"--format", "json"}, {"--format=json"}, {"--json"}, {"--json", "--format", "json"}, {"--json", "{}"}} {
		out, _, err := runFormat(t, -1, append([]string{"pane", "list"}, args...)...)
		if err != nil || out != plain {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	table, _, err := runFormat(t, -1, "pane", "list", "--format=table")
	if err != nil || !strings.HasPrefix(table, "pane ") {
		t.Fatalf("--format=table: %v\n%s", err, table)
	}
	if got, _, err := runFormat(t, -1, "pane", "list", "--json", "{}", "--format", "table"); err != nil || got != table {
		t.Fatalf("--json ARGS --format table: %v\n%s", err, got)
	}
	if got, _, err := runFormat(t, -1, "pane", "list", "--format", "table", "--json", `{}`); err != nil || got != table {
		t.Fatalf("--format table --json ARGS: %v\n%s", err, got)
	}
	if _, _, err := runFormat(t, -1, "pane", "list", "--format", "yaml"); err == nil || !strings.Contains(err.Error(), `--format "yaml": one of json, table, markdown, toon`) {
		t.Fatalf("unknown format: %v", err)
	}
	if _, _, err := runFormat(t, -1, "pane", "list", "--json", "--format", "table"); err == nil || !strings.Contains(err.Error(), "bare --json") {
		t.Fatalf("bare --json with table: %v", err)
	}
	if _, _, err := runFormat(t, -1, "pane", "list", "--format"); err == nil || !strings.Contains(err.Error(), "--format needs a value") {
		t.Fatalf("--format without a value: %v", err)
	}

	r := ochazuke.NewRuntime()
	r.Define("dump").Verb("run", func(c ochazuke.Ctx) ochazuke.Result {
		return ochazuke.Done([]map[string]any{{"format": ochazuke.StringArg(c.Args, "format", "")}})
	}, "Dump")
	op := r.Operations["dump.run"]
	op.Parameters = map[string]ochazuke.Parameter{"format": {Type: "string"}}
	r.Operations["dump.run"] = op
	var out bytes.Buffer
	if err := New(r, "tool", strings.NewReader(""), &out, &out).Run(context.Background(), []string{"dump", "run", "--format", "table"}); err != nil {
		t.Fatal(err)
	}
	if out.String() != "[\n  {\"format\":\"table\"}\n]\n" {
		t.Fatalf("own format parameter: %q", out.String())
	}
}
