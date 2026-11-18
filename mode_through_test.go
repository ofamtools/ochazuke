package ochazuke

import (
	"reflect"
	"slices"
	"testing"
	"time"
)

// throughFixture is a live terminal with normal-mode rows that record which
// verb ran: d and d d share a first key, x is a single key, g g has no shorter
// match, and m opens a modal.
func throughFixture(t *testing.T) (*Runtime, *fakeStream, *[]string) {
	t.Helper()
	r, _, stream := terminalFixture(t)
	var ran []string
	probe := r.Define("probe")
	for _, verb := range []string{"d", "dd", "x", "gg", "motion"} {
		probe.Verb(verb, func(Ctx) Result { ran = append(ran, verb); return Done(nil) })
	}
	op := r.Operations.At(Key("probe.motion"))
	op.Motion = true
	r.Operations.Set(op)
	probe.Verb("modal", func(c Ctx) Result {
		ran = append(ran, "modal")
		if err := c.Runtime.Push("through-modal", "pane"); err != nil {
			t.Fatal(err)
		}
		return Done(nil)
	})
	r.Define("ochazuke.terminal").Mode("normal").
		Bind("d", Do("probe.d")).
		Bind("d d", Do("probe.dd")).
		Bind("x", Do("probe.x")).
		Bind("g g", Do("probe.gg")).
		Bind("w", Do("probe.motion")).
		Bind("m", Do("probe.modal"))
	return r, stream, &ran
}

func throughKeys(t *testing.T, r *Runtime, keys ...string) {
	t.Helper()
	for _, key := range keys {
		if _, err := r.Dispatch(MacroKey{Key: key}); err != nil {
			t.Fatalf("key %s: %v", key, err)
		}
	}
}

func TestThroughResolvesOneBindingThenReturns(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		keys []string
		ran  []string
		sent []string
		mode string
	}{
		{name: "sequence", keys: []string{"d", "d"}, ran: []string{"dd"}, mode: "terminal"},
		{name: "shorter match then the next key", keys: []string{"d", "x"}, ran: []string{"d", "x"}, mode: "terminal"},
		{name: "next key starts a sequence", keys: []string{"d", "g"}, ran: []string{"d"}, mode: "normal g"},
		{name: "next key completes that sequence", keys: []string{"d", "g", "g"}, ran: []string{"d", "gg"}, mode: "terminal"},
		{name: "motion", keys: []string{"w"}, ran: []string{"motion"}, mode: "terminal"},
		{name: "count", keys: []string{"3", "w"}, ran: []string{"motion"}, mode: "terminal"},
		{name: "esc after a count sends nothing", keys: []string{"3", "esc"}, mode: "terminal"},
		{name: "key with no row reaches the pane", keys: []string{"y"}, sent: []string{"y"}, mode: "terminal"},
		{name: "shorter match then a key with no row", keys: []string{"d", "y"}, ran: []string{"d"}, sent: []string{"y"}, mode: "terminal"},
		{name: "esc returns and sends nothing", keys: []string{"esc"}, mode: "terminal"},
		{name: "esc cancels a held sequence and stays", keys: []string{"d", "esc"}, mode: "ephemeral"},
		{name: "second esc returns", keys: []string{"d", "esc", "esc"}, mode: "terminal"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r, stream, ran := throughFixture(t)
			throughKeys(t, r, "ctrl+a", "_")
			if r.Mode.Name != "ephemeral" {
				t.Fatalf("entered %q", r.Mode.Name)
			}
			throughKeys(t, r, tc.keys...)
			if !slices.Equal(*ran, tc.ran) {
				t.Fatalf("ran %v, want %v", *ran, tc.ran)
			}
			var sent []string
			for _, keys := range stream.sent {
				sent = append(sent, keys...)
			}
			if !slices.Equal(sent, tc.sent) {
				t.Fatalf("sent %v, want %v", sent, tc.sent)
			}
			if r.Mode.Name != tc.mode || r.Focus != "pane" {
				t.Fatalf("mode %q focus %q, want %q on the pane", r.Mode.Name, r.Focus, tc.mode)
			}
		})
	}
}

// A sequence that resolves on its deadline returns to the pane, whether the
// timer fires or the next key finds the deadline passed; that next key is
// then typed into the pane.
func TestThroughReturnsWhenHeldSequenceExpires(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		expire func(t *testing.T, r *Runtime)
		sent   [][]string
	}{
		{"timer", func(t *testing.T, r *Runtime) {
			msg, ok := r.BindingTimeoutTick()
			if !ok {
				t.Fatal("no sequence timer in flight")
			}
			if _, err := r.HandleBindingTimeout(msg); err != nil {
				t.Fatal(err)
			}
		}, nil},
		{"next key", func(t *testing.T, r *Runtime) {
			r.prefix.deadline = time.Now().Add(-time.Second)
			throughKeys(t, r, "x")
		}, [][]string{{"x"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r, stream, ran := throughFixture(t)
			throughKeys(t, r, "ctrl+a", "_", "d")
			tc.expire(t, r)
			if !reflect.DeepEqual(*ran, []string{"d"}) || r.Mode.Name != "terminal" || !reflect.DeepEqual(stream.sent, tc.sent) {
				t.Fatalf("ran %v mode %q sent %v", *ran, r.Mode.Name, stream.sent)
			}
		})
	}
}

// A binding that takes the person elsewhere leaves them there, with nothing
// left to fire later.
func TestThroughLeavesAMovedFocusAlone(t *testing.T) {
	t.Parallel()
	r, _, _ := throughFixture(t)
	throughKeys(t, r, "ctrl+a", "_", "a")
	if r.Focus == "pane" || r.Mode.Name != "normal" || r.UI().through.mode != "" {
		t.Fatalf("focus %q mode %q state %+v", r.Focus, r.Mode.Name, r.UI().through)
	}
}

// sequence-unknown swallow drops the held keys: nothing resolved, so the
// passthrough keeps waiting and the pane receives nothing.
func TestThroughSequenceUnknownSwallow(t *testing.T) {
	t.Parallel()
	r, stream, ran := throughFixture(t)
	if err := r.Settings().Set(SequenceUnknownSetting, SequenceSwallow); err != nil {
		t.Fatal(err)
	}
	throughKeys(t, r, "ctrl+a", "_", "d", "x")
	if len(*ran) != 0 || len(stream.sent) != 0 || r.Mode.Name != "ephemeral" {
		t.Fatalf("ran %v sent %v mode %q", *ran, stream.sent, r.Mode.Name)
	}
}

// A modal the binding opens returns to the live pane when it closes.
func TestThroughModalReturnsToLive(t *testing.T) {
	t.Parallel()
	r, _, _ := throughFixture(t)
	throughKeys(t, r, "ctrl+a", "_", "m")
	if r.TopModal() != "through-modal" {
		t.Fatalf("modal %q", r.TopModal())
	}
	if r.UI().through.mode != "" {
		t.Fatalf("state survived the modal: %+v", r.UI().through)
	}
	if err := r.Pop(); err != nil {
		t.Fatal(err)
	}
	if r.Mode.Name != "terminal" || r.Focus != "pane" {
		t.Fatalf("mode %q focus %q", r.Mode.Name, r.Focus)
	}
}
