package ochazuke

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

// at splits "text with a | cursor" into its runes and the cursor offset.
func at(marked string) ([]rune, int) {
	i := strings.Index(marked, "|")
	return []rune(marked[:i] + marked[i+1:]), len([]rune(marked[:i]))
}

func TestCompletionTokenBoundaries(t *testing.T) {
	triggers := []string{"@", "$", "[", "[["}
	for _, tc := range []struct {
		marked  string
		ok      bool
		token   string // the token's text, trigger included
		trigger string
		query   string
	}{
		{marked: "Please review @src/ma|", ok: true, token: "@src/ma", trigger: "@", query: "src/ma"},
		{marked: "@|", ok: true, token: "@", trigger: "@", query: ""},
		{marked: "a @src/main.go| b", ok: true, token: "@src/main.go", trigger: "@", query: "src/main.go"},
		// The cursor inside an existing token: the query is still the whole token.
		{marked: "@src/m|ain.go and", ok: true, token: "@src/main.go", trigger: "@", query: "src/main.go"},
		{marked: "@|src/main.go", ok: true, token: "@src/main.go", trigger: "@", query: "src/main.go"},
		// A slash never splits the token.
		{marked: "see @a/b/c/|d", ok: true, token: "@a/b/c/d", trigger: "@", query: "a/b/c/d"},
		{marked: "run $sk|", ok: true, token: "$sk", trigger: "$", query: "sk"},
		// Longest trigger: [[ beats [.
		{marked: "[[no|te", ok: true, token: "[[note", trigger: "[[", query: "note"},
		{marked: "[no|te", ok: true, token: "[note", trigger: "[", query: "note"},
		// A newline ends a token like any whitespace.
		{marked: "one\n@tw|o\nthree", ok: true, token: "@two", trigger: "@", query: "two"},
		{marked: "tab\t@x|", ok: true, token: "@x", trigger: "@", query: "x"},
		// A trigger only counts at a token start.
		{marked: "mail me@host|", ok: false},
		// The cursor before the trigger, or on whitespace after the token.
		{marked: "a |@src", ok: false},
		{marked: "@src |", ok: false},
		{marked: "plain wor|d", ok: false},
		{marked: "|", ok: false},
		// Inside a two-character trigger is not past it: the token's
		// trigger is still the longest, [[.
		{marked: "[|[note", ok: false},
	} {
		text, cursor := at(tc.marked)
		tok, i, ok := FindCompletionToken(text, cursor, triggers)
		if ok != tc.ok {
			t.Fatalf("%q: ok %v, want %v (%+v)", tc.marked, ok, tc.ok, tok)
		}
		if !ok {
			if i != -1 {
				t.Fatalf("%q: index %d without a token", tc.marked, i)
			}
			continue
		}
		if got := string(text[tok.Start:tok.End]); got != tc.token || tok.Trigger != tc.trigger || tok.Query != tc.query || triggers[i] != tc.trigger {
			t.Fatalf("%q: token %q trigger %q query %q (index %d), want %q %q %q", tc.marked, got, tok.Trigger, tok.Query, i, tc.token, tc.trigger, tc.query)
		}
	}
}

func TestCompletionTriggerOrderDoesNotDecideOverlap(t *testing.T) {
	text, cursor := at("[[wi|ki")
	for _, triggers := range [][]string{{"[", "[["}, {"[[", "["}} {
		tok, _, ok := FindCompletionToken(text, cursor, triggers)
		if !ok || tok.Trigger != "[[" || tok.Query != "wiki" {
			t.Fatalf("%v: %+v %v, want the longest trigger [[", triggers, tok, ok)
		}
	}
}

func TestCompletionTriggersInputBeforeNoun(t *testing.T) {
	r := NewRuntime()
	none := func(context.Context, string) ([]Suggestion, error) { return nil, nil }
	r.Define("chat").Completions(CompletionTrigger{Trigger: "$", Search: none})
	own := NewInput("own", "", false)
	if err := r.AddInput(own, Kind("chat"), Completions(CompletionTrigger{Trigger: "@", Search: none})); err != nil {
		t.Fatal(err)
	}
	shared := NewInput("shared", "", false)
	if err := r.AddInput(shared, Kind("chat")); err != nil {
		t.Fatal(err)
	}
	plain := NewInput("plain", "", false)
	if err := r.AddInput(plain); err != nil {
		t.Fatal(err)
	}
	names := func(id string) []string {
		var out []string
		for _, tr := range r.CompletionTriggers(id) {
			out = append(out, tr.Trigger)
		}
		return out
	}
	if got := names("own"); !slices.Equal(got, []string{"@"}) {
		t.Fatalf("own input: %v", got)
	}
	if got := names("shared"); !slices.Equal(got, []string{"$"}) {
		t.Fatalf("noun's input: %v", got)
	}
	if got := names("plain"); len(got) != 0 {
		t.Fatalf("plain input: %v", got)
	}
}

func TestCompletionTriggersAreValidated(t *testing.T) {
	r := NewRuntime()
	none := func(context.Context, string) ([]Suggestion, error) { return nil, nil }
	for i, tr := range []CompletionTrigger{{Trigger: "", Search: none}, {Trigger: "@ x", Search: none}, {Trigger: "@"}} {
		in := NewInput("in"+string(rune('a'+i)), "", false)
		if err := r.AddInput(in, Completions(tr)); err == nil {
			t.Fatalf("trigger %+v was accepted", tr)
		}
	}
	defer func() {
		if recover() == nil {
			t.Fatal("NounBuilder.Completions accepted an empty trigger")
		}
	}()
	r.Define("bad").Completions(CompletionTrigger{Search: none})
}

func TestSearchStrings(t *testing.T) {
	search := SearchStrings(func(_ context.Context, q string) ([]string, error) {
		if q == "fail" {
			return nil, errors.New("down")
		}
		return []string{q + "1", q + "2"}, nil
	})
	got, err := search(context.Background(), "a")
	if err != nil || len(got) != 2 || got[0] != (Suggestion{Text: "a1"}) || got[1].label() != "a2" {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := search(context.Background(), "fail"); err == nil {
		t.Fatal("the error was lost")
	}
}
