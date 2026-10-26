package ochazuke

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
)

// Inline completion: an application gives an input (or every input of a
// noun) triggers such as "@", "$" or "[[", each with a search. Typing a
// trigger at the start of a token offers the search's suggestions for the
// rest of the token, while typing keeps editing the same input.
//
// The token runs from the trigger to the next whitespace (a "/" never
// splits it) and the query is the whole token after the trigger, wherever
// the cursor sits inside it. Accepting a suggestion replaces the whole
// token, keeping the trigger, as one undoable edit. Of triggers that
// overlap ("[" and "[["), the longest that starts the token wins.

// Suggestion is one completion a search offers. Text replaces the token
// after its trigger; Label is what the list shows ("" shows Text); Detail
// is drawn muted after the label.
type Suggestion struct {
	Text   string `json:"text"`
	Label  string `json:"label,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// label is what the suggestion list draws for the suggestion.
func (s Suggestion) label() string {
	if s.Label != "" {
		return s.Label
	}
	return s.Text
}

// CompletionSearch answers a query, the token after its trigger, with
// suggestions. It runs off the loop, as a job; its context is cancelled
// when the answer can no longer be shown (the suggestions were dismissed,
// or a newer answer is already on screen). An error is shown in the list.
type CompletionSearch func(ctx context.Context, query string) ([]Suggestion, error)

// SearchStrings adapts a search that answers plain strings, for the usual
// case where what the list shows is what is inserted.
func SearchStrings(search func(ctx context.Context, query string) ([]string, error)) CompletionSearch {
	return func(ctx context.Context, query string) ([]Suggestion, error) {
		texts, err := search(ctx, query)
		if err != nil {
			return nil, err
		}
		out := make([]Suggestion, len(texts))
		for i, text := range texts {
			out[i] = Suggestion{Text: text}
		}
		return out, nil
	}
}

// CompletionTrigger is one trigger of an input: the characters that start
// a completion token and the search that answers its query.
type CompletionTrigger struct {
	Trigger string
	Search  CompletionSearch
}

// Completions gives an input its completion triggers (AddInput reads it).
// They replace the triggers its noun declares (NounBuilder.Completions).
func Completions(triggers ...CompletionTrigger) InstanceOption {
	return func(s *instanceSpec) { s.completions = slices.Clone(triggers) }
}

// Completions declares the completion triggers of every input of the noun
// that has none of its own (Input.Completions).
func (b *NounBuilder) Completions(triggers ...CompletionTrigger) *NounBuilder {
	if err := validTriggers(triggers); err != nil {
		panic(err)
	}
	b.n.Completions = slices.Clone(triggers)
	return b
}

// validTriggers refuses a trigger with no characters, with whitespace (a
// token never holds any) or without a search.
func validTriggers(triggers []CompletionTrigger) error {
	for _, t := range triggers {
		switch {
		case t.Trigger == "":
			return errors.New("completion trigger must not be empty")
		case strings.IndexFunc(t.Trigger, unicode.IsSpace) >= 0:
			return fmt.Errorf("completion trigger %q must not hold whitespace", t.Trigger)
		case t.Search == nil:
			return fmt.Errorf("completion trigger %q needs a search", t.Trigger)
		}
	}
	return nil
}

// CompletionTriggers are the triggers an input completes with: its own,
// else its noun's.
func (r *Runtime) CompletionTriggers(id string) []CompletionTrigger {
	in := r.Input(id)
	if in == nil {
		return nil
	}
	if len(in.Completions) > 0 {
		return in.Completions
	}
	if inst := r.Instances[id]; inst != nil {
		if n := r.Nouns[inst.Noun]; n != nil {
			return n.Completions
		}
	}
	return nil
}

// CompletionToken is the trigger token under an input's cursor: the rune
// range [Start, End) it covers, its trigger and its query (the token after
// the trigger).
type CompletionToken struct {
	Start   int    `json:"start"`
	End     int    `json:"end"`
	Trigger string `json:"trigger"`
	Query   string `json:"query"`
}

// FindCompletionToken finds the token holding the cursor in text and the
// index of the trigger it starts with. A token is a run of non-whitespace;
// it counts only when it starts with a trigger (the longest one, when
// triggers overlap) and the cursor is past that trigger, at the token's
// end or anywhere inside it.
func FindCompletionToken(text []rune, cursor int, triggers []string) (CompletionToken, int, bool) {
	cursor = max(0, min(cursor, len(text)))
	start, end := cursor, cursor
	for start > 0 && !unicode.IsSpace(text[start-1]) {
		start--
	}
	for end < len(text) && !unicode.IsSpace(text[end]) {
		end++
	}
	token := string(text[start:end])
	best := -1
	for i, t := range triggers {
		if t != "" && strings.HasPrefix(token, t) && (best < 0 || len(t) > len(triggers[best])) {
			best = i
		}
	}
	if best < 0 {
		return CompletionToken{}, -1, false
	}
	trigger := triggers[best]
	if cursor < start+len([]rune(trigger)) {
		return CompletionToken{}, -1, false
	}
	return CompletionToken{Start: start, End: end, Trigger: trigger, Query: strings.TrimPrefix(token, trigger)}, best, true
}

// completionToken is FindCompletionToken over an input's triggers.
func completionToken(in *Input, triggers []CompletionTrigger) (CompletionToken, CompletionSearch, bool) {
	names := make([]string, len(triggers))
	for i, t := range triggers {
		names[i] = t.Trigger
	}
	tok, i, ok := FindCompletionToken(in.Text, in.Cursor, names)
	if !ok {
		return CompletionToken{}, nil, false
	}
	return tok, triggers[i].Search, true
}
