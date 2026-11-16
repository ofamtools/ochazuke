package ochazuke_test

import (
	"testing"

	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/grid"
	"ofam.dev/ochazuke/internal/demo"
	"ofam.dev/ochazuke/markdown"
	"ofam.dev/ochazuke/rulecheck"
)

// ochazuke holds itself to the rule checks (rulecheck): its framework
// runtime (with the model, the picker, markdown and grid) and the demo
// app. The static scans read the root, cli, markdown and grid packages'
// non-test Go. Each check is a subtest with its own baseline under
// testdata: names (namecheck.txt), refusals (refusecheck.txt), lexicon
// (lexiconcheck.txt, words lexicon-words.tsv), nouns (no baseline: every
// thing noun has the core verbs and a key prop), deadkeys
// (rules/deadkeys.txt) and hiddenkeys (rules/hiddenkeys.txt; it also walks
// rules/places.json, the framework nouns' places as matomeru recorded
// them, so the tree, timeline, rail, forms and pickers it nests the lists
// in are walked though neither runtime here builds them), modeexits
// (rules/modeexits.txt) and outcomes (rules/outcomes.txt: every offered
// verb's dry run, the places' coverage; it replaces TestEveryOfferIsAccepted).
// Every baseline only shrinks (RULECHECK_UPDATE=1).
// This test replaces TestNoNewNameBranches, TestNoNewRefusals,
// TestLexicon and TestNounsAreThingsOrCommands.
func TestRules(t *testing.T) {
	r := ochazuke.NewRuntime()
	ochazuke.NewModel(r)
	ochazuke.NewPicker(r)
	markdown.Register(r)
	grid.Register(r)
	places, err := rulecheck.LoadPlaces("testdata/rules/places.json")
	if err != nil {
		t.Fatal(err)
	}
	rulecheck.Check(t, rulecheck.Config{
		Places:        places,
		FrameworkRows: true,
		Runtime:       r,
		Runtimes:      []*ochazuke.Runtime{demo.New().Runtime},
		Dirs:          []string{".", "cli", "markdown", "grid"},
	})
}
