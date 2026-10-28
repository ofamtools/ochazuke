package ochazuke

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"testing"
)

// frameworkRuntime builds a runtime with every framework component whose
// verbs the embedded files name: the model (app.json, palette.json) and a
// picker (picker.json).
func frameworkRuntime(t *testing.T) *Runtime {
	t.Helper()
	r := NewRuntime()
	r.Config().Disable()
	NewModel(r)
	NewPicker(r)
	return r
}

func frameworkFiles(t *testing.T) []string {
	t.Helper()
	names, err := fs.Glob(frameworkBindings, "bindings/*.json")
	if err != nil || len(names) == 0 {
		t.Fatalf("embedded files: %v %v", names, err)
	}
	return names
}

// Every embedded framework file parses and passes the rule check on a
// built runtime: every action, argument, source, guard and mode it names
// exists, every mode.enter goes into a pending mode, and no two rows of a
// table share a key and guard.
func TestFrameworkBindingFilesPassTheRuleCheck(t *testing.T) {
	r := frameworkRuntime(t)
	for _, name := range frameworkFiles(t) {
		data, _ := frameworkBindings.ReadFile(name)
		if problems := r.CheckDefaultBindings(name, data); len(problems) > 0 {
			t.Errorf("%s:\n  %s", name, strings.Join(problems, "\n  "))
		}
	}
}

// No two framework files bind the same key and guard in one table: a
// later file would replace the earlier file's row, and the key would be
// written in two places.
func TestFrameworkBindingFilesDoNotClash(t *testing.T) {
	r := frameworkRuntime(t)
	seen := map[string]string{}
	for _, name := range frameworkFiles(t) {
		data, _ := frameworkBindings.ReadFile(name)
		file, _, err := ParseBindings(data)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, g := range file.Groups {
			for _, row := range g.Rows {
				row.Noun = r.CanonicalNoun(g.Noun)
				row, err := r.validateRow(row)
				if err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				id := sourceKey(withImplicitGuard(row))
				if other, ok := seen[id]; ok && other != name {
					t.Errorf("%s and %s both bind %s %s/%s %q", other, name, row.Noun, row.Mode, row.When, row.Keys+row.Pattern)
				}
				seen[id] = name
			}
		}
	}
}

// Every embedded file is loaded: by NewRuntime in frameworkBindingFiles'
// order, or by the component that registers its verbs; each of its rows
// stands in the default layer tagged with the file (a fallback row unless
// the application bound its key first).
func TestEveryFrameworkBindingFileIsLoaded(t *testing.T) {
	r := frameworkRuntime(t)
	lazy := []string{"app.json", "palette.json", "picker.json"}
	bySource := map[string]int{}
	for _, row := range r.DefaultBindings() {
		bySource[row.Source]++
	}
	for _, name := range frameworkFiles(t) {
		base := strings.TrimPrefix(name, "bindings/")
		if !slices.Contains(frameworkBindingFiles, base) && !slices.Contains(lazy, base) {
			t.Errorf("%s is embedded but nothing loads it", name)
		}
		data, _ := frameworkBindings.ReadFile(name)
		file, _, _ := ParseBindings(data)
		want := 0
		for _, g := range file.Groups {
			want += len(g.Rows)
		}
		if got := bySource[FrameworkBindingsSource+name]; got != want {
			t.Errorf("%s: %d rows in the default layer, want %d", name, got, want)
		}
	}
}

// An embedded default is checked strictly: a bad row fails the load with
// the source, the table and the key, where the user's file would skip it.
func TestLoadDefaultBindingsFailsLoudly(t *testing.T) {
	for _, tc := range []struct{ file, want string }{
		{`{"bindings": [{"context": "app/normal", "bindings": {"x": "app.nope"}}]}`, `mtm/app.json: app/normal "x": unknown operation "app.nope"`},
		{`{"bindings": [{"context": "app/normal", "bindings": {"x": {"action": "app.quit", "when": "nope"}}}]}`, `unknown guard "nope"`},
		{`{"bindings": [{"context": "app/nope", "bindings": {"x": "app.quit"}}]}`, `unknown mode "nope"`},
		{`{"bindings": [{"context": "app/normal", "bindings": {"x": {"action": "ochazuke.list.move", "args": {"delta": 1, "nope": 1}}}}]}`, `unknown argument "nope"`},
		{`{"bindings": [{"context": "app/normal", "bindings": {"x": {"action": "app.quit", "from": {"x": "key"}}}}]}`, `has no argument "x"`},
		{`{"bindings": [{"context": "app/normal", "extends": "nope"}]}`, `extends unknown mode "nope"`},
		{`{"bindings": [{"context": "app/normal", "bindings": {"x": "app.quit", "x": "app.quit"}}]}`, `key "x" twice`},
		{`{"bindings": [{"context": "app/normal", "bindings": {"x": {"action": "app.quit", "nope": true}}}]}`, `unknown field "nope"`},
		{`{"rows": []}`, `unknown field "rows"`},
	} {
		r := NewRuntime()
		err := r.LoadDefaultBindings("mtm/app.json", []byte(tc.file))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want %q", tc.file, err, tc.want)
		}
	}
	defer func() {
		if recover() == nil {
			t.Fatal("MustLoadDefaultBindings did not panic")
		}
	}()
	NewRuntime().MustLoadDefaultBindings("mtm/app.json", []byte(`{"bindings": [{"context": "app/normal", "bindings": {"x": "app.nope"}}]}`))
}

// A file's rows sit in the default layer exactly as the same rows bound in
// Go: modes, guarded rows before the unguarded row on their key, extends,
// an Enter payload with its request, an instance table, an unbind; and
// the user's bindings.json replaces them as it replaces a Go row.
func TestLoadDefaultBindingsMatchesTheGoBuilder(t *testing.T) {
	file := `{
  "modes": {"demo_pending": {"pending": true}},
  "bindings": [
    {"context": "app/normal", "bindings": {
      "z": {"action": "ochazuke.mode.enter", "args": {"mode": "demo_pending", "prefix": "z", "request": {"noun": "test", "verb": "alpha"}}, "hint": "demo"},
      "a": ["test.alpha", {"action": "test.beta", "when": "no-modal", "hidden": true}],
      "g g": {"action": "ochazuke.list.move", "args": {"delta": 2}, "from": {"steps": "count"}}
    }},
    {"context": "app/demo_pending", "bindings": {"any": {"action": "test.gamma", "replay": true}}},
    {"context": "app/count_pending", "extends": "normal"},
    {"instance": "box", "mode": "normal", "bindings": {"b": {"action": "test.beta", "capture": true}}}
  ]
}`
	alpha := Request{Noun: "test", Verb: "alpha"}
	build := func(fromFile bool) *Runtime {
		r := NewRuntime()
		recorder(t, r, "alpha", "beta", "gamma")
		r.Instance("box")
		if fromFile {
			if err := r.LoadDefaultBindings("test.json", []byte(file)); err != nil {
				t.Fatal(err)
			}
			return r
		}
		_ = r.DefineMode("demo_pending", true)
		r.Define("app").Mode("normal").
			Bind("z", Enter("demo_pending", Payload{Prefix: "z", Request: &alpha}).Hinted("demo")).
			Bind("a", Do("test.alpha")).
			Bind("a", Do("test.beta").Guard("no-modal").Hide()).
			Bind("g g", Do("ochazuke.list.move", Arguments{"delta": 2}).FromPayload("steps", SourceCount)).
			Mode("demo_pending").Pattern(Any, Binding{Request: Request{Noun: "test", Verb: "gamma"}, Replay: true}).
			Mode("count_pending").Extends("normal")
		row := Do("test.beta").Captured()
		row.Instance, row.Mode, row.Keys = "box", "normal", "b"
		if err := r.Bind(row); err != nil {
			t.Fatal(err)
		}
		return r
	}
	tables := func(r *Runtime) string {
		var b strings.Builder
		for _, row := range r.DefaultBindings() {
			// Arguments by their JSON (mode.enter's request is a Request
			// from Enter, the decoded object from a file: both run alike).
			var args any
			data, _ := json.Marshal(row.Request.Args)
			_ = json.Unmarshal(data, &args)
			data, _ = json.Marshal(args)
			fmt.Fprintf(&b, "%s %s %s %s%s %s %v %s %v %q %v %v %v\n", row.Noun, row.Instance, row.Mode, row.Keys, row.Pattern, row.Request.Key(), row.When, data, row.From, row.Hint, row.Hidden, row.Replay, row.Capture)
		}
		fmt.Fprintf(&b, "extends %v pending %v\n", r.Nouns["app"].Extends, r.pendingModes["demo_pending"])
		return b.String()
	}
	if code, data := tables(build(false)), tables(build(true)); code != data {
		t.Fatalf("Go rows:\n%s\nfile rows:\n%s", code, data)
	}
	// The guarded row answers first while its guard holds.
	r := build(true)
	rows := r.Nouns["app"].Bindings["normal"]
	if i, j := slices.IndexFunc(rows, func(b Binding) bool { return b.Keys == "a" && b.When == "no-modal" }), slices.IndexFunc(rows, func(b Binding) bool { return b.Keys == "a" && b.When == "" }); i < 0 || j < 0 || i > j {
		t.Fatalf("a rows %v", rows)
	}
	// An unbind row removes a default; a fallback group leaves a bound key.
	if err := r.LoadDefaultBindings("more.json", []byte(`{"bindings": [
		{"context": "app/normal", "bindings": {"g g": null}},
		{"context": "app/normal", "fallback": true, "bindings": {"a": "test.gamma", "ctrl+t": "test.gamma"}}
	]}`)); err != nil {
		t.Fatal(err)
	}
	keys := map[string]string{}
	for _, b := range r.Nouns["app"].Bindings["normal"] {
		if b.When == "" {
			keys[b.Keys] = b.Request.Key()
		}
	}
	if _, ok := keys["g g"]; ok || keys["a"] != "test.alpha" || keys["ctrl+t"] != "test.gamma" {
		t.Fatalf("after the unbind and the fallback: %v", keys)
	}
}

// bindings defaults prints the default layer as a bindings file: every
// row with its source (the embedded file, go for a row bound in Go,
// generated for a prefix cancel), the user's file left out and the row it
// replaced back in place. The output reads back as a bindings file.
func TestBindingsDefaultsPrintsTheDefaultLayer(t *testing.T) {
	r, m, _, dir := configFixture(t)
	writeConfig(t, dir, BindingsFile, `[
		{"noun": "app", "mode": "normal", "keys": "?", "request": {"noun": "test", "verb": "gamma"}},
		{"noun": "app", "mode": "normal", "keys": "w", "request": {"noun": "test", "verb": "gamma"}}
	]`)
	m.Init()
	got, err := r.Execute(Request{Noun: "ochazuke.bindings", Verb: "defaults"})
	if err != nil {
		t.Fatal(err)
	}
	text := string(got.(Verbatim))
	file, _, err := ParseBindings([]byte(text))
	if err != nil {
		t.Fatalf("the output does not read back: %v\n%s", err, text)
	}
	if !strings.Contains(text, `{"context": "app/normal", "source": "go", "bindings": {
      "a": "test.alpha",`) {
		t.Errorf("the fixture's Go rows are not tagged go:\n%s", text)
	}
	if !strings.Contains(text, `"source": "ochazuke/bindings/app.json", "bindings": {
      ":": "ochazuke.palette.open",
      "ctrl+y": {"action": "ochazuke.yank-history.open", "hidden": true},
      "?": "ochazuke.bindings.help",`) {
		t.Errorf("the default ? is not back in place:\n%s", text)
	}
	if strings.Contains(text, "test.gamma") {
		t.Errorf("the user's rows are printed:\n%s", text)
	}
	if !strings.Contains(text, `"source": "generated"`) || !strings.Contains(text, `"g esc": "ochazuke.mode.leave"`) {
		t.Errorf("no generated prefix cancel:\n%s", text)
	}
	// Every default row is printed once, in its table, with its source.
	n := 0
	for _, g := range file.Groups {
		n += len(g.Rows)
	}
	if want := len(r.DefaultBindings()); n != want {
		t.Errorf("%d rows printed, %d in the default layer", n, want)
	}
	// The user's row still answers: defaults leave the live tables alone.
	if got := helpKeys(r)["?"]; got != "test.gamma" {
		t.Errorf("? answers %q after bindings defaults", got)
	}
}

// The terminal leader setting moves the leader rows; they stay the file's.
func TestTerminalLeaderKeepsTheRowsSource(t *testing.T) {
	r := NewRuntime()
	if err := r.SetTerminalLeader("ctrl+b"); err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, row := range r.DefaultBindings() {
		if row.Noun == "ochazuke.terminal" && row.Keys == "ctrl+b" {
			found++
			if row.Source != FrameworkBindingsSource+"bindings/terminal.json" {
				t.Errorf("%s %s: source %q", row.Mode, row.Keys, row.Source)
			}
		}
	}
	if found != 2 {
		t.Fatalf("%d leader rows on ctrl+b, want 2", found)
	}
}
