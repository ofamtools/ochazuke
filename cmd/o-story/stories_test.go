package main

import (
	"flag"
	"fmt"
	"testing"

	story "ofam.dev/monogatari"
	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/cmd/o-story/demo"
)

var update = flag.Bool("update", false, "rewrite story goldens")

var hostApps = config("o-story").Apps

// One replay per area checks its behavioral assertions, binding validity and
// typing shadows. Invariants observe the same frames, not a second suite replay.
func TestStories(t *testing.T) {
	stories, err := story.LoadDir("stories")
	if err != nil {
		t.Fatal(err)
	}
	lib := story.Library{Apps: hostApps, Stories: stories}
	for _, s := range stories {
		t.Run(s.Area+"/"+story.Slug(s.Name), func(t *testing.T) {
			t.Parallel()
			var previous *ochazuke.Runtime
			states, seen := map[string]bool{}, map[string]bool{}
			res := lib.Run(s, story.RunOptions{Golden: true, Update: *update, Observe: func(p story.RunPosition) {
				r := p.Session.R
				if r != previous {
					previous = r
					states, seen = map[string]bool{}, map[string]bool{}
					if _, err := r.ValidateBindings(); err != nil {
						t.Errorf("after %d: %v", p.After, err)
					}
				}
				state := fmt.Sprint(r.FocusID(), r.CurrentMode().Name, r.TopModal(), len(r.Instances))
				if states[state] {
					return
				}
				states[state] = true
				for _, line := range r.TypingShadows() {
					if !seen[line] {
						seen[line] = true
						t.Errorf("after %d: %s", p.After, line)
					}
				}
			}})
			if res.Skipped {
				t.Skip(s.Skip)
			}
			if res.Err != nil {
				t.Fatal(res.Err)
			}
			for _, failure := range res.Failed {
				t.Error(failure)
			}
		})
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
		op, ok := ss.R.Operations.Find(ochazuke.Key(demo.Demo + ".delete"))
		if !ok || op.Standard != c.standard {
			t.Fatalf("noun %q: demo.delete %v standard %q, want %q", c.noun, ok, op.Standard, c.standard)
		}
		if c.noun != "" {
			if op, ok := ss.R.Operations.Find(ochazuke.Key(c.noun + ".delete")); !ok || op.Standard != "delete" {
				t.Fatalf("%s.delete %v %q", c.noun, ok, op.Standard)
			}
		}
		if _, err := ss.R.ValidateBindings(); err != nil {
			t.Fatalf("noun %q: %v", c.noun, err)
		}
		ss.Close()
	}
}

func TestCompositeRecordingKeepsOpeningFixture(t *testing.T) {
	s, err := story.Parse([]byte(`{
		"name": "recorded checkpoints", "seed": -1,
		"page": {"lists": [{"id": "a", "rows": 2}]},
		"checkpoints": [
			{"name": "rows", "page": {"lists": [{"id": "a", "rows": 2}]}},
			{"name": "text", "page": {"inputs": [{"id": "text", "text": "two", "pin": "insert"}], "focus": "text"}}
		]
	}`), "recorded checkpoints")
	if err != nil {
		t.Fatal(err)
	}
	lib := story.Library{Apps: hostApps}
	ss, err := lib.Build(s, story.Options{Mono: true})
	if err != nil {
		t.Fatal(err)
	}
	d := story.NewDriver(ss, story.Options{Mono: true})
	defer d.Close()
	rec := story.NewRecorder(s, nil)
	var baked *story.Story
	for i, event := range []story.Event{
		{Key: "ctrl+alt+n"}, {Key: "j"}, {Key: "ctrl+alt+n"},
		{Key: "end"}, {Type: "!"}, {Key: "ctrl+c"},
	} {
		rec.Add(event)
		if err := d.Step(event); err != nil {
			t.Fatal(err)
		}
		if i == 1 {
			snap, err := d.S.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			baked = &story.Story{Name: "baked checkpoints", Seed: snap.Seed, Fields: snap.Fields}
		}
	}
	store := story.Store{Dir: t.TempDir()}
	if _, err := store.Push(&rec.Rec); err != nil {
		t.Fatal(err)
	}
	d.Close()
	entry, err := store.Load("1")
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := lib.Build(entry.Rec.Initial, story.Options{Mono: true})
	if err != nil {
		t.Fatal(err)
	}
	replay := story.NewDriver(replayed, story.Options{Mono: true})
	defer replay.Close()
	if err := replay.Play(entry.Rec.Events, nil); err != nil {
		t.Fatal(err)
	}
	in := replay.S.R.Input("text")
	if in == nil || string(in.Text) != "two!" || !replay.S.R.Quit {
		t.Fatalf("partial recording did not replay the second fixture's edit: input=%+v, quit=%v", in, replay.S.R.Quit)
	}
	fromSnapshot, err := lib.Build(baked, story.Options{Mono: true})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := story.NewDriver(fromSnapshot, story.Options{Mono: true})
	defer snapshot.Close()
	if snapshot.S.R.FocusID() != "a2" {
		t.Fatalf("snapshot lost the selected row: %s", snapshot.S.R.FocusID())
	}
	if err := snapshot.Play([]story.Event{{Key: "ctrl+alt+n"}, {Key: "end"}, {Type: "!"}}, nil); err != nil {
		t.Fatal(err)
	}
	in = snapshot.S.R.Input("text")
	if in == nil || string(in.Text) != "two!" {
		t.Fatalf("snapshot did not advance to its next checkpoint: %+v", in)
	}
}
