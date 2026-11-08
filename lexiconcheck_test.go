package ochazuke_test

import (
	"testing"

	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/grid"
	"ofam.dev/ochazuke/lexiconcheck"
	"ofam.dev/ochazuke/markdown"
)

// ochazuke holds its own registered operations to the lexicon: no word is
// both a noun and an action name, nouns are singular, namespaces are not
// noun words and keys have three parts. The known violations are
// testdata/lexiconcheck.txt, which only shrinks (LEXICONCHECK_UPDATE=1
// drops rows that no longer apply); testdata/lexicon-words.tsv is the word
// list, a copy of ~/hub/tools/lexicon-audit/words.tsv.
func TestLexicon(t *testing.T) {
	r := ochazuke.NewRuntime()
	ochazuke.NewModel(r)
	ochazuke.NewPicker(r)
	markdown.Register(r)
	grid.Register(r)
	lexiconcheck.Check(t, lexiconcheck.Config{
		Operations: &r.Operations,
		Words:      "testdata/lexicon-words.tsv",
		Baseline:   "testdata/lexiconcheck.txt",
	})
}
