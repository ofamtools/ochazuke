package ochazuke

import (
	"strings"
	"testing"
	"time"

	"ofam.dev/ochazuke/keytest"
)

// Every noun has a kind, read off its store or derived from its verbs,
// and a storeless noun may declare it.
func TestNounKinds(t *testing.T) {
	t.Parallel()
	r := NewRuntime()
	done := func(Ctx) Result { return Done(nil) }
	s := (&derivedStore{key: "id"}).store()
	r.Define("parcel").Props(PropSpec{Name: "id", Key: true}).Store(s)
	r.Define("setup").Verb("run", done, "Run the setup")
	r.Define("turn-line").Verb("fold", func(*Runtime, Context, Arguments) (any, error) { return nil, nil }, "Fold the line")
	r.Define("daemon-row").Kind(NounKindPart).Verb("restart", done, "Restart it")
	r.Define("mirror").Kind(NounKindFacts).Props(PropSpec{Name: "id", Key: true}).Store(s)
	want := map[string]string{
		"parcel":                 NounKindRecord,
		"setup":                  NounKindCommand,
		"turn-line":              NounKindPart,
		"daemon-row":             NounKindPart,
		"mirror":                 NounKindFacts,
		"ochazuke.noun":          NounKindFacts,
		"ochazuke.snapshot":      NounKindFacts,
		"ochazuke.page":          NounKindInstances,
		"ochazuke.history-entry": NounKindFacts,
	}
	all := r.AllNounKinds()
	for noun, kind := range want {
		if got := r.NounKind(noun); got != kind {
			t.Errorf("NounKind(%s) = %q, want %q", noun, got, kind)
		}
		if all[noun] != kind {
			t.Errorf("AllNounKinds[%s] = %q, want %q", noun, all[noun], kind)
		}
	}
	for noun := range r.Nouns {
		if r.NounKind(noun) != all[noun] {
			t.Errorf("%s: NounKind %q and AllNounKinds %q disagree", noun, r.NounKind(noun), all[noun])
		}
	}
	defer func() {
		if recover() == nil {
			t.Fatal("a stored kind other than facts must not be declarable")
		}
	}()
	r.Define("bad").Kind(NounKindRecord)
}

// fieldKindFixture is a noun with a time prop and an object prop, focused.
func fieldKindFixture(t *testing.T) (*Runtime, *Model, *derivedStore) {
	t.Helper()
	r := NewRuntime()
	s := &derivedStore{key: "id", things: []Thing{{"id": "e1", "title": "Launch", "when": "2026-11-10T09:30:00Z", "meta": map[string]any{"a": 1.0}}}}
	r.Define("gathering").Props(
		PropSpec{Name: "id", Key: true},
		PropSpec{Name: "title"},
		PropSpec{Name: "when", Parameter: Parameter{Type: PropTime}},
		PropSpec{Name: "meta", Parameter: Parameter{Type: PropObject}},
	).Store(s.store())
	r.Instance("row", Kind("gathering")).Data = Thing{"id": "e1"}
	if err := r.SetFocus("row"); err != nil {
		t.Fatal(err)
	}
	return r, NewModel(r), s
}

// A time prop reads RFC 3339, a date and time, or a date, and stores RFC
// 3339 in UTC; anything else is refused by set with what it must look like.
func TestTimePropValues(t *testing.T) {
	t.Parallel()
	r, _, s := fieldKindFixture(t)
	mustRun(t, r, Request{Noun: "gathering", Verb: "set", Args: Arguments{"id": "e1", "props": map[string]any{"when": "2026-12-01T08:00:00+01:00"}}})
	if got := s.find("e1")["when"]; got != "2026-12-01T07:00:00Z" {
		t.Fatalf("stored %v", got)
	}
	day, _ := time.ParseInLocation("2006-01-02", "2026-12-24", time.Local)
	mustRun(t, r, Request{Noun: "gathering", Verb: "set", Args: Arguments{"id": "e1", "props": map[string]any{"when": "2026-12-24"}}})
	if got := s.find("e1")["when"]; got != day.UTC().Format(time.RFC3339) {
		t.Fatalf("a date stored %v", got)
	}
	_, err := r.Execute(Request{Noun: "gathering", Verb: "set", Args: Arguments{"id": "e1", "props": map[string]any{"when": "next tuesday"}}})
	if err == nil || !strings.Contains(err.Error(), "2026-11-10") {
		t.Fatalf("a time that does not read must be refused naming the form: %v", err)
	}
	if v, err := ParseFlagValue(PropTime, "2026-11-10 09:30"); err != nil || !strings.HasPrefix(v.(string), "2026-11-") {
		t.Fatalf("flag %v %v", v, err)
	}
}

// In the edit form a time field and a JSON field take any text: what does
// not read is kept as typed and marked, never written, and fixing it
// writes it.
func TestTimeAndJSONFieldsEditInAForm(t *testing.T) {
	t.Parallel()
	r, m, s := fieldKindFixture(t)
	mustRun(t, r, Request{Noun: "gathering", Verb: "edit"})
	e := r.RecordEditor(editFormID)
	if e == nil {
		t.Fatal("edit did not open the form")
	}
	if got := e.Values()["meta"]; got != `{"a":1}` {
		t.Fatalf("object field value %q", got)
	}
	for _, f := range e.fields {
		if f.Name == "meta" && !f.Multiline {
			t.Fatal("an object field is several lines high")
		}
	}
	// title, when, meta: j moves to when.
	for _, key := range []string{"j", "i", "ctrl+u"} {
		keytest.Press(m, key)
	}
	typeText(m, "soon")
	keytest.Press(m, "esc")
	if got := s.find("e1")["when"]; got != "2026-11-10T09:30:00Z" {
		t.Fatalf("an unreadable time was written: %v", got)
	}
	if e.Values()["when"] != "soon" || e.Problem("when") == "" {
		t.Fatalf("the typed text is kept and marked: %q %q", e.Values()["when"], e.Problem("when"))
	}
	for _, key := range []string{"A", "ctrl+u"} {
		keytest.Press(m, key)
	}
	typeText(m, "2026-11-11T10:00:00Z")
	keytest.Press(m, "esc")
	if got := s.find("e1")["when"]; got != "2026-11-11T10:00:00Z" {
		t.Fatalf("time not written: %v (values %v problem %q log %v focus %s)", got, e.Values(), e.Problem("when"), s.log, r.Focus)
	}
	for _, key := range []string{"j", "i", "ctrl+u"} {
		keytest.Press(m, key)
	}
	typeText(m, `{"a": `)
	keytest.Press(m, "esc")
	if e.Values()["meta"] != `{"a": ` || e.Problem("meta") == "" {
		t.Fatalf("broken JSON is kept and marked: %q %q", e.Values()["meta"], e.Problem("meta"))
	}
	if got := s.find("e1")["meta"]; got.(map[string]any)["a"] != 1.0 {
		t.Fatalf("broken JSON was written: %v", got)
	}
	keytest.Press(m, "A")
	typeText(m, `2, "b": [true]}`)
	keytest.Press(m, "esc")
	got, _ := s.find("e1")["meta"].(map[string]any)
	if got["a"] != 2.0 || got["b"] == nil {
		t.Fatalf("JSON not written: %v (problem %q)", s.find("e1")["meta"], e.Problem("meta"))
	}
}

// The args form holds a JSON parameter as a multi-line field instead of
// leaving it out, and reads it as JSON.
func TestArgsFormHoldsJSON(t *testing.T) {
	t.Parallel()
	op := Operation{Key: "x.y", Parameters: map[string]Parameter{"spec": {Type: PropObject, Required: true}, "tags": {Type: "array"}}}
	fields, err := ArgsFields(op)
	if err != nil {
		t.Fatalf("a required object parameter is a form field now: %v", err)
	}
	if len(fields) != 2 || !fields[0].Multiline {
		t.Fatalf("fields %+v", fields)
	}
	args, err := argumentsFrom(op, nil, Record{"spec": `{"k": "v"}`, "tags": `[1]`})
	if err != nil || args["spec"].(map[string]any)["k"] != "v" || len(args["tags"].([]any)) != 1 {
		t.Fatalf("args %v %v", args, err)
	}
	if _, err := argumentsFrom(op, nil, Record{"spec": `nope`}); err == nil {
		t.Fatal("text that is not an object does not run")
	}
}
