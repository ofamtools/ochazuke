package ochazuke

import (
	"context"
	"fmt"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// remoteListNouns defines n remote nouns whose stores only list (no Query):
// each read is a StoreCall answered by a job. lists counts the reads that
// started; sources collects who each read was for, on the loop and in the
// job's work.
func remoteListNouns(r *Runtime, n int, lists *int, sources *[]RequestSource) {
	for i := range n {
		name := fmt.Sprintf("remote%d", i)
		r.Define(name).Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "title", Role: RoleLabel}).Store(Store{
			Kind: KindCollection, Remote: true,
			List: func(c Ctx, _ ListQuery) ([]Thing, error) {
				v, err := c.Await(StoreCall{Store: name, Op: "list"}, func() Result {
					*lists++
					*sources = append(*sources, c.Source())
					return Pending(func(ctx context.Context) (any, error) {
						*sources = append(*sources, RequestSourceOf(ctx))
						return []Thing{{"id": "a", "title": "A"}, {"id": "b", "title": "B"}}, nil
					})
				}, time.Second)
				if err != nil {
					return nil, err
				}
				return v.([]Thing), nil
			},
		})
	}
}

// drainOnce runs every command scheduled now and answers their messages
// (the work handed out together is the work in flight together).
func drainOnce(r *Runtime) []tea.Msg {
	var msgs []tea.Msg
	var run func(tea.Cmd)
	run = func(cmd tea.Cmd) {
		if cmd == nil {
			return
		}
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				run(c)
			}
			return
		}
		if msg != nil {
			msgs = append(msgs, msg)
		}
	}
	run(r.PendingCommands())
	return msgs
}

// Opening the Nouns page reads no remote store that only lists (by default
// nouns.count-lists is off, as for a local one); with counting on, the
// counts are background jobs and at most jobs.background-max run at once.
func TestNounsPageRemoteListCountsAreBounded(t *testing.T) {
	t.Parallel()
	const nouns, bound = 7, 2
	for _, countLists := range []bool{false, true} {
		r := NewRuntime()
		r.Config().Disable()
		lists := 0
		var sources []RequestSource
		remoteListNouns(r, nouns, &lists, &sources)
		if err := r.Settings().Set(JobsBackgroundMaxSetting, bound); err != nil {
			t.Fatal(err)
		}
		if err := r.Settings().Set(NounsCountListsSetting, countLists); err != nil {
			t.Fatal(err)
		}
		if err := r.EnsureNounsPage(); err != nil {
			t.Fatal(err)
		}
		// A key opens the page: the counts are still background work.
		restore := r.Sourced("key:g n")
		if err := r.OpenPage(NounsPageID); err != nil {
			t.Fatal(err)
		}
		restore()
		if !countLists {
			if lists != 0 || len(r.Jobs()) != 0 {
				t.Fatalf("count-lists off: %d whole reads, jobs %+v", lists, r.Jobs())
			}
			if _, _, known, _ := r.NounCount("remote0"); known {
				t.Fatal("an uncounted remote noun shows no count")
			}
			continue
		}
		for round := 0; ; round++ {
			msgs := drainOnce(r)
			if len(msgs) == 0 {
				break
			}
			if len(msgs) > bound {
				t.Fatalf("round %d: %d reads in flight at once, bound %d", round, len(msgs), bound)
			}
			for _, msg := range msgs {
				if _, err := r.Deliver(msg); err != nil {
					t.Fatal(err)
				}
			}
			if round > 4*nouns {
				t.Fatal("counts never settled")
			}
		}
		for i := range nouns {
			if count, _, known, _ := r.NounCount(fmt.Sprintf("remote%d", i)); !known || count != 2 {
				t.Fatalf("remote%d: count %d known %v", i, count, known)
			}
		}
		if lists != nouns {
			t.Fatalf("%d reads for %d nouns", lists, nouns)
		}
		for _, s := range sources {
			if !s.Background() {
				t.Fatalf("a count's store call is for %q, want background: %v", s, sources)
			}
		}
	}
}

// Ctx.Source says who asked: a key, the palette, the CLI or MCP for a
// request, background for the runtime's own reads; each source's class
// and whether a person asked follow from it.
func TestRequestSources(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		source             string
		class              RequestSource
		person, background bool
	}{
		{"key:g g", RequestFromKey, true, false},
		{"macro:q", RequestFromMacro, true, false},
		{"palette", RequestFromPalette, true, false},
		{helpSource, RequestFromHelp, true, false},
		{"cli", RequestFromCLI, false, false},
		{"mcp", RequestFromMCP, false, false},
		{"hook:app.quitting", RequestFromHook, false, false},
		{"", RequestFromAPI, false, false},
		{"background", RequestFromBackground, false, true},
	} {
		s := RequestSource(tc.source)
		if s.Class() != tc.class || s.Person() != tc.person || s.Background() != tc.background {
			t.Errorf("%q: class %q person %v background %v", tc.source, s.Class(), s.Person(), s.Background())
		}
	}
	r := NewRuntime()
	r.Config().Disable()
	var got []RequestSource
	r.Define("probe").Verb("ask", func(c Ctx) Result {
		got = append(got, c.Source())
		return Pending(func(ctx context.Context) (any, error) { return RequestSourceOf(ctx), nil })
	}, "Say who asked")
	for _, source := range []string{"cli", "mcp"} {
		reply := make(chan Response, 1)
		r.HandleCall(CallMsg{Source: source, Request: Request{Noun: "probe", Verb: "ask"}, Reply: reply})
		if resp := <-reply; resp.Error != "" {
			t.Fatal(resp.Error)
		}
		for _, msg := range drainOnce(r) {
			if c, ok := msg.(Completion); ok && c.Value != RequestSource(source) {
				t.Fatalf("%s: the job's context says %v", source, c.Value)
			}
		}
	}
	if len(got) != 2 || got[0] != RequestFromCLI || got[1] != RequestFromMCP {
		t.Fatalf("handler sources %v", got)
	}
	if s := r.backgroundCtx().Source(); !s.Background() {
		t.Fatalf("a refresh's Ctx says %q", s)
	}
}

// Shutdown runs every store watch's stop once and cancels every running
// job's context; it may be called again, from any goroutine.
func TestShutdownStopsWatchesAndJobs(t *testing.T) {
	t.Parallel()
	r := NewRuntime()
	r.Config().Disable()
	stops := 0
	r.Define("feed").Props(PropSpec{Name: "id", Key: true}).Store(Store{
		Kind: KindCollection,
		List: func(Ctx, ListQuery) ([]Thing, error) { return nil, nil },
		Watch: func(Ctx, WatchQuery, func(WatchEvent)) (func(), error) {
			return func() { stops++ }, nil
		},
	})
	w, err := r.Watch("feed", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Watch("feed", Arguments{}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Launch("app", "work", Pending(func(ctx context.Context) (any, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}), Ctx{}); err != nil {
		t.Fatal(err)
	}
	ended := make(chan []tea.Msg, 1)
	cmd := r.PendingCommands()
	go func() {
		var msgs []tea.Msg
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				if c != nil {
					msgs = append(msgs, c())
				}
			}
		}
		ended <- msgs
	}()
	done := make(chan struct{})
	go func() { r.Shutdown(); close(done) }()
	<-done
	r.Shutdown()
	select {
	case msgs := <-ended:
		var cancelled bool
		for _, m := range msgs {
			if c, ok := m.(Completion); ok && c.Err == context.Canceled {
				cancelled = true
			}
		}
		if !cancelled {
			t.Fatalf("the blocked job did not see its context done: %+v", msgs)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the job and the watch's listener did not end")
	}
	if stops != 2 {
		t.Fatalf("store watch stops ran %d times, want once per watch (2)", stops)
	}
	w.Stop() // the loop's own stop after Shutdown runs nothing again
	if stops != 2 || !r.ShuttingDown() || r.Lifetime().Err() == nil {
		t.Fatalf("after Stop: stops %d, shutting down %v", stops, r.ShuttingDown())
	}
	if _, ctx, err := r.Issue("app", "late", "", Ctx{}); err != nil || ctx.Err() == nil {
		t.Fatalf("a job issued after Shutdown starts done: %v %v", err, ctx.Err())
	}
}
