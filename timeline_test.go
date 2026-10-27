package ochazuke

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// timelineFixture is a page holding a timeline of cards over a list
// ("below", two rows) at 40x24 in the mono theme. calls counts the body
// renders per card id.
type timelineFixture struct {
	r     *Runtime
	m     *Model
	tl    *Timeline
	calls map[string]int
	frame string // the timeline's last frame, stripped
}

func newTimelineFixture(t testing.TB, cards []Card, opts ...InstanceOption) *timelineFixture {
	t.Helper()
	f := &timelineFixture{calls: map[string]int{}}
	r := NewRuntime()
	r.Define("turn")
	r.Instance("p")
	tl, err := r.AddTimeline("tl", f.counted(cards), append([]InstanceOption{Parent("p")}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	below, err := r.AddList("below", []ListItem{{ID: "b1", Label: "b1", Noun: "turn"}, {ID: "b2", Label: "b2", Noun: "turn"}}, Parent("p"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.AddPage("p", "P", func(r *Runtime, w, h int) Block {
		text := tl.View(w, h-2)
		f.frame = ansi.Strip(text)
		return Text(text + "\n" + below.View(w, 2))
	}, Focus(tl.FocusTarget())); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("p"); err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	m.Appearance = &Appearance{Theme: "mono", Dark: true}
	m.Width, m.Height = 40, 24
	m.View()
	f.r, f.m, f.tl = r, m, tl
	return f
}

// counted wraps every card's body so the fixture counts its renders.
func (f *timelineFixture) counted(cards []Card) []Card {
	out := make([]Card, len(cards))
	for i, c := range cards {
		if c.Body != nil {
			body, id := c.Body, c.ID
			c.Body = func(w int) string { f.calls[id]++; return body(w) }
		}
		out[i] = c
	}
	return out
}

func (f *timelineFixture) press(keys ...string) {
	for _, key := range keys {
		f.m.handleKey(MacroKey{Key: key})
		f.m.View()
	}
}

func turns(n int) []Card {
	cards := make([]Card, n)
	for i := range cards {
		id := fmt.Sprintf("c%d", i+1)
		cards[i] = Card{ID: id, Noun: "turn", Header: "prompt " + id, Meta: "12:0" + fmt.Sprint(i%10) + " · done", Body: TextBody("answer of " + id)}
	}
	return cards
}

func TestTimelineWrapsHeadersWithoutCutting(t *testing.T) {
	header := "a prompt long enough to wrap across several lines of a narrow card, with 日本語の文字 wide runes in it too"
	f := newTimelineFixture(t, []Card{{ID: "c1", Header: header}})
	lines := strings.Split(f.frame, "\n")
	var text []string
	for _, line := range lines {
		if w := ansi.StringWidth(line); w > 40 {
			t.Fatalf("line %q is %d cells wide", line, w)
		}
		if strings.Contains(line, "…") {
			t.Fatalf("line %q is cut", line)
		}
		line = strings.Trim(line, "│┃╭╮╰╯━─ ▸")
		if line != "" {
			text = append(text, strings.TrimSpace(line))
		}
	}
	if got := strings.Join(text, " "); !strings.Contains(strings.ReplaceAll(got, " ", ""), strings.ReplaceAll(header, " ", "")) {
		t.Fatalf("header text lost: %q", got)
	}
	if !strings.HasPrefix(lines[0], "╭") || !strings.Contains(f.frame, "╯") {
		t.Fatalf("no box:\n%s", f.frame)
	}
}

func TestTimelineHeaderLinesCapsAClosedCard(t *testing.T) {
	header := strings.Repeat("word ", 40)
	f := newTimelineFixture(t, []Card{{ID: "c1", Header: header}}, HeaderLines(2))
	if !strings.Contains(f.frame, "more lines") {
		t.Fatalf("no more-lines line:\n%s", f.frame)
	}
	f.press("enter")
	if strings.Contains(f.frame, "more lines") {
		t.Fatalf("open card still capped:\n%s", f.frame)
	}
}

func TestTimelineOpenCloseAndLazyBodies(t *testing.T) {
	f := newTimelineFixture(t, turns(3))
	if len(f.calls) != 0 {
		t.Fatalf("closed cards rendered bodies: %v", f.calls)
	}
	if strings.Contains(f.frame, "answer of c1") {
		t.Fatal("closed card shows its body")
	}
	f.press("enter")
	if !f.tl.IsOpen("c1") || !strings.Contains(f.frame, "answer of c1") {
		t.Fatalf("enter did not open c1:\n%s", f.frame)
	}
	f.press("j", "j")
	if f.calls["c1"] != 1 {
		t.Fatalf("an unchanged body rendered %d times, want 1 (cached)", f.calls["c1"])
	}
	f.press("h")
	if f.tl.IsOpen("c3") {
		t.Fatal("h left c3 open")
	}
	f.press("l")
	if !f.tl.IsOpen("c3") {
		t.Fatal("l did not open c3")
	}
	f.press("z", "M")
	for _, c := range f.tl.Cards() {
		if f.tl.IsOpen(c.ID) {
			t.Fatalf("z M left %s open", c.ID)
		}
	}
	f.press("z", "R")
	if !f.tl.IsOpen("c2") {
		t.Fatal("z R did not open c2")
	}
}

func TestTimelineBodiesOnlyRenderOnScreen(t *testing.T) {
	cards := turns(60)
	for i := range cards {
		cards[i].Open = true
	}
	f := newTimelineFixture(t, cards)
	if len(f.calls) == 0 || len(f.calls) > 8 {
		t.Fatalf("bodies rendered for %d cards, want only those on screen: %v", len(f.calls), f.calls)
	}
	if f.calls["c60"] != 0 {
		t.Fatal("an offscreen card's body rendered")
	}
}

func TestTimelineSectionsFold(t *testing.T) {
	card := Card{ID: "c1", Header: "prompt", Open: true, Sections: []CardSection{
		{Title: "thinking", Role: CardRoleMuted, Body: TextBody("pondering"), Folded: true, Summary: "1 line"},
		{Title: "answer", Body: TextBody("forty-two")},
	}}
	f := newTimelineFixture(t, []Card{card})
	if strings.Contains(f.frame, "pondering") || !strings.Contains(f.frame, "▸ 1 thinking · 1 line") || !strings.Contains(f.frame, "forty-two") {
		t.Fatalf("initial folds wrong:\n%s", f.frame)
	}
	f.press("z", "1")
	if !strings.Contains(f.frame, "pondering") || !strings.Contains(f.frame, "▾ 1 thinking") {
		t.Fatalf("z 1 did not unfold thinking:\n%s", f.frame)
	}
	f.press("z", "2")
	if strings.Contains(f.frame, "forty-two") || !f.tl.Folded("c1", "answer") {
		t.Fatalf("z 2 did not fold the answer:\n%s", f.frame)
	}
	// Folds follow the id across SetCards.
	if err := f.tl.SetCards([]Card{{ID: "c0", Header: "new"}, card}); err != nil {
		t.Fatal(err)
	}
	if !f.tl.Folded("c1", "answer") || f.tl.Folded("c1", "thinking") {
		t.Fatal("folds did not follow the card")
	}
}

func TestTimelineSelectionAndOpenFollowIDs(t *testing.T) {
	f := newTimelineFixture(t, turns(4))
	f.press("j", "j", "enter")
	if f.tl.SelectedID() != "c3" || f.r.Focus != "c3" {
		t.Fatalf("selected %s focus %s, want c3", f.tl.SelectedID(), f.r.Focus)
	}
	cards := turns(4)
	cards = append([]Card{{ID: "c0", Noun: "turn", Header: "first"}}, cards[2], cards[0], cards[3])
	if err := f.tl.SetCards(cards); err != nil {
		t.Fatal(err)
	}
	f.m.View()
	if f.tl.SelectedID() != "c3" || f.r.Focus != "c3" || !f.tl.IsOpen("c3") {
		t.Fatalf("selection %s focus %s open %v after SetCards", f.tl.SelectedID(), f.r.Focus, f.tl.IsOpen("c3"))
	}
	if _, kept := f.tl.open["c2"]; kept {
		t.Fatal("a removed card's state was kept")
	}
	if f.r.Instances["c0"] == nil || f.r.Instances["c0"].Noun != "turn" || f.r.Instances["c2"] != nil {
		t.Fatal("card instances not kept in step with the cards")
	}
}

func TestTimelineMotionsCountsAndSpill(t *testing.T) {
	f := newTimelineFixture(t, turns(8))
	f.press("3", "j")
	if f.tl.SelectedID() != "c4" {
		t.Fatalf("3j selected %s", f.tl.SelectedID())
	}
	f.press("G")
	if f.tl.SelectedID() != "c8" {
		t.Fatalf("G selected %s", f.tl.SelectedID())
	}
	f.press("g", "g")
	if f.tl.SelectedID() != "c1" {
		t.Fatalf("gg selected %s", f.tl.SelectedID())
	}
	f.press("ctrl+d")
	if f.tl.SelectedID() == "c1" {
		t.Fatal("ctrl+d did not move")
	}
	f.press("G", "j")
	if f.r.Focus != "b1" {
		t.Fatalf("j past the last card focused %s, want the list below", f.r.Focus)
	}
	f.press("k")
	if f.r.Focus != "c8" {
		t.Fatalf("k back focused %s", f.r.Focus)
	}
}

func TestTimelineNoSpillClamps(t *testing.T) {
	f := newTimelineFixture(t, turns(2), NoSpill())
	f.press("j", "j", "j")
	if f.r.Focus != "c2" {
		t.Fatalf("NoSpill timeline let the focus go to %s", f.r.Focus)
	}
}

// selectedCard reports whether the frame shows card id's header as the
// heavy-bordered selection.
func lineWith(frame, text string) string {
	for _, line := range strings.Split(frame, "\n") {
		if strings.Contains(line, text) {
			return line
		}
	}
	return ""
}

func TestTimelineFollowTail(t *testing.T) {
	f := newTimelineFixture(t, turns(10), FollowTail())
	if f.tl.SelectedID() != "c10" || !f.tl.Following() {
		t.Fatalf("FollowTail starts on %s following %v", f.tl.SelectedID(), f.tl.Following())
	}
	if !strings.Contains(f.frame, "prompt c10") {
		t.Fatalf("the last card is not on screen:\n%s", f.frame)
	}
	// Appending while following selects the newest and keeps the end on screen.
	if err := f.tl.Patch(Card{ID: "c11", Noun: "turn", Header: "prompt c11", Open: true, Body: TextBody("line 1")}); err != nil {
		t.Fatal(err)
	}
	f.m.View()
	if f.tl.SelectedID() != "c11" || f.r.Focus != "c11" || !strings.Contains(f.frame, "line 1") {
		t.Fatalf("append: selected %s focus %s\n%s", f.tl.SelectedID(), f.r.Focus, f.frame)
	}
	// The last card grows (streaming): its newest line stays on screen.
	body := "line 1"
	for i := 2; i <= 30; i++ {
		body += fmt.Sprintf("\nline %d", i)
		if err := f.tl.Patch(Card{ID: "c11", Noun: "turn", Header: "prompt c11", Body: TextBody(body), Revision: uint64(i)}); err != nil {
			t.Fatal(err)
		}
		f.m.View()
		if !strings.Contains(f.frame, fmt.Sprintf("line %d\n", i)) && !strings.Contains(f.frame, fmt.Sprintf("line %d ", i)) {
			t.Fatalf("growth to line %d left the end off screen:\n%s", i, f.frame)
		}
	}
	// Scrolling up stops following: an append keeps the selection.
	f.press("K")
	if f.tl.Following() {
		t.Fatal("K kept following")
	}
	if err := f.tl.Patch(Card{ID: "c12", Noun: "turn", Header: "prompt c12"}); err != nil {
		t.Fatal(err)
	}
	f.m.View()
	if f.tl.SelectedID() != "c11" || strings.Contains(f.frame, "prompt c12") {
		t.Fatalf("not following, yet the append moved: %s\n%s", f.tl.SelectedID(), f.frame)
	}
	// G resumes: the newest is selected and its end shows.
	f.press("G")
	if f.tl.SelectedID() != "c12" || !f.tl.Following() || !strings.Contains(f.frame, "prompt c12") {
		t.Fatalf("G: selected %s following %v\n%s", f.tl.SelectedID(), f.tl.Following(), f.frame)
	}
	// k stops; j back to the last card at the bottom resumes.
	f.press("k")
	if f.tl.Following() {
		t.Fatal("k kept following")
	}
	f.press("j")
	if !f.tl.Following() {
		t.Fatal("j back onto the last card at the bottom did not resume")
	}
	// Scrolling up then down to the end resumes too.
	f.press("K", "K")
	if f.tl.Following() {
		t.Fatal("K kept following")
	}
	f.press("J", "J", "J", "J")
	if !f.tl.Following() {
		t.Fatalf("J to the end did not resume following:\n%s", f.frame)
	}
}

func TestTimelineScrollKeepsSelectionOnScreen(t *testing.T) {
	cards := turns(10)
	long := strings.TrimSpace(strings.Repeat("line\n", 60))
	cards[0].Body, cards[0].Open = TextBody(long), true
	f := newTimelineFixture(t, cards)
	f.press("J", "J", "J")
	if f.tl.top.id != "c1" || f.tl.top.line != 3 || f.tl.SelectedID() != "c1" {
		t.Fatalf("J scrolled to %v selecting %s", f.tl.top, f.tl.SelectedID())
	}
	f.press("8", "0", "J")
	if f.tl.SelectedID() == "c1" {
		t.Fatalf("the selection stayed off screen: top %v", f.tl.top)
	}
}

func TestTimelineActiveAndMutedDrawing(t *testing.T) {
	cards := turns(2)
	cards[0].Muted = true
	cards[1].Active = true
	f := newTimelineFixture(t, cards)
	f.press("j")
	top := ""
	for _, line := range strings.Split(f.frame, "\n") {
		if strings.Contains(line, "●") {
			top = line
		}
	}
	if !strings.HasPrefix(top, "╭") {
		t.Fatalf("active card has no ● in its top border:\n%s", f.frame)
	}
	raw := f.tl.View(40, 20)
	header := lineWith(raw, "prompt c2")
	if !strings.Contains(header, "\x1b[1") && !strings.Contains(header, ";1m") && !strings.Contains(header, ";1;") {
		t.Fatalf("active header not bold: %q", header)
	}
	if muted := lineWith(raw, "prompt c1"); !strings.Contains(muted, "\x1b[2") && !strings.Contains(muted, ";2m") && !strings.Contains(muted, ";2;") {
		t.Fatalf("muted header not faint: %q", muted)
	}
	// The selected card (c2) is the heavy border; c1 keeps the light one.
	if sel := lineWith(f.frame, "prompt c2"); !strings.HasPrefix(sel, "┃") {
		t.Fatalf("selected card not heavy: %q", sel)
	}
	if other := lineWith(f.frame, "prompt c1"); !strings.HasPrefix(other, "│") {
		t.Fatalf("other card heavy: %q", other)
	}
}

func TestTimelineGroupRules(t *testing.T) {
	cards := turns(3)
	cards[0].Group, cards[1].Group, cards[2].Group = "session a", "session a", "session b"
	f := newTimelineFixture(t, cards)
	if strings.Count(f.frame, "── session a ─") != 1 || strings.Count(f.frame, "── session b ─") != 1 {
		t.Fatalf("group rules wrong:\n%s", f.frame)
	}
}

// BenchmarkTimeline is a keypress and a frame (k, j) at 120x40, a
// streaming last card patched then drawn, and SetCards of every card, over
// n cards (every third open): a frame draws the cards on screen only, so it
// should not grow with n.
func BenchmarkTimeline(b *testing.B) {
	for _, n := range []int{500, 5000} {
		cards := make([]Card, n)
		for i := range cards {
			id := fmt.Sprintf("c%d", i)
			cards[i] = Card{ID: id, Noun: "turn", Header: strings.Repeat("a prompt that wraps ", 6), Meta: "12:00 · done · 3s", Body: TextBody(strings.Repeat("answer line\n", 20)), Open: i%3 == 0}
		}
		b.Run(fmt.Sprintf("frame/%d", n), func(b *testing.B) {
			f := newTimelineFixture(b, cards, FollowTail())
			f.m.Width, f.m.Height = 120, 40
			keys := []string{"k", "j"}
			i := 0
			for b.Loop() {
				f.m.handleKey(MacroKey{Key: keys[i%2]})
				f.m.View()
				i++
			}
		})
		b.Run(fmt.Sprintf("stream/%d", n), func(b *testing.B) {
			f := newTimelineFixture(b, cards, FollowTail())
			f.m.Width, f.m.Height = 120, 40
			last := cards[len(cards)-1]
			i := 0
			for b.Loop() {
				i++
				last.Revision = uint64(i)
				if err := f.tl.Patch(last); err != nil {
					b.Fatal(err)
				}
				f.m.View()
			}
		})
		b.Run(fmt.Sprintf("setcards/%d", n), func(b *testing.B) {
			f := newTimelineFixture(b, cards)
			for b.Loop() {
				if err := f.tl.SetCards(cards); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
