package ochazuke_test

import (
	"testing"

	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/diffview"
	"ofam.dev/ochazuke/grid"
	"ofam.dev/ochazuke/internal/demo"
	"ofam.dev/ochazuke/markdown"
	"ofam.dev/ochazuke/namecheck"
)

// Every tracked Go, JSON and Markdown file of ochazuke names only verbs its
// runtimes have, or removed ones (listed, with what replaces them, in
// removed_verbs.json). A verb purged from the catalog fails here until the
// last reference to it is gone; a dated CHANGELOG section is history.
func TestVerbReferences(t *testing.T) {
	cat := ochazuke.NewRuntime().VerbCatalog()
	for _, r := range []*ochazuke.Runtime{demo.New().Runtime, func() *ochazuke.Runtime {
		r := ochazuke.NewRuntime()
		ochazuke.NewModel(r)
		ochazuke.NewPicker(r)
		markdown.Register(r)
		grid.Register(r)
		diffview.Register(r)
		return r
	}()} {
		c := r.VerbCatalog()
		for k, v := range c.Nouns {
			cat.Nouns[k] = v
		}
		for k, v := range c.Known {
			if old, ok := cat.Known[k]; !ok || v == "" {
				_ = old
				cat.Known[k] = v
			}
		}
	}
	cfg := namecheck.VerbsConfig{
		Catalog: cat,
		Removed: ochazuke.NewRuntime().RemovedVerbs(),
		Root:    ".",
		// The removed list and the check's own tests name removed verbs on purpose, as
		// testdata does.
		Skip: []string{"docs/reference/", "removed_verbs.json", "verb_check_test.go", "namecheck/verbs_test.go"},
		Allow: []string{
			// Registered by cli.PageView when a catalog page mounts; no
			// runtime built here has it.
			"ochazuke.detail.fields",
			// The csv example's own verbs (examples/csv/verbs.go), on its
			// own nouns: not the purged thing verbs.
			"row.duplicate", "column.rename",
		},
	}
	namecheck.VerbsCheck(t, cfg)
	// ochazuke's own requests use set's one shape (props): a deprecated
	// parameter is reported outside tests.
	rep, err := namecheck.Verbs(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range rep.Deprecated() {
		t.Errorf("%s", h)
	}
}
