package ochazuke

import (
	"context"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// offLoopTasks makes the fixture's task store read through Ctx.OffLoop, as
// a SQLite store does, with a third project's task; on says whether the
// stack reads off the loop (RelatedBackgroundSetting).
func offLoopTasks(on bool) func(*Runtime) {
	return func(r *Runtime) {
		if _, err := r.Settings().SetSession(RelatedBackgroundSetting, on); err != nil {
			panic(err)
		}
		st := r.Nouns["task"].Store
		list := st.List
		st.List = func(c Ctx, q ListQuery) ([]Thing, error) {
			v, err := c.OffLoop(StoreCall{Store: "task", Op: "list"}, func(context.Context) (any, error) {
				return list(c, q) // the test store's rows: nothing the loop writes meanwhile
			})
			things, _ := v.([]Thing)
			return things, err
		}
	}
}

// update runs one update with no message of its own: the end of an
// update brings the drawn stacks in step and answers their reads' commands.
func (f *relFixture) update() tea.Cmd {
	_, cmd := f.m.Update(struct{}{})
	return cmd
}

// Read off the loop, a section shows exactly what a read on the loop
// shows once its read lands, and the previous thing's rows until then.
func TestRelatedReadsOffTheLoopLandAsAReadOnTheLoop(t *testing.T) {
	t.Parallel()
	off := newRelFixtureWith(t, offLoopTasks(true), nil) // reads off the loop
	on := newRelFixtureWith(t, offLoopTasks(false), nil) // reads on the loop
	landLoads(off.m, off.update())
	for _, f := range []*relFixture{off, on} {
		f.key = "p2"
	}
	cmd := off.update()
	if text := off.frame(); !strings.Contains(text, "write") || strings.Contains(text, "ship") {
		t.Fatalf("before its read lands the stack shows p1's tasks:\n%s", text)
	}
	if off.r.PendingCommands() != nil || cmd == nil {
		t.Fatal("the update started the read and the draw read nothing")
	}
	landLoads(off.m, cmd)
	if got, want := off.frame(), on.frame(); got != want || !strings.Contains(got, "ship") {
		t.Fatalf("landed off the loop:\n%s\nread on the loop:\n%s", got, want)
	}
}

// A fast j j j ends showing the last thing's rows, whatever order the
// earlier reads finish in.
func TestRelatedReadsOffTheLoopKeepTheLastSelection(t *testing.T) {
	t.Parallel()
	f := newRelFixtureWith(t, offLoopTasks(true), nil)
	f.tasks.add("id", "t4", "title", "launch", "project", "p3")
	landLoads(f.m, f.update())
	var cmds []tea.Cmd
	for _, key := range []string{"p2", "p3", "p1", "p3"} {
		f.key = key
		cmds = append(cmds, f.update())
		f.frame()
	}
	slices.Reverse(cmds)
	for _, cmd := range cmds {
		landLoads(f.m, cmd)
	}
	text := f.frame()
	if !strings.Contains(text, "▾ Tasks (1)") || !strings.Contains(text, "launch") {
		t.Fatalf("the stack does not show p3's task:\n%s", text)
	}
	for _, other := range []string{"write", "ship"} {
		if strings.Contains(text, other) {
			t.Fatalf("an earlier selection's task %q shows:\n%s", other, text)
		}
	}
}
