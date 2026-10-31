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
	"reflect"
	"slices"
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

func TestShellListsAllBindingTables(t *testing.T) {
	f := newFixture(t)
	f.r.Define("detached").Mode("insert").
		Bind("ctrl+x", ochazuke.Do("ochazuke.key.ignore").Guard("input"))
	if err := f.r.Bind(ochazuke.Binding{
		Instance: "w2", Mode: "insert", Keys: "ctrl+z",
		Request: ochazuke.Request{Noun: "ochazuke.key", Verb: "ignore"},
	}); err != nil {
		t.Fatal(err)
	}
	current, _, err := f.run("", "bindings", "list")
	if err != nil {
		t.Fatal(err)
	}
	explicit, _, err := f.run("", "bindings", "list", "--all=false")
	if err != nil || explicit != current {
		t.Fatalf("--all=false changed the active listing: %v\n%s", err, explicit)
	}
	out, _, err := f.run("", "bindings", "list", "--all")
	if err != nil {
		t.Fatal(err)
	}
	var rows []ochazuke.Binding
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatal(err)
	}
	var detached, instance bool
	for _, row := range rows {
		if row.Noun == "detached" && row.Mode == "insert" && row.Keys == "ctrl+x" {
			detached = row.Instance == "" && row.When == "input" && row.Request.Key() == "ochazuke.key.ignore"
		}
		if row.Instance == "w2" && row.Mode == "insert" && row.Keys == "ctrl+z" {
			instance = row.Noun == "" && row.Request.Key() == "ochazuke.key.ignore"
		}
	}
	if !detached || !instance {
		t.Fatalf("missing inactive noun or instance bindings:\n%s", out)
	}
	if strings.Contains(current, "ctrl+x") || strings.Contains(current, "ctrl+z") {
		t.Fatalf("inactive bindings leaked into the default listing:\n%s", current)
	}
	if _, err := f.r.Execute(ochazuke.Request{Noun: "ochazuke.mode", Verb: "set", Args: ochazuke.Arguments{"mode": "insert"}}); err != nil {
		t.Fatal(err)
	}
	again, _, err := f.run("", "bindings", "list", "--all")
	if err != nil || again != out {
		t.Fatalf("all tables depend on the active mode or map iteration: %v", err)
	}
	if _, err := f.r.LoadBindings([]byte(out)); err != nil {
		t.Fatalf("export is not loadable binding JSON: %v", err)
	}
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

// TestShellBareJSONAsksForJSONOutput: --json with no value (last, or before
// another --flag) is the output format, which the shell always prints, so it
// is accepted and ignored; --json '{...}' stays the arguments, directly and
// through the oden mount.
func TestShellBareJSONAsksForJSONOutput(t *testing.T) {
	for _, args := range [][]string{
		{"widget", "rename", "--target", "w1", "--name", "uno", "--json"},
		{"widget", "rename", "--json", "--target", "w1", "--name", "uno"},
		{"widget", "rename", "--json", "--target", "w1", "uno"},
	} {
		f := newFixture(t)
		out, errOut, err := f.run("", args...)
		if err != nil || len(f.calls) != 1 || f.calls[0].Args["name"] != "uno" || len(f.calls[0].Args) != 1 {
			t.Fatalf("%s: %v %+v\n%s", strings.Join(args, " "), err, f.calls, errOut)
		}
		var got struct{ Target string }
		if err := json.Unmarshal([]byte(out), &got); err != nil || got.Target != "w1" {
			t.Fatalf("%s: stdout %q (%v)", strings.Join(args, " "), out, err)
		}
	}
	f := newFixture(t)
	if _, errOut, err := f.run("", "widget", "rename", "--json", `{"name":"dos"}`, "--target", "w2"); err != nil || len(f.calls) != 1 || f.calls[0].Args["name"] != "dos" {
		t.Fatalf("--json arguments: %v %+v\n%s", err, f.calls, errOut)
	}
	if _, _, err := f.run("", "widget", "rename", "--target", "w1", "--json", "uno"); err == nil {
		t.Fatal("--json WORD is still the arguments and must fail to parse")
	}
	f = newFixture(t)
	var out bytes.Buffer
	app := oden.New("tool", []string{"widget", "rename", "--target", "w1", "--name", "tres", "--json"},
		oden.WithStdio(strings.NewReader(""), &out, io.Discard), oden.WithPath(t.TempDir()))
	Mount(app, New(f.r, "tool", strings.NewReader(""), &out, io.Discard))
	if err := app.Run(context.Background()); err != nil || len(f.calls) != 1 || f.calls[0].Args["name"] != "tres" {
		t.Fatalf("mounted bare --json: %v %+v\n%s", err, f.calls, out.String())
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
	// widget.refresh is quiet: a success prints no job line.
	if errOut.Len() != 0 || settles < 2 {
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

// TestShellJobLinesFollowQuiet: the shell reports a job's end on stderr as
// the jobs ticker would show it: a quiet verb's success prints nothing, any
// other job prints its line, and a failure prints even for a quiet verb.
func TestShellJobLinesFollowQuiet(t *testing.T) {
	f := newFixture(t)
	f.r.Define("widget").
		Verb("sync", func(ochazuke.Ctx) ochazuke.Result {
			return ochazuke.Pending(func(context.Context) (any, error) { return "synced", nil })
		}, "Sync a widget off the loop").
		Verb("probe", func(ochazuke.Ctx) ochazuke.Result {
			return ochazuke.Pending(func(context.Context) (any, error) { return nil, errors.New("probe lost") })
		}, "Probe a widget, quietly").Quiet()
	for _, c := range []struct {
		verb, stderr string
		fails        bool
	}{
		{"refresh", "", false},
		{"sync", "job #2 widget.sync done\n", false},
		{"break", "job #3 widget.break failed\n", true},
		{"probe", "job #4 widget.probe failed\n", true},
	} {
		_, errOut, err := f.run("", "widget", c.verb, "--target", "w1")
		if (err != nil) != c.fails || errOut != c.stderr {
			t.Errorf("%s: err %v, stderr %q, want %q", c.verb, err, errOut, c.stderr)
		}
	}
}

// TestShellKeepsTheHandlersKeyOrder: a verb's result prints its keys in the
// order its value marshals in (a struct's field order, not sorted), whether
// the verb answers at once (Done, through HandleCall's copy) or off the
// loop (Pending, the delivered value), and so does each element of a list.
func TestShellKeepsTheHandlersKeyOrder(t *testing.T) {
	type report struct {
		Zeta  string `json:"zeta"`
		Alpha int    `json:"alpha"`
		Mid   bool   `json:"mid"`
	}
	value := report{Zeta: "z", Alpha: 1, Mid: true}
	f := newFixture(t)
	f.r.Define("widget").
		Verb("report", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(value) }, "Report at once").
		Verb("later", func(ochazuke.Ctx) ochazuke.Result {
			return ochazuke.Pending(func(context.Context) (any, error) { return value, nil })
		}, "Report off the loop").
		Verb("reports", func(ochazuke.Ctx) ochazuke.Result {
			return ochazuke.Done([]report{value, value})
		}, "Report a list at once")
	want := "{\n  \"zeta\": \"z\",\n  \"alpha\": 1,\n  \"mid\": true\n}\n"
	for _, verb := range []string{"report", "later"} {
		if out, errOut, err := f.run("", "widget", verb, "--target", "w1"); err != nil || out != want {
			t.Errorf("%s: %v\n%s\nstdout %q, want %q", verb, err, errOut, out, want)
		}
	}
	line := `{"zeta":"z","alpha":1,"mid":true}`
	if out, _, err := f.run("", "widget", "reports", "--target", "w1"); err != nil || out != "[\n  "+line+",\n  "+line+"\n]\n" {
		t.Errorf("reports: %v, stdout %q", err, out)
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
	// A bad argument is not the target's fault: no candidates are listed.
	if err := sh.Run(context.Background(), []string{"widget", "paint"}); err == nil || strings.Contains(err.Error(), "--target") {
		t.Fatalf("argument error names targets: %v", err)
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
	// tool-widget answers no catalog, so the widget noun merges it as a
	// pass-through: noun verbs (and their prefixes) still win.
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	marker := filepath.Join(t.TempDir(), "ran")
	dir := bins(t, map[string]string{"tool-widget": `case "$1" in catalog|commands) exit 1;; esac; touch ` + marker, "tool-web": `echo "web $*"; exit 7`})
	f := newFixture(t)
	if _, errOut, err := f.tool(dir, "wid", "sh", "--target", "w1"); err != nil || len(f.calls) != 1 {
		t.Fatalf("tool wid sh: %v %+v\n%s", err, f.calls, errOut)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the tool-widget bin ran instead of the noun")
	}
	out, _, err := f.tool(dir, "help")
	if err != nil || !strings.Contains(out, "merges external "+filepath.Join(dir, "tool-widget")) || !strings.Contains(out, "\n  catalog ") {
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
	if err := DeclareSide(f.r, CatalogSide{Name: "diff", Title: "Side diff", Key: "g", Command: []string{"{bin}", "view", "--repo", "{dir}"}}); err != nil {
		t.Fatal(err)
	}
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
	// The framework's own nouns are in it, their verbs builtin (the
	// outline's application verbs too); the golden keeps the test noun's
	// part so a framework verb does not churn it.
	seen := map[string]bool{}
	nouns := doc.Nouns[:0]
	for _, n := range doc.Nouns {
		seen[n.Name] = true
		for _, v := range n.Verbs {
			if v.Builtin != (n.Name != "widget") {
				t.Errorf("%s.%s builtin = %v", n.Name, v.Name, v.Builtin)
			}
		}
		if n.Name == "widget" {
			nouns = append(nouns, n)
		}
	}
	if !seen["ochazuke.confirm"] || !seen["ochazuke.history"] {
		t.Fatalf("framework nouns missing: %v", seen)
	}
	rows := doc.Bindings[:0]
	for _, b := range doc.Bindings {
		if b.Noun == "widget" {
			rows = append(rows, b)
		}
	}
	// Every framework noun is ochazuke.<noun> with its short name as an alias;
	// the golden keeps the test noun's part, so the aliases are checked here.
	if doc.Aliases["settings"] != "ochazuke.settings" || doc.Aliases["confirm"] != "ochazuke.confirm" || doc.CanonicalNoun("history") != "ochazuke.history" {
		t.Fatalf("aliases %v", doc.Aliases)
	}
	doc.Nouns, doc.Bindings, doc.Aliases, doc.Commands = nouns, rows, nil, nil
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

// The outline's verbs are application verbs and builtin; a tool's own verb
// on a framework noun is not builtin, and AppNouns keeps only the tool's.
func TestCatalogMarksBuiltinVerbs(t *testing.T) {
	r := ochazuke.NewRuntime()
	done := func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) }
	r.Define("ochazuke.history").Verb("capture-hook", done, "Capture a hook")
	r.Define("gadget").Verb("spin", done, "Spin a gadget")
	ochazuke.NewModel(r)
	out, err := Catalog(r, "tool", "1")
	if err != nil {
		t.Fatal(err)
	}
	var doc CatalogDoc
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	verb := func(noun, name string) CatalogVerb {
		n, _ := doc.noun(noun)
		for _, v := range n.Verbs {
			if v.Name == name {
				return v
			}
		}
		t.Fatalf("%s.%s not in the catalog", noun, name)
		return CatalogVerb{}
	}
	if v := verb("outline", "add"); !v.Builtin || v.Kind != ochazuke.KindApplication {
		t.Fatalf("outline.add = %+v", v)
	}
	if v := verb("history", "list"); !v.Builtin {
		t.Fatalf("history.list = %+v", v)
	}
	if v := verb("history", "capture-hook"); v.Builtin {
		t.Fatalf("history.capture-hook = %+v", v)
	}
	var got []string
	for _, n := range doc.AppNouns() {
		for _, v := range n.Verbs {
			got = append(got, n.Name+"."+v.Name)
		}
	}
	if want := []string{"gadget.spin", "ochazuke.history.capture-hook"}; !slices.Equal(got, want) {
		t.Fatalf("AppNouns = %v, want %v", got, want)
	}
}

// A noun whose verbs sort around the nouns nested under it (app.quit,
// app.rows.list, app.state, app.zz.list) is one catalog entry, and so is
// every other noun.
func TestCatalogListsEachNounOnce(t *testing.T) {
	r := ochazuke.NewRuntime()
	done := func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) }
	r.Define("app").Verb("state", done, "The app's state")
	r.Define("app.rows").Verb("list", done, "Rows")
	r.Define("app").Verb("zz", done, "Last")
	r.Define("app.zz2").Verb("list", done, "Rows")
	ochazuke.NewModel(r)
	out, err := Catalog(r, "tool", "1")
	if err != nil {
		t.Fatal(err)
	}
	var doc CatalogDoc
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, n := range doc.Nouns {
		if seen[n.Name] {
			t.Fatalf("noun %s listed twice", n.Name)
		}
		seen[n.Name] = true
	}
	app, _ := doc.noun("app")
	var verbs []string
	for _, v := range app.Verbs {
		verbs = append(verbs, v.Name)
	}
	if want := []string{"quit", "state", "zz"}; !slices.Equal(verbs, want) {
		t.Fatalf("app verbs %v, want %v", verbs, want)
	}
	if !seen["app.rows"] || !seen["ochazuke.state"] {
		t.Fatalf("nouns %v", seen)
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
			f.r.Define("ochazuke.binding")
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

func TestMountUnknownWordsReportNotFound(t *testing.T) {
	f := newFixture(t)
	dir := t.TempDir()
	_, errOut, err := f.tool(dir, "widgets", "list")
	if oden.ExitCode(err) != 1 || errOut != "tool: command \"widgets\" not found\ndid you mean: widget\n(tool --help lists every command)\n" {
		t.Fatalf("tool widgets: %v\n%s", err, errOut)
	}
	_, errOut, err = f.tool(dir, "widget", "fly")
	if oden.ExitCode(err) != 1 || !strings.HasPrefix(errOut, "tool widget: subcommand \"fly\" not found\n") || !strings.Contains(errOut, "List the widgets") || len(f.calls) != 0 {
		t.Fatalf("tool widget fly: %v %v\n%s", err, f.calls, errOut)
	}
}

func TestUsagePointsAtHelp(t *testing.T) {
	sh := New(newFixture(t).r, "tool", strings.NewReader(""), io.Discard, io.Discard)
	err := sh.Run(context.Background(), nil)
	want := "(tool help lists the commands; tool NOUN --help lists a noun's verbs and parameters)"
	if err == nil || !strings.HasSuffix(err.Error(), "\n"+want) || strings.Contains(err.Error(), "operations list") {
		t.Fatalf("usage: %v", err)
	}
}

// The user bindings file applies to the CLI: bindings list shows the
// override, and a skipped row is a warning on stderr, not a failure.
func TestShellAppliesTheUserBindingsFile(t *testing.T) {
	f := newFixture(t)
	dir := t.TempDir()
	f.r.Config().Dir = dir
	file := `[{"noun": "widget", "mode": "normal", "keys": "ctrl+t", "request": {"noun": "widget", "verb": "show"}},
		{"noun": "widget", "mode": "normal", "keys": "ctrl+u", "request": {"noun": "widget", "verb": "nope"}}]`
	if err := os.WriteFile(filepath.Join(dir, "bindings.json"), []byte(file), 0o600); err != nil {
		t.Fatal(err)
	}
	out, errOut, err := f.run("", "bindings", "list", "--all")
	if err != nil || !strings.Contains(out, `"ctrl+t"`) || !strings.Contains(errOut, `warning: bindings.json: row 2 ctrl+u: unknown operation "widget.nope" (skipped)`) {
		t.Fatalf("out %s\nstderr %s\nerr %v", out, errOut, err)
	}
	out, _, err = f.run("", "bindings", "path")
	if err != nil || !strings.Contains(out, filepath.Join(dir, "bindings.json")) {
		t.Fatalf("path %s %v", out, err)
	}
}

// settings set NAME VALUE takes bare words, validates and writes the file,
// after asking: a CLI process has no session, so the write goes to
// settings.json, and --yes (or y on stdin) confirms it.
func TestShellSetsASetting(t *testing.T) {
	f := newFixture(t)
	dir := t.TempDir()
	f.r.Config().Dir = dir
	if err := f.r.DeclareSetting(ochazuke.Setting{Name: "scrolloff", Type: ochazuke.SettingInteger, Default: 2, Description: "Rows around the focus"}); err != nil {
		t.Fatal(err)
	}
	if _, errOut, err := f.run("", "settings", "set", "scrolloff", "4"); err == nil || !strings.Contains(err.Error(), "declined") || !strings.Contains(errOut, "Write scrolloff = 4 to "+filepath.Join(dir, "settings.json")+"? [y/N]") {
		t.Fatalf("unanswered set: %s %v", errOut, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "settings.json")); err == nil {
		t.Fatal("a declined set wrote settings.json")
	}
	out, _, err := f.run("y\n", "settings", "set", "scrolloff", "5")
	if err != nil || !strings.Contains(out, `"value": 5`) {
		t.Fatalf("set: %s %v", out, err)
	}
	if _, _, err := f.run("", "settings", "set", "scrolloff", "many"); err == nil || !strings.Contains(err.Error(), "must be an integer") {
		t.Fatalf("bad value: %v", err)
	}
	if _, _, err := f.run("", "settings", "get", "nope"); err == nil {
		t.Fatal("got an unknown setting")
	}
	data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil || !strings.Contains(string(data), `"scrolloff": 5`) {
		t.Fatalf("file %s %v", data, err)
	}
	// reset asks too, --yes answers; with nothing in the file it does not ask.
	if _, _, err := f.run("", "settings", "reset", "scrolloff"); err == nil || !strings.Contains(err.Error(), "Remove scrolloff from") {
		t.Fatalf("unanswered reset: %v", err)
	}
	if out, _, err := f.run("", "settings", "reset", "scrolloff", "--yes"); err != nil || !strings.Contains(out, `"value": 2`) {
		t.Fatalf("reset --yes: %s %v", out, err)
	}
	if _, _, err := f.run("", "settings", "reset", "scrolloff"); err != nil {
		t.Fatalf("reset of an unset setting asked: %v", err)
	}
	// reset-all asks how many it resets; write has no session values here.
	if _, _, err := f.run("", "settings", "set", "scrolloff", "6", "-y"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.run("n\n", "settings", "reset-all"); err == nil || !strings.Contains(err.Error(), "Reset all 1 changed setting to their defaults in "+filepath.Join(dir, "settings.json")) {
		t.Fatalf("reset-all: %v", err)
	}
	if out, _, err := f.run("", "settings", "save"); err != nil || strings.TrimSpace(out) != "[]" {
		t.Fatalf("save with no session: %s %v", out, err)
	}
	if out, _, err := f.run("yes\n", "settings", "reset-all"); err != nil || !strings.Contains(out, "scrolloff") {
		t.Fatalf("reset-all yes: %s %v", out, err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "settings.json")); strings.Contains(string(data), "scrolloff") {
		t.Fatalf("reset-all left %s", data)
	}
}

// The :set line and the setting verbs from a CLI process: no session, so
// every write goes to settings.json after one question naming every change
// (-y answers it); a show prints its line on stderr and writes nothing.
func TestShellAppliesASetLine(t *testing.T) {
	f := newFixture(t)
	dir := t.TempDir()
	f.r.Config().Dir = dir
	if err := f.r.DeclareSetting(
		ochazuke.Setting{Name: "scrolloff", Type: ochazuke.SettingInteger, Default: 2, Description: "Rows around the focus"},
		ochazuke.Setting{Name: "wrap", Type: ochazuke.SettingBoolean, Default: true, Description: "Wrap lines"},
		ochazuke.Setting{Name: "paths", Type: ochazuke.SettingList, Default: []string{"a"}, Description: "Paths"},
	); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "settings.json")
	read := func() map[string]any {
		var got map[string]any
		data, err := os.ReadFile(file)
		if err != nil {
			return nil
		}
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	if _, errOut, err := f.run("", "settings", "apply", "scrolloff=5 nowrap paths+=b"); err == nil || !strings.Contains(errOut, "Write scrolloff = 5, wrap = false, paths = a,b to "+file+"? [y/N]") {
		t.Fatalf("unanswered apply: %s %v", errOut, err)
	}
	if read() != nil {
		t.Fatal("a declined line wrote settings.json")
	}
	out, _, err := f.run("", "settings", "apply", "scrolloff=5 nowrap paths+=b", "-y")
	if err != nil || !strings.Contains(out, `"changed"`) {
		t.Fatalf("apply -y: %s %v", out, err)
	}
	if got := read(); got["scrolloff"] != 5.0 || got["wrap"] != false || !reflect.DeepEqual(got["paths"], []any{"a", "b"}) {
		t.Fatalf("file %v", got)
	}
	if _, errOut, err := f.run("", "settings", "apply", "scrolloff? wrap?"); err != nil || !strings.Contains(errOut, "status: scrolloff=5  nowrap") {
		t.Fatalf("show: %s %v", errOut, err)
	}
	if _, errOut, err := f.run("", "settings", "apply", "verbose scrolloff?"); err != nil || !strings.Contains(errOut, "status: scrolloff=5 — set in "+file+" line ") {
		t.Fatalf("verbose show: %s %v", errOut, err)
	}
	if _, _, err := f.run("", "settings", "apply", "scrolloff=x wrap"); err == nil || !strings.Contains(err.Error(), "scrolloff takes a whole number: x; nothing was changed") {
		t.Fatalf("bad line: %v", err)
	}
	// The same through each verb directly.
	for _, c := range []struct {
		args []string
		key  string
		want any
	}{
		{[]string{"setting", "set", "scrolloff", "7"}, "scrolloff", 7.0},
		{[]string{"setting", "increase", "scrolloff", "3"}, "scrolloff", 10.0},
		{[]string{"setting", "decrease", "scrolloff", "4"}, "scrolloff", 6.0},
		{[]string{"setting", "multiply", "scrolloff", "2"}, "scrolloff", 12.0},
		{[]string{"setting", "toggle", "wrap"}, "wrap", true},
		{[]string{"setting", "cycle", "wrap"}, "wrap", false},
		{[]string{"setting", "append", "paths", "c,d"}, "paths", []any{"a", "b", "c", "d"}},
		{[]string{"setting", "prepend", "paths", "z"}, "paths", []any{"z", "a", "b", "c", "d"}},
		{[]string{"setting", "remove", "paths", "a,c"}, "paths", []any{"z", "b", "d"}},
		{[]string{"setting", "reset", "scrolloff"}, "scrolloff", nil},
	} {
		if _, errOut, err := f.run("", c.args...); err == nil || !strings.Contains(errOut, "? [y/N]") {
			t.Fatalf("%v did not ask: %s %v", c.args, errOut, err)
		}
		out, _, err := f.run("", append(c.args, "--yes")...)
		if err != nil || !strings.Contains(out, `"written": "file"`) {
			t.Fatalf("%v --yes: %s %v", c.args, out, err)
		}
		if got := read()[c.key]; !reflect.DeepEqual(got, c.want) {
			t.Fatalf("%v: file holds %v; want %v", c.args, got, c.want)
		}
	}
	if _, errOut, err := f.run("", "setting", "show", "paths", "--verbose"); err != nil || !strings.Contains(errOut, "status: paths=z,b,d — set in "+file) {
		t.Fatalf("setting show: %s %v", errOut, err)
	}
}

// config export DIR takes the directory as a bare word and --force as a
// flag; config path prints the directories.
func TestShellExportsTheConfig(t *testing.T) {
	f := newFixture(t)
	dir := t.TempDir()
	f.r.Config().Dir = dir
	out := filepath.Join(t.TempDir(), "ref")
	got, _, err := f.run("", "config", "export", out)
	if err != nil || !strings.Contains(got, filepath.Join(out, ochazuke.BindingsReferenceFile)) {
		t.Fatalf("export: %s %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(out, ochazuke.ThemesExampleFile)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.run("", "config", "export", dir); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("export over the live files: %v", err)
	}
	if _, _, err := f.run("", "config", "export", dir, "--force"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "settings.json")); err != nil {
		t.Fatal("--force did not write settings.json")
	}
	got, _, err = f.run("", "config", "path")
	if err != nil || !strings.Contains(got, filepath.Join(dir, ochazuke.ReferenceDir)) {
		t.Fatalf("path: %s %v", got, err)
	}
}

// A resolver maps a foreign id to its row; Reveal still loads the rows a
// resolver cannot find yet, and the confirm names the resolved row.
func TestShellResolveTargetComposesWithReveal(t *testing.T) {
	f := newFixture(t)
	var out, errOut bytes.Buffer
	sh := New(f.r, "tool", strings.NewReader(""), &out, &errOut)
	f.r.ResolveTarget = func(id string) (string, error) {
		if !strings.HasPrefix(id, "ext:") {
			return "", nil
		}
		row := "w" + strings.TrimPrefix(id, "ext:")
		if f.r.Instances[row] == nil {
			return "", fmt.Errorf("no row holds %s", id)
		}
		return row, nil
	}
	revealed := 0
	if err := sh.Run(context.Background(), []string{"widget", "show", "--target", "ext:2"}); err != nil || f.calls[0].Target != "w2" {
		t.Fatalf("resolved target: %v %+v", err, f.calls)
	}
	sh.Reveal = func(id string) error {
		revealed++
		return f.r.List("widgets/list").SetItems([]ochazuke.ListItem{{ID: "w1", Noun: "widget"}, {ID: "w7", Label: "seventh", Noun: "widget"}})
	}
	if err := sh.Run(context.Background(), []string{"widget", "show", "--target", "ext:1"}); err != nil || f.calls[1].Target != "w1" || revealed != 0 {
		t.Fatalf("a resolvable target skips Reveal: %v %+v, revealed %d", err, f.calls, revealed)
	}
	if err := sh.Run(context.Background(), []string{"widget", "remove", "--target", "ext:7", "--yes"}); err != nil || f.calls[2].Target != "w7" || revealed != 1 {
		t.Fatalf("Reveal then the resolver: %v %+v, revealed %d", err, f.calls, revealed)
	}
	sh.Reveal = nil
	if err := sh.Run(context.Background(), []string{"widget", "show", "--target", "ext:9"}); err == nil || !strings.Contains(err.Error(), `target "ext:9": no row holds ext:9`) {
		t.Fatalf("a resolver error is the refusal: %v", err)
	}
	if err := sh.Run(context.Background(), []string{"widget", "show", "--target", "nope"}); err == nil || !strings.Contains(err.Error(), `unknown target "nope"`) {
		t.Fatalf("an unresolved target: %v", err)
	}
}

// The user's actions.json verbs are commands: Mount installs them before
// oden sees the registry, so help lists them and dispatch runs them; the
// command's JSON output is the printed result and a bad entry is one
// warning.
func TestMountRunsUserActions(t *testing.T) {
	f := newFixture(t)
	dir := t.TempDir()
	f.r.Config().Dir = dir
	file := `[{"noun": "widget", "verb": "open", "description": "Open a widget with echo", "command": ["echo", "{\"opened\": \"{{target}}\", \"mode\": \"{{mode}}\"}"], "parameters": {"mode": {"type": "string", "enum": ["ro", "rw"]}}},
		{"noun": "nope", "verb": "x", "command": "true"}]`
	if err := os.WriteFile(filepath.Join(dir, "actions.json"), []byte(file), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (string, string) {
		t.Helper()
		var out, errOut bytes.Buffer
		app := oden.New("tool", args, oden.WithBuiltins(), oden.WithStdio(strings.NewReader(""), &out, &errOut), oden.WithPath(t.TempDir()))
		Mount(app, New(f.r, "tool", strings.NewReader(""), &out, &errOut))
		if err := app.Run(context.Background()); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, errOut.String())
		}
		return out.String(), errOut.String()
	}
	help, errOut := run("widget", "--help")
	if !strings.Contains(help, "Open a widget with echo") || !strings.Contains(errOut, `warning: actions.json: action 2 (nope.x): unknown noun "nope"`) {
		t.Fatalf("help %s\nstderr %s", help, errOut)
	}
	out, errOut := run("widget", "open", "--target", "w1", "--mode", "rw")
	if !strings.Contains(out, `"opened": "w1"`) || !strings.Contains(out, `"mode": "rw"`) || strings.Count(errOut, "warning:") != 0 {
		t.Fatalf("out %s\nstderr %s", out, errOut)
	}
}

// A framework noun is ochazuke.<noun>: the CLI reaches it nested (tool
// ochazuke settings names), as one dotted word (tool ochazuke.settings
// names), by the v0.16.0 spelling (tool app settings names) and by its
// short name (tool settings names); a noun of the application's own takes
// the short name and the framework's stays nested. The catalog lists both.
func TestShellAndMountReadNamespacedNouns(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	f := newFixture(t)
	dir := t.TempDir()
	answer := func(args ...string) string {
		t.Helper()
		out, errOut, err := f.tool(dir, args...)
		if err != nil {
			t.Fatalf("tool %s: %v\n%s", strings.Join(args, " "), err, errOut)
		}
		return out
	}
	want := answer("ochazuke", "settings", "names")
	for _, args := range [][]string{{"ochazuke.settings", "names"}, {"settings", "names"}, {"ochazuke", "settings", "na"}, {"app", "settings", "names"}, {"app.settings", "names"}} {
		if got := answer(args...); got != want {
			t.Fatalf("tool %s = %q, want %q", strings.Join(args, " "), got, want)
		}
		if got, _, err := f.run("", args...); err != nil || got != want {
			t.Fatalf("shell %s = %q (%v), want %q", strings.Join(args, " "), got, err, want)
		}
	}
	help := answer("ochazuke")
	if !strings.Contains(help, "settings") || strings.Contains(help, "quit") {
		t.Fatalf("tool ochazuke lists no nested nouns (or app's verbs):\n%s", help)
	}
	f.r.DefineOwn("settings").Verb("names", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done("own") }, "The tool's own settings")
	if got := answer("settings", "names"); strings.TrimSpace(got) != `"own"` {
		t.Fatalf("tool settings names = %q", got)
	}
	if got := answer("ochazuke", "settings", "names"); got != want {
		t.Fatalf("tool ochazuke settings names = %q, want the framework's %q", got, want)
	}
	out := answer("catalog")
	var doc CatalogDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	own, okOwn := doc.noun("settings")
	framework, okFramework := doc.noun("ochazuke.settings")
	if !okOwn || !okFramework || own.Name != "settings" || framework.Name != "ochazuke.settings" || len(own.Verbs) != 1 || own.Verbs[0].Builtin || len(framework.AppVerbs()) != 0 {
		t.Fatalf("catalog settings %+v / ochazuke.settings %+v", own, framework)
	}
	if _, aliased := doc.Aliases["settings"]; aliased || doc.Aliases["list"] != "ochazuke.list" {
		t.Fatalf("aliases %v", doc.Aliases)
	}
}

// A host mounting an extension's catalog names its nouns under the
// extension (Namespace), and Local reads them back.
func TestNamespaceAndLocal(t *testing.T) {
	ns := Namespace("ai")
	if got := ns("skills"); got != "ai.skills" {
		t.Fatalf("Namespace = %q", got)
	}
	if noun, ok := Local("ai", "ai.skills"); !ok || noun != "skills" {
		t.Fatalf("Local = %q %v", noun, ok)
	}
	if _, ok := Local("ai", "aix.skills"); ok {
		t.Fatal("Local read another namespace")
	}
}

// A framework shortcut is an alias; an app command owns its name in either order.
func TestMountAppCommandsWin(t *testing.T) {
	for _, name := range []string{"mcp", "view"} {
		for _, after := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/after=%t", name, after), func(t *testing.T) {
				f := newFixture(t)
				f.r.Config().Disable()
				var out bytes.Buffer
				sh := New(f.r, "tool", strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{\"protocolVersion\":\"2025-11-25\"}}\n"), &out, io.Discard)
				app := oden.New("tool", []string{name, "serve"}, oden.WithBuiltins(), oden.WithPath(""), oden.WithStdio(strings.NewReader(""), &out, io.Discard))
				own := oden.Command{Name: name, Summary: "app-owned", Run: func(_ context.Context, args []string) error {
					fmt.Fprintf(&out, "app-owned %s", strings.Join(args, " "))
					return nil
				}}
				if !after {
					app.Add(own)
				}
				Mount(app, sh)
				if after {
					app.Add(own)
				}
				if err := app.Run(context.Background()); err != nil || out.String() != "app-owned serve" {
					t.Fatalf("short name: %v %q", err, out.String())
				}
				out.Reset()
				app.Args = []string{"--help"}
				if err := app.Run(context.Background()); err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(out.String(), "app-owned") || !strings.Contains(out.String(), "ochazuke."+name) {
					t.Fatalf("help: %s", &out)
				}
				out.Reset()
				app.Args = []string{"catalog"}
				if err := app.Run(context.Background()); err != nil {
					t.Fatal(err)
				}
				var doc CatalogDoc
				if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
					t.Fatal(err)
				}
				if _, ok := doc.Aliases[name]; ok {
					t.Fatalf("catalog advertises shadowed alias %s", name)
				}
				if _, ok := doc.noun("ochazuke." + name); !ok {
					t.Fatalf("framework noun missing: %s", name)
				}
				found := false
				for _, command := range doc.Commands {
					if command.Name == name && command.Summary == "app-owned" {
						found = true
					}
					if command.Name == "ochazuke."+name && slices.Contains(command.Aliases, name) {
						t.Fatalf("command catalog advertises shadowed alias: %+v", command)
					}
				}
				if !found {
					t.Fatalf("app command missing from catalog: %+v", doc.Commands)
				}
				if name == "mcp" {
					out.Reset()
					app.Args = []string{"ochazuke", "mcp", "serve"}
					if err := app.Run(context.Background()); err != nil || !strings.Contains(out.String(), `"protocolVersion":"2025-11-25"`) {
						t.Fatalf("framework MCP: %v %s", err, &out)
					}
				}
			})
		}
	}
}

func TestMountAppNounsWin(t *testing.T) {
	for _, name := range []string{"mcp", "view"} {
		for _, catalogNoun := range []bool{false, true} {
			for _, beforeShell := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/catalog=%t/before-shell=%t", name, catalogNoun, beforeShell), func(t *testing.T) {
					f := newFixture(t)
					f.r.Config().Disable()
					var out bytes.Buffer
					var sh *Shell
					newShell := func() { sh = New(f.r, "tool", strings.NewReader(""), &out, io.Discard) }
					if !beforeShell {
						newShell()
					}
					handler := func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done("app-owned") }
					if catalogNoun {
						got := MountNouns(f.r, []CatalogNoun{{Name: name, Verbs: []CatalogVerb{{Name: "show"}}}}, HostMount{
							Local: func(n string) string { return n },
							Proxy: func(string, CatalogVerb) any { return handler },
						})
						if len(got.Warnings) > 0 {
							t.Fatal(got.Warnings)
						}
					} else {
						f.r.DefineOwn(name).Verb("show", handler)
					}
					if beforeShell {
						newShell()
					}
					app := oden.New("tool", []string{name, "show"}, oden.WithPath(""), oden.WithStdio(strings.NewReader(""), &out, io.Discard))
					Mount(app, sh)
					if command := app.Catalog().Lookup("ochazuke." + name); command == nil || slices.Contains(command.Aliases, name) {
						t.Fatalf("help must list the framework under its full name: %+v", command)
					}
					if err := app.Run(context.Background()); err != nil || strings.TrimSpace(out.String()) != `"app-owned"` {
						t.Fatalf("app noun: %v %s", err, &out)
					}
					out.Reset()
					if err := sh.Run(context.Background(), []string{name, "show"}); err != nil || strings.TrimSpace(out.String()) != `"app-owned"` {
						t.Fatalf("shell app noun: %v %s", err, &out)
					}
					if op, ok := f.r.Operation("ochazuke." + name + map[string]string{"mcp": ".tools", "view": ".scroll"}[name]); !ok || !op.Builtin {
						t.Fatal("framework verb was lost")
					}
				})
			}
		}
	}
}
