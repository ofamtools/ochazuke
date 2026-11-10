package ochazuke

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"ofam.dev/ochazuke/internal/readertest"
)

// defineReaderFixture registers readertest's operations on r: each with
// its parameters and alias, the main noun marked, every run recorded in ran
// as "noun.action k=v,...".
func defineReaderFixture(r *Runtime, ran *[]string) {
	for _, o := range readertest.Ops {
		params, positional := map[string]Parameter{}, []string(nil)
		for _, p := range o.Params {
			params[p.Name] = Parameter{Type: p.Type, Enum: p.Enum}
			if p.Positional {
				positional = append(positional, p.Name)
			}
		}
		key := KeyOf(o.Noun, o.Action)
		r.Register(Operation{Key: key, Parameters: params, Positional: positional, Run: func(_ *Runtime, _ Context, a Arguments) (any, error) {
			*ran = append(*ran, key.Short()+" "+ArgsText(a))
			return nil, nil
		}})
		if o.Alias != "" {
			if err := r.AddActionAlias(ActionAlias{Alias: o.Noun + "." + o.Alias, Action: key.Short()}); err != nil {
				panic(err)
			}
		}
	}
	r.Nouns[readertest.MainNoun].Main = true
}

// ArgsText is a request's arguments as readertest spells them.
func ArgsText(a Arguments) string {
	var out []string
	for k, v := range a {
		out = append(out, k+"="+fmt.Sprint(v))
	}
	slices.Sort(out)
	return strings.Join(out, ",")
}

// The palette reads readertest's lines as the CLI does (cli's
// TestReaderTable runs the same rows).
func TestReaderTablePalette(t *testing.T) {
	r := NewRuntime()
	reachOff(t, r) // the table is the CLI's reading: only the verbs the focus allows
	var ran []string
	defineReaderFixture(r, &ran)
	p := NewPalette(r)
	paletteRequest(t, r, "open", "palette", nil)
	for _, row := range readertest.Rows {
		c := p.parse(row.Line)
		got, args := c.Op.Key.Short(), ArgsText(c.Args)
		if c.Op.Name() == "" {
			got = ""
		}
		switch {
		case row.Err != "":
			if c.Err == nil || !strings.Contains(c.Err.Error(), row.Err) {
				t.Errorf("%q: op %q err %v, want an error holding %q", row.Line, got, c.Err, row.Err)
			}
		case c.Err != nil || got != row.Op || args != row.Args:
			t.Errorf("%q: op %q args %q err %v, want %s %q", row.Line, got, args, c.Err, row.Op, row.Args)
		}
	}
	for _, tc := range readertest.Completions {
		rd := p.reading(p.index(), nil)
		if got := r.CompleteWords(rd, tc.Words); !slices.Equal(got, tc.Want) {
			t.Errorf("complete %q = %v, want %v", tc.Words, got, tc.Want)
		}
	}
}

// The registry's own lookups: both orders, an empty word a prefix of
// everything, punctuation narrowing, the ties when two readings fit.
func TestOperationsLookup(t *testing.T) {
	r := NewRuntime()
	var ran []string
	defineReaderFixture(r, &ran)
	keys := func(cs []Candidate) (out []string) {
		for _, c := range cs {
			out = append(out, c.Operation.Key.Short())
		}
		slices.Sort(out)
		return slices.Compact(out)
	}
	for _, tc := range []struct {
		line string
		want []string
	}{
		{"quad warp", []string{"quad.warp"}},
		{"warp quad", []string{"quad.warp"}},
		{"json.doc.rows", []string{"json.doc.rows"}},
		{"rows, md.doc", []string{"md.doc.rows"}},
		{"q w", []string{"quad.warp", "quill.wipe"}},
		{"define plan", []string{"plan.define"}},
		{DefaultNamespace + ".plan.define", []string{"plan.define"}},
	} {
		if got := keys(r.Operations.Lookup(tc.line)); !slices.Equal(got, tc.want) {
			t.Errorf("Lookup(%q) = %v, want %v", tc.line, got, tc.want)
		}
	}
	if got := keys(r.Operations.SplitLookup("wipe", "")); !slices.Equal(got, []string{"quill.wipe"}) {
		t.Errorf("SplitLookup(wipe, \"\") = %v", got)
	}
	if got := keys(r.Operations.SplitLookup("quill", "wipe")); !slices.Equal(got, []string{"quill.wipe"}) {
		t.Errorf("SplitLookup(quill, wipe) = %v", got)
	}
}

// With lexicon.punctuation off, a comma is an ordinary character: rows,
// names nothing.
func TestReaderPunctuationSetting(t *testing.T) {
	r := NewRuntime()
	var ran []string
	defineReaderFixture(r, &ran)
	p := NewPalette(r)
	paletteRequest(t, r, "open", "palette", nil)
	if c := p.parse("rows, json.doc"); c.Op.Key.Short() != "json.doc.rows" {
		t.Fatalf("on: %q %v", c.Op.Key.Short(), c.Err)
	}
	if err := r.Settings().Set(LexiconPunctuationSetting, "false"); err != nil {
		t.Fatal(err)
	}
	if c := p.parse("rows, json.doc"); c.Op.Name() != "" {
		t.Fatalf("off: %q, want nothing read", c.Op.Key.Short())
	}
}

// A noun's default action runs when no action word follows it.
func TestReaderDefaultAction(t *testing.T) {
	r := NewRuntime()
	var ran []string
	defineReaderFixture(r, &ran)
	r.Nouns["plan"].DefaultAction = "define"
	p := NewPalette(r)
	paletteRequest(t, r, "open", "palette", nil)
	if c := p.parse("plan"); c.Op.Key.Short() != "plan.define" || !c.Complete() {
		t.Fatalf("plan: %q missing %q err %v", c.Op.Key.Short(), c.Missing, c.Err)
	}
	if c := p.parse("quill"); c.Op.Name() != "" || c.Missing != "verb" {
		t.Fatalf("quill (one action, no default): %q missing %q, want the verb missing", c.Op.Key.Short(), c.Missing)
	}
}
