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
// picker, markdown and grid) and the demo app. There is no baseline: every
// thing noun has the core verbs and a key prop, and no command noun has a
// verb named like a core verb (och-command-nouns renamed the last ones),
// so any break fails.
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
	nouncheck.CheckRows(t, rows, nouncheck.Config{})
}
