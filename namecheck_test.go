package ochazuke_test

import (
	"testing"

	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/grid"
	"ofam.dev/ochazuke/internal/demo"
	"ofam.dev/ochazuke/markdown"
	"ofam.dev/ochazuke/namecheck"
)

// ochazuke holds itself to namecheck: its non-test Go (the root, the cli,
// markdown and grid packages) adds no comparison, case label or lookup
// table keyed by a noun, verb, mode, motion or parameter name its runtimes
// declare, and no function grows or passes complexity 25. The accepted
// uses are testdata/namecheck.txt, which only shrinks (README of the
// package: NAMECHECK_UPDATE=1 removes rows, a new row is a reviewed edit).
func TestNoNewNameBranches(t *testing.T) {
	vocab := map[string][]string{}
	merge := func(r *ochazuke.Runtime) {
		for name, cats := range r.NameVocab() {
			for _, c := range cats {
				if !contains(vocab[name], c) {
					vocab[name] = append(vocab[name], c)
				}
			}
		}
	}
	r := ochazuke.NewRuntime()
	ochazuke.NewModel(r)
	ochazuke.NewPicker(r)
	markdown.Register(r)
	grid.Register(r)
	merge(r)
	merge(demo.New().Runtime)
	namecheck.Check(t, namecheck.Config{
		Vocab:    vocab,
		Baseline: "testdata/namecheck.txt",
		Dirs:     []string{".", "cli", "markdown", "grid"},
	})
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
