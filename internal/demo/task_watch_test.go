package demo

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/cli"
	"ofam.dev/ochazuke/keytest"
	"ofam.dev/oden"
)

// demo task watch streams a JSON line per task added, modified or deleted:
// the three tasks first, then each change to the store as the demo reports
// it (Runtime.Changed, here from a job delivered by the shell's loop).
func TestTaskWatchFromTheCLI(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir+"/config")
	t.Setenv("XDG_STATE_HOME", dir+"/state")
	t.Setenv("XDG_CACHE_HOME", dir+"/cache")
	task := newDemoTask()
	m := newWith(task)
	r := m.Runtime
	steps := make(chan func())
	quit := make(chan struct{})
	defer close(quit)
	var arm func()
	arm = func() {
		_, err := r.Start("task", func(ctx context.Context) (any, error) {
			select {
			case step := <-steps:
				return step, nil
			case <-quit:
				return nil, context.Canceled
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}, func(c ochazuke.Ctx, v any, err error) {
			if step, ok := v.(func()); ok {
				step()
				c.Runtime.Changed("task")
				arm()
			}
		}, ochazuke.Ctx{Runtime: r})
		if err != nil {
			panic(err)
		}
	}
	arm()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pr, pw := io.Pipe()
	done := make(chan error, 1)
	go func() {
		app := oden.New("demo", []string{"task", "watch"}, oden.WithStdio(strings.NewReader(""), pw, io.Discard), oden.WithPath(t.TempDir()))
		cli.Mount(app, cli.New(r, "demo", strings.NewReader(""), pw, io.Discard))
		done <- app.Run(ctx)
		pw.Close()
	}()
	lines := make(chan string, 16)
	go func() {
		sc := bufio.NewScanner(pr)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	next := func() ochazuke.WatchEvent {
		t.Helper()
		select {
		case line := <-lines:
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
	var got []string
	for range 3 {
		e := next()
		got = append(got, e.Type+" "+e.ID)
	}
	steps <- func() { task.tasks["t2"]["status"] = "done" }
	e := next()
	got = append(got, e.Type+" "+e.ID)
	if e.Thing["status"] != "done" {
		t.Fatalf("modified t2: %+v", e)
	}
	steps <- func() {
		delete(task.tasks, "t3")
		task.order = slices.DeleteFunc(task.order, func(id string) bool { return id == "t3" })
	}
	e = next()
	got = append(got, e.Type+" "+e.ID)
	steps <- func() {
		task.tasks["t4"] = ochazuke.Record{"title": "Sweep the porch", "status": "todo", "estimate": "1"}
		task.order = append(task.order, "t4")
	}
	e = next()
	got = append(got, e.Type+" "+e.ID)
	want := []string{"added t1", "added t2", "added t3", "modified t2", "deleted t3", "added t4"}
	if !slices.Equal(got, want) {
		t.Fatalf("events %q, want %q", got, want)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("an interrupted watch ends cleanly: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("demo task watch did not end within 5s")
	}
}

// The Record page's editor writes the store as you type; a watch of task
// sees each write as a modified task (the editor tells the runtime).
func TestTaskWatchSeesTheEditorsWrites(t *testing.T) {
	m := New()
	r := m.Runtime
	var events []ochazuke.WatchEvent
	w, err := r.Watch("task", ochazuke.Arguments{"id": "t1"}, func(e ochazuke.WatchEvent) { events = append(events, e) })
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()
	if len(events) != 1 || events[0].Type != ochazuke.WatchAdded {
		t.Fatalf("first events %+v", events)
	}
	if _, err := r.Execute(ochazuke.Request{Verb: "open", Noun: "page", Args: ochazuke.Arguments{"id": "task"}}); err != nil {
		t.Fatal(err)
	}
	keytest.Press(m, "A")
	keytest.Type(m, "!")
	keytest.Press(m, "esc")
	last := events[len(events)-1]
	if last.Type != ochazuke.WatchModified || last.ID != "t1" || last.Thing["title"] != "Water the plants!" {
		t.Fatalf("after typing, the last event is %+v", last)
	}
}
