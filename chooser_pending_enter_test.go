package ochazuke

import (
	"context"
	"slices"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"ofam.dev/ochazuke/keytest"
)

// slowLive opens a chooser whose Live source answers only once gate is
// closed (or is cancelled), as a dead mount would.
func slowLive(t *testing.T) (*Runtime, *Model, *Chooser, *[]string, chan struct{}) {
	t.Helper()
	r := NewRuntime()
	m := NewModel(r)
	gate := make(chan struct{})
	var chosen []string
	src := ChooserSource{Name: "slow", Live: true, Load: func(q string) Result {
		return Pending(func(ctx context.Context) (any, error) {
			select {
			case <-gate:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return []ChooserRow{{ID: "r:" + q, Label: "r " + q}}, nil
		})
	}}
	c, err := r.OpenChooser(ChooserSpec{Kind: "slow-pick", Sources: []ChooserSource{src, rowsSource("other", "o")},
		Choose: func(row ChooserRow) Result { chosen = append(chosen, row.ID); return Done(nil) }})
	if err != nil {
		t.Fatal(err)
	}
	return r, m, c, &chosen, gate
}

// Enter on a Live source's query still loading returns at once (nothing
// blocks), waits, and acts on the rows of the query as typed when they land.
func TestChooserEnterWaitsForLiveLoad(t *testing.T) {
	r, m, c, chosen, gate := slowLive(t)
	typed := keytest.Type(m, "x")
	start := time.Now()
	enter := keytest.Press(m, "enter")
	if d := time.Since(start); d > time.Second {
		t.Fatalf("enter blocked for %v", d)
	}
	if len(*chosen) != 0 || r.Chooser("slow-pick") != c {
		t.Fatalf("picked early: %v", *chosen)
	}
	close(gate)
	landLoads(m, tea.Batch(typed, enter))
	if !slices.Equal(*chosen, []string{"r:x"}) {
		t.Fatalf("chosen %v", *chosen)
	}
}

// Esc while enter waits closes the chooser and picks nothing.
func TestChooserEscWhileEnterWaits(t *testing.T) {
	r, m, _, chosen, gate := slowLive(t)
	typed := keytest.Type(m, "x")
	keytest.Press(m, "enter")
	start := time.Now()
	keytest.Press(m, "esc")
	if d := time.Since(start); d > time.Second {
		t.Fatalf("esc blocked for %v", d)
	}
	if r.Chooser("slow-pick") != nil {
		t.Fatal("esc left the chooser open")
	}
	close(gate)
	landLoads(m, typed)
	if len(*chosen) != 0 {
		t.Fatalf("chosen %v", *chosen)
	}
}

// Typing while enter waits drops it: the person changed their mind.
func TestChooserTypingDropsWaitingEnter(t *testing.T) {
	r, m, c, chosen, gate := slowLive(t)
	var cmds []tea.Cmd
	cmds = append(cmds, keytest.Type(m, "x"), keytest.Press(m, "enter"), keytest.Type(m, "y"))
	close(gate)
	landLoads(m, tea.Batch(cmds...))
	if len(*chosen) != 0 || r.Chooser("slow-pick") != c {
		t.Fatalf("chosen %v", *chosen)
	}
	if got := labels(c.Matches()); !slices.Equal(got, []string{"r xy"}) {
		t.Fatalf("rows %v", got)
	}
}

// A load that fails while enter waits drops the enter, shows the error as
// the empty line and keeps the chooser open.
func TestChooserFailedLoadDropsWaitingEnter(t *testing.T) {
	r := NewRuntime()
	m := NewModel(r)
	var chosen []string
	src := ChooserSource{Name: "dead", Live: true, Load: func(string) Result {
		return Pending(func(context.Context) (any, error) { return nil, errTest("mount is gone") })
	}}
	c, err := r.OpenChooser(ChooserSpec{Kind: "dead-pick", Sources: []ChooserSource{src, rowsSource("other", "o")},
		Choose: func(row ChooserRow) Result { chosen = append(chosen, row.ID); return Done(nil) }})
	if err != nil {
		t.Fatal(err)
	}
	typed := keytest.Type(m, "x")
	enter := keytest.Press(m, "enter")
	landLoads(m, tea.Batch(typed, enter))
	if len(chosen) != 0 || r.Chooser("dead-pick") != c || c.pending {
		t.Fatalf("chosen %v pending %v", chosen, c.pending)
	}
	if got := c.emptyLine(); got != "! mount is gone" {
		t.Fatalf("empty line %q", got)
	}
}

type errTest string

func (e errTest) Error() string { return string(e) }
