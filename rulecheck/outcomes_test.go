package rulecheck_test

import (
	"slices"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/rulecheck"
)

// An offered verb whose dry run answers a plain failure (a verb check
// that refuses blue) is reported; one that asks for a missing value is
// not, nor one declared unavailable (it runs and answers nothing). A focusable noun at no place is unwalked until a place names it; a
// place naming a noun no runtime has is skipped and reported.
func TestOutcomes(t *testing.T) {
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	ok := func(*ochazuke.Runtime, ochazuke.Context, ochazuke.Arguments) (any, error) { return nil, nil }
	r.Register(ochazuke.Operation{Key: "box.open", Run: ok})
	r.Register(ochazuke.Operation{Key: "box.name", Parameters: map[string]ochazuke.Parameter{"name": {Type: "string", Required: true}}, Run: ok})
	r.Register(ochazuke.Operation{Key: "box.paint", Parameters: map[string]ochazuke.Parameter{"colour": {Type: "string", Enum: []string{"red", "blue"}}}, Run: ok})
	if err := r.AddVerbCheck("box.paint", ochazuke.VerbCheck{Name: "no-blue", Check: func(_ *ochazuke.Runtime, _ ochazuke.Context, a ochazuke.Arguments) ochazuke.Verdict {
		if a["colour"] == "blue" {
			return ochazuke.Refuse("no blue")
		}
		return ochazuke.Allow()
	}}); err != nil {
		t.Fatal(err)
	}
	r.Register(ochazuke.Operation{Key: "box.ship", Unavailable: "boxes stay here", Run: ok})
	r.Define("gauge").Focusable()
	all := func(int) bool { return true }
	mine := func(found []rulecheck.Finding) []string {
		var out []string
		for _, f := range found {
			if strings.Contains(f.Fields[1], "box.") || f.Fields[1] == "gauge" || f.Fields[1] == "gone" {
				out = append(out, f.Fields[0]+" "+strings.TrimPrefix(f.Fields[1], "main."))
			}
		}
		slices.Sort(out)
		return out
	}

	found, cov := rulecheck.Outcomes([]*ochazuke.Runtime{r}, nil, all)
	got := mine(found)
	want := []string{`failed box.paint {"colour":"blue"}`, "unwalked gauge"}
	if !slices.Equal(got, want) || cov.Runs == 0 || cov.ByStatus["choose"] == 0 || cov.Unavailable == 0 {
		t.Fatalf("findings %v, want %v (coverage %s)", got, want, cov)
	}

	found, cov = rulecheck.Outcomes([]*ochazuke.Runtime{r}, []ochazuke.NounPlace{{Noun: "gauge"}, {Noun: "gone"}}, all)
	got = mine(found)
	want = []string{`failed box.paint {"colour":"blue"}`, "unknown-noun gone"}
	if !slices.Equal(got, want) || cov.Skipped != 1 || !slices.Equal(cov.Unknown, []string{"gone"}) {
		t.Fatalf("with places: findings %v, want %v (coverage %s)", got, want, cov)
	}
}
