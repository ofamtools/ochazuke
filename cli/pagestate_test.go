package cli

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"ofam.dev/ochazuke"
)

// stateDoc is the catalog of stateTool's extension: skills and agents on
// one page, answered by status.state; delta declares the protocol.
func stateDoc(delta bool) CatalogDoc {
	doc := CatalogDoc{Schema: Schema, Name: "tool", State: "status.state", Nouns: []CatalogNoun{
		{Name: "agent", Verbs: []CatalogVerb{{Name: "list", Rows: "agent"}}},
		{Name: "skill", Verbs: []CatalogVerb{{Name: "list", Rows: "skill"}}},
		{Name: "status", Verbs: []CatalogVerb{{Name: "state"}}},
	}}
	if delta {
		doc.StateDelta = StateDeltaVersion
	}
	doc.Pages = []CatalogPage{{ID: "ai", Title: "AI", Lists: []CatalogList{{ID: "ai/skills", Noun: "skill"}, {ID: "ai/agents", Noun: "agent"}}}}
	return doc
}

// statePage mounts stateDoc over st: Fetch runs the plain call,
// FetchFlags the delta call (through tamper, when set). calls records
// each call's flags; landings counts OnRows by list.
type statePage struct {
	m        *ochazuke.Model
	v        *PageView
	calls    [][]string
	landings map[string]int
	tamper   func(string) string
}

func mountState(t *testing.T, st *stateTool, delta bool) *statePage {
	t.Helper()
	sp := &statePage{landings: map[string]int{}}
	cfg := PageConfig{
		Fetch: func(_ context.Context, noun, verb string) ([]byte, error) {
			sp.calls = append(sp.calls, nil)
			out, _ := st.run(t)
			return []byte(out), nil
		},
		FetchFlags: func(_ context.Context, noun, verb string, flags []string) ([]byte, error) {
			sp.calls = append(sp.calls, flags)
			out, _ := st.run(t, flags...)
			if sp.tamper != nil {
				out = sp.tamper(out)
			}
			return []byte(out), nil
		},
		OnRows: func(list string, _ []Row) { sp.landings[list]++ },
	}
	sp.m, sp.v = mountedPage(t, stateDoc(delta), cfg, 100, 40)
	return sp
}

func (sp *statePage) reload(t *testing.T) {
	t.Helper()
	if err := sp.v.Reload(); err != nil {
		t.Fatal(err)
	}
	if err := Settle(sp.m.Runtime, nil); err != nil {
		t.Fatal(err)
	}
}

// instances are the row instances of the page, by id.
func (sp *statePage) instances() map[string]*ochazuke.Instance {
	out := map[string]*ochazuke.Instance{}
	for id, inst := range sp.m.Runtime.Instances {
		if strings.HasPrefix(id, "ai/skills/") || strings.HasPrefix(id, "ai/agents/") {
			out[id] = inst
		}
	}
	return out
}

func labels(l *ochazuke.List) []string {
	var out []string
	for _, item := range l.Items() {
		out = append(out, item.Label)
	}
	return out
}

func TestMountPageAppliesStateDeltas(t *testing.T) {
	agents := []Row{{Noun: "agent", ID: "agent/a1", Label: "ada"}}
	st := newStateTool(t, append(skillRows(5, plain), agents...)...)
	sp := mountState(t, st, true)
	sp.reload(t)
	if got := sp.calls[0]; !slices.Contains(got, "--since=agent=,skill=") {
		t.Fatalf("the first call asks for every list with no hashes: %v", got)
	}
	skills := sp.v.List("ai/skills")
	if skills.Len() != 5 || sp.v.List("ai/agents").Len() != 1 {
		t.Fatalf("rows = %d / %d", skills.Len(), sp.v.List("ai/agents").Len())
	}
	if err := skills.Select("ai/skills/skill/s2"); err != nil {
		t.Fatal(err)
	}
	if err := skills.ToggleMark("ai/skills/skill/s4"); err != nil {
		t.Fatal(err)
	}
	before := sp.instances()
	landed := maps0(map[string]string{})
	for k, n := range sp.landings {
		landed[k] = fmt.Sprint(n)
	}

	// An unchanged poll: no row is touched, no landing.
	sp.reload(t)
	if got := sp.calls[1]; len(got) != 3 || got[0] != "--since-cache" || !strings.HasPrefix(got[2], "--since=agent=") {
		t.Fatalf("a poll sends the cache id and the hashes: %v", got)
	}
	after := sp.instances()
	if len(after) != len(before) {
		t.Fatalf("instances %d -> %d", len(before), len(after))
	}
	for id, inst := range before {
		if after[id] != inst {
			t.Fatalf("row instance %s was recreated by an unchanged poll", id)
		}
	}
	for k, n := range sp.landings {
		if fmt.Sprint(n) != landed[k] {
			t.Fatalf("an unchanged poll landed rows on %s", k)
		}
	}

	// One record changed: exactly that row is replaced, in place.
	rows := append(skillRows(5, func(i int) string {
		if i == 1 {
			return "renamed"
		}
		return plain(i)
	}), agents...)
	st.set(rows...)
	sp.reload(t)
	if got := labels(skills); !slices.Equal(got, []string{"skill 0", "renamed", "skill 2", "skill 3", "skill 4"}) {
		t.Fatalf("labels = %v", got)
	}
	after = sp.instances()
	for id, inst := range before {
		if after[id] != inst {
			t.Fatalf("row instance %s was recreated by a one-row delta", id)
		}
	}
	if sp.m.Runtime.Instances["ai/skills/skill/s1"].Label != "renamed" {
		t.Fatal("the upserted row's instance keeps the old label")
	}
	if sp.landings["ai/agents"] != 1 || sp.landings["ai/skills"] != 2 {
		t.Fatalf("landings = %v: the skills once more, the agents not", sp.landings)
	}
	if sel, _ := skills.Selected(); sel.ID != "ai/skills/skill/s2" {
		t.Fatalf("selection = %s", sel.ID)
	}
	if row, _ := sp.v.Row("ai/skills/skill/s1"); row.Label != "renamed" {
		t.Fatalf("the page's row = %+v", row)
	}

	// A removal: that instance closes, the marks and selection stay.
	st.set(append(append(slices.Clone(rows[:3]), rows[4]), agents...)...)
	sp.reload(t)
	if sp.m.Runtime.Instances["ai/skills/skill/s3"] != nil || skills.Len() != 4 {
		t.Fatal("the removed row stays")
	}
	if marked := skills.Marked(); len(marked) != 1 || marked[0].ID != "ai/skills/skill/s4" {
		t.Fatalf("marks = %v", marked)
	}

	// Refresh asks for every list in full; the rows stay the same rows.
	if err := sp.v.Refresh(); err != nil {
		t.Fatal(err)
	}
	if err := Settle(sp.m.Runtime, nil); err != nil {
		t.Fatal(err)
	}
	if got := sp.calls[len(sp.calls)-1]; !slices.Contains(got, "--since=agent=,skill=") {
		t.Fatalf("Refresh must send no hashes: %v", got)
	}
	if sp.m.Runtime.Instances["ai/skills/skill/s0"] != before["ai/skills/skill/s0"] {
		t.Fatal("a full answer must keep the instance of a row that stays")
	}
}

// A delta whose result does not hash to the answer's is fetched again in
// full, once.
func TestMountPageRefetchesOnAHashMismatch(t *testing.T) {
	st := newStateTool(t, skillRows(4, plain)...)
	sp := mountState(t, st, true)
	sp.reload(t)
	broken := true
	sp.tamper = func(out string) string {
		if !broken {
			return out
		}
		broken = false
		// Drop the row lines: the delta then misses its upserts.
		var keep []string
		for _, line := range strings.Split(out, "\n") {
			if !strings.HasPrefix(line, `{"noun"`) {
				keep = append(keep, line)
			}
		}
		return strings.Join(keep, "\n")
	}
	st.set(skillRows(4, func(i int) string {
		if i == 2 {
			return "changed"
		}
		return plain(i)
	})...)
	n := len(sp.calls)
	sp.reload(t)
	if len(sp.calls) != n+2 {
		t.Fatalf("calls = %d, want a poll and one full refetch", len(sp.calls)-n)
	}
	if got := sp.calls[n+1]; !strings.HasSuffix(got[len(got)-1], ",skill=") || strings.HasSuffix(got[len(got)-1], "agent=,skill=") {
		t.Fatalf("the refetch must send no hash for the mismatched list only: %v", got)
	}
	if got := labels(sp.v.List("ai/skills")); !slices.Equal(got, []string{"skill 0", "skill 1", "changed", "skill 3"}) {
		t.Fatalf("labels after the refetch = %v", got)
	}
}

// Against an extension without the protocol (no stateDelta in its
// catalog) the page reloads through Fetch, full answers, as before.
func TestMountPageOlderExtensionGetsFullAnswers(t *testing.T) {
	st := newStateTool(t, skillRows(3, plain)...)
	sp := mountState(t, st, false)
	sp.reload(t)
	sp.reload(t)
	for _, c := range sp.calls {
		if c != nil {
			t.Fatalf("FetchFlags ran against a catalog without stateDelta: %v", c)
		}
	}
	if sp.v.List("ai/skills").Len() != 3 {
		t.Fatal("rows missing")
	}
	// A plain answer through ApplyState is SetState.
	a, err := DecodeState([]byte(first(st.run(t))))
	if err != nil || a.Delta {
		t.Fatalf("a plain answer decodes as plain: %v %+v", err, a)
	}
	if bad, err := sp.v.ApplyState(a); err != nil || len(bad) != 0 || sp.v.List("ai/skills").Len() != 3 {
		t.Fatalf("ApplyState(plain) = %v, %v", bad, err)
	}
}

// A RowReader hands out a delta answer's rows and keeps the answer; an
// answer of unchanged lists has no rows and is no error.
func TestRowReaderReadsADeltaAnswer(t *testing.T) {
	st := newStateTool(t, skillRows(3, plain)...)
	h := newHost()
	out, _ := st.run(t, h.flags()...)
	rr := NewRowReader(strings.NewReader(out))
	var rows []Row
	for {
		batch, done, err := rr.Next(2, 0)
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, batch...)
		if done {
			break
		}
	}
	a := rr.Answer()
	if len(rows) != 3 || !a.Delta || len(a.Hashes) != 3 || len(a.Lists) != 1 {
		t.Fatalf("rows %d, answer %+v", len(rows), a)
	}
	h.apply(t, a)
	out, _ = st.run(t, h.flags()...)
	rr = NewRowReader(strings.NewReader(out))
	if batch, done, err := rr.Next(10, time.Second); err != nil || len(batch) != 0 || !done || rr.Answer().Lists[0].Answer != AnswerUnchanged {
		t.Fatalf("unchanged answer = %v %v %v %+v", batch, done, err, rr.Answer())
	}
}
