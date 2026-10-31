package ochazuke

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"ofam.dev/ochazuke/keytest"
)

// placedRowsFixture is a page holding a panel holding a list of item rows,
// with rows on the panel's noun and the page's, so the levels between a
// row's noun and app answer keys of their own.
func placedRowsFixture(t *testing.T) (*Runtime, *Model) {
	t.Helper()
	r := NewRuntime()
	noop := func(Ctx) Result { return Done(nil) }
	r.Define("item").Verb("open", noop, "Open the item").Mode("normal").Bind("enter", Do("item.open"))
	r.Define("panel").Verb("refresh", noop, "Refresh the panel").Mode("normal").Bind("R", Do("panel.refresh"))
	r.Define("ochazuke.page").Verb("tag", noop, "Tag the page").Mode("normal").Bind("T", Do("ochazuke.page.tag"))
	if _, err := r.AddPage("ctx", "Context", func(r *Runtime, w, h int) Block { return Text(r.List("rows").View(w, h)) }); err != nil {
		t.Fatal(err)
	}
	r.Instance("panel", Parent("ctx"), Kind("panel"))
	list, err := r.AddList("rows", []ListItem{{ID: "a", Label: "one", Noun: "item"}, {ID: "b", Label: "two", Noun: "item"}}, Parent("panel"))
	if err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	if err := r.OpenPage("ctx"); err != nil {
		t.Fatal(err)
	}
	if err := list.Focus(); err != nil {
		t.Fatal(err)
	}
	m.Width, m.Height = 120, 40
	return r, m
}

// helpRowIn reports whether rows hold one with the key, operation (a
// guarded row's " ?" aside) and answering noun.
func helpRowIn(rows []HelpRow, want HelpRow) bool {
	return slices.ContainsFunc(rows, func(h HelpRow) bool {
		return h.Key == want.Key && strings.TrimSuffix(h.Operation, " ?") == want.Operation && h.Noun == want.Noun
	})
}

// Pick at a list row's recorded place answers as here does for that row in
// the same mode, guards aside: the list's, the panel's and the page's keys
// included, which the noun alone (no place) lacks.
func TestPlaceHelpMatchesTheLiveRow(t *testing.T) {
	r, _ := placedRowsFixture(t)
	if r.Focus != "a" {
		t.Fatalf("focus %q", r.Focus)
	}
	live := r.placementOf(r.Focus, "item")
	places := r.NounPlaces("item")
	if len(places) == 0 || places[0].key() != live.key() {
		t.Fatalf("the row's place %+v is not the most seen (%+v)", live, places)
	}
	place := places[0]
	if got := place.Chain(); !slices.Equal(got, []string{"ochazuke.page", "panel", "ochazuke.list", "item"}) {
		t.Fatalf("chain %v", got)
	}
	for _, mode := range []string{"normal", "insert"} {
		if err := r.SetMode(mode); err != nil {
			t.Fatal(err)
		}
		sameAnswers(t, r, r.levelRows(r.Focus), r.nounRows(place, r.Mode), "item", "list", "panel", "page", "app")
		here, pick := BindingHelp(r, r.Focus), PlaceHelp(r, place, r.Mode)
		for _, row := range here {
			if !helpRowIn(pick, row) {
				t.Errorf("%s: here's %s → %s at %s missing from pick", mode, row.Key, row.Operation, row.Noun)
			}
		}
	}
	if err := r.SetMode("normal"); err != nil {
		t.Fatal(err)
	}
	pick, bare := PlaceHelp(r, place, r.Mode), NounHelp(r, "item", r.Mode)
	for _, want := range []HelpRow{{Key: "R", Operation: "panel.refresh", Noun: "panel"}, {Key: "T", Operation: "page.tag", Noun: "ochazuke.page"}, {Key: "j", Operation: "list.move", Noun: "ochazuke.list"}} {
		if !helpRowIn(pick, want) {
			t.Errorf("pick at the place lacks %s → %s", want.Key, want.Operation)
		}
		if helpRowIn(bare, want) {
			t.Errorf("no place has %s → %s", want.Key, want.Operation)
		}
	}
	// No place keeps the old walk: the noun, then app.
	for _, row := range r.nounRows(NounPlace{Noun: "item"}, r.Mode) {
		if row.level != "item" && row.level != "app" {
			t.Errorf("no place answers at %s (%s)", row.level, row.Keys)
		}
	}
}

// A place inside a modal walks the modal noun before app, as the live walk
// does for a focus in the modal.
func TestPlaceHelpInsideAModal(t *testing.T) {
	r, _ := placedRowsFixture(t)
	in := NewInput("answer", "", false)
	if _, err := r.OpenModal("ask", Payload{}, WithInput(in, Kind("ochazuke.prompt"))); err != nil {
		t.Fatal(err)
	}
	var place NounPlace
	for _, p := range r.NounPlaces("ochazuke.prompt") {
		if p.Modal == "ask" {
			place = p
		}
	}
	if place.Modal == "" || placeLabel(r, place) != "Context › ask · modal ask" {
		t.Fatalf("modal place %+v (%q)", place, placeLabel(r, place))
	}
	for _, mode := range []string{"insert", "normal"} {
		if err := r.SetMode(mode); err != nil {
			t.Fatal(err)
		}
		rows := r.nounRows(place, r.Mode)
		sameAnswers(t, r, r.levelRows(r.Focus), rows, "prompt", "ask", "modal", "app", "editor")
		if slices.ContainsFunc(r.nounRows(NounPlace{Noun: "ochazuke.prompt"}, r.Mode), func(row levelRow) bool { return row.level == "modal" }) {
			t.Fatalf("%s: no place has modal rows", mode)
		}
		if !slices.ContainsFunc(r.nounWalk(place, r.Mode), func(lv level) bool { return lv.inst == nil && lv.name() == "ochazuke.modal" }) {
			t.Fatalf("%s: no modal noun level at a modal place", mode)
		}
	}
}

// p chooses the place: no place first, then the noun's places with their
// counts; changing the noun resets it to that noun's most seen place, and
// a noun never seen has no place and no p.
func TestHelpPlaceChooser(t *testing.T) {
	r, m := placedRowsFixture(t)
	pressAll(m, "?", "S")
	frame := ansi.Strip(m.View().Content)
	if !strings.Contains(frame, "noun: item · mode: normal · place: Context › page › panel › list") || !helpListHas(r, "panel.refresh") {
		t.Fatalf("pick at open:\n%s", frame)
	}
	if hints := BindingHints(r, r.Focus); !strings.Contains(hints, "p place") {
		t.Fatalf("pick hints: %q", hints)
	}
	keytest.Press(m, "p")
	if r.TopModal() != helpPlaceKind {
		t.Fatalf("p opened %q", r.TopModal())
	}
	rows := r.Chooser(helpPlaceKind).Matches()
	if len(rows) != 2 || rows[0].ID != "none" || rows[1].Cells[0] != "Context › page › panel › list" || rows[1].Cells[1] != "1×" || rows[1].Cells[2] != "current" {
		t.Fatalf("place rows %+v", rows)
	}
	keytest.Press(m, "enter")
	frame = ansi.Strip(m.View().Content)
	if r.TopModal() != helpID || !strings.Contains(frame, "place: none") || helpListHas(r, "panel.refresh") {
		t.Fatalf("pick with no place (%s):\n%s", m.Status, frame)
	}
	// The table resolves at the place too.
	keytest.Press(m, "S")
	if frame = ansi.Strip(m.View().Content); !strings.Contains(frame, "Keys · table") || !strings.Contains(frame, "place: none") {
		t.Fatalf("table keeps the place:\n%s", frame)
	}
	keytest.Press(m, "S") // back to here
	keytest.Press(m, "S") // pick
	keytest.Press(m, "n")
	pressAll(m, "l", "i", "s", "t", "enter")
	if frame = ansi.Strip(m.View().Content); !strings.Contains(frame, "noun: list · mode: normal · place: Context › page › panel") {
		t.Fatalf("noun change resets the place:\n%s", frame)
	}
	keytest.Press(m, "n")
	pressAll(m, "c", "o", "n", "f", "i", "r", "m", "enter")
	if frame = ansi.Strip(m.View().Content); !strings.Contains(frame, "noun: confirm · mode: normal · place: none") {
		t.Fatalf("a noun never seen:\n%s", frame)
	}
	if hints := BindingHints(r, r.Focus); strings.Contains(hints, "p place") {
		t.Fatalf("p offered without places: %q", hints)
	}
	for _, p := range r.NounPlaces("") {
		if p.Modal == helpID {
			t.Fatalf("the help's own instances are a place: %+v", p)
		}
	}
}

// helpListHas reports whether a row of the open help's list names op.
func helpListHas(r *Runtime, op string) bool {
	return slices.ContainsFunc(r.List(helpListID).Items(), func(item ListItem) bool { return slices.Contains(item.Cells, op) })
}

// The place defaults to where ? was opened: the origin row's place even
// when the noun was seen more often elsewhere; another noun chosen with n
// takes its most seen place, and choosing the origin's noun again brings
// the origin's place back.
func TestHelpPlaceDefaultsToTheOrigin(t *testing.T) {
	r, m := placedRowsFixture(t)
	r.Instance("side", Parent("ctx"), Kind("ochazuke.page"))
	other, err := r.AddList("others", nil, Parent("side"))
	if err != nil {
		t.Fatal(err)
	}
	for range 3 { // item is seen more often under side than under panel
		other.SetItems([]ListItem{{ID: "x", Label: "x", Noun: "item"}})
	}
	places := r.NounPlaces("item")
	if len(places) < 2 || placeLabel(r, places[0]) != "Context › page › page › list" {
		t.Fatalf("item places %+v", places)
	}
	if r.Focus != "a" {
		t.Fatalf("focus %q", r.Focus)
	}
	pressAll(m, "?", "S")
	frame := ansi.Strip(m.View().Content)
	if !strings.Contains(frame, "noun: item · mode: normal · place: Context › page › panel › list") || !helpListHas(r, "panel.refresh") {
		t.Fatalf("pick at open is not the origin's place:\n%s", frame)
	}
	keytest.Press(m, "n")
	pressAll(m, "l", "i", "s", "t", "enter")
	if frame = ansi.Strip(m.View().Content); !strings.Contains(frame, "noun: list · mode: normal · place: Context › page › ") {
		t.Fatalf("another noun has no most seen place:\n%s", frame)
	}
	keytest.Press(m, "n")
	pressAll(m, "i", "t", "e", "m", "enter")
	if frame = ansi.Strip(m.View().Content); !strings.Contains(frame, "noun: item · mode: normal · place: Context › page › panel › list") {
		t.Fatalf("the origin's noun again is not at the origin's place:\n%s", frame)
	}
}

// The origin's place is computed as the recorder does, so it is the place
// before the record settled it.
func TestHelpHomeIsTheRecordersPlace(t *testing.T) {
	r, _ := placedRowsFixture(t)
	home := helpHome(r, r.Focus)
	if home == nil || home.key() != r.placementOf(r.Focus, "item").key() || !slices.Equal(home.Chain(), []string{"ochazuke.page", "panel", "ochazuke.list", "item"}) {
		t.Fatalf("home %+v", home)
	}
	if helpHome(r, "missing") != nil {
		t.Fatal("a missing focus has a place")
	}
}
