package ochazuke_test

import (
	"slices"
	"testing"

	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/grid"
	"ofam.dev/ochazuke/internal/demo"
	"ofam.dev/ochazuke/markdown"
	"ofam.dev/ochazuke/nouncheck"
)

// ochazuke holds itself to the thing and command noun rules
// (Runtime.ValidateNouns): the framework's runtime (with the model, the
// picker, markdown and grid) and the demo app. The command nouns that
// still have a verb named like a core verb (focus.set, mode.set, the
// list-only pseudo-nouns, ...) are testdata/command-noun-collisions.txt,
// which only shrinks: och-registry-things and och-command-nouns remove
// them.
func TestNounsAreThingsOrCommands(t *testing.T) {
	r := ochazuke.NewRuntime()
	ochazuke.NewModel(r)
	ochazuke.NewPicker(r)
	markdown.Register(r)
	grid.Register(r)
	rows := nouncheck.Rows(r)
	for _, row := range nouncheck.Rows(demo.New().Runtime) {
		if !slices.Contains(rows, row) {
			rows = append(rows, row)
		}
	}
	nouncheck.CheckRows(t, rows, nouncheck.Config{Baseline: "testdata/command-noun-collisions.txt"})
}
