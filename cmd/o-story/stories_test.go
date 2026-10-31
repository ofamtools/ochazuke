package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	story "github.com/unixsuperhero/monogatari"
	"github.com/unixsuperhero/monogatari/host"
	"github.com/unixsuperhero/ochazuke/cmd/o-story/demo"
)

var update = flag.Bool("update", false, "rewrite story goldens (and the README's verbs section)")

// hostApps are the apps o-story's stories build.
var hostApps = config("o-story").Apps

// TestStories plays every story in stories/ headless (host.Test): per-step
// and final expectations, else the golden frame. A story marked skip (a
// known framework gap) runs but is reported as skipped.
func TestStories(t *testing.T) {
	cfg := config("o-story")
	cfg.Dir, cfg.Update = "stories", *update
	host.Test(t, cfg)
}

// The README's minimum-verbs section is generated from the stories.
func TestReadmeVerbsTableIsCurrent(t *testing.T) {
	want, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(t.TempDir(), "README.md")
	if err := os.WriteFile(tmp, want, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config("o-story")
	cfg.Getenv = func(string) string { return t.TempDir() }
	if err := host.Main(cfg, []string{"verbs", "--dir", "stories", "--readme", tmp}, os.Stderr, os.Stderr); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if *update {
		if err := os.WriteFile("README.md", got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	if string(got) != string(want) {
		t.Fatal("README minimum-verbs section is stale: cd cmd/o-story && go run . verbs --dir stories --readme README.md")
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

// TestStoryTypingIsNotShadowed checks every story runtime the way
// Runtime.TypingShadows does, at the start and after each event of its
// script (a filter, a chooser or the palette open): while a field takes
// text, every printable key and space reaches it; no row binds a key a
// person types there.
func TestStoryTypingIsNotShadowed(t *testing.T) {
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
		d := story.NewDriver(ss, story.Options{Mono: true})
		seen, states := map[string]bool{}, map[string]bool{}
		check := func(n int) error {
			r := d.S.R
			// A state already checked (the same focus, mode, top modal
			// and instances) is not checked again.
			state := fmt.Sprint(r.Focus, r.Mode.Name, r.TopModal(), len(r.Instances))
			if states[state] {
				return nil
			}
			states[state] = true
			for _, line := range r.TypingShadows() {
				if !seen[line] {
					seen[line] = true
					t.Errorf("%s (after event %d): %s", s.Name, n, line)
				}
			}
			return nil
		}
		_ = check(0)
		events := append(append([]story.Event{}, s.Setup...), s.Script...)
		// A story whose events fail (its own run reports that) is checked
		// up to the failing event.
		_ = d.Play(events, check)
		d.Close()
	}
}
