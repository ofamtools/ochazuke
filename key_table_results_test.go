package ochazuke

import (
	"context"
	"testing"
)

// A key presents a mounted or local verb's answer by the kind it declares:
// text in the text view, rows in the results table, any other answer not at
// all (the caller keeps its status line). keys.text-results and
// keys.table-results turn each off.
func TestKeyResultsByDeclaredKind(t *testing.T) {
	rows := []any{map[string]any{"name": "a", "n": 1}, map[string]any{"name": "b", "n": 2}}
	cases := []struct {
		name   string
		build  func(*NounBuilder) *NounBuilder
		answer any
		off    string
		modal  string
	}{
		{"text", func(b *NounBuilder) *NounBuilder { return b.TextResult() }, "one\ntwo", "", TextViewID},
		{"text off", func(b *NounBuilder) *NounBuilder { return b.TextResult() }, "one\ntwo", KeyTextResultsSetting, ""},
		{"markdown", func(b *NounBuilder) *NounBuilder { return b.MarkdownResult() }, "# Title\n\nbody", "", "markdown-view"},
		{"rows", func(b *NounBuilder) *NounBuilder { return b.RowsResult() }, rows, "", paletteResultsID},
		{"rows off", func(b *NounBuilder) *NounBuilder { return b.RowsResult() }, rows, KeyTableResultsSetting, ""},
		{"rows not objects", func(b *NounBuilder) *NounBuilder { return b.RowsResult() }, "not a list", "", ""},
		{"undeclared", func(b *NounBuilder) *NounBuilder { return b }, rows, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := NewRuntime()
			r.Define("record").Focusable()
			r.Instance("origin", Kind("record"))
			if err := r.SetFocus("origin"); err != nil {
				t.Fatal(err)
			}
			b := r.Define("record").Verb("answer", func(Ctx) Result { return Done(c.answer) }, "answers")
			c.build(b).Mode("normal").Bind("v", Do("record.answer"))
			if c.off != "" {
				if err := r.Settings().Set(c.off, false); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := r.Dispatch(MacroKey{Key: "v"}); err != nil {
				t.Fatal(err)
			}
			if got := r.TopModal(); got != c.modal {
				t.Fatalf("top modal %q, want %q", got, c.modal)
			}
		})
	}
}

// A rows answer that lands late does not cover a changed interaction.
func TestKeyRowsResultLate(t *testing.T) {
	r, _ := textResultFixture(t)
	r.Define("record").Verb("later-rows", func(Ctx) Result {
		return Pending(func(context.Context) (any, error) { return []any{map[string]any{"a": 1}}, nil })
	}, "rows off the loop").RowsResult().Mode("normal").Bind("w", Do("record.later-rows"))
	if _, err := r.Dispatch(MacroKey{Key: "w"}); err != nil {
		t.Fatal(err)
	}
	pending := r.PendingCommands()
	r.OpenTextView("newer", "keep me")
	for _, msg := range runCmd(pending) {
		if _, err := r.Deliver(msg); err != nil {
			t.Fatal(err)
		}
	}
	if in := r.Input(textViewInputID); in == nil || string(in.Text) != "keep me" {
		t.Fatalf("late rows covered the newer modal: %s", r.TopModal())
	}
}
