package cli

import (
	"context"
	"slices"
	"testing"
)

// A noun declared live keeps its rows out of the state answer: its list
// fills through its own row source when the page loads, and a state
// answer never empties it.
func TestMountPageFillsALiveNounThroughItsRowSource(t *testing.T) {
	for _, delta := range []bool{false, true} {
		st := newStateTool(t, Row{Noun: "skill", ID: "skill/s1", Label: "review"})
		sp := &statePage{landings: map[string]int{}}
		var live []string
		fetch := func(_ context.Context, noun, verb string, flags []string) ([]byte, error) {
			if noun == "quota" {
				live = append(live, verb)
				return []byte(`{"id":"quota/claude:5h","label":"claude 5h"}` + "\n" + `{"id":"quota/codex:week","label":"codex week"}`), nil
			}
			out, _ := st.run(t, flags...)
			return []byte(out), nil
		}
		cfg := PageConfig{
			Fetch:      func(ctx context.Context, noun, verb string) ([]byte, error) { return fetch(ctx, noun, verb, nil) },
			FetchFlags: fetch,
		}
		doc := stateDoc(delta)
		doc.Nouns = append(doc.Nouns, CatalogNoun{Name: "quota", Live: true, Verbs: []CatalogVerb{{Name: "list", Rows: "quota"}}})
		doc.Pages[0].Lists = append(doc.Pages[0].Lists, CatalogList{ID: "ai/quota", Noun: "quota"})
		sp.m, sp.v = mountedPage(t, doc, cfg, 100, 40)
		for round := 1; round <= 2; round++ {
			sp.reload(t)
			if got := labels(sp.v.List("ai/quota")); !slices.Equal(got, []string{"claude 5h", "codex week"}) {
				t.Fatalf("delta=%v round %d: quota rows = %v", delta, round, got)
			}
			if got := labels(sp.v.List("ai/skills")); !slices.Equal(got, []string{"review"}) {
				t.Fatalf("delta=%v round %d: skill rows = %v", delta, round, got)
			}
		}
		if len(live) != 2 {
			t.Fatalf("delta=%v: the live verb ran %d times for 2 loads", delta, len(live))
		}
	}
}
