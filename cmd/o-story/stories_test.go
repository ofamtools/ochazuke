package main

import (
	"flag"
	"strings"
	"testing"

	story "github.com/unixsuperhero/monogatari"
	"github.com/unixsuperhero/ochazuke/cmd/o-story/demo"
)

var update = flag.Bool("update", false, "rewrite story goldens")

// TestStories plays every story in stories/ headless: per-step and final
// expectations, else the golden frame. A story marked skip (a known
// framework gap) runs but is reported as skipped.
func TestStories(t *testing.T) {
	stories, err := story.LoadDir("stories")
	if err != nil {
		t.Fatal(err)
	}
	if len(stories) < 20 {
		t.Fatalf("%d stories, want at least 20", len(stories))
	}
	lib := story.Library{Stories: stories, Store: story.Store{Dir: t.TempDir()}, Apps: hostApps}
	for _, s := range stories {
		t.Run(s.Area+"/"+story.Slug(s.Name), func(t *testing.T) {
			t.Parallel()
			res := lib.Run(s, story.RunOptions{Golden: true, Update: *update})
			if res.Skipped {
				t.Skipf("%s (passes now: %v)", s.Skip, res.Err == nil && len(res.Failed) == 0)
			}
			if res.Err != nil {
				t.Fatal(res.Err)
			}
			if len(res.Failed) > 0 {
				t.Fatal(strings.Join(res.Failed, "\n"))
			}
		})
	}
}

// TestStoryBindingsAreAtomic validates every story runtime's binding rows.
func TestStoryBindingsAreAtomic(t *testing.T) {
	stories, err := story.LoadDir("stories")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range stories {
		if s.Base != "" {
			continue
		}
		ss, err := story.Library{Apps: hostApps}.Build(s, story.Options{Mono: true})
		if err != nil {
			t.Fatalf("%s: %v", s.Name, err)
		}
		if _, err := ss.R.ValidateBindings(); err != nil {
			t.Fatalf("%s: %v", s.Name, err)
		}
		ss.Close()
	}
}

// A page's store replaces the row-removing delete only on the noun the
// store sits on: on the demo noun demo.delete becomes the store's
// standard delete; a store on another noun (StoreSpec.Noun) leaves
// demo.delete the row remover, so x stays bound.
func TestStoreDeleteFollowsTheStoreNoun(t *testing.T) {
	for _, c := range []struct {
		noun     string
		standard string
	}{{"", "delete"}, {"parcel", ""}} {
		s, err := story.Parse([]byte(`{"name": "store delete `+c.noun+`", "area": "palette", "page": {"store": {"noun": "`+c.noun+`", "props": [{"name": "id", "key": true}, {"name": "title", "role": "label"}], "things": [{"id": "t1", "title": "x"}]}}}`), "test")
		if err != nil {
			t.Fatal(err)
		}
		ss, err := story.Library{Apps: hostApps}.Build(s, story.Options{Mono: true})
		if err != nil {
			t.Fatalf("noun %q: %v", c.noun, err)
		}
		op, ok := ss.R.Operations[demo.Demo+".delete"]
		if !ok || op.Standard != c.standard {
			t.Fatalf("noun %q: demo.delete %v standard %q, want %q", c.noun, ok, op.Standard, c.standard)
		}
		if c.noun != "" {
			if op, ok := ss.R.Operations[c.noun+".delete"]; !ok || op.Standard != "delete" {
				t.Fatalf("%s.delete %v %q", c.noun, ok, op.Standard)
			}
		}
		if _, err := ss.R.ValidateBindings(); err != nil {
			t.Fatalf("noun %q: %v", c.noun, err)
		}
		ss.Close()
	}
}
