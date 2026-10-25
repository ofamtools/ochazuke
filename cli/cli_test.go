package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/unixsuperhero/ochazuke"
	"github.com/unixsuperhero/oden"
)

var update = flag.Bool("update", false, "rewrite testdata/catalog.golden")

// fixture is a runtime with one test noun, widget, on a page with a list of
// two rows, and the requests its verbs saw.
type fixture struct {
	r     *ochazuke.Runtime
	calls []ochazuke.Request
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{r: ochazuke.NewRuntime()}
	record := func(c ochazuke.Ctx) ochazuke.Result {
		if c.Instance == nil || c.Instance.Noun != "widget" {
			return ochazuke.Failed(errors.New("no widget"))
		}
		id := c.Instance.ID
		f.calls = append(f.calls, ochazuke.Request{Noun: "widget", Target: id, Args: c.Args})
		return ochazuke.Done(map[string]any{"target": id, "args": c.Args})
	}
	f.r.Define("widget").Pin("normal").
		Verb("list", func(ochazuke.Ctx) ochazuke.Result {
			return ochazuke.Done([]map[string]any{{"id": "w1"}, {"id": "w2"}})
		}, "List the widgets").Rows("widget").
		Verb("state", func(ochazuke.Ctx) ochazuke.Result {
			return ochazuke.Done(Lines{Row{Noun: "widget", ID: "w1"}, Row{Noun: "widget", ID: "w2"}})
		}, "Every list's rows in one call").State().
		Verb("show", record, "Show a widget").
		Verb("rename", record, "Rename a widget").
		Verb("paint", record, "Paint a widget").
		Verb("link", record, "Link a widget by name").Positional("name").
		Verb("tag", record, "Tag a widget").
		Verb("remove", record, "Remove a widget").Confirm("Remove {{label}}?").
		Verb("refresh", func(ochazuke.Ctx) ochazuke.Result {
			return ochazuke.Pending(func(context.Context) (any, error) { return "fresh", nil })
		}, "Reload a widget").Quiet().
		Verb("break", func(ochazuke.Ctx) ochazuke.Result {
			return ochazuke.Pending(func(context.Context) (any, error) { return nil, errors.New("disk on fire") })
		}, "Fail off the loop").
		Verb("ask", func(c ochazuke.Ctx) ochazuke.Result {
			_, err := c.Runtime.OpenModal("question", ochazuke.Payload{Prompt: "Name?"})
			if err != nil {
				return ochazuke.Failed(err)
			}
			return ochazuke.Done(nil)
		}, "Ask in a modal").
		Mode("normal").Bind("enter", ochazuke.Do("widget.show")).Bind("x", ochazuke.Do("widget.remove"))
	op := f.r.Operations["widget.rename"]
	op.Parameters = map[string]ochazuke.Parameter{
		"name":  {Type: "string", Required: true},
		"count": {Type: "integer"},
		"loud":  {Type: "boolean"},
		"tags":  {Type: "array"},
		"like":  {Type: "string", Source: "widget.list"},
	}
	f.r.Operations["widget.rename"] = op
	op = f.r.Operations["widget.paint"]
	op.Parameters = map[string]ochazuke.Parameter{"color": {Type: "string", Required: true}, "coats": {Type: "integer"}}
	f.r.Operations["widget.paint"] = op
	for _, verb := range []string{"widget.link", "widget.tag"} {
		op = f.r.Operations[verb]
		op.Parameters = map[string]ochazuke.Parameter{"all": {Type: "boolean"}, "name": {Type: "string"}, "aim": {Type: "string"}}
		f.r.Operations[verb] = op
	}
	page, err := f.r.AddPage("widgets", "Widgets", func(*ochazuke.Runtime, int, int) ochazuke.Block { return ochazuke.Block{} })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.r.AddList("widgets/list", []ochazuke.ListItem{{ID: "w1", Label: "one", Noun: "widget"}, {ID: "w2", Label: "two", Noun: "widget"}}, ochazuke.Parent(page.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.r.ValidateBindings(); err != nil {
		t.Fatal(err)
	}
	return f
}

// run runs one shell command with stdin.
func (f *fixture) run(stdin string, args ...string) (string, string, error) {
	var out, errOut bytes.Buffer
	err := New(f.r, "tool", strings.NewReader(stdin), &out, &errOut).Run(context.Background(), args)
	return out.String(), errOut.String(), err
}

func TestShellTypesFlagsByTheOperationsParameters(t *testing.T) {
	f := newFixture(t)
	out, errOut, err := f.run("", "widget", "rename", "--target", "w1", "--name", "uno", "--count=3", "--loud", "--tags", `["a","b"]`)
	if err != nil {
		t.Fatalf("rename: %v\n%s", err, errOut)
	}
	args := f.calls[0].Args
	if f.calls[0].Target != "w1" || args["name"] != "uno" || args["count"] != 3 || args["loud"] != true || len(args["tags"].([]any)) != 2 {
		t.Fatalf("call = %+v", f.calls[0])
	}
	var got struct{ Target string }
	if err := json.Unmarshal([]byte(out), &got); err != nil || got.Target != "w1" {
		t.Fatalf("stdout %q (%v)", out, err)
	}
	if _, _, err := f.run("", "widget", "rename", "--target", "w1", "--count", "many"); err == nil || !strings.Contains(err.Error(), "--count: ") {
		t.Fatalf("bad integer: %v", err)
	}
}

// Positional("name") puts name first, ahead of aim and the boolean all;
// without it the order is required, then by name, and a boolean takes no
// bare word unless Positional names it.
func TestShellPositionalOrderIsDeclared(t *testing.T) {
	f := newFixture(t)
	if got := f.r.Operations["widget.link"].ParameterOrder(); fmt.Sprint(got) != "[name aim all]" {
		t.Fatalf("link order %v", got)
	}
	if got := f.r.Operations["widget.tag"].ParameterOrder(); fmt.Sprint(got) != "[aim all name]" {
		t.Fatalf("tag order %v", got)
	}
	for _, c := range []struct {
		args []string
		want ochazuke.Arguments
	}{
		{[]string{"widget", "link", "foo"}, ochazuke.Arguments{"name": "foo"}},
		{[]string{"widget", "link", "foo", "up"}, ochazuke.Arguments{"name": "foo", "aim": "up"}},
		{[]string{"widget", "tag", "up", "foo"}, ochazuke.Arguments{"aim": "up", "name": "foo"}},
		{[]string{"widget", "tag", "up", "--all"}, ochazuke.Arguments{"all": true, "aim": "up"}},
	} {
		f.calls = nil
		if _, errOut, err := f.run("", append(c.args, "--target", "w1")...); err != nil || len(f.calls) != 1 {
			t.Fatalf("%v: %v\n%s", c.args, err, errOut)
		}
		if got := f.calls[0].Args; fmt.Sprint(got) != fmt.Sprint(c.want) {
			t.Fatalf("%v: args = %#v, want %#v", c.args, got, c.want)
		}
	}
	if _, _, err := f.run("", "widget", "tag", "--target", "w1", "a", "b", "c"); err == nil || err.Error() != `unexpected argument "c"; widget.tag takes 2 positional argument(s): aim, name` {
		t.Fatalf("a boolean took a bare word: %v", err)
	}
}

func TestShellPositionalArguments(t *testing.T) {
	f := newFixture(t)
	for _, c := range []struct {
		args []string
		want ochazuke.Arguments
	}{
		{[]string{"red", "2"}, ochazuke.Arguments{"color": "red", "coats": 2}},
		{[]string{"red"}, ochazuke.Arguments{"color": "red"}},
		// A named flag wins; the words fill what is left, wherever the flag is.
		{[]string{"3", "--color", "blue"}, ochazuke.Arguments{"color": "blue", "coats": 3}},
		{[]string{"--json", `{"coats":1}`, "green"}, ochazuke.Arguments{"color": "green", "coats": float64(1)}},
	} {
		f.calls = nil
		args := append([]string{"widget", "paint", "--target", "w1"}, c.args...)
		if _, errOut, err := f.run("", args...); err != nil || len(f.calls) != 1 {
			t.Fatalf("%v: %v\n%s", c.args, err, errOut)
		}
		if got := f.calls[0].Args; len(got) != len(c.want) || fmt.Sprint(got) != fmt.Sprint(c.want) {
			t.Fatalf("%v: args = %#v, want %#v", c.args, got, c.want)
		}
	}
	if _, _, err := f.run("", "widget", "paint", "--target", "w1", "red", "2", "extra"); err == nil || err.Error() != `unexpected argument "extra"; widget.paint takes 2 positional argument(s): color, coats` {
		t.Fatalf("too many: %v", err)
	}
	if _, _, err := f.run("", "widget", "paint", "--target", "w1", "--coats", "2", "red", "x"); err == nil || err.Error() != `unexpected argument "x"; widget.paint takes 1 positional argument(s): color` {
		t.Fatalf("too many after a flag: %v", err)
	}
	if _, _, err := f.run("", "widget", "paint", "--target", "w1", "red", "many"); err == nil || !strings.HasPrefix(err.Error(), "coats (argument 2): ") {
		t.Fatalf("bad integer: %v", err)
	}
}

func TestShellJSONArgumentsAndHistorySource(t *testing.T) {
	f := newFixture(t)
	if _, errOut, err := f.run("", "widget", "rename", "--target", "w2", "--json", `{"name":"dos","count":2}`); err != nil {
		t.Fatalf("rename: %v\n%s", err, errOut)
	}
	if a := f.calls[0].Args; a["name"] != "dos" || a["count"] != float64(2) {
		t.Fatalf("args = %+v", a)
	}
	out, _, err := f.run("", "history", "list")
	if err != nil {
		t.Fatal(err)
	}
	var entries []ochazuke.HistoryEntry
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		found = found || (e.Request.Key() == "widget.rename" && e.Context.Source == "cli")
	}
	if !found {
		t.Fatalf("history lacks the cli rename: %+v", entries)
	}
	// A list result prints one compact element per line.
	if !strings.HasPrefix(out, "[\n  {") || strings.Count(out, "\n") != len(entries)+2 {
		t.Fatalf("list output:\n%s", out)
	}
}

func TestShellVerbPrefix(t *testing.T) {
	f := newFixture(t)
	if _, errOut, err := f.run("", "widget", "sh", "--target", "w1"); err != nil || len(f.calls) != 1 {
		t.Fatalf("widget sh: %v %+v\n%s", err, f.calls, errOut)
	}
	if _, _, err := f.run("", "widget", "re", "--target", "w1"); err == nil || err.Error() != "widget re is ambiguous: refresh, remove, rename" {
		t.Fatalf("widget re: %v", err)
	}
}

func TestShellConfirm(t *testing.T) {
	f := newFixture(t)
	_, errOut, err := f.run("n\n", "widget", "remove", "--target", "w1")
	if err == nil || err.Error() != "declined: Remove one?" || !strings.Contains(errOut, "Remove one? [y/N]") {
		t.Fatalf("declined: %v %q", err, errOut)
	}
	if len(f.calls) != 0 || f.r.TopModal() != "" {
		t.Fatalf("calls %+v, modal %q", f.calls, f.r.TopModal())
	}
	if _, _, err := f.run("", "widget", "remove", "--target", "w1"); err == nil || len(f.calls) != 0 {
		t.Fatalf("end of input: %v %+v", err, f.calls)
	}
	if _, errOut, err := f.run("YES\n", "widget", "remove", "--target", "w1"); err != nil || len(f.calls) != 1 {
		t.Fatalf("stdin yes: %v %+v\n%s", err, f.calls, errOut)
	}
	if _, errOut, err := f.run("", "widget", "remove", "--target", "w2", "--yes"); err != nil || len(f.calls) != 2 || strings.Contains(errOut, "[y/N]") {
		t.Fatalf("--yes: %v %+v %q", err, f.calls, errOut)
	}
}

func TestShellSettlesPendingWork(t *testing.T) {
	f := newFixture(t)
	settles := 0
	var out, errOut bytes.Buffer
	sh := New(f.r, "tool", strings.NewReader(""), &out, &errOut)
	sh.Settle = func() error { settles++; return nil }
	if err := sh.Run(context.Background(), []string{"widget", "refresh", "--target", "w1"}); err != nil {
		t.Fatalf("refresh: %v\n%s", err, errOut.String())
	}
	if !strings.Contains(errOut.String(), "job #1 widget.refresh done") || settles < 2 {
		t.Fatalf("stderr %q, settles %d", errOut.String(), settles)
	}
	if jobs := f.r.Jobs(); len(jobs) != 1 || jobs[0].State != ochazuke.JobDone {
		t.Fatalf("jobs = %+v", jobs)
	}
	// The delivered value is the result, not the ticket.
	if out.String() != "\"fresh\"\n" {
		t.Fatalf("stdout %q", out.String())
	}
	out.Reset()
	errOut.Reset()
	err := sh.Run(context.Background(), []string{"widget", "break", "--target", "w1"})
	if err == nil || err.Error() != "disk on fire" || oden.ExitCode(err) != 1 || out.Len() != 0 || !strings.Contains(errOut.String(), "job #2 widget.break failed") {
		t.Fatalf("failed job: %v (exit %d), stdout %q, stderr %q", err, oden.ExitCode(err), out.String(), errOut.String())
	}
}

func TestSettleDeliversScheduledWork(t *testing.T) {
	f := newFixture(t)
	if _, err := f.r.Execute(ochazuke.Request{Verb: "refresh", Noun: "widget", Target: "w1"}); err != nil {
		t.Fatal(err)
	}
	rounds := 0
	if err := Settle(f.r, func() error { rounds++; return nil }); err != nil || rounds < 2 {
		t.Fatalf("settle: %v, rounds %d", err, rounds)
	}
	if jobs := f.r.Jobs(); len(jobs) != 1 || jobs[0].State != ochazuke.JobDone {
		t.Fatalf("jobs = %+v", jobs)
	}
	if _, err := f.r.Execute(ochazuke.Request{Verb: "break", Noun: "widget", Target: "w1"}); err != nil {
		t.Fatal(err)
	}
	if err := Settle(f.r, nil); err == nil || err.Error() != "disk on fire" {
		t.Fatalf("failed job: %v", err)
	}
}

func TestShellOwnersRevealAndPrepare(t *testing.T) {
	f := newFixture(t)
	var out, errOut bytes.Buffer
	sh := New(f.r, "tool", strings.NewReader(""), &out, &errOut)
	// Two rows and no owner: the error names them.
	if err := sh.Run(context.Background(), []string{"widget", "show"}); err == nil || !strings.Contains(err.Error(), "pass --target, one of: w1, w2") {
		t.Fatalf("no target: %v", err)
	}
	sh.Owners = map[string]string{"widget": "w2"}
	if err := sh.Run(context.Background(), []string{"widget", "show"}); err != nil || f.calls[0].Target != "w2" {
		t.Fatalf("owner: %v %+v", err, f.calls)
	}
	prepared := 0
	sh.Prepare = func(context.Context) error { prepared++; return nil }
	sh.Reveal = func(id string) error {
		return f.r.List("widgets/list").SetItems([]ochazuke.ListItem{{ID: id, Label: "late", Noun: "widget"}})
	}
	if err := sh.Run(context.Background(), []string{"widget", "show", "--target", "w9"}); err != nil || f.calls[1].Target != "w9" || prepared != 1 {
		t.Fatalf("reveal: %v %+v, prepared %d", err, f.calls, prepared)
	}
}

func TestShellErrors(t *testing.T) {
	f := newFixture(t)
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"nope", "list"}, `unknown operation "nope.list"`},
		{[]string{"widget", "fly"}, `unknown operation "widget.fly"`},
		{[]string{"widget", "show", "--target", "w99"}, `unknown target "w99"`},
		{[]string{"widget", "rename", "--target", "w1", "--colour", "red"}, `widget.rename has no parameter "colour"`},
		{[]string{"widget", "rename", "--target", "w1", "--name"}, "--name needs a value"},
		{[]string{"widget", "show", "w1"}, `unexpected argument "w1"`},
		{[]string{"widget", "show", "--json", "[1]"}, "arguments"},
		{[]string{"widget", "ask", "--target", "w1"}, "widget.ask opened a question modal, which needs the TUI; pass its arguments instead (tool operations list)"},
	} {
		_, _, err := f.run("", c.args...)
		if err == nil || !strings.Contains(err.Error(), c.want) || oden.ExitCode(err) != 1 {
			t.Errorf("tool %s: err = %v (exit %d), want %q", strings.Join(c.args, " "), err, oden.ExitCode(err), c.want)
		}
	}
}

// bins writes executable scripts into a fresh PATH directory.
func bins(t *testing.T, scripts map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range scripts {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// tool runs one invocation of an oden app named tool with the fixture mounted.
func (f *fixture) tool(dir string, args ...string) (string, string, error) {
	var out, errOut bytes.Buffer
	app := oden.New("tool", args, oden.WithBuiltins(), oden.WithStdio(strings.NewReader(""), &out, &errOut), oden.WithPath(dir))
	sh := New(f.r, "tool", strings.NewReader(""), &out, &errOut)
	sh.Version = "1.2.3"
	Mount(app, sh)
	err := app.Run(context.Background())
	return out.String(), errOut.String(), err
}

func TestMountResolvesNounPrefixesAndShadowsBins(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	dir := bins(t, map[string]string{"tool-widget": "touch " + marker, "tool-web": `echo "web $*"; exit 7`})
	f := newFixture(t)
	if _, errOut, err := f.tool(dir, "wid", "sh", "--target", "w1"); err != nil || len(f.calls) != 1 {
		t.Fatalf("tool wid sh: %v %+v\n%s", err, f.calls, errOut)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the tool-widget bin ran instead of the noun")
	}
	out, _, err := f.tool(dir, "help")
	if err != nil || !strings.Contains(out, "shadows external "+filepath.Join(dir, "tool-widget")) || !strings.Contains(out, "\n  catalog ") {
		t.Fatalf("help: %v\n%s", err, out)
	}
	if out, _, err := f.tool(dir, "which", "widget"); err != nil || out != "widget inline noun\n" {
		t.Fatalf("which widget: %v %q", err, out)
	}
	if out, _, err := f.tool(dir, "web", "x"); oden.ExitCode(err) != 7 || out != "web x\n" {
		t.Fatalf("tool web: %v %q", err, out)
	}
	if _, errOut, err := f.tool(dir, "w", "show"); oden.ExitCode(err) != 1 || !strings.Contains(errOut, `"w" is ambiguous`) {
		t.Fatalf("tool w: %v %q", err, errOut)
	}
}

func TestCatalogGolden(t *testing.T) {
	f := newFixture(t)
	out, _, err := f.tool(t.TempDir(), "catalog")
	if err != nil {
		t.Fatal(err)
	}
	var doc CatalogDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Schema != 1 || doc.Name != "tool" || doc.Version != "1.2.3" || doc.State != "widget.state" {
		t.Fatalf("header = %d %q %q state %q", doc.Schema, doc.Name, doc.Version, doc.State)
	}
	// The framework's own nouns are in it; the golden keeps the test
	// noun's part so a framework verb does not churn it.
	seen := map[string]bool{}
	nouns := doc.Nouns[:0]
	for _, n := range doc.Nouns {
		seen[n.Name] = true
		if n.Name == "widget" {
			nouns = append(nouns, n)
		}
	}
	if !seen["confirm"] || !seen["history"] {
		t.Fatalf("framework nouns missing: %v", seen)
	}
	rows := doc.Bindings[:0]
	for _, b := range doc.Bindings {
		if b.Noun == "widget" {
			rows = append(rows, b)
		}
	}
	doc.Nouns, doc.Bindings = nouns, rows
	// A pick list's source stays the producing runtime's noun.verb,
	// verbatim: a host runs it through the extension's bin.
	for _, v := range nouns[0].Verbs {
		if v.Name == "rename" && v.Params["like"].Source != "widget.list" {
			t.Fatalf("rename like source = %q", v.Params["like"].Source)
		}
	}
	got, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	golden := filepath.Join("testdata", "catalog.golden")
	if *update {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("catalog differs from %s (go test ./cli -update):\n%s", golden, got)
	}
}

func TestMountContextualHelpAndDispatch(t *testing.T) {
	for _, args := range [][]string{
		{"help"}, {"widget"}, {"widget", "-h"}, {"widget", "--help"},
		{"widget", "rename", "-h"}, {"widget", "rename", "--help"},
		{"binding"}, {"orphan"}, {"widget", "list"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			f := newFixture(t)
			f.r.Define("binding")
			op := f.r.Operations["widget.rename"]
			op.Parameters["name"] = ochazuke.Parameter{
				Type: "string", Required: true, Description: "Widget name",
				Enum: []string{"one", "two"}, Source: "widget.list", Free: true,
			}
			f.r.Operations["widget.rename"] = op
			orphan := f.r.Operations["widget.list"]
			orphan.Noun = "orphan"
			f.r.Operations["orphan.list"] = orphan
			var out bytes.Buffer
			app := oden.New("tool", args, oden.WithBuiltins(),
				oden.WithStdio(strings.NewReader(""), &out, io.Discard),
				oden.WithPath(t.TempDir()))
			sh := New(f.r, "tool", strings.NewReader(""), &out, io.Discard)
			prepared := 0
			executing := len(args) == 2 && args[1] == "list"
			sh.Prepare = func(context.Context) error {
				prepared++
				if !executing {
					return errors.New("help must not prepare the runtime")
				}
				return nil
			}
			Mount(app, sh)
			if err := app.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			text := out.String()
			if executing {
				if prepared != 1 || !strings.Contains(text, `"w1"`) || !strings.Contains(text, `"w2"`) {
					t.Fatalf("operation did not dispatch: prepared %d, output %s", prepared, text)
				}
				return
			}
			if prepared != 0 || len(f.calls) != 0 {
				t.Fatalf("help executed runtime: prepared %d, calls %v", prepared, f.calls)
			}
			if args[0] == "help" {
				for _, noun := range []string{"widget", "modal", "mode", "bindings", "history", "input", "app", "binding", "orphan"} {
					if !strings.Contains(text, noun) {
						t.Errorf("root help lacks %s: %s", noun, text)
					}
				}
				return
			}
			if args[0] != "widget" {
				return
			}
			for _, want := range []string{"Rename a widget", "name", "string", "Widget name", "one", "two", "widget.list"} {
				if !strings.Contains(text, want) {
					t.Errorf("contextual help lacks %q: %s", want, text)
				}
			}
			if strings.Contains(text, "Undo the last") || strings.Contains(text, "input undo") {
				t.Errorf("unrelated noun in help: %s", text)
			}
			if len(args) < 3 && !strings.Contains(text, "List the widgets") {
				t.Errorf("noun help lacks sibling verb: %s", text)
			}
			if len(args) == 3 && strings.Contains(text, "List the widgets") {
				t.Errorf("leaf help includes sibling verb: %s", text)
			}
		})
	}
}
