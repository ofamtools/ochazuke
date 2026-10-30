package ochazuke

import (
	"slices"
	"strings"
	"testing"
)

func paragraphCards() []Card {
	return []Card{
		{ID: "c1", Noun: "turn", Header: "prompt c1", Body: TextBody("one\n\ntwo\n\n```\ncode a\n\ncode b\n```\n\nthree")},
		{ID: "c2", Noun: "turn", Header: "prompt c2", Body: TextBody("next")},
	}
}

func TestParagraphChunksKeepAFenceWhole(t *testing.T) {
	var got []string
	for _, ch := range paragraphChunks("one\n\ntwo\nmore\n\n\n```\na\n\nb\n```\nthree") {
		if ch.blank {
			got = append(got, "")
			continue
		}
		got = append(got, ch.text)
	}
	want := []string{"one", "", "two\nmore", "", "", "```\na\n\nb\n```\nthree"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("chunks %q, want %q", got, want)
	}
}

// l and h mean what the place says: on a closed card open and the parent
// stop, on an open card step inside and close, inside the body enter and
// leave; ? help shows only the row that answers there.
func TestTimelineLAndHByPlace(t *testing.T) {
	f := newTimelineFixture(t, paragraphCards(), ParentStop("below"))
	help := func() map[string]string {
		out := map[string]string{}
		for _, row := range BindingHelp(f.r, f.r.Focus) {
			if row.Key == "l" || row.Key == "h" {
				out[row.Key] = row.Operation
			}
		}
		return out
	}
	check := func(place, l, h string) {
		t.Helper()
		got := help()
		if !strings.HasSuffix(got["l"], l) || !strings.HasSuffix(got["h"], h) {
			t.Fatalf("%s: help l=%q h=%q, want %s and %s", place, got["l"], got["h"], l, h)
		}
	}
	check("closed card", "timeline.open", "timeline.leave")
	f.press("l")
	if !f.tl.IsOpen("c1") || f.r.Focus != "c1" {
		t.Fatalf("l on a closed card: open %v focus %s", f.tl.IsOpen("c1"), f.r.Focus)
	}
	check("open card", "timeline.enter", "timeline.close")
	f.press("l")
	if f.r.Focus != f.tl.BodyID() || !f.tl.InBody() {
		t.Fatalf("l on an open card: focus %s", f.r.Focus)
	}
	check("body", "timeline.enter", "timeline.leave")
	if card, step, ok := f.tl.Step(); !ok || card != "c1" || step != 0 {
		t.Fatalf("Step %s %d %v", card, step, ok)
	}
	if f.r.Mode.Name != "normal" {
		t.Fatalf("mode %s", f.r.Mode.Name)
	}
	f.press("h")
	if f.r.Focus != "c1" || !f.tl.IsOpen("c1") {
		t.Fatalf("h inside: focus %s open %v", f.r.Focus, f.tl.IsOpen("c1"))
	}
	f.press("h", "h")
	if f.r.Focus != "b1" {
		t.Fatalf("h on the closed card: focus %s, want the parent stop's row b1", f.r.Focus)
	}
}

// Without a parent stop h on a closed card stays.
func TestTimelineLeaveWithoutParentStopStays(t *testing.T) {
	f := newTimelineFixture(t, paragraphCards())
	f.press("h")
	if f.r.Focus != "c1" {
		t.Fatalf("focus %s", f.r.Focus)
	}
}

// Paragraphs are steps, a fenced block one of them; the step's text is
// what y yanks, and a count stops at the last step instead of spilling.
func TestTimelineStepsWalkParagraphs(t *testing.T) {
	f := newTimelineFixture(t, paragraphCards())
	f.press("l", "l")
	var texts []string
	for range 4 {
		text, _ := f.tl.StepText()
		texts = append(texts, text)
		f.press("j")
	}
	want := []string{"one", "two", "```\ncode a\n\ncode b\n```", "three"}
	if strings.Join(texts, "|") != strings.Join(want, "|") {
		t.Fatalf("steps %q, want %q", texts, want)
	}
	if f.r.Focus != "c2" {
		t.Fatalf("j past the last step: focus %s, want c2", f.r.Focus)
	}
	f.press("k", "l", "9", "j")
	if _, step, ok := f.tl.Step(); !ok || step != 3 {
		t.Fatalf("9j: step %d in %v", step, ok)
	}
	f.press("y")
	if y, _ := f.r.ReadYank("\""); y.Text != "three" {
		t.Fatalf("yank %q", y.Text)
	}
	if !strings.Contains(f.frame, "▌three") || strings.Contains(f.frame, "▌one") {
		t.Fatalf("marker:\n%s", f.frame)
	}
}

// The body keeps the focus while its card stays selected (a streaming
// patch, SetCards of the same cards); a removed card or a selection that
// moves (G) gives it to the new card.
func TestTimelineBodyFollowsTheSelection(t *testing.T) {
	f := newTimelineFixture(t, paragraphCards())
	f.press("l", "l", "j")
	cards := f.tl.Cards()
	cards[0].Revision++
	if err := f.tl.Patch(cards[0]); err != nil {
		t.Fatal(err)
	}
	if err := f.tl.SetCards(paragraphCards()); err != nil {
		t.Fatal(err)
	}
	f.m.View()
	if _, step, ok := f.tl.Step(); !ok || step != 1 {
		t.Fatalf("after a refresh: focus %s step %d", f.r.Focus, step)
	}
	f.press("G")
	if f.r.Focus != "c2" {
		t.Fatalf("G inside: focus %s", f.r.Focus)
	}
	f.press("k", "l")
	if !f.tl.InBody() {
		t.Fatalf("back in: focus %s", f.r.Focus)
	}
	if err := f.tl.SetCards(paragraphCards()[1:]); err != nil {
		t.Fatal(err)
	}
	if f.r.Focus != "c2" {
		t.Fatalf("card removed: focus %s", f.r.Focus)
	}
}

// Closing the card from inside (enter, z M) puts the focus on the card.
func TestTimelineClosingFromInsideLeaves(t *testing.T) {
	f := newTimelineFixture(t, paragraphCards())
	f.press("l", "l", "enter")
	if f.r.Focus != "c1" || f.tl.IsOpen("c1") {
		t.Fatalf("enter inside: focus %s open %v", f.r.Focus, f.tl.IsOpen("c1"))
	}
	f.press("l", "l", "z", "M")
	if f.r.Focus != "c1" {
		t.Fatalf("z M inside: focus %s", f.r.Focus)
	}
}

// Explicit steps: the header step, a folded step that l opens (z 1 folds
// the same unit), stepping with the CLI verb.
func TestTimelineExplicitSteps(t *testing.T) {
	cards := []Card{{ID: "c1", Noun: "turn", Header: "the prompt", Open: true, Steps: []CardStep{
		{Header: true},
		{ID: "tool", Title: "tool Bash", Folded: true, Body: TextBody("ok")},
		{Body: TextBody("plain text")},
	}}}
	f := newTimelineFixture(t, cards)
	if _, err := f.r.Execute(Request{Noun: timelineNoun, Verb: "enter", Target: "c1"}); err != nil {
		t.Fatal(err)
	}
	f.m.View()
	if text, _ := f.tl.StepText(); text != "the prompt" {
		t.Fatalf("first step %q", text)
	}
	if !strings.Contains(f.frame, "▌▾ the prompt") {
		t.Fatalf("header marked:\n%s", f.frame)
	}
	f.press("j")
	if !f.tl.Folded("c1", "tool") || !strings.Contains(f.frame, "▌▸ 1 tool Bash") {
		t.Fatalf("folded tool:\n%s", f.frame)
	}
	f.press("l")
	if f.tl.Folded("c1", "1") || !strings.Contains(f.frame, "▌  ok") {
		t.Fatalf("l opens the step:\n%s", f.frame)
	}
	if text, _ := f.tl.StepText(); text != "tool Bash\nok" {
		t.Fatalf("tool text %q", text)
	}
	f.press("j")
	if text, _ := f.tl.StepText(); text != "plain text" {
		t.Fatalf("last step %q", text)
	}
	if got := strings.Count(f.frame, "▌"); got != 1 {
		t.Fatalf("%d marked lines:\n%s", got, f.frame)
	}
}

// A card without Steps draws exactly as before: sections stay numbered
// and folded the same, and each section is one step.
func TestTimelineSectionsAreSteps(t *testing.T) {
	cards := []Card{{ID: "c1", Noun: "turn", Header: "p", Open: true, Body: TextBody("intro"), Sections: []CardSection{
		{Title: "thinking", Folded: true, Body: TextBody("hmm")},
		{Title: "answer", Body: TextBody("yes\n\nand more")},
	}}}
	f := newTimelineFixture(t, cards)
	f.press("l", "j")
	if !strings.Contains(f.frame, "▌▸ 1 thinking") {
		t.Fatalf("section step:\n%s", f.frame)
	}
	f.press("j")
	if !strings.Contains(f.frame, "▌▾ 2 answer") || !strings.Contains(f.frame, "▌  and more") {
		t.Fatalf("a section is one step:\n%s", f.frame)
	}
}

// The timeline's keys carry no conditions: its rows have no guards, and
// what l and h do on a closed card, an open card and inside one comes from
// the actions' availability. Each state reports exactly the actions that
// make sense there, and a call naming its card is judged by that card.
func TestTimelineActionsReportAvailabilityByCardState(t *testing.T) {
	f := newTimelineFixture(t, paragraphCards(), ParentStop("below"))
	r := f.r
	for _, noun := range []string{timelineNoun, "ochazuke.timeline-body"} {
		for mode, rows := range r.Nouns[noun].Bindings {
			for _, b := range rows {
				if b.When != "" {
					t.Errorf("%s/%s %q runs %s when %s: the timeline's rows take no guard", noun, mode, b.Keys, b.Request.Key(), b.When)
				}
			}
		}
	}
	available := func() map[string]bool {
		out := map[string]bool{}
		for _, verb := range []string{"open", "close", "enter", "leave", "step", "fold-step", "yank-step"} {
			out[verb] = r.Available(Request{Noun: timelineNoun, Verb: verb}, r.Focus, r.Mode)
		}
		return out
	}
	want := func(place string, verbs ...string) {
		t.Helper()
		got := available()
		for verb, on := range got {
			if on != slices.Contains(verbs, verb) {
				t.Errorf("%s: %s available %v, want only %v", place, verb, on, verbs)
			}
		}
	}
	want("closed card", "open", "enter", "leave")
	f.press("l")
	want("open card", "close", "enter", "leave")
	f.press("l")
	want("inside, on a plain paragraph (z c still closes the card)", "close", "enter", "leave", "step", "yank-step")
	f.press("h")
	if r.Focus != "c1" || !f.tl.IsOpen("c1") {
		t.Fatalf("h inside: focus %s open %v", r.Focus, f.tl.IsOpen("c1"))
	}
	// A sequence on an action with nothing to do here is consumed with
	// its keys: z o on an open card does nothing (the card is open
	// already) and o never runs alone.
	if outcome, _ := r.DispatchOutcome(MacroKey{Key: "z"}); outcome != KeyHandled {
		t.Fatalf("z starts the sequence: %v", outcome)
	}
	if outcome, _ := r.DispatchOutcome(MacroKey{Key: "o"}); outcome != KeyConsumed || !f.tl.IsOpen("c1") {
		t.Fatalf("z o on an open card: %v, open %v", outcome, f.tl.IsOpen("c1"))
	}
	// A call names its card: open c2 (closed) is available from c1.
	if !r.Available(Request{Noun: timelineNoun, Verb: "open", Args: Arguments{"card": "c2"}}, r.Focus, r.Mode) {
		t.Fatal("open {card: c2} is available while c2 is closed")
	}
	if _, err := r.Execute(Request{Noun: timelineNoun, Verb: "open", Args: Arguments{"card": "c2"}}); err != nil || !f.tl.IsOpen("c2") {
		t.Fatalf("open {card: c2}: %v", err)
	}
	if _, err := r.Execute(Request{Noun: timelineNoun, Verb: "open", Args: Arguments{"card": "nope"}}); err == nil || !strings.Contains(err.Error(), `unknown card "nope"`) {
		t.Fatalf("an unknown card is the verb's own error: %v", err)
	}
	f.press("h")
	want("closed again (h on the open card closed it)", "open", "enter", "leave")
}

// An empty timeline has no card to open, enter, close or leave from: l and
// h are not the timeline's there and go on out, as they did with guards.
func TestTimelineEmptyLeavesLAndHUnhandled(t *testing.T) {
	f := newTimelineFixture(t, nil, ParentStop("below"))
	if err := f.r.SetFocus(f.tl.List().id); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"l", "h"} {
		focus := f.r.Focus
		outcome, err := f.r.DispatchOutcome(MacroKey{Key: key})
		if err != nil || outcome == KeyHandled && f.r.Focus != focus {
			t.Errorf("%s on an empty timeline: %v %v, focus %s", key, outcome, err, f.r.Focus)
		}
		for _, verb := range []string{"open", "close", "enter", "leave"} {
			if f.r.Available(Request{Noun: timelineNoun, Verb: verb}, f.r.Focus, f.r.Mode) {
				t.Errorf("%s is available on an empty timeline", verb)
			}
		}
	}
}
