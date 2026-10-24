package ochazuke

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func paletteRequest(t *testing.T, r *Runtime, verb, noun string, args Arguments) any {
	t.Helper()
	result, err := r.Execute(Request{Verb: verb, Noun: noun, Args: args})
	if err != nil {
		t.Fatalf("%s.%s: %v", noun, verb, err)
	}
	return result
}

// paletteLine replaces the command line and runs it.
func paletteLine(t *testing.T, r *Runtime, line string) {
	t.Helper()
	paletteRequest(t, r, "set", "palette", Arguments{"text": line})
	paletteRequest(t, r, "run", "palette", nil)
}

func paletteKeys(t *testing.T, p *Palette, keys ...string) {
	t.Helper()
	for _, key := range keys {
		handled, err := p.HandleKey(key)
		if !handled || err != nil {
			t.Fatalf("palette key %q: handled=%v error=%v", key, handled, err)
		}
	}
}

// typeLine types text into the palette one key at a time.
func typeLine(t *testing.T, p *Palette, text string) {
	t.Helper()
	for _, ch := range text {
		key := string(ch)
		if ch == ' ' {
			key = "space"
		}
		paletteKeys(t, p, key)
	}
}

func paletteInputSnapshot(t *testing.T, in *Input) string {
	t.Helper()
	data, err := json.Marshal(in.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// lineRuntime is a sheet with a cell focused, stack [r1:c1, r1, sheet,
// app], and a column beside it: cell, row and column transform a name
// (upper, lower, title, trim); row deletes; sheet saves; text tidies (so
// "t" is ambiguous; "tr" is transform or the noun transformations, which the
// next word settles); item lists and list clears (list is a
// verb and a noun, and no list verb is on the focus path); note adds text; counter bumps by an integer. ran
// records each request with the instance it acted for.
func lineRuntime(t *testing.T) (*Runtime, *Palette, *[]string) {
	t.Helper()
	r := NewRuntime()
	ran := new([]string)
	run := func(r *Runtime, c Context, a Arguments) (any, error) {
		names := make([]string, 0, len(a))
		for k := range a {
			names = append(names, k)
		}
		slices.Sort(names)
		entry := focusOf(c)
		for _, k := range names {
			entry += " " + k + "=" + jsonText(a[k])
		}
		*ran = append(*ran, entry)
		return nil, nil
	}
	name := map[string]Parameter{"name": {Type: "string", Required: true, Enum: []string{"upper", "lower", "title", "trim"}}}
	for _, noun := range []string{"cell", "row", "column"} {
		r.Register(Operation{Verb: "transform", Noun: noun, Parameters: name, Run: run})
	}
	r.Register(Operation{Verb: "delete", Noun: "row", Run: run})
	r.Register(Operation{Verb: "save", Noun: "sheet", Run: run})
	r.Register(Operation{Verb: "tidy", Noun: "text", Run: run})
	r.Register(Operation{Verb: "list", Noun: "item", Run: run})
	r.Register(Operation{Verb: "clear", Noun: "list", Run: run})
	r.Register(Operation{Verb: "list", Noun: "transformations", Run: run})
	r.Register(Operation{Verb: "add", Noun: "note", Parameters: map[string]Parameter{"text": {Type: "string", Required: true}}, Run: run})
	r.Register(Operation{Verb: "bump", Noun: "counter", Parameters: map[string]Parameter{"by": {Type: "integer"}}, Run: run})
	r.Instance("sheet", Kind("sheet"))
	r.Instance("r1", Parent("sheet"), Kind("row"))
	r.Instance("c1", Parent("sheet"), Kind("column"))
	r.Instance("r1:c1", Parent("r1"), Kind("cell"))
	if err := r.SetFocus("r1:c1"); err != nil {
		t.Fatal(err)
	}
	p := NewPalette(r)
	paletteRequest(t, r, "open", "palette", nil)
	return r, p, ran
}

func jsonText(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}

// The grammar: the first word against verbs and nouns, the second against
// the complement (or a parameter when the noun is omitted), then the
// parameters; each word may be a unique prefix.
func TestPaletteLineGrammar(t *testing.T) {
	_, p, _ := lineRuntime(t)
	for _, tc := range []struct {
		line, op string
		args     Arguments
		implied  bool
		missing  string
		err      string
	}{
		{line: "transform row upper", op: "row.transform", args: Arguments{"name": "upper"}},
		{line: "row transform upper", op: "row.transform", args: Arguments{"name": "upper"}},
		{line: "tr r up", op: "row.transform", args: Arguments{"name": "upper"}},
		{line: "ROW TR UP", op: "row.transform", args: Arguments{"name": "upper"}},
		{line: "tr", err: `"tr" is ambiguous: transform, transformations`},
		{line: "tr li", op: "transformations.list", args: Arguments{}},
		{line: "transform upper", op: "cell.transform", args: Arguments{"name": "upper"}, implied: true},
		{line: "transform column lower", op: "column.transform", args: Arguments{"name": "lower"}},
		{line: "transform", op: "cell.transform", args: Arguments{}, implied: true, missing: "name"},
		{line: "transform row", op: "row.transform", args: Arguments{}, missing: "name"},
		{line: "row", missing: "verb"},
		{line: "", missing: "verb or noun"},
		{line: "t r up", err: `"t" is ambiguous: tidy, transform, terminal, text`},
		{line: "transform c up", err: `"c" is ambiguous: cell, column`},
		{line: "transform row t", op: "row.transform", err: `name: "t" is ambiguous: title, trim`},
		{line: "transform row purple", op: "row.transform", err: "name must be one of upper, lower, title, trim"},
		{line: "transform row upper more", op: "row.transform", err: `unexpected word "more"`},
		{line: "zap", err: `"zap" names no verb or noun here`},
		{line: "row zap", missing: "verb", err: `"zap" names no verb of row here`},
		{line: "delete", op: "row.delete", args: Arguments{}, implied: true},
		{line: "save", op: "sheet.save", args: Arguments{}, implied: true},
		{line: "tidy", op: "text.tidy", args: Arguments{}, implied: true},
		{line: "list", missing: "noun"},
		{line: "list clear", op: "list.clear", args: Arguments{}},
		{line: `add note "two words"`, op: "note.add", args: Arguments{"text": "two words"}},
		{line: "bump counter 3", op: "counter.bump", args: Arguments{"by": 3}},
		{line: "bump counter", op: "counter.bump", args: Arguments{}},
		{line: "bump counter x", op: "counter.bump", err: "by must be integer"},
	} {
		t.Run(tc.line, func(t *testing.T) {
			c := p.parse(tc.line)
			errText := ""
			if c.Err != nil {
				errText = c.Err.Error()
			}
			if c.Op.Key() != tc.op && !(tc.op == "" && !c.known()) || c.Implied != tc.implied || c.Missing != tc.missing || !strings.HasPrefix(errText, tc.err) || (tc.err == "") != (errText == "") {
				t.Fatalf("parse = op %q implied %v missing %q err %q", c.Op.Key(), c.Implied, c.Missing, errText)
			}
			if tc.args != nil && !reflect.DeepEqual(c.Args, tc.args) {
				t.Fatalf("args = %#v, want %#v", c.Args, tc.args)
			}
		})
	}
}

func completionValues(p *Palette) (out []string) {
	for _, e := range p.completions() {
		out = append(out, e.Kind+":"+e.Value)
	}
	return out
}

// The list under the line completes the next word: verbs then nouns first,
// the complement after a verb or a noun (with the omitted-noun parameter's
// choices after a verb), then the parameter's choices; the word being typed
// narrows it by prefix.
func TestPaletteCompletionsPerPosition(t *testing.T) {
	r, p, _ := lineRuntime(t)
	set := func(line string) { paletteRequest(t, r, "set", "palette", Arguments{"text": line}) }
	all := completionValues(p)
	split := slices.IndexFunc(all, func(v string) bool { return strings.HasPrefix(v, "noun:") })
	if split < 1 || !slices.Contains(all, "verb:transform") || slices.ContainsFunc(all[split:], func(v string) bool { return !strings.HasPrefix(v, "noun:") }) {
		t.Fatalf("root list = %v", all)
	}
	// Nouns of the focus path first, the focused noun first.
	if nouns := all[split:]; !reflect.DeepEqual(nouns[:4], []string{"noun:cell", "noun:row", "noun:sheet", "noun:app"}) {
		t.Fatalf("root nouns = %v", nouns)
	}
	for _, tc := range []struct {
		line string
		want []string
	}{
		{"t", []string{"verb:tidy", "verb:transform", "noun:terminal", "noun:text", "noun:transformations"}},
		{"tr", []string{"verb:transform", "noun:transformations"}},
		{"transform ", []string{"noun:cell", "noun:row", "noun:column", "choice:upper", "choice:lower", "choice:title", "choice:trim"}},
		{"transform c", []string{"noun:cell", "noun:column"}},
		{"row ", []string{"verb:delete", "verb:transform"}},
		{"row tr", []string{"verb:transform"}},
		{"transform row ", []string{"choice:upper", "choice:lower", "choice:title", "choice:trim"}},
		{"transform row t", []string{"choice:title", "choice:trim"}},
		{"transform upper ", nil},
		{"zap ", nil},
	} {
		set(tc.line)
		if got := completionValues(p); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%q completes %v, want %v", tc.line, got, tc.want)
		}
	}
}

// Filter codes :v, :n and :p typed as the current word restrict the list
// to verbs, nouns or choices and are consumed, not appended; the filter
// holds while that word is typed and lapses at the next one.
func TestPaletteFilterCodes(t *testing.T) {
	_, p, _ := lineRuntime(t)
	only := func(kind string) {
		t.Helper()
		values := completionValues(p)
		if len(values) == 0 || slices.ContainsFunc(values, func(v string) bool { return !strings.HasPrefix(v, kind+":") }) {
			t.Fatalf("filter %s: %v", kind, values)
		}
	}
	typeLine(t, p, ":v")
	if p.Text() != "" {
		t.Fatalf(":v was appended: %q", p.Text())
	}
	only("verb")
	typeLine(t, p, "tr")
	if got := completionValues(p); !reflect.DeepEqual(got, []string{"verb:transform"}) {
		t.Fatalf("typing under :v: %v", got)
	}
	paletteKeys(t, p, "ctrl+u")
	typeLine(t, p, ":n")
	only("noun")
	typeLine(t, p, "row ")
	if p.filter != "" || p.Text() != "row " {
		t.Fatalf("the filter outlived its word: %q line=%q", p.filter, p.Text())
	}
	paletteKeys(t, p, "ctrl+u")
	typeLine(t, p, "transform :p")
	if p.Text() != "transform " {
		t.Fatalf("line = %q", p.Text())
	}
	only("choice")
	if got := p.state()["filter"]; got != "p" {
		t.Fatalf("state filter = %v", got)
	}
}

// No completion is highlighted until ctrl+n or ctrl+p moves onto the list:
// enter runs the line as typed (abbreviations resolve, an ambiguous word or
// a missing one is reported and the line stays), and only a highlight makes
// enter or tab append a word. Typing clears the highlight.
func TestPaletteEnterRunsUnlessAWordIsHighlighted(t *testing.T) {
	r, p, ran := lineRuntime(t)
	typeLine(t, p, "t")
	if _, err := p.HandleKey("enter"); err == nil || !strings.Contains(err.Error(), "ambiguous") || p.Text() != "t" || !p.IsOpen() {
		t.Fatalf("enter on an ambiguous word: %v line=%q", err, p.Text())
	}
	typeLine(t, p, "r")
	if _, err := p.HandleKey("enter"); err == nil || !strings.Contains(err.Error(), "ambiguous") || p.Text() != "tr" {
		t.Fatalf("enter on tr picked a completion or ran: %v line=%q", err, p.Text())
	}
	paletteKeys(t, p, "ctrl+n")
	if view := p.View(); !strings.Contains(view, "> transform") {
		t.Fatalf("ctrl+n did not highlight:\n%s", view)
	}
	typeLine(t, p, "a")
	if p.state()["highlight"] != -1 {
		t.Fatalf("typing kept the highlight: %v", p.state()["highlight"])
	}
	paletteKeys(t, p, "backspace", "ctrl+n", "enter")
	if p.Text() != "transform " {
		t.Fatalf("enter on the highlight: %q", p.Text())
	}
	// A complete verb with its noun omitted names the missing parameter.
	if _, err := p.HandleKey("enter"); err == nil || !strings.HasPrefix(err.Error(), "missing name") || p.Text() != "transform " {
		t.Fatalf("enter on transform: %v line=%q", err, p.Text())
	}
	if view := p.View(); !strings.Contains(view, "Error: missing name (string, pick)") {
		t.Fatalf("missing word not indicated:\n%s", view)
	}
	paletteKeys(t, p, "ctrl+p", "ctrl+p")
	if entry, _ := p.highlighted(p.completions()); entry.Value != "title" {
		t.Fatalf("ctrl+p from none lands on the last, then moves up: %+v", entry)
	}
	paletteKeys(t, p, "tab")
	if p.Text() != "transform title " {
		t.Fatalf("tab on the highlight: %q", p.Text())
	}
	// tab with nothing highlighted completes a word with one completion.
	paletteKeys(t, p, "ctrl+u")
	typeLine(t, p, "transform ro")
	paletteKeys(t, p, "tab")
	typeLine(t, p, "up")
	paletteKeys(t, p, "enter")
	if p.IsOpen() || !reflect.DeepEqual(*ran, []string{`r1 name="upper"`}) || r.Focus != "r1:c1" {
		t.Fatalf("ran %v open=%v focus=%s", *ran, p.IsOpen(), r.Focus)
	}
}

// A line without a noun acts on the nearest instance of the verb's noun on
// the focus path; a named noun acts on its instance on that path; the
// focus never moves.
func TestPaletteTargetsTheInstanceOnTheFocusPath(t *testing.T) {
	r, p, ran := lineRuntime(t)
	for _, line := range []string{"transform upper", "transform row lower", "tr ro ti", "delete", "save"} {
		if !p.IsOpen() {
			paletteRequest(t, r, "open", "palette", nil)
		}
		paletteLine(t, r, line)
	}
	want := []string{`r1:c1 name="upper"`, `r1 name="lower"`, `r1 name="title"`, "r1", "sheet"}
	if !reflect.DeepEqual(*ran, want) || r.Focus != "r1:c1" {
		t.Fatalf("ran %v focus=%s", *ran, r.Focus)
	}
}

// Esc clears a non-empty line, then closes; ctrl+c closes at once. The line
// is a prompt: insert is pinned and Readline's ctrl+w deletes a word.
func TestPaletteLineIsAPrompt(t *testing.T) {
	r, p, _ := lineRuntime(t)
	if r.Focus != paletteLineID || r.Mode.Name != "insert" || r.Instances[paletteLineID].Noun != "prompt" {
		t.Fatalf("line focus=%s mode=%s", r.Focus, r.Mode.Name)
	}
	typeLine(t, p, "transform row")
	paletteKeys(t, p, "ctrl+w")
	if p.Text() != "transform " {
		t.Fatalf("ctrl+w: %q", p.Text())
	}
	paletteKeys(t, p, "esc")
	if p.Text() != "" || !p.IsOpen() || r.Mode.Name != "insert" {
		t.Fatalf("esc on a line: %q open=%v mode=%s", p.Text(), p.IsOpen(), r.Mode.Name)
	}
	paletteKeys(t, p, "esc")
	if p.IsOpen() || r.Focus != "r1:c1" {
		t.Fatalf("esc on an empty line: open=%v focus=%s", p.IsOpen(), r.Focus)
	}
	paletteRequest(t, r, "open", "palette", nil)
	typeLine(t, p, "row")
	paletteKeys(t, p, "ctrl+c")
	if p.IsOpen() {
		t.Fatal("ctrl+c left the palette open")
	}
	if handled, err := p.HandleKey("j"); handled || err != nil {
		t.Fatalf("closed palette consumed a key: %v %v", handled, err)
	}
}

// bindings.list inside the palette lists the palette noun's rows over the
// prompt's editor rows; nothing else names a palette key.
func TestPaletteBindingsListIsTruthful(t *testing.T) {
	r, _, _ := lineRuntime(t)
	rows := map[string]string{}
	for _, b := range r.ListBindings(r.Focus) {
		if _, seen := rows[b.Keys]; !seen && b.Keys != "" {
			rows[b.Keys] = b.Request.Key()
		}
	}
	for key, want := range map[string]string{"enter": "palette.run", "tab": "palette.complete", "ctrl+n": "palette.move", "ctrl+p": "palette.move", "up": "palette.recall", "down": "palette.recall", "esc": "palette.cancel", "ctrl+c": "palette.close"} {
		if rows[key] != want {
			t.Fatalf("%s lists %q, want %s (%v)", key, rows[key], want, rows)
		}
	}
	if rows["ctrl+w"] == "" {
		t.Fatalf("Readline rows missing: %v", rows)
	}
	for _, noun := range []string{"palette-field", "palette-argument"} {
		if r.Nouns[noun] != nil {
			t.Fatalf("stage noun %s still registered", noun)
		}
	}
}

// Requests a line runs are recorded with the source "palette"; up and down
// recall the lines from the action history, newest first, leaving out what
// a request read while it ran.
func TestPaletteHistoryRecall(t *testing.T) {
	r, p, _ := lineRuntime(t)
	r.Register(Operation{Verb: "list", Noun: "shades", Run: func(*Runtime, Context, Arguments) (any, error) { return []string{"dark", "light"}, nil }})
	r.Register(Operation{Verb: "paint", Noun: "cell", Parameters: map[string]Parameter{"shade": {Type: "string", Required: true, Source: "shades.list"}}, Run: func(*Runtime, Context, Arguments) (any, error) { return nil, nil }})
	for _, line := range []string{"transform row upper", "paint cell dark", "save"} {
		if !p.IsOpen() {
			paletteRequest(t, r, "open", "palette", nil)
		}
		paletteLine(t, r, line)
	}
	last := r.History(2)[0]
	if last.Request.Key() != "sheet.save" || last.Context.Source != "palette" || last.Depth != 1 {
		t.Fatalf("last entry = %+v", last)
	}
	paletteRequest(t, r, "open", "palette", nil)
	typeLine(t, p, "draft")
	for _, step := range []struct{ key, want string }{
		{"up", "save sheet"}, {"up", "paint cell dark"}, {"up", "transform row upper"}, {"up", "transform row upper"},
		{"down", "paint cell dark"}, {"down", "save sheet"}, {"down", "draft"},
	} {
		paletteKeys(t, p, step.key)
		if p.Text() != step.want {
			t.Fatalf("%s: line %q, want %q", step.key, p.Text(), step.want)
		}
	}
	paletteKeys(t, p, "up", "enter")
	if last := r.History(2)[0]; last.Request.Key() != "sheet.save" || p.IsOpen() {
		t.Fatalf("recalled line did not run: %+v", last)
	}
}

// A failing request reopens the palette over the same origin with the line
// and the error; fixing the line runs it.
func TestPaletteHandlerFailureKeepsTheLine(t *testing.T) {
	r := NewRuntime()
	var published string
	r.Register(Operation{Verb: "publish", Noun: "record", Parameters: map[string]Parameter{"text": {Type: "string", Required: true}}, Run: func(_ *Runtime, _ Context, a Arguments) (any, error) {
		if text := StringArg(a, "text", ""); text != "bad" {
			published = text
			return text, nil
		}
		return nil, errors.New("publication rejected")
	}})
	p := NewPalette(r)
	paletteRequest(t, r, "open", "palette", nil)
	typeLine(t, p, "publish record bad")
	if _, err := p.HandleKey("enter"); err == nil || !p.IsActive() || len(r.Modals()) != 1 || p.Text() != "publish record bad" || r.Mode.Name != "insert" {
		t.Fatalf("failure: %v open=%v line=%q mode=%s", err, p.IsOpen(), p.Text(), r.Mode.Name)
	}
	if view := p.View(); !strings.Contains(view, "publication rejected") {
		t.Fatalf("error not shown:\n%s", view)
	}
	paletteKeys(t, p, "ctrl+w")
	typeLine(t, p, "good")
	paletteKeys(t, p, "enter")
	if published != "good" || p.IsOpen() {
		t.Fatalf("retry: published=%q open=%v", published, p.IsOpen())
	}
}

func TestPaletteRecursiveExecutionIsVisibleAndCancelable(t *testing.T) {
	r := NewRuntime()
	p := NewPalette(r)
	paletteRequest(t, r, "open", "palette", nil)
	paletteRequest(t, r, "set", "palette", Arguments{"text": "run palette"})
	if _, err := r.Execute(Request{Verb: "run", Noun: "palette"}); err == nil || !p.IsOpen() || !strings.Contains(p.View(), "recursive palette execution") {
		t.Fatalf("recursive execution: %v\n%s", err, p.View())
	}
	paletteKeys(t, p, "ctrl+c")
	if p.IsOpen() || len(r.Modals()) != 0 {
		t.Fatal("recursive execution prevented cancellation")
	}
}

// Requests run in the context captured on open: undo acts on the input the
// palette opened over, focus moves the origin, not the palette.
func TestPaletteRunsInTheCapturedContext(t *testing.T) {
	r := NewRuntime()
	origin := NewInput("origin", "base", false)
	other := NewInput("other", "untouched", false)
	for _, in := range []*Input{origin, other} {
		if err := r.AddInput(in); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.SetFocus("origin"); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"1", "2", "3"} {
		if err := origin.Replace(len(origin.Text), len(origin.Text), text); err != nil {
			t.Fatal(err)
		}
	}
	otherBefore := paletteInputSnapshot(t, other)
	p := NewPalette(r)
	paletteRequest(t, r, "open", "palette", nil)
	if err := r.SetFocus("other"); err == nil || err.Error() != "focus is outside the open modal" {
		t.Fatalf("focus left the palette: %v", err)
	}
	typeLine(t, p, "undo input 2")
	paletteKeys(t, p, "enter")
	if got := string(origin.Text); got != "base1" || paletteInputSnapshot(t, other) != otherBefore {
		t.Fatalf("captured undo = %q", got)
	}
	paletteRequest(t, r, "open", "palette", nil)
	paletteLine(t, r, "focus input other")
	if p.IsOpen() || len(r.Modals()) != 0 || r.Context().InputID != "other" {
		t.Fatalf("focus was applied to the palette: %+v", r.Context())
	}
}

// The enter hint names both outcomes (the hint cuts at the first comma, so
// the description has none before its semicolon): nothing is highlighted until
// palette.move, so appending a completion is the exception.
func TestPaletteEnterHintLeadsWithRun(t *testing.T) {
	r, _, _ := lineRuntime(t)
	hints := BindingHints(r, r.Focus)
	if !strings.Contains(hints, "enter palette.run: Run the line or append the highlighted completion ·") {
		t.Fatalf("hints: %q", hints)
	}
}
