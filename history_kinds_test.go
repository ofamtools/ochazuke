package ochazuke

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// kindsRuntime is a page with a focused list of rows and a noun with a
// mutating verb, a declared fetch and a row source.
func kindsRuntime(t *testing.T) *Runtime {
	t.Helper()
	r := NewRuntime()
	r.Define("a")
	r.Define("w").
		Verb("remove", func(Ctx) Result { return Done(nil) }, "Remove a widget").
		Verb("show", func(Ctx) Result { return Done("data") }, "Show a widget").Fetch().
		Verb("rows", func(Ctx) Result { return Done(nil) }, "List the widgets").Rows("w")
	if err := r.RegisterPage(&Page{ID: "page", Title: "page", Render: func(*Runtime, int, int) Block { return Text("") }}); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("page"); err != nil {
		t.Fatal(err)
	}
	list, err := r.AddList("l", benchRows("r", 5), Parent("page"))
	if err != nil {
		t.Fatal(err)
	}
	if err := list.Focus(); err != nil {
		t.Fatal(err)
	}
	return r
}

func run(t *testing.T, r *Runtime, key string, args Arguments) {
	t.Helper()
	noun, verb := SplitKey(key)
	if _, err := r.Execute(Request{Noun: noun, Verb: verb, Args: args}); err != nil {
		t.Fatal(err)
	}
}

func historyKeys(entries []HistoryEntry) string {
	var keys []string
	for _, e := range entries {
		keys = append(keys, e.Kind+":"+e.Request.Key())
	}
	return strings.Join(keys, " ")
}

// Every request is one entry with a kind: a motion and a focus move are
// actions, a mutating verb a change, a declared fetch and a row source
// fetches. history.list shows actions and changes unless asked for kinds.
func TestHistoryEntriesHaveKinds(t *testing.T) {
	r := kindsRuntime(t)
	run(t, r, "app.list.move", Arguments{"delta": 1})
	run(t, r, "w.remove", nil)
	run(t, r, "w.show", nil)
	run(t, r, "w.rows", nil)
	run(t, r, "app.state.get", Arguments{"path": "/focus"})
	if got, want := historyKeys(r.History(0)), "action:app.list.move change:w.remove fetch:w.show fetch:w.rows fetch:app.state.get"; !strings.HasSuffix(got, want) {
		t.Fatalf("history %s, want it to end %s", got, want)
	}
	for arg, want := range map[string]string{
		"":             "action:app.list.move change:w.remove",
		"fetch":        "fetch:w.show fetch:w.rows fetch:app.state.get",
		"change,fetch": "change:w.remove fetch:w.show fetch:w.rows fetch:app.state.get",
		"all":          "action:app.list.move change:w.remove fetch:w.show fetch:w.rows fetch:app.state.get",
	} {
		out, err := r.Execute(Request{Noun: "app.history", Verb: "list", Args: Arguments{"kind": arg}})
		if err != nil {
			t.Fatal(err)
		}
		got := historyKeys(out.([]HistoryEntry))
		fetches := arg == "all" || strings.Contains(arg, "fetch")
		if !strings.Contains(got, want) || !fetches && strings.Contains(got, "fetch:") || arg == "fetch" && strings.Contains(got, "change:") {
			t.Fatalf("history.list kind %q: %s, want %s", arg, got, want)
		}
	}
	if _, err := r.Execute(Request{Noun: "app.history", Verb: "list", Args: Arguments{"kind": "nope"}}); err == nil {
		t.Fatal("an unknown kind must fail")
	}
}

// A host polling a fetch never pushes the user's entries out: fetches are
// bounded apart (HistoryFetchLimit). A fetch typed into the palette is the
// user's line: it stays with the user's entries and palette recall offers it.
func TestHistoryFetchesAreBoundedApart(t *testing.T) {
	r := kindsRuntime(t)
	run(t, r, "app.list.move", Arguments{"delta": 1})
	run(t, r, "w.remove", nil)
	for range 3 * HistoryFetchLimit {
		run(t, r, "w.show", nil)
	}
	if got := historyKeys(r.HistoryOf(2, historyUserKinds...)); got != "action:app.list.move change:w.remove" {
		t.Fatalf("the user's entries: %s", got)
	}
	if n := len(r.HistoryOf(0, HistoryFetch)); n != HistoryFetchLimit {
		t.Fatalf("fetches kept: %d", n)
	}
	runPaletteLine(t, r, Request{Noun: "w", Verb: "show"})
	for range 3 * HistoryFetchLimit {
		run(t, r, "w.show", nil)
	}
	typed := r.HistoryOf(0, HistoryFetch)
	if typed[0].Context.Source != "palette" || typed[0].Request.Key() != "w.show" {
		t.Fatalf("the typed fetch was pushed out: %+v", typed[0])
	}
	if lines := NewPalette(r).recallLines(); len(lines) == 0 || !strings.Contains(lines[0], "show") {
		t.Fatalf("recall: %q", lines)
	}
}

// Fetches stay out of the history file unless history-fetches is on; a
// fetch typed into the palette is kept either way.
func TestHistoryFileKeepsFetchesOnlyWhenAsked(t *testing.T) {
	for _, on := range []bool{false, true} {
		config, state := t.TempDir(), t.TempDir()
		if on {
			if err := os.WriteFile(filepath.Join(config, SettingsFile), []byte(`{"history-fetches": true}`), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		r := historyModel(t, config, state).Runtime
		run(t, r, "app.jobs.list", nil)
		runPaletteLine(t, r, Request{Noun: "app.modes", Verb: "list"})
		run(t, r, "app.notice.clear", nil)
		text := fileText(t, r)
		if !strings.Contains(text, `"noun":"app.modes"`) || !strings.Contains(text, `"noun":"app.notice"`) || strings.Contains(text, `"noun":"app.jobs"`) != on {
			t.Fatalf("history-fetches %v: %s", on, text)
		}
		if on {
			next := historyModel(t, config, state).Runtime
			if got := historyKeys(next.HistoryOf(0, HistoryFetch)); !strings.Contains(got, "fetch:app.jobs.list") {
				t.Fatalf("restored fetches: %s", got)
			}
		}
	}
}

// Dot-repeat reads changes only: actions and fetches between a change and
// the dot do not hide it.
func TestDotSkipsActionsAndFetches(t *testing.T) {
	r, in := editorTestRuntime(t, "abcdef", false)
	editorTestKeys(t, r, "x")
	run(t, r, "app.jobs.list", nil)
	editorTestKeys(t, r, "l", ".")
	if string(in.Text) != "bdef" {
		t.Fatalf("text %q", in.Text)
	}
}
