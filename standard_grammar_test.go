package ochazuke

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

// grammarRuntime is the write runtime with a palette open over row-t2 (a
// task row: the focus is the subject when the key is left out).
func grammarRuntime(t *testing.T) (*Runtime, *Palette, *writeStore) {
	t.Helper()
	r, s := newWriteRuntime(t)
	if err := r.SetFocus("row-t2"); err != nil {
		t.Fatal(err)
	}
	p := NewPalette(r)
	paletteRequest(t, r, "open", "palette", nil)
	return r, p, s
}

func setLine(t *testing.T, r *Runtime, line string) {
	t.Helper()
	paletteRequest(t, r, "set", "palette", Arguments{"text": line})
}

// TestNounWordPrecedence: the word after a noun is a verb, then a prop,
// then a key or instance (owner decision 18), each tier a whole word
// before a prefix.
func TestNounWordPrecedence(t *testing.T) {
	r, _ := newWriteRuntime(t)
	verbs := []string{"create", "delete", "get", "list", "set", "summarize", "watch"}
	for _, tc := range []struct {
		word string
		want NounWord
	}{
		{"get", NounWord{Kind: NounWordVerb, Value: "get"}},
		{"status", NounWord{Kind: NounWordProp, Value: "status"}},
		{"summary", NounWord{Kind: NounWordProp, Value: "summary"}}, // a whole prop before a verb's prefix (summarize)
		{"summ", NounWord{Kind: NounWordVerb, Value: "summarize"}},  // a verb's prefix before a prop's
		{"stat", NounWord{Kind: NounWordProp, Value: "status"}},
		{"st", NounWord{Candidates: []string{"status", "starred"}}},
		{"s", NounWord{Kind: NounWordVerb, Value: ""}}, // set and summarize
		{"e", NounWord{Kind: NounWordProp, Value: "estimate"}},
		{"t", NounWord{Candidates: []string{"title", "token"}}},
		{"t2", NounWord{}}, // a key: the caller's reading
	} {
		got := r.ReadNounWord("task", tc.word, verbs)
		if tc.word == "s" {
			if got.Kind != "" || !slices.Equal(got.Candidates, []string{"set", "summarize"}) {
				t.Fatalf("%q = %+v, want the verbs set and summarize", tc.word, got)
			}
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%q = %+v, want %+v", tc.word, got, tc.want)
		}
	}
	// A noun with no store has no props: the word is the caller's.
	r.Define("plain").Verb("go", func(Ctx) Result { return Done(nil) })
	if got := r.ReadNounWord("plain", "status", []string{"go"}); got.Kind != "" {
		t.Fatalf("plain status = %+v", got)
	}
}

// TestPropShorthandInThePalette: :task PROP reads the focused task's prop,
// :task PROP VALUE writes it, --toggle flips a boolean and --KEY names
// another thing; history keeps the request prop-shaped.
func TestPropShorthandInThePalette(t *testing.T) {
	r, p, s := grammarRuntime(t)
	c := p.parse("task status")
	if !c.Complete() || c.Prop != "status" || c.Request().Prop != "status" || c.Request().Writes() {
		t.Fatalf("task status = %+v", c)
	}
	paletteLine(t, r, "task status done")
	if got := s.find("t2")["status"]; got != "done" {
		t.Fatalf("t2 status %v", got)
	}
	paletteRequest(t, r, "open", "palette", nil)
	paletteLine(t, r, "task starred --toggle")
	if got := s.find("t2")["starred"]; got != true {
		t.Fatalf("t2 starred %v", got)
	}
	paletteRequest(t, r, "open", "palette", nil)
	paletteLine(t, r, "task status doing --id t3")
	if got := s.find("t3")["status"]; got != "doing" {
		t.Fatalf("t3 status %v", got)
	}
	last := r.HistoryOf(0, HistoryChange)
	if q := last[len(last)-1].Request; q.Prop != "status" || q.Value != "doing" || q.Args["id"] != "t3" {
		t.Fatalf("history keeps %+v", q)
	}
	// A read-only prop reads; a value is refused by name.
	paletteRequest(t, r, "open", "palette", nil)
	if c := p.parse("task summary x"); c.Err == nil || !strings.Contains(c.Err.Error(), "read-only") {
		t.Fatalf("task summary x = %v", c.Err)
	}
	if c := p.parse("task summary"); !c.Complete() || c.Op.Verb != "get" {
		t.Fatalf("task summary = %+v", c)
	}
}

// TestOptionalKeyWord: after a verb with a subject, a first word that is a
// prop (or prop=value for set) leaves the subject to the focus; any other
// word is the key; a key equal to a prop name is reached with --KEY.
func TestOptionalKeyWord(t *testing.T) {
	r, p, s := grammarRuntime(t)
	for _, tc := range []struct {
		line string
		want Arguments
	}{
		{"task set status done", Arguments{"prop": "status", "value": "done"}},
		{"task set t1 status done", Arguments{"id": "t1", "prop": "status", "value": "done"}},
		{"task set title=Ship estimate=5", Arguments{"prop": "title=Ship", "value": "estimate=5"}},
		{"task set --id status status done", Arguments{"id": "status", "prop": "status", "value": "done"}},
		{"task get status", Arguments{"prop": "status"}},
		{"task get t3 status", Arguments{"id": "t3", "prop": "status"}},
		{"task delete t3", Arguments{"id": "t3"}},
		{"task list --where status=todo --limit 1", Arguments{"where": "status=todo", "limit": 1}},
	} {
		c := p.parse(tc.line)
		if c.Err != nil || !reflect.DeepEqual(c.Args, tc.want) {
			t.Fatalf("%q = %v %v, want %v", tc.line, c.Args, c.Err, tc.want)
		}
	}
	paletteLine(t, r, "task set estimate 8")
	if got := s.find("t2")["estimate"]; got != 8 {
		t.Fatalf("t2 estimate %v", got)
	}
	// A --NAME still waiting for its value is what the line misses.
	paletteRequest(t, r, "open", "palette", nil)
	if c := p.parse("task list --where"); c.Complete() || c.Missing != "where" || c.Flag != "where" {
		t.Fatalf("task list --where = %+v", c)
	}
	// The same words from the CLI's positional rule.
	set := r.Operations["task.set"]
	open := []string{"id", "prop", "value", "pairs"}
	for word, want := range map[string]bool{"status": true, "title=x": true, "t1": false, "nope=1": false} {
		if got := set.SubjectOmitted(open, word); got != want {
			t.Fatalf("SubjectOmitted(%q) = %v", word, got)
		}
	}
	if r.Operations["task.delete"].SubjectOmitted([]string{"id"}, "status") {
		t.Fatal("delete has no word after its subject")
	}
}

// TestPropValueCompletion: prop names after a noun and after set, a
// prop's values after set PROP and NOUN PROP (its Enum, true and false),
// where's prop= then the values, spliced so the cursor stays after =.
func TestPropValueCompletion(t *testing.T) {
	r, p, _ := grammarRuntime(t)
	has := func(line string, want ...string) {
		t.Helper()
		setLine(t, r, line)
		got := completionValues(p)
		for _, w := range want {
			if !slices.Contains(got, w) {
				t.Fatalf("%q completes %v, want %s", line, got, w)
			}
		}
	}
	has("task ", "verb:get", "verb:set", "prop:status", "prop:summary")
	has("task st", "prop:status", "prop:starred")
	has("task set ", "choice:status", "choice:title", "choice:t1")
	has("task set status ", "choice:todo", "choice:doing", "choice:done")
	has("task set t1 starred ", "choice:true", "choice:false")
	has("task status ", "choice:todo", "choice:done")
	has("task status d", "choice:doing", "choice:done")
	has("task list --where ", "choice:status=", "choice:title=")
	has("task list --where status=", "choice:status=todo", "choice:status=done")
	setLine(t, r, "task list --where st")
	var entry PaletteEntry
	for _, e := range p.completions() {
		if e.Value == "status=" {
			entry = e
		}
	}
	if !entry.Splice {
		t.Fatalf("status= is not spliced: %+v", entry)
	}
	paletteRequest(t, r, "complete", "palette", Arguments{"value": "status="})
	if got := p.Text(); got != "task list --where status=" {
		t.Fatalf("after taking status= the line is %q", got)
	}
	// A value outside a prop's Enum is refused on the line.
	if c := p.parse("task status nope"); c.Err == nil {
		t.Fatal("task status nope parsed")
	}
}

// TestSetReadsOnlyOneValueNouns: :set NOUN reads a noun whose set is the
// application's own or whose store is a singleton (owner decision 17), so
// a collection named pa... never takes :set pa; and no declared setting
// name reads as a shadow noun, so every setting is reached by :set NAME.
func TestSetReadsOnlyOneValueNouns(t *testing.T) {
	r := NewRuntime()
	m := NewModel(r)
	s := &writeStore{taskStore: newTaskStore()}
	r.Define("parcel").Props(taskWriteProps()...).Store(s.store())
	r.Define("theme").Props(PropSpec{Name: "name"}).Store(Store{Kind: Singleton,
		Get:   func(Ctx, string) (Thing, error) { return Thing{"name": "kombu"}, nil },
		Patch: func(Ctx, string, Thing) (Thing, error) { return nil, nil }})
	words := func(line string) []string {
		tokens, _ := tokenize(line)
		var out []string
		for _, t := range tokens {
			out = append(out, t.Text)
		}
		return out
	}
	if reading, _, err := r.ReadBareAlias(r.Context(), words("set parcel status done"), nil); err != nil || reading.Noun != "" {
		t.Fatalf("set parcel = %+v %v: a collection is not a shadow noun", reading, err)
	}
	if reading, _, err := r.ReadBareAlias(r.Context(), words("set theme name x"), nil); err != nil || reading.Noun != "theme" {
		t.Fatalf("set theme = %+v %v: a singleton is", reading, err)
	}
	if reading, _, err := r.ReadBareAlias(r.Context(), words("set mode normal"), nil); err != nil || reading.Noun != "mode" {
		t.Fatalf("set mode = %+v %v: a named set is", reading, err)
	}
	_, _, err := r.ReadBareAlias(r.Context(), words("set pa"), nil)
	if err == nil || strings.Contains(err.Error(), "parcel") {
		t.Fatalf("set pa = %v: palette and the settings, never parcel", err)
	}
	// The collision check: every setting reads as a setting.
	for _, one := range r.Settings().Declared() {
		reading, _, err := r.ReadBareAlias(r.Context(), []string{"set", one.Name}, nil)
		if err != nil || reading.Noun != "" {
			t.Errorf("set %s reads as noun %q (%v): a setting name collides with a shadow noun", one.Name, reading.Noun, err)
		}
	}
	for _, word := range r.shadowWords(r.shadowNouns("set"), "set") {
		for _, one := range r.Settings().Declared() {
			if one.Name == word || strings.HasPrefix(word, one.Name) {
				t.Errorf("setting %s is a prefix of the shadow noun %s", one.Name, word)
			}
		}
	}
	// The palette lists the same nouns after set.
	r.Instance("x")
	if err := r.SetFocus("x"); err != nil {
		t.Fatal(err)
	}
	paletteRequest(t, r, "open", "palette", nil)
	setLine(t, r, "set pa")
	got := completionValues(m.Palette)
	if slices.Contains(got, "noun:parcel") || !slices.Contains(got, "noun:palette") || !slices.Contains(got, "choice:palette.standard-verbs") {
		t.Fatalf("set pa completes %v", got)
	}
}

// TestStandardVerbsSetting: palette.standard-verbs orders the verb column
// (last, mixed, hidden), tags standard verbs std, and shows get as prop and
// value rows; each part off is today's palette.
func TestStandardVerbsSetting(t *testing.T) {
	r, p, _ := grammarRuntime(t)
	r.Define("task").Verb("zap", func(Ctx) Result { return Done(nil) })
	verbs := func() (out []string) {
		setLine(t, r, "task ")
		for _, e := range p.completions() {
			if e.Kind == "verb" {
				out = append(out, e.Label)
			}
		}
		return out
	}
	if got := verbs(); !strings.HasPrefix(got[0], "zap") || !strings.Contains(got[1], "· std") || strings.Contains(got[0], "std") {
		t.Fatalf("default verbs %v: named first, standard tagged", got)
	}
	set := func(parts ...string) {
		t.Helper()
		if err := r.Settings().Set(StandardVerbsSetting, parts); err != nil {
			t.Fatal(err)
		}
	}
	set("mixed")
	if got := verbs(); strings.HasPrefix(got[0], "zap") || strings.Contains(strings.Join(got, ","), "std") {
		t.Fatalf("mixed, no tag: %v", got)
	}
	set("hidden", "tag")
	if got := verbs(); len(got) != 1 || !strings.HasPrefix(got[0], "zap") {
		t.Fatalf("hidden: %v", got)
	}
	if c := p.parse("task get"); !c.Complete() {
		t.Fatalf("a hidden verb still runs when typed: %+v", c)
	}
	if v := r.StandardVerbsView(); v.Order != StandardVerbsHidden || !v.Tag || v.Record {
		t.Fatalf("view %+v", v)
	}
	set()
	if v := r.StandardVerbsView(); v != (StandardVerbsView{Order: StandardVerbsMixed}) {
		t.Fatalf("[] is today's palette: %+v", v)
	}
	// record: get opens the results view as prop and value rows; off, a
	// record is shown as any verb's result (no list).
	for _, record := range []bool{true, false} {
		parts := []string{"last"}
		if record {
			parts = append(parts, "record")
		}
		set(parts...)
		paletteRequest(t, r, "open", "palette", nil)
		paletteLine(t, r, "task get")
		list := r.List(paletteResultsListID)
		if record != (list != nil) {
			t.Fatalf("record %v: results list %v", record, list != nil)
		}
		if record {
			if top := r.Instances[r.TopModal()]; top == nil || top.Data != "task.get" {
				t.Fatalf("results modal %v", top)
			}
			row, _ := list.Items()[2].Data.(map[string]any)
			if row["prop"] != "status" || row["value"] != "doing" {
				t.Fatalf("third row %v", row)
			}
			if err := r.Pop(); err != nil {
				t.Fatal(err)
			}
		}
	}
}
