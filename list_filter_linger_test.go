package ochazuke

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func lingerClock(t *testing.T, m *Model) *time.Time {
	t.Helper()
	now := time.Unix(1000, 0)
	m.Now = func() time.Time { return now }
	return &now
}

func expireFilter(t *testing.T, m *Model, now *time.Time) {
	t.Helper()
	m.Update(struct{}{}) // drain component commands and arm the one timer
	tick, ok := m.JobLingerTick()
	if !ok {
		t.Fatal("no linger timer")
	}
	*now = tick.At
	m.Update(tick)
}

func TestFilterLingerListTransitions(t *testing.T) {
	for _, columns := range []bool{false, true} {
		name := "list"
		if columns {
			name = "table"
		}
		t.Run(name, func(t *testing.T) {
			r, m, l := filterFixture(t)
			now := lingerClock(t, m)
			t.Cleanup(l.Close)
			if columns {
				l.SetColumns(Column{Title: "Name", Wrap: true}, Column{Title: "State"})
			}
			l.SetFilter("al")
			if err := l.Patch(ListPatch{Upsert: []ListItem{{ID: "r1", Label: "changed", Noun: "a"}, {ID: "r3", Label: "new alpha", Noun: "a"}}}); err != nil {
				t.Fatal(err)
			}
			if got := shownIDs(l); !slices.Equal(got, []string{"r1", "r3", "r4"}) {
				t.Fatal(got)
			}
			if !l.Lingering("r1") || r.Focus != "r1" {
				t.Fatal("changed row lost its place or focus")
			}
			if got, err := r.Execute(Request{Noun: "a", Verb: "touch"}); err != nil || got != "r1" {
				t.Fatalf("lingering action: %v %v", got, err)
			}
			if !slices.ContainsFunc(l.rowTones(l.items[0]), func(s ToneSpan) bool { return s.Tone == ToneLingering }) {
				t.Fatal("missing lingering tone")
			}
			frame := l.View(40, 12)
			if !strings.Contains(ansi.Strip(frame), "~") || !strings.Contains(ansi.Strip(frame), "changed") {
				t.Fatal(frame)
			}
			if !strings.Contains(frame, "\x1b[") {
				t.Fatal("no styled row")
			}
			if l.PendingCommand() == nil || l.PendingCommand() != nil {
				t.Fatal("timer must arm only once")
			}
			*now = now.Add(29 * time.Second)
			_ = l.SetItems(l.Items()) // repeated refresh does not restart the deadline
			if !l.Lingering("r1") {
				t.Fatal("expired before 30s")
			}
			expireFilter(t, m, now)
			if now.Unix() != 1030 || l.Lingering("r1") || l.shows("r1") || r.Focus != "r3" {
				t.Fatalf("expiry: now %v shown %v focus %s", now, shownIDs(l), r.Focus)
			}
			if l.PendingCommand() != nil {
				t.Fatal("idle timer after expiry")
			}
		})
	}
}

func TestFilterLingerUndoDeletionFilterChangeAndOptOut(t *testing.T) {
	for _, action := range []string{"undo", "delete", "text", "props", "saved", "flush", "off"} {
		t.Run(action, func(t *testing.T) {
			r, m, l := propFilterFixture(t)
			lingerClock(t, m)
			t.Cleanup(l.Close)
			_ = l.ApplyFilter("mine")
			original := l.Items()[0]
			changed := original
			changed.Data = Thing{"by": "agent", "score": 1}
			_ = l.Patch(ListPatch{Upsert: []ListItem{changed}})
			if !l.Lingering(original.ID) {
				t.Fatal("not lingering")
			}
			l.PendingCommand()
			switch action {
			case "undo":
				_ = l.Patch(ListPatch{Upsert: []ListItem{original}})
			case "delete":
				_ = l.Patch(ListPatch{Remove: []string{original.ID}})
			case "text":
				l.SetFilter("a")
			case "props":
				_ = l.SetPropFilters([]PropFilter{{Prop: "score", Value: 1}})
			case "saved":
				_ = l.ApplyFilter("agents")
			case "flush":
				if _, err := r.Execute(Request{Noun: "list", Verb: "flush-lingering"}); err != nil {
					t.Fatal(err)
				}
			case "off":
				_ = r.Settings().Set(ListFilterLingerSetting, "0")
				l.PendingCommand()
			}
			if l.Lingering(original.ID) || !l.linger.tickAt.IsZero() {
				t.Fatal("did not flush linger")
			}
			if action == "undo" && !l.shows(original.ID) {
				t.Fatal("undo must immediately match again")
			}
			if action == "delete" && r.Instances[original.ID] != nil {
				t.Fatal("deleted row remains")
			}
		})
	}
	r, _, l := filterFixture(t)
	_ = r.Settings().Set(ListFilterLingerSetting, "0")
	l.SetFilter("al")
	_ = l.Patch(ListPatch{Upsert: []ListItem{{ID: "r1", Label: "gone", Noun: "a"}}})
	if l.shows("r1") || l.PendingCommand() != nil {
		t.Fatal("zero did not preserve immediate removal")
	}
}

func TestFilterLingerStaggeredAndStaleTimers(t *testing.T) {
	_, m, l := filterFixture(t)
	now := lingerClock(t, m)
	t.Cleanup(l.Close)
	l.SetFilter("a")
	_ = l.Patch(ListPatch{Upsert: []ListItem{{ID: "r1", Label: "one", Noun: "a"}}})
	l.PendingCommand()
	stale, _ := m.JobLingerTick()
	l.SetFilter("a") // same query explicitly reapplied also flushes
	_ = l.Patch(ListPatch{Upsert: []ListItem{{ID: "r1", Label: "alpha", Noun: "a"}}})
	_ = l.Patch(ListPatch{Upsert: []ListItem{{ID: "r1", Label: "one", Noun: "a"}}})
	l.PendingCommand()
	if l.HandleMessage(stale) {
		t.Fatal("accepted cancelled timer at same deadline")
	}
	*now = now.Add(10 * time.Second)
	_ = l.Patch(ListPatch{Upsert: []ListItem{{ID: "r3", Label: "three", Noun: "a"}}})
	expireFilter(t, m, now)
	if l.shows("r1") || !l.Lingering("r3") {
		t.Fatal(shownIDs(l))
	}
	expireFilter(t, m, now)
	if l.shows("r3") || l.PendingCommand() != nil {
		t.Fatal("last deadline did not drain")
	}
}

func TestFilterLingerTreeAncestors(t *testing.T) {
	r, tree := guideFixture(t, Guides())
	m := NewModel(r)
	now := lingerClock(t, m)
	t.Cleanup(tree.list.Close)
	_ = tree.list.SetPropFilters([]PropFilter{{Prop: "by", Value: "owner"}})
	nodes := tree.Nodes()
	nodes[2].Data = Thing{"by": "agent"}
	if err := tree.SetNodes(nodes); err != nil {
		t.Fatal(err)
	}
	if !tree.list.Lingering("a1") || !tree.list.shows("root") || !tree.list.shows("a") {
		t.Fatal(shownIDs(tree.list))
	}
	if !strings.Contains(ansi.Strip(tree.View(60, 10)), "Apple") {
		t.Fatal("lost descendant")
	}
	expireFilter(t, m, now)
	if tree.list.Shown() != 0 {
		t.Fatal(shownIDs(tree.list))
	}
}

func TestFilterLingerChooserTextPropsAndExpiry(t *testing.T) {
	for _, mode := range []string{"text", "props", "custom"} {
		t.Run(mode, func(t *testing.T) {
			_, m, c := chooserPropFixture(t)
			now := lingerClock(t, m)
			t.Cleanup(c.Close)
			switch mode {
			case "text":
				c.SetQuery("alpha")
			case "props":
				_ = c.ApplyFilter("mine")
			case "custom":
				c.spec.Filter = func(q string, row ChooserRow) bool { return strings.Contains(row.Label, q) }
				c.SetQuery("alpha")
			}
			c.SetRows([]ChooserRow{{ID: "a", Label: "changed", Props: Thing{"by": "agent"}}, {ID: "b", Label: "alpha new", Props: Thing{"by": "owner"}}})
			if !c.filters.Lingering("a") || !slices.Equal(chooserIDs(c), []string{"a", "b"}) || highlighted(c) != "a" {
				t.Fatalf("%v %s", chooserIDs(c), highlighted(c))
			}
			if !strings.Contains(ansi.Strip(c.View(60, 10).Text), "~ ") {
				t.Fatal("no marker")
			}
			expireFilter(t, m, now)
			if !slices.Equal(chooserIDs(c), []string{"b"}) {
				t.Fatal(chooserIDs(c))
			}
			c.SetRows([]ChooserRow{{ID: "b", Label: "gone", Props: Thing{"by": "agent"}}})
			c.SetQuery("gone")
			if c.filters.Lingering("b") {
				t.Fatal("query change did not flush")
			}
		})
	}
}

func TestFilterLingerChooserPropOptOutAndDeletion(t *testing.T) {
	r, m, c := chooserPropFixture(t)
	lingerClock(t, m)
	t.Cleanup(c.Close)
	c.spec.NoPropFilters = true
	c.SetQuery("alpha")
	c.SetRows([]ChooserRow{{ID: "a", Label: "changed"}})
	if !c.filters.Lingering("a") {
		t.Fatal("text-only chooser did not linger")
	}
	if _, err := r.Execute(Request{Noun: "list", Verb: "flush-lingering"}); err != nil {
		t.Fatal(err)
	}
	if len(c.Matches()) != 0 {
		t.Fatal("flush did not reach text-only chooser")
	}
	c.SetRows([]ChooserRow{{ID: "a", Label: "alpha"}})
	c.SetRows(nil)
	if len(c.Matches()) != 0 || c.filters.Lingering("a") {
		t.Fatal("deleted chooser row lingered")
	}
	c.SetQuery("")
	c.SetRows([]ChooserRow{{Label: "one", Fixed: true}, {Label: "two", Fixed: true}})
	if got := c.Matches(); len(got) != 2 || got[0].Label != "one" || got[1].Label != "two" {
		t.Fatal(got)
	}
	if err := r.Settings().Set(ListFilterLingerSetting, "-1s"); err == nil {
		t.Fatal("negative linger accepted")
	}
}

func TestFilterLingerIndependentViewsAndPosition(t *testing.T) {
	r, m, l := filterFixture(t)
	now := lingerClock(t, m)
	t.Cleanup(l.Close)
	other, err := r.AddList("other", []ListItem{{ID: "other-row", Label: "alpha", Noun: "a"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(other.Close)
	l.SetFilter("al")
	other.SetFilter("al")
	// Keep the departure at its old drawing position despite source reordering.
	_ = l.SetItems([]ListItem{{ID: "r4", Label: "alphabet", Noun: "a"}, {ID: "r1", Label: "changed", Noun: "a"}})
	_ = other.SetItems([]ListItem{{ID: "other-row", Label: "changed", Noun: "a"}})
	if !slices.Equal(shownIDs(l), []string{"r1", "r4"}) {
		t.Fatal(shownIDs(l))
	}
	expireFilter(t, m, now)
	expireFilter(t, m, now)
	if l.shows("r1") || other.Shown() != 0 {
		t.Fatal("tick reached the wrong view")
	}
}

// Removing rows before a departure, then adding matches, must not move the
// departure back toward an index that belonged to an earlier refresh.
func TestFilterLingerPositionAcrossRefreshes(t *testing.T) {
	_, m, l := filterFixture(t)
	lingerClock(t, m)
	t.Cleanup(l.Close)
	l.SetFilter("a")
	_ = l.Patch(ListPatch{Upsert: []ListItem{{ID: "r4", Label: "gone", Noun: "a", Cells: []string{"gone"}}}})
	_ = l.Patch(ListPatch{Remove: []string{"r1", "r2", "r3"}})
	if !slices.Equal(shownIDs(l), []string{"r5", "r4"}) {
		t.Fatal(shownIDs(l))
	}
	_ = l.Patch(ListPatch{Upsert: []ListItem{{ID: "r5", Label: "gone", Noun: "a", Cells: []string{"gone"}}, {ID: "new1", Label: "alpha", Noun: "a"}, {ID: "new2", Label: "beta", Noun: "a"}, {ID: "new3", Label: "gamma", Noun: "a"}}})
	if !slices.Equal(shownIDs(l), []string{"r5", "r4", "new1", "new2", "new3"}) {
		t.Fatal(shownIDs(l))
	}
}
