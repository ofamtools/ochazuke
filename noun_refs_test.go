package ochazuke

import (
	"slices"
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

// Picking a noun lists its things; picking one inserts #noun:Label,
// quoted when it has a space, and the token runs over the quotes; the
// last row creates the typed name and inserts its reference.
func TestNounRefsPickThingAndCreate(t *testing.T) {
	g := nounRefRig(t, true)
	g.typeText("see #tag")
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
