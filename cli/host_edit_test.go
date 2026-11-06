package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	gomaps "maps"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/keytest"
)

func mountedEditFixture(t *testing.T, mode string, beforeSet ...func(context.Context) error) (*ochazuke.Runtime, *ochazuke.Model, ochazuke.Thing) {
	t.Helper()
	ext := ochazuke.NewRuntime()
	thing := ochazuke.Thing{"id": "t1", "title": "Original", "status": "todo", "active": false, "stamp": "fixed"}
	ext.Define("parcel").Props(
		ochazuke.PropSpec{Name: "id", Key: true},
		ochazuke.PropSpec{Name: "title", Parameter: ochazuke.Parameter{Required: true}},
		ochazuke.PropSpec{Name: "status", Parameter: ochazuke.Parameter{Enum: []string{"todo", "done"}}},
		ochazuke.PropSpec{Name: "active", Parameter: ochazuke.Parameter{Type: "boolean"}},
		ochazuke.PropSpec{Name: "stamp", ReadOnly: true},
	).Store(ochazuke.Store{
		Get: func(ochazuke.Ctx, string) (ochazuke.Thing, error) { return gomaps.Clone(thing), nil },
		List: func(ochazuke.Ctx, ochazuke.ListQuery) ([]ochazuke.Thing, error) {
			return []ochazuke.Thing{gomaps.Clone(thing)}, nil
		},
		Patch: func(_ ochazuke.Ctx, _ string, changes ochazuke.Thing) (ochazuke.Thing, error) {
			gomaps.Copy(thing, changes)
			return gomaps.Clone(thing), nil
		},
	})
	raw, err := Catalog(ext, "extension", "v1")
	if err != nil {
		t.Fatal(err)
	}
	var doc CatalogDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	r := ochazuke.NewRuntime()
	if _, err := r.Settings().SetSession(ochazuke.EditApplySetting, mode); err != nil {
		t.Fatal(err)
	}
	m := ochazuke.NewModel(r)
	m.Width, m.Height = 100, 24
	local := func(noun string) string { return "ext-" + noun }
	mounted := MountNouns(r, doc.AppNouns(), HostMount{Local: local, Proxy: func(noun string, v CatalogVerb) any {
		return func(c ochazuke.Ctx) ochazuke.Result {
			if v.Name == "set" {
				return ochazuke.Pending(func(ctx context.Context) (any, error) {
					if len(beforeSet) > 0 {
						if err := beforeSet[0](ctx); err != nil {
							return nil, err
						}
					}
					return ext.Execute(ochazuke.Request{Noun: noun, Verb: v.Name, Args: c.Args})
				})
			}
			value, err := ext.Execute(ochazuke.Request{Noun: noun, Verb: v.Name, Args: c.Args})
			if err != nil {
				return ochazuke.Failed(err)
			}
			if v.Name == "get" {
				return ochazuke.Pending(func(context.Context) (any, error) { return value, nil })
			}
			return ochazuke.Done(value)
		}
	}})
	if len(mounted.Warnings) > 0 {
		t.Fatal(mounted.Warnings)
	}
	page := CatalogPage{ID: "parcels", Title: "Parcels", Lists: []CatalogList{{ID: "rows", Noun: "parcel"}}}
	pv, err := MountPage(m, doc, page, PageConfig{Noun: local})
	if err != nil {
		t.Fatal(err)
	}
	if err := pv.SetRows("rows", []Row{{ID: "t1", Noun: "parcel", Label: "Original", Data: map[string]any(thing)}}); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("parcels"); err != nil {
		t.Fatal(err)
	}
	m.View()
	if err := r.SetFocus("rows/t1"); err != nil {
		t.Fatal(err)
	}
	return r, m, thing
}

func TestMountedPageStandardEdit(t *testing.T) {
	t.Parallel()
	r, m, thing := mountedEditFixture(t, "confirm")
	if handled, err := r.HandleBinding("i"); err != nil || !handled {
		t.Fatalf("edit key: %t %v", handled, err)
	}
	if err := Settle(r, nil); err != nil {
		t.Fatal(err)
	}
	e := r.RecordEditor("edit-form")
	if e == nil || !reflect.DeepEqual(e.Values(), ochazuke.Record{"title": "Original", "status": "todo", "active": "false"}) {
		t.Fatalf("mounted form: %v", e)
	}
	keytest.Press(m, "A")
	keytest.Press(m, "x")
	keytest.Press(m, "esc")
	keytest.Press(m, "esc")
	if thing["title"] != "Original" || r.TopModal() != "" {
		t.Fatalf("cancel: %v modal %s", thing, r.TopModal())
	}
	if handled, err := r.HandleBinding("i"); err != nil || !handled {
		t.Fatalf("edit key: %t %v", handled, err)
	}
	if err := Settle(r, nil); err != nil {
		t.Fatal(err)
	}
	keytest.Press(m, "A")
	keytest.Press(m, "x")
	if _, err := r.HandleBinding("enter"); err != nil {
		t.Fatal(err)
	}
	if err := Settle(r, nil); err != nil {
		t.Fatal(err)
	}
	if thing["title"] != "Originalx" || thing["stamp"] != "fixed" || r.TopModal() != "" {
		t.Fatalf("confirmed %v modal %s", thing, r.TopModal())
	}
	var out bytes.Buffer
	sh := New(r, "host", nil, &out, &out)
	if err := sh.Run(context.Background(), []string{"ext-parcel", "edit", "--id", "t1", "--status", "done", "--active", "true"}); err != nil {
		t.Fatal(err)
	}
	if thing["status"] != "done" || thing["active"] != true || strings.Contains(out.String(), "error:") {
		t.Fatalf("CLI: %v output %s", thing, out.String())
	}
}

func TestMountedStandardEditOptOut(t *testing.T) {
	t.Parallel()
	r := ochazuke.NewRuntime()
	if _, err := r.Settings().SetSession(ochazuke.EditFormsSetting, false); err != nil {
		t.Fatal(err)
	}
	doc := hostTestCatalog(t)
	got := MountNouns(r, doc.AppNouns(), HostMount{
		Local: func(noun string) string { return "ext-" + noun },
		Proxy: func(_ string, v CatalogVerb) any {
			return func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done("proxy " + v.Name) }
		},
	})
	if len(got.Warnings) > 0 {
		t.Fatal(got.Warnings)
	}
	if _, err := r.Execute(ochazuke.Request{Noun: "ext-task", Verb: "edit", Args: ochazuke.Arguments{"id": "t1"}}); err == nil {
		t.Fatal("disabled mounted editor ran")
	}
	// set runs here over the proxy store, whose set the proxy answers.
	if _, err := r.Execute(ochazuke.Request{Noun: "ext-task", Verb: "set", Args: ochazuke.Arguments{"id": "t1", "prop": "title", "value": "Still writable"}}); err != nil {
		t.Fatalf("existing write: %v", err)
	}
}

func TestMountedPageChangeEditsSurviveClose(t *testing.T) {
	t.Parallel()
	r, _, thing := mountedEditFixture(t, "change")
	if handled, err := r.HandleBinding("i"); err != nil || !handled {
		t.Fatalf("edit key %t %v", handled, err)
	}
	if err := Settle(r, nil); err != nil {
		t.Fatal(err)
	}
	e := r.RecordEditor("edit-form")
	for _, key := range []string{"A", "x", "esc", "j", "space", "j", "enter", "esc"} {
		if _, err := r.HandleBinding(key); err != nil {
			t.Fatal(err)
		}
		e.PendingCommand()
		if thing["title"] != "Original" || thing["status"] != "todo" {
			t.Fatalf("pending write ran inline after %s: %v", key, thing)
		}
	}
	if r.TopModal() != "" {
		t.Fatalf("slow save trapped form: %s", r.TopModal())
	}
	if err := Settle(r, nil); err != nil {
		t.Fatal(err)
	}
	if thing["title"] != "Originalx" || thing["status"] != "done" || thing["stamp"] != "fixed" {
		t.Fatalf("mounted save %v", thing)
	}
}

func TestEditGetLandsAfterFocusMoved(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"focus", "page", "mode", "modal", "escape", "repeat"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			r, _, thing := mountedEditFixture(t, "change")
			if _, err := r.HandleBinding("i"); err != nil {
				t.Fatal(err)
			}
			switch change {
			case "focus":
				if err := r.SetFocus("rows"); err != nil {
					t.Fatal(err)
				}
			case "page":
				if _, err := r.AddPage("other", "Other", func(*ochazuke.Runtime, int, int) ochazuke.Block { return ochazuke.Text("Other") }); err != nil {
					t.Fatal(err)
				}
				if err := r.OpenPage("other"); err != nil {
					t.Fatal(err)
				}
			case "mode":
				if err := r.SetMode("insert"); err != nil {
					t.Fatal(err)
				}
			case "modal":
				if _, err := r.OpenModal("other", ochazuke.Payload{}); err != nil {
					t.Fatal(err)
				}
			case "escape":
				if _, err := r.HandleBinding("esc"); err != nil {
					t.Fatal(err)
				}
			case "repeat":
				if _, err := r.HandleBinding("i"); err != nil {
					t.Fatal(err)
				}
			}
			running := 0
			for _, j := range r.Jobs() {
				if j.State == ochazuke.JobRunning {
					running++
				}
			}
			if change == "escape" && running != 0 || change == "repeat" && running != 1 {
				t.Fatalf("pending opens after %s: %d", change, running)
			}
			if err := Settle(r, nil); err != nil {
				t.Fatal(err)
			}
			want := ""
			if change == "modal" {
				want = "other"
			} else if change == "repeat" {
				want = "edit-form"
			}
			if r.TopModal() != want || thing["title"] != "Original" {
				t.Fatalf("late open after %s: modal %q store %v", change, r.TopModal(), thing)
			}
		})
	}
}

func TestReopenOrdersWritesPerThing(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"change", "confirm"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			testReopenOrdersWrites(t, mode)
		})
	}
}

func testReopenOrdersWrites(t *testing.T, mode string) {
	t.Helper()
	// The first write waits at the extension, as a slow one would.
	gate := make(chan struct{})
	r, _, thing := mountedEditFixture(t, mode, func(ctx context.Context) error {
		select {
		case <-gate:
		case <-ctx.Done():
		}
		return nil
	})
	exit := "esc"
	if mode == "confirm" {
		exit = "enter"
	}
	pump := newPump(t, r)
	open := func() { t.Helper(); pump.open() }
	press := func(keys ...string) {
		t.Helper()
		e := r.RecordEditor("edit-form")
		for _, key := range keys {
			if _, err := r.HandleBinding(key); err != nil {
				t.Fatal(err)
			}
			e.PendingCommand()
		}
	}
	open()
	press("A", "x", "esc", exit)
	open()
	if got := r.RecordEditor("edit-form").Values()["title"]; got != "Originalx" {
		t.Fatalf("reopened pending value: %q", got)
	}
	press("A", "y", "esc", exit)
	writes := 0
	for _, j := range r.Jobs() {
		if j.Verb == "ext-parcel.set" && j.State == ochazuke.JobRunning {
			writes++
		}
	}
	if writes != 1 || thing["title"] != "Original" {
		t.Fatalf("writes must queue without racing: %d store %v", writes, thing)
	}
	close(gate)
	pump.until("the writes to end", pump.idle)
	if thing["title"] != "Originalxy" {
		t.Fatalf("serialized writes lost an edit: %v", thing)
	}
}

func TestMountedEditOutsideWrite(t *testing.T) {
	t.Parallel()
	for _, policy := range []string{"reload", "close"} {
		t.Run(policy, func(t *testing.T) {
			t.Parallel()
			r, _, thing := mountedEditFixture(t, "change")
			if err := r.Settings().Set(ochazuke.FormsOutsideWriteSetting, policy); err != nil {
				t.Fatal(err)
			}
			if _, err := r.HandleBinding("i"); err != nil {
				t.Fatal(err)
			}
			if err := Settle(r, nil); err != nil {
				t.Fatal(err)
			}
			_, err := r.Execute(ochazuke.Request{Noun: "ext-parcel", Verb: "set", Args: ochazuke.Arguments{"id": "t1", "changes": map[string]any{"title": "Outside"}}})
			if err != nil {
				t.Fatal(err)
			}
			if err := Settle(r, nil); err != nil {
				t.Fatal(err)
			}
			if thing["title"] != "Outside" {
				t.Fatalf("outside write: %v", thing)
			}
			if policy == "close" {
				if r.TopModal() != "" {
					t.Fatalf("outside write did not close form: %s", r.TopModal())
				}
			} else if got := r.RecordEditor("edit-form").Values()["title"]; got != "Outside" {
				t.Fatalf("outside write did not reload: %q", got)
			}
		})
	}
}

func TestHistoryUndoDoesNotSkipMountedChange(t *testing.T) {
	t.Parallel()
	r, _, thing := mountedEditFixture(t, "change")
	local := ochazuke.Thing{"id": "p1", "title": "Local"}
	r.Define("local").Props(ochazuke.PropSpec{Name: "id", Key: true}, ochazuke.PropSpec{Name: "title"}).Store(ochazuke.Store{
		Undo: true,
		Get:  func(ochazuke.Ctx, string) (ochazuke.Thing, error) { return gomaps.Clone(local), nil },
		Patch: func(_ ochazuke.Ctx, _ string, changes ochazuke.Thing) (ochazuke.Thing, error) {
			gomaps.Copy(local, changes)
			return gomaps.Clone(local), nil
		},
	})
	if _, err := r.Execute(ochazuke.Request{Noun: "local", Verb: "set", Args: ochazuke.Arguments{"id": "p1", "changes": map[string]any{"title": "Local edited"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.HandleBinding("i"); err != nil {
		t.Fatal(err)
	}
	if err := Settle(r, nil); err != nil {
		t.Fatal(err)
	}
	e := r.RecordEditor("edit-form")
	for _, key := range []string{"A", "x", "esc", "esc"} {
		if _, err := r.HandleBinding(key); err != nil {
			t.Fatal(err)
		}
		e.PendingCommand()
	}
	if err := Settle(r, nil); err != nil {
		t.Fatal(err)
	}
	_, err := r.Execute(ochazuke.Request{Noun: "ochazuke.history", Verb: "undo"})
	if err == nil {
		err = Settle(r, nil) // over a remote store the undo is a job
	}
	// The mounted change records its inverse like a local one, so undo
	// reverts it and leaves the local thing alone.
	if err != nil {
		t.Fatalf("mounted change must undo: %v", err)
	}
	if thing["title"] != "Original" || local["title"] != "Local edited" {
		t.Fatalf("undo skipped to a different thing: mounted %v local %v", thing, local)
	}
}

func TestClosedMountedEditFailureIsNotice(t *testing.T) {
	t.Parallel()
	r, m, thing := mountedEditFixture(t, "change", func(context.Context) error { return errors.New("backend refused write") })
	if _, err := r.HandleBinding("i"); err != nil {
		t.Fatal(err)
	}
	if err := Settle(r, nil); err != nil {
		t.Fatal(err)
	}
	e := r.RecordEditor("edit-form")
	for _, key := range []string{"A", "x", "esc", "esc"} {
		if _, err := r.HandleBinding(key); err != nil {
			t.Fatal(err)
		}
		e.PendingCommand()
	}
	_ = Settle(r, nil)                       // the failed job ends: its error reaches the editor
	m.Update(keytest.Msg("j", false, false)) // A later key must not erase the failed value.
	notices := strings.Join(m.Notices, "\n")
	for _, want := range []string{"Edit ext-parcel: Original", "title", `"Originalx"`, "backend refused write"} {
		if !strings.Contains(notices, want) {
			t.Fatalf("notice lost %q: %s", want, notices)
		}
	}
	if thing["title"] != "Original" || r.TopModal() != "" {
		t.Fatalf("failed write changed the thing or reopened a form: %v, %s", thing, r.TopModal())
	}
}

func TestQuitWithHungMountedWriteCanLeave(t *testing.T) {
	t.Parallel()
	for _, exit := range []string{"ctrl+c", "q", "force", "deadline"} {
		t.Run(exit, func(t *testing.T) {
			t.Parallel()
			started := make(chan struct{})
			r, m, thing := mountedEditFixture(t, "change", func(ctx context.Context) error {
				close(started)
				<-ctx.Done()
				return ctx.Err()
			})
			if exit == "ctrl+c" || exit == "q" {
				if _, err := r.Settings().SetSession("quit.force-after", "0s"); err != nil {
					t.Fatal(err)
				}
			}
			if exit == "deadline" {
				if _, err := r.Settings().SetSession("quit.write-wait", "40ms"); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := r.HandleBinding("i"); err != nil {
				t.Fatal(err)
			}
			if err := Settle(r, nil); err != nil {
				t.Fatal(err)
			}
			e := r.RecordEditor("edit-form")
			for _, key := range []string{"A", "x", "esc", "esc"} {
				if _, err := r.HandleBinding(key); err != nil {
					t.Fatal(err)
				}
				e.PendingCommand()
			}
			p := tea.NewProgram(m, tea.WithInput(nil), tea.WithOutput(io.Discard), tea.WithoutRenderer(), tea.WithoutSignalHandler())
			done := make(chan error, 1)
			go func() {
				_, err := p.Run()
				done <- err
			}()
			defer p.Kill()
			select {
			case <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("mounted write did not start")
			}
			p.Send(keytest.Msg("ctrl+c", false, false))
			p.Send(keytest.Msg(":", false, false))
			if exit == "force" {
				p.Send(ochazuke.CallMsg{Request: ochazuke.Request{Noun: "app", Verb: "quit", Args: ochazuke.Arguments{"force": true}}, Reply: make(chan ochazuke.Response, 1)})
			} else if exit != "deadline" {
				p.Send(keytest.Msg(exit, false, false))
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("hung write trapped quit")
			}
			if r.TopModal() != "" || thing["title"] != "Original" {
				t.Fatalf("quit opened a modal or wrote: %s %v", r.TopModal(), thing)
			}
			if !strings.Contains(m.Info, "Saving Edit ext-parcel: Original") {
				t.Fatalf("quit did not name its pending write: %s", m.Info)
			}
			notices := strings.Join(m.Notices, "\n")
			for _, want := range []string{"Edit ext-parcel: Original", "title", `"Originalx"`, "completion was not confirmed"} {
				if !strings.Contains(notices, want) {
					t.Fatalf("unfinished write lost %q: %s", want, notices)
				}
			}
		})
	}
}
