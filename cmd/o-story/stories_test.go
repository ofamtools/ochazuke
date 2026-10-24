package main

import (
	"flag"
	"strings"
	"testing"

	"github.com/unixsuperhero/ochazuke/story"
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
	lib := story.Library{Stories: stories, Store: story.Store{Dir: t.TempDir()}}
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
		ss, err := story.Build(s, story.Options{Mono: true})
		if err != nil {
			t.Fatalf("%s: %v", s.Name, err)
		}
		if _, err := ss.R.ValidateBindings(); err != nil {
			t.Fatalf("%s: %v", s.Name, err)
		}
		ss.Close()
	}
}
