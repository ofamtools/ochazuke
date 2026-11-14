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
// thing noun has the core verbs and a key prop) and deadkeys
// (rules/deadkeys.txt). Every baseline only shrinks (RULECHECK_UPDATE=1).
// This test replaces TestNoNewNameBranches, TestNoNewRefusals,
// TestLexicon and TestNounsAreThingsOrCommands.
func TestRules(t *testing.T) {
	r := ochazuke.NewRuntime()
	ochazuke.NewModel(r)
	ochazuke.NewPicker(r)
	markdown.Register(r)
	grid.Register(r)
	rulecheck.Check(t, rulecheck.Config{
		Runtime:  r,
		Runtimes: []*ochazuke.Runtime{demo.New().Runtime},
		Dirs:     []string{".", "cli", "markdown", "grid"},
	})
}
