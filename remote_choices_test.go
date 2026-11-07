package ochazuke

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// widgetFacts defines widget, a list-facts noun whose list verb answers
// ListItem rows through list (Pending or not, as list returns).
func widgetFacts(t *testing.T, list func(Ctx) Result) (*Runtime, *Model) {
	t.Helper()
	r := NewRuntime()
	r.Config().Disable()
	m := NewModel(r)
	r.DefineOwn("widget").Verb("list", list, "List widgets").Rows("widget").Fetch()
	r.DefineListFacts(ListFacts{Noun: "widget", List: list})
	return r, m
}

func remoteWidgetRows() []ListItem {
	return []ListItem{
		{ID: "widget/a", Label: "Alpha W", Noun: "widget", Data: map[string]any{"size": 1}},
		{ID: "widget/b", Label: "Beta W", Noun: "widget", Data: map[string]any{"size": 2}},
	}
}

// A list verb's rows ([]ListItem, the shape ListFacts documents) are
// choices: a parameter naming a list-facts noun offers them (R5.19, N5).
func TestListFactsNounIsAChoiceSource(t *testing.T) {
	t.Parallel()
	r, _ := widgetFacts(t, func(Ctx) Result { return Done(remoteWidgetRows()) })
	p := Parameter{Noun: "widget"}
	if got := r.ChoicesSource(p); got != "widget.list" {
		t.Fatalf("source %q", got)
	}
	choices, err := r.ChoicesFor(r.Context(), p, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(choices) != 2 || choices[0] != (Choice{ID: "widget/a", Label: "Alpha W"}) || choices[1].ID != "widget/b" {
		t.Fatalf("choices %+v", choices)
	}
}

func TestParseChoicesReadsRowShapes(t *testing.T) {
	t.Parallel()
	type row struct {
		ID    string `json:"id"`
		Label string `json:"label"`
	}
	for name, result := range map[string]any{
		"ListItem":  []ListItem{{ID: "a", Label: "A"}},
		"*ListItem": []*ListItem{{ID: "a", Label: "A"}},
		"Thing":     []Thing{{"id": "a", "label": "A"}},
		"struct":    []row{{ID: "a", Label: "A"}},
		"answer":    ListAnswer{Items: []map[string]any{{"id": "a", "label": "A"}}},
	} {
		got, err := ParseChoices(result)
		if err != nil || len(got) != 1 || got[0] != (Choice{ID: "a", Label: "A"}) {
			t.Fatalf("%s: %+v %v", name, got, err)
		}
	}
	// A row whose Data is a plain string names that thing (a Commands row
	// command:greet holds greet).
	if got, err := ParseChoices([]ListItem{{ID: "command:greet", Label: "greet", Data: "greet"}}); err != nil || got[0].ID != "greet" {
		t.Fatalf("string data: %+v %v", got, err)
	}
	if _, err := ParseChoices(42); err == nil {
		t.Fatal("an int read as choices")
	}
}

// A failed choices source says why in the one format, naming the noun,
// even when the chooser has fixed rows ("(none)") under it (R5.19).
func TestChoiceChooserFailureShowsOverFixedRows(t *testing.T) {
	t.Parallel()
	r, _ := widgetFacts(t, func(Ctx) Result { return Failed(errors.New("widget host down")) })
	c, err := r.openChoiceChooser(ChooserSpec{Kind: "pick-widget", Title: "Choose widget", Choose: func(ChooserRow) Result { return Done(nil) }}, choiceLoad{
		ctx:      r.Context(),
		param:    Parameter{Noun: "widget"},
		siblings: func() Arguments { return nil },
		label:    func(c Choice) string { return c.Label },
		fixed:    []ChooserRow{{ID: "", Label: "(none)", Fixed: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	view := c.View(80, 10).Text
	if !strings.Contains(view, "no widgets: ") || !strings.Contains(view, "widget host down") || !strings.Contains(view, "(none)") {
		t.Fatalf("view:\n%s", view)
	}
}

func TestPluralWord(t *testing.T) {
	t.Parallel()
	for word, want := range map[string]string{"widget": "widgets", "box": "boxes", "query": "queries", "day": "days", "branch": "branches", "": ""} {
		if got := pluralWord(word); got != want {
			t.Fatalf("%q: %q, want %q", word, got, want)
		}
	}
}

// Typing a noun parameter's word in the palette never waits on a Pending
// list (N4): the keys echo at once, the list runs once per line as a job
// of the palette, the completions say they are loading until it lands,
// then offer its rows.
func TestPaletteRemoteChoicesLoadOffTheLoop(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	var calls atomic.Int32
	r, m := widgetFacts(t, func(Ctx) Result {
		calls.Add(1)
		return Pending(func(context.Context) (any, error) {
			<-release
			return remoteWidgetRows(), nil
		})
	})
	p := m.Palette
	if err := p.Open(); err != nil {
		t.Fatal(err)
	}
	typed := make(chan string, 1)
	go func() {
		for _, line := range []string{"widget get ", "widget get x", "widget get xy"} {
			if err := p.setText(line); err != nil {
				typed <- err.Error()
				return
			}
			_ = p.View()
		}
		typed <- p.View()
	}()
	var view string
	select {
	case view = <-typed:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("typing waited on the pending list")
	}
	if !strings.Contains(view, ": widget get xy") || !strings.Contains(view, "loading widgets…") {
		t.Fatalf("view while loading:\n%s", view)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("list called %d times for one line", n)
	}
	close(release)
	settleAll(t, r)
	if err := p.setText("widget get "); err != nil {
		t.Fatal(err)
	}
	view = p.View()
	if !strings.Contains(view, "widget/a") || strings.Contains(view, "loading") {
		t.Fatalf("view after landing:\n%s", view)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("list called %d times after landing", n)
	}
	// Opening again loads afresh: the answer was for that line.
	p.Close()
	if err := p.Open(); err != nil {
		t.Fatal(err)
	}
	if err := p.setText("widget get "); err != nil {
		t.Fatal(err)
	}
	if view := p.View(); !strings.Contains(view, "loading widgets…") {
		t.Fatalf("view after reopening:\n%s", view)
	}
	if n := calls.Load(); n != 2 {
		t.Fatalf("list called %d times after reopening", n)
	}
}

// A boolean parameter given by name reads true and false as booleans,
// for every boolean (N7): NAME=VALUE as the line spells a request back,
// --NAME=VALUE and --NAME VALUE alike; a bare word fills a boolean only
// when it is true or false, else the next parameter that takes it.
func TestPaletteBooleanByName(t *testing.T) {
	t.Parallel()
	r := NewRuntime()
	r.Config().Disable()
	r.Register(Operation{Noun: "task", Verb: "edit", Parameters: map[string]Parameter{
		"id":      {Type: "string", Required: true},
		"field":   {Type: "string"},
		"chooser": {Type: "boolean"},
	}, Run: func(*Runtime, Context, Arguments) (any, error) { return true, nil }})
	r.Register(Operation{Noun: "app", Verb: "halt", Parameters: map[string]Parameter{"force": {Type: "boolean"}},
		Run: func(*Runtime, Context, Arguments) (any, error) { return true, nil }})
	p := NewPalette(r)
	p.origin = r.Context()
	for line, want := range map[string]Arguments{
		"task edit a field=status chooser=false": {"id": "a", "field": "status", "chooser": false},
		"task edit a chooser=true field=status":  {"id": "a", "field": "status", "chooser": true},
		"task edit a --chooser=false":            {"id": "a", "chooser": false},
		"task edit a --chooser false status":     {"id": "a", "field": "status", "chooser": false},
		"task edit a status":                     {"id": "a", "field": "status"},
		"task edit a status false":               {"id": "a", "field": "status", "chooser": false},
		"task edit a true status":                {"id": "a", "field": "status", "chooser": true},
		"app halt force=true":                    {"force": true},
		"app halt false":                         {"force": false},
	} {
		c := p.parse(line)
		if c.Err != nil || len(c.Args) != len(want) {
			t.Fatalf("%q: %v %v", line, c.Args, c.Err)
		}
		for k, v := range want {
			if c.Args[k] != v {
				t.Fatalf("%q: %s=%#v, want %#v (%v)", line, k, c.Args[k], v, c.Args)
			}
		}
	}
	for _, line := range []string{"task edit a chooser=maybe", "app halt maybe"} {
		if c := p.parse(line); c.Err == nil {
			t.Fatalf("%q read: %v", line, c.Args)
		}
	}
}
