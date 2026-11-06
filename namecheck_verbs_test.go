package ochazuke_test

import (
	"testing"

	"ofam.dev/ochazuke"
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
	namecheck.VerbsCheck(t, namecheck.VerbsConfig{
		Catalog: cat,
		Removed: ochazuke.NewRuntime().RemovedVerbs(),
		Root:    ".",
		Skip:    []string{"docs/reference/"},
		// Registered by cli.PageView when a catalog page mounts; no
		// runtime built here has it.
		Allow: []string{"ochazuke.detail.fields"},
	})
}
