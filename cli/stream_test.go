package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"ofam.dev/ochazuke"
)

// watchFixture is a runtime with a thing store and thing.feed, a change
// verb whose job waits for the test's next step and applies it on the loop
// (as a daemon client's event job would), then waits again.
type watchFixture struct {
	r      *ochazuke.Runtime
	things []ochazuke.Thing
	steps  chan func()
	quit   chan struct{}
}

func newWatchFixture(t *testing.T) *watchFixture {
	t.Helper()
	f := &watchFixture{r: ochazuke.NewRuntime(), steps: make(chan func()), quit: make(chan struct{}),
		things: []ochazuke.Thing{{"id": "a", "title": "Alpha"}, {"id": "b", "title": "Beta"}}}
	t.Cleanup(func() { close(f.quit) })
	f.r.Define("thing").Props(
		ochazuke.PropSpec{Name: "id", Key: true},
		ochazuke.PropSpec{Name: "title", Role: ochazuke.RoleLabel},
	).Store(ochazuke.Store{List: func(ochazuke.Ctx, ochazuke.ListQuery) ([]ochazuke.Thing, error) { return f.things, nil }}).
		Verb("feed", func(c ochazuke.Ctx) ochazuke.Result { return f.feed() }, "Apply the test's next step")
	return f
}

func (f *watchFixture) feed() ochazuke.Result {
	return ochazuke.Pending(func(ctx context.Context) (any, error) {
		select {
		case step := <-f.steps:
			return step, nil
		case <-f.quit:
			return nil, context.Canceled
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}).Then(func(c ochazuke.Ctx, v any, err error) {
		if step, ok := v.(func()); ok {
			step()
			f.arm()
		}
	})
}

// arm launches the feed job; its completion is a change verb's.
func (f *watchFixture) arm() {
	if _, err := f.r.Launch("app", "thing.feed", f.feed(), ochazuke.Ctx{Runtime: f.r}); err != nil {
		panic(err)
	}
}

// streamRun runs one shell command whose output streams: lines arrive on
// the channel as printed; done answers Run's error.
func streamRun(t *testing.T, ctx context.Context, r *ochazuke.Runtime, cacheDir string, args ...string) (lines <-chan string, done <-chan error) {
	t.Helper()
	pr, pw := io.Pipe()
	out := make(chan string, 64)
	end := make(chan error, 1)
	sh := New(r, "tool", strings.NewReader(""), pw, io.Discard)
	sh.StateCache.Dir = cacheDir
	go func() {
		err := sh.Run(ctx, args)
		pw.Close()
		end <- err
	}()
	go func() {
		sc := bufio.NewScanner(pr)
		for sc.Scan() {
			out <- sc.Text()
		}
		close(out)
	}()
	return out, end
}

// nextEvent reads one event line, with a deadline.
func nextEvent(t *testing.T, lines <-chan string) ochazuke.WatchEvent {
	t.Helper()
	select {
	case line, ok := <-lines:
		if !ok {
			t.Fatal("the stream ended")
		}
		var e ochazuke.WatchEvent
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("line %q: %v", line, err)
		}
		return e
	case <-time.After(5 * time.Second):
		t.Fatal("no event within 5s")
	}
	return ochazuke.WatchEvent{}
}

func wantEvent(t *testing.T, e ochazuke.WatchEvent, typ, id string) {
	t.Helper()
	if e.Type != typ || e.ID != id {
		t.Fatalf("event %+v, want %s %s", e, typ, id)
	}
}

func waitDone(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("the command did not end within 5s")
	}
	return nil
}

// TestShellStreamsAWatch: tool thing watch prints a JSON line per event
// as it happens (the changes arrive as a change verb's job completions,
// delivered by the shell's loop) until its context ends, cleanly; a later
// command, in another runtime, resumes from the last line's hash through
// the state cache.
func TestShellStreamsAWatch(t *testing.T) {
	isolate(t)
	cache := t.TempDir()
	f := newWatchFixture(t)
	f.arm()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	stop, cancelStream := context.WithCancel(ctx)
	lines, done := streamRun(t, stop, f.r, cache, "thing", "watch")
	wantEvent(t, nextEvent(t, lines), "added", "a")
	if e := nextEvent(t, lines); e.Type != "added" || e.ID != "b" || e.Thing["title"] != "Beta" || e.Hash == "" {
		t.Fatalf("event %+v", e)
	}
	f.steps <- func() { f.things[0] = ochazuke.Thing{"id": "a", "title": "Alpha 2"} }
	wantEvent(t, nextEvent(t, lines), "modified", "a")
	f.steps <- func() { f.things = f.things[:1] }
	wantEvent(t, nextEvent(t, lines), "deleted", "b")
	f.steps <- func() { f.things = append(f.things, ochazuke.Thing{"id": "c", "title": "Gamma"}) }
	last := nextEvent(t, lines)
	wantEvent(t, last, "added", "c")
	cancelStream()
	if err := waitDone(t, done); err != nil {
		t.Fatalf("an interrupted watch ends cleanly, got %v", err)
	}

	// Another process: a new runtime over the changed store.
	g := newWatchFixture(t)
	g.things = []ochazuke.Thing{{"id": "a", "title": "Alpha 2"}, {"id": "c", "title": "Gamma 2"}, {"id": "d", "title": "Delta"}}
	stop2, cancel2 := context.WithCancel(ctx)
	lines, done = streamRun(t, stop2, g.r, cache, "thing", "watch", "--since", last.Hash)
	wantEvent(t, nextEvent(t, lines), "modified", "c")
	wantEvent(t, nextEvent(t, lines), "added", "d")
	cancel2()
	if err := waitDone(t, done); err != nil {
		t.Fatal(err)
	}
	for line := range lines {
		t.Fatalf("nothing else streams: %s", line)
	}

	// One thing, by its key; an unknown since is refused.
	stop3, cancel3 := context.WithCancel(ctx)
	lines, done = streamRun(t, stop3, g.r, cache, "thing", "watch", "d")
	wantEvent(t, nextEvent(t, lines), "added", "d")
	cancel3()
	if err := waitDone(t, done); err != nil {
		t.Fatal(err)
	}
	_, done = streamRun(t, ctx, g.r, cache, "thing", "watch", "--since", strings.Repeat("ab", 16))
	if err := waitDone(t, done); err == nil || !strings.Contains(err.Error(), "watch without since") {
		t.Fatalf("unknown since: %v", err)
	}
}

// isolate points the user's config, state and cache directories at a
// temporary one.
func isolate(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir+"/config")
	t.Setenv("XDG_STATE_HOME", dir+"/state")
	t.Setenv("XDG_CACHE_HOME", dir+"/cache")
}
