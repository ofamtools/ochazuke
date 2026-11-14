package ochazuke

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestUniquePrefix(t *testing.T) {
	t.Parallel()
	words := []string{"context-item", "copy", "cmd.cmd", "git.repo", "git.branch", "tag", "tag", "tags", "ochazuke.binding"}
	for word, want := range map[string]int{
		"context-item":     3, // con: c is shared with copy and cmd, co with copy
		"copy":             3, // cop
		"cmd.cmd":          2, // cm
		"git.repo":         5, // git.r: the dot is no boundary
		"git.branch":       5, // git.b
		"tag":              3, // a prefix of tags, and shown as noun and verb: whole
		"tags":             4,
		"ochazuke.binding": 1,
	} {
		if got := uniquePrefix(word, words); got != want {
			t.Errorf("uniquePrefix(%q) = %d, want %d", word, got, want)
		}
	}
}

func TestBoardFilterIsPrefixFirstAndNeverFuzzy(t *testing.T) {
	t.Parallel()
	entries := []PaletteEntry{
		{Value: "attach", Kind: "verb", Nouns: []string{"x"}}, {Value: "task", Kind: "noun", Verbs: []string{"x"}}, {Value: "json.doc", Kind: "noun", Verbs: []string{"x"}},
		{Value: "tag", Kind: "verb", Nouns: []string{"x"}}, {Value: "tag", Kind: "noun", Verbs: []string{"x"}}, {Value: "delta", Kind: "verb", Nouns: []string{"x"}},
	}
	var got []string
	for _, e := range boardFilter(slices.Clone(entries), "ta") {
		got = append(got, e.Kind+":"+e.Value)
	}
	if want := []string{"noun:task", "noun:tag", "verb:tag"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ta = %v, want %v (attach and delta hold t, a but do not start with it)", got, want)
	}
	got = nil
	for _, e := range boardFilter(slices.Clone(entries), "doc") {
		got = append(got, e.Value)
	}
	if !reflect.DeepEqual(got, []string{"json.doc"}) {
		t.Fatalf("doc = %v: typing after the dot reaches json.doc", got)
	}
}

// boardRuntime is a runtime with the board style on: notes and tags, with
// tag a noun and a verb, and the palette open.
func boardRuntime(t *testing.T) (*Runtime, *Palette, *[]string) {
	t.Helper()
	r := NewRuntime()
	if err := r.Settings().Set(PaletteStyleSetting, PaletteStyleBoard); err != nil {
		t.Fatal(err)
	}
	ran := new([]string)
	run := func(_ *Runtime, c Context, a Arguments) (any, error) {
		*ran = append(*ran, focusOf(c))
		return nil, nil
	}
	for _, key := range []Key{"note.tag", "note.show", "task.tag", "task.complete", "tag.show", "tag.merge", "attach.add"} {
		r.Register(Operation{Key: key, Run: run})
	}
	r.Instance("home")
	p := NewPalette(r)
	paletteRequest(t, r, "open", "palette", nil)
	return r, p, ran
}

func cellValues(p *Palette) (out []string) {
	for _, e := range p.completions() {
		out = append(out, e.Kind+":"+e.Value)
	}
	return out
}

// The board lists nouns then verbs; letters narrow by prefix, the cell
// left commits itself, and the second word offers only the other kind.
func TestBoardNarrowsCommitsAndShowsTheOtherKind(t *testing.T) {
	_, p, _ := boardRuntime(t)
	if got := cellValues(p); !slices.Contains(got, "noun:note") || !slices.Contains(got, "verb:tag") || slices.Index(got, "noun:note") > slices.Index(got, "verb:tag") {
		t.Fatalf("the empty board = %v: nouns, then verbs", got)
	}
	typeLine(t, p, "ta")
	if got, want := cellValues(p), []string{"noun:tag", "noun:task", "verb:tag"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ta = %v, want %v: tag as a noun and as a verb, task; attach is no prefix hit", got, want)
	}
	if p.Text() != "ta" {
		t.Fatalf("two readings left, yet the line committed: %q", p.Text())
	}
	// Choose the verb reading of tag: only the nouns that have it follow.
	paletteKeys(t, p, "ctrl+n", "ctrl+n", "ctrl+n")
	paletteKeys(t, p, "enter")
	if p.Text() != "tag " {
		t.Fatalf("enter on the verb cell = %q", p.Text())
	}
	if got, want := cellValues(p), []string{"noun:note", "noun:task"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after the verb tag = %v, want %v", got, want)
	}
	// One letter narrows to one cell and the pair runs.
	typeLine(t, p, "n")
	if p.IsOpen() {
		t.Fatalf("the one cell left did not commit and run: %q", p.Text())
	}
}

func TestBoardNounFirstShowsOnlyThatNounsVerbs(t *testing.T) {
	_, p, _ := boardRuntime(t)
	typeLine(t, p, "tas")
	if p.Text() != "task " {
		t.Fatalf("a whole noun left alone commits itself: %q", p.Text())
	}
	if got, want := cellValues(p), []string{"verb:complete", "verb:tag"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("task's verbs = %v, want %v", got, want)
	}
	// backspace goes back a layer, esc goes up, an empty esc closes.
	paletteKeys(t, p, "backspace")
	if p.Text() != "" {
		t.Fatalf("backspace after a committed word left %q", p.Text())
	}
	typeLine(t, p, "n")
	paletteKeys(t, p, "esc")
	if p.Text() != "" || !p.IsOpen() {
		t.Fatalf("esc with letters = %q open=%v", p.Text(), p.IsOpen())
	}
	paletteKeys(t, p, "esc")
	if p.IsOpen() {
		t.Fatal("esc on an empty board left it open")
	}
}

func TestBoardCellEdges(t *testing.T) {
	_, p, _ := boardRuntime(t)
	p.cols = 2
	for to, want := range map[string]int{"last": len(p.completions()) - 1, "first": 0} {
		if err := p.cellTo(to); err != nil || p.highlight != want {
			t.Fatalf("%s: highlight %d (%v), want %d", to, p.highlight, err, want)
		}
	}
	_ = p.cellTo("first")
	_ = p.cellTo("right")
	_ = p.cellTo("last-col")
	rows := boardRows(p.completions(), 2)
	if want := rows[0][len(rows[0])-1]; p.highlight != want {
		t.Fatalf("last-col = %d, want %d", p.highlight, want)
	}
	_ = p.cellTo("first-col")
	if p.highlight != rows[0][0] {
		t.Fatalf("first-col = %d", p.highlight)
	}
	_ = p.cellTo("down")
	if p.highlight != rows[1][0] {
		t.Fatalf("down = %d, want %d", p.highlight, rows[1][0])
	}
}

// The board draws the unique prefix on each cell and a help line for the
// current one.
func TestBoardViewShowsPrefixesCountsAndHelp(t *testing.T) {
	_, p, _ := boardRuntime(t)
	typeLine(t, p, "ta")
	view := p.View()
	for _, want := range []string{"nouns (2)", "verbs (1)", "tag is a noun with 2 verbs: merge show"} {
		if !strings.Contains(ansiStrip(view), want) {
			t.Fatalf("board lacks %q:\n%s", want, view)
		}
	}
}

func ansiStrip(s string) string {
	var out strings.Builder
	skip := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			skip = true
		case skip && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'):
			skip = false
		case !skip:
			out.WriteRune(r)
		}
	}
	return out.String()
}

// A verb guarded off where the palette opened is still offered, and
// choosing it runs it at a thing of its noun.
func TestPaletteReachRunsAGuardedVerbAtItsNoun(t *testing.T) {
	r := NewRuntime()
	r.DefineGuard("on-card", func(r *Runtime, ev KeyEvent) bool {
		return r.Instances[ev.Focus] != nil && r.Instances[ev.Focus].Noun == "card"
	})
	var ranAt []string
	r.Define("card").Verb("flip", func(_ *Runtime, c Context, _ Arguments) (any, error) {
		ranAt = append(ranAt, focusOf(c))
		return nil, nil
	}).When("on-card")
	r.Instance("home")
	r.Instance("card-1", Kind("card"))
	p := NewPalette(r)
	paletteRequest(t, r, "open", "palette", nil)
	typeLine(t, p, "card flip")
	if c := p.parse(p.Text()); c.Err != nil || c.Op.Key.Short() != "card.flip" {
		t.Fatalf("card flip from home = %v %v", c.Op.Key, c.Err)
	}
	paletteKeys(t, p, "enter")
	if !reflect.DeepEqual(ranAt, []string{"card-1"}) {
		t.Fatalf("flip ran at %v, want card-1 (err %q)", ranAt, p.message)
	}
	// Off, the verb is left out where its guard is false.
	reachOff(t, r)
	paletteRequest(t, r, "open", "palette", nil)
	paletteRequest(t, r, "fill", "palette", Arguments{"text": "card flip"})
	if c := p.parse(p.Text()); c.Err == nil {
		t.Fatal("palette.reach off still reads card flip at home")
	}
}

// A target nothing focused supplies is asked for with the noun's picker
// (the args form), never refused; focused, the line runs as it did.
func TestPaletteMissingTargetOpensThePicker(t *testing.T) {
	build := func() (*Runtime, *Palette, *[]string) {
		r := NewRuntime()
		ran := new([]string)
		r.Register(Operation{Key: "card.flip", Parameters: map[string]Parameter{"card": {Type: "string", Noun: "card", Focus: true, Accept: AcceptKeep}}, Run: func(_ *Runtime, _ Context, a Arguments) (any, error) {
			*ran = append(*ran, StringArg(a, "card", ""))
			return nil, nil
		}})
		r.Instance("home")
		r.Instance("card-1", Kind("card"))
		p := NewPalette(r)
		return r, p, ran
	}
	r, p, ran := build()
	paletteRequest(t, r, "open", "palette", nil)
	typeLine(t, p, "card flip")
	if c := p.parse(p.Text()); c.Missing != "card" || c.Err != nil {
		t.Fatalf("nothing focused: missing %q err %v, want the card to ask for", c.Missing, c.Err)
	}
	paletteKeys(t, p, "enter")
	if r.Instances[ArgsFormID] == nil || len(*ran) != 0 {
		t.Fatalf("enter did not open the fill-in form (ran %v, err %q)", *ran, p.message)
	}
	// Focused on a card, the focus supplies it.
	r, p, _ = build()
	if err := r.SetFocus("card-1"); err != nil {
		t.Fatal(err)
	}
	paletteRequest(t, r, "open", "palette", nil)
	if c := p.parse("card flip"); c.Missing != "" {
		t.Fatalf("a focused card still missing %q", c.Missing)
	}
}
