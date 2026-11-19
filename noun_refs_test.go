package ochazuke

import (
	"slices"
	"strings"
	"testing"
)

// nounRefRig is a completion rig with a tag noun that creates, holding
// "Alpha" and "North Star", and text.noun-refs set to on.
func nounRefRig(t *testing.T, on bool) *completionRig {
	t.Helper()
	g := newCompletionRig(t, "", true)
	g.r.Define("tag").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "title", Role: RoleLabel}).Store(DocStore(NewMemDocTable(g.r, "tag"), "tag"))
	for _, name := range []string{"Alpha", "North Star"} {
		if _, err := g.r.createNamed("tag", name); err != nil {
			t.Fatal(err)
		}
	}
	if err := g.r.Settings().Set(NounRefsSetting, on); err != nil {
		t.Fatal(err)
	}
	return g
}

// text.noun-refs: off, "#" is only text; on, it lists the nouns with a
// picker, and "# " (a heading) closes the list and leaves the text.
func TestNounRefsSetting(t *testing.T) {
	for _, on := range []bool{false, true} {
		g := nounRefRig(t, on)
		g.typeText("#ta")
		g.settle()
		if g.c.Open() != on || on && !slices.Contains(g.shown(), "tag:") {
			t.Fatalf("on %v: open %v shown %v", on, g.c.Open(), g.shown())
		}
	}
	g := nounRefRig(t, true)
	g.typeText("# Notes")
	g.settle()
	if g.c.Open() || string(g.in.Text) != "# Notes" {
		t.Fatalf("heading: open %v text %q", g.c.Open(), string(g.in.Text))
	}
}

// "#ta" and tab complete the noun with its colon and list its things;
// picking one inserts #noun:Label,
// quoted when it has a space, and the token runs over the quotes; the
// last row creates the typed name and inserts its reference.
func TestNounRefsPickThingAndCreate(t *testing.T) {
	g := nounRefRig(t, true)
	g.typeText("see #ta")
	g.settle()
	g.keys("tab")
	g.answers()
	if string(g.in.Text) != "see #tag:" || !g.c.Open() || !slices.Contains(g.shown(), "tag:Alpha") {
		t.Fatalf("noun: text %q open %v shown %v", string(g.in.Text), g.c.Open(), g.shown())
	}
	g.typeText("nor")
	g.settle()
	g.keys("tab")
	if want := `see #tag:"North Star"`; string(g.in.Text) != want || g.c.Open() {
		t.Fatalf("thing: text %q, want %q (open %v)", string(g.in.Text), want, g.c.Open())
	}
	if tok, _, ok := FindCompletionTokenIn(g.in.Text, 12, g.r.CompletionTriggers(g.in.ID)); !ok || tok.Start != 4 || tok.End != len(g.in.Text) {
		t.Fatalf("quoted token %+v ok %v", tok, ok)
	}

	g.typeText(" #tag:Fresh")
	g.settle()
	rows := g.c.Suggestions()
	if len(rows) != 1 || rows[0].Label != `Create "Fresh"…` {
		t.Fatalf("create row %+v", rows)
	}
	g.keys("tab")
	if want := `see #tag:"North Star" #tag:Fresh`; string(g.in.Text) != want {
		t.Fatalf("create: text %q, want %q", string(g.in.Text), want)
	}
	if tags, err := g.r.Choices(g.r.Context(), Parameter{Noun: "tag"}); err != nil || len(tags) != 3 {
		t.Fatalf("tags after create: %v %v", tags, err)
	}
}

// A reference is drawn as a chip (text.noun-ref-chips) that backspace
// erases whole; off, it is plain text erased a rune at a time.
func TestNounRefChips(t *testing.T) {
	for _, on := range []bool{true, false} {
		g := nounRefRig(t, true)
		if err := g.r.Settings().Set(NounRefChipsSetting, on); err != nil {
			t.Fatal(err)
		}
		g.typeText("see #tag:Alpha")
		g.c.dismiss()
		if drawn := InputBlock(g.in, 40, 1).Text; strings.Contains(drawn, "\x1b[4m#tag:Alpha\x1b[24m") != on {
			t.Fatalf("on %v: drawn %q", on, drawn)
		}
		g.keys("backspace")
		want := "see "
		if !on {
			want = "see #tag:Alph"
		}
		if string(g.in.Text) != want {
			t.Fatalf("on %v: after backspace %q, want %q", on, string(g.in.Text), want)
		}
	}
}

// A form over a thing links on its write, not on typing: the references
// the written text holds are related (kind mentions), one removed before
// the write never is, and one the text loses is unrelated.
func TestNounRefsLinkOnWrite(t *testing.T) {
	g := nounRefRig(t, true)
	g.r.SetRelationStore(NewMemRelations())
	self, err := g.r.createNamed("tag", "Self")
	if err != nil {
		t.Fatal(err)
	}
	spec, err := g.r.EditSpec("tag", self)
	if err != nil {
		t.Fatal(err)
	}
	e, err := g.r.AddRecordEditor("placed", spec)
	if err != nil {
		t.Fatal(err)
	}
	c := Ctx{Runtime: g.r, Context: g.r.Context()}
	key := func(label string) string {
		k, ok := g.r.nounRefKey(c, Ref{Noun: "tag", Key: label})
		if !ok {
			t.Fatalf("no tag %q", label)
		}
		return "tag:" + k
	}
	links := func() []string {
		rows, err := g.r.RelationStore().Relations(RelationQuery{Kind: NounRefKind})
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, x := range rows {
			out = append(out, x.To)
		}
		return out
	}
	f := e.field("title")
	write := func(texts ...string) {
		for _, text := range texts {
			if err := e.set(f, text); err != nil {
				t.Fatal(err)
			}
		}
		if err := e.seal(f); err != nil {
			t.Fatal(err)
		}
	}
	write("Self #tag:Alpha", `Self #tag:"North Star"`)
	if got := links(); !slices.Equal(got, []string{key("North Star")}) {
		t.Fatalf("after write: %v", got)
	}
	write("Self #tag:Alpha")
	if got := links(); !slices.Equal(got, []string{key("Alpha")}) {
		t.Fatalf("after swap: %v", got)
	}
}
