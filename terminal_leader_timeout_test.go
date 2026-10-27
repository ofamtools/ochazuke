package ochazuke

import (
	"reflect"
	"testing"
	"time"
)

// leaderTimeoutFixture is terminalFixture with the key-sequence timeout
// set to binding and the terminal settings declared; file, when not
// empty, is settings.json.
func leaderTimeoutFixture(t *testing.T, binding time.Duration, file string, declare ...Setting) (*Runtime, *fakeStream) {
	t.Helper()
	r, _, stream := terminalFixture(t)
	r.BindingTimeout = binding
	dir := t.TempDir()
	r.Config().Dir = dir
	if file != "" {
		writeConfig(t, dir, SettingsFile, file)
	}
	if err := r.DeclareSetting(declare...); err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	r.EnsureTerminalLeader()
	m.Init()
	return r, stream
}

// waitPastTimer arms nothing when the leader waits forever: no timer is
// scheduled, a stale timer message does nothing, and the leader is still
// pending well past BindingTimeout.
func waitPastTimer(t *testing.T, r *Runtime, wait time.Duration) {
	t.Helper()
	if cmd := r.BindingTimeoutCommand(); cmd != nil {
		t.Fatal("the leader scheduled a timer")
	}
	time.Sleep(wait)
	if handled, err := r.HandleBindingTimeout(BindingTimeoutMsg{runtime: r, version: r.bindingVersion}); handled || err != nil {
		t.Fatalf("timer message: handled=%v err=%v", handled, err)
	}
	if r.Mode.Name != "leader_pending" || r.Pending != "ctrl+a" {
		t.Fatalf("leader expired: mode %q pending %q", r.Mode.Name, r.Pending)
	}
}

// terminal-leader-timeout 0: the leader waits for the next key however
// long it takes; then d detaches, an unbound key sends the leader and
// itself, the leader again sends one leader. Every other sequence keeps
// BindingTimeout, and a focus change or the pane's removal still clears
// the waiting leader.
func TestTerminalLeaderTimeoutZeroWaitsForTheNextKey(t *testing.T) {
	const binding = 5 * time.Millisecond
	r, stream := leaderTimeoutFixture(t, binding, `{"terminal-leader-timeout": "0s"}`)
	calls := recorder(t, r, "top")
	r.Define("app").Mode("normal").Bind("g g", Do("test.top"))
	if got := r.terminalLeaderTimeout(); got != 0 {
		t.Fatalf("leader timeout %v", got)
	}
	press(t, r, "ctrl+a")
	waitPastTimer(t, r, 20*binding)
	press(t, r, "x")
	press(t, r, "ctrl+a")
	waitPastTimer(t, r, 20*binding)
	press(t, r, "ctrl+a")
	if want := [][]string{{"ctrl+a", "x"}, {"ctrl+a"}}; !reflect.DeepEqual(stream.sent, want) || r.Mode.Name != "terminal" {
		t.Fatalf("sent %v mode %q", stream.sent, r.Mode.Name)
	}
	press(t, r, "ctrl+a")
	waitPastTimer(t, r, 20*binding)
	press(t, r, "d")
	if r.Focus != "console" || r.Mode.Name != "normal" || r.Pending != "" || len(stream.sent) != 2 {
		t.Fatalf("detach: focus %q mode %q pending %q sent %v", r.Focus, r.Mode.Name, r.Pending, stream.sent)
	}

	// g g still times out at BindingTimeout.
	press(t, r, "g")
	cmd := r.BindingTimeoutCommand()
	if cmd == nil || r.Pending != "g" {
		t.Fatalf("g: timer %v pending %q", cmd != nil, r.Pending)
	}
	if left := time.Until(r.prefix.deadline); left > binding {
		t.Fatalf("g waits %v, more than BindingTimeout", left)
	}
	if handled, err := r.HandleBindingTimeout(runCommands(cmd)[0].(BindingTimeoutMsg)); !handled || err != nil || r.Pending != "" || len(*calls) != 0 {
		t.Fatalf("g timeout: handled=%v err=%v pending %q calls %v", handled, err, r.Pending, *calls)
	}

	// The focus moving away clears the waiting leader.
	attach := Request{Verb: "attach", Noun: "ochazuke.terminal", Args: Arguments{"id": "pane"}}
	if _, err := r.Execute(attach); err != nil {
		t.Fatal(err)
	}
	press(t, r, "ctrl+a")
	if err := r.SetFocus("console"); err != nil {
		t.Fatal(err)
	}
	if r.Pending != "" || r.Mode.Name != "normal" {
		t.Fatalf("focus moved: pending %q mode %q", r.Pending, r.Mode.Name)
	}
	// So does the pane closing.
	if _, err := r.Execute(attach); err != nil {
		t.Fatal(err)
	}
	press(t, r, "ctrl+a")
	if err := r.RemoveInstance("pane"); err != nil {
		t.Fatal(err)
	}
	if r.Pending != "" || r.Mode.Name == "leader_pending" || r.Focus == "pane" {
		t.Fatalf("pane closed: pending %q mode %q focus %q", r.Pending, r.Mode.Name, r.Focus)
	}
	if len(stream.sent) != 2 {
		t.Fatalf("sent %v", stream.sent)
	}
}

// A positive terminal-leader-timeout is the leader's own timeout, not
// BindingTimeout, either way round.
func TestTerminalLeaderTimeoutPositiveIsTheLeadersOwn(t *testing.T) {
	r, stream := leaderTimeoutFixture(t, time.Hour, `{"terminal-leader-timeout": "20ms"}`)
	press(t, r, "ctrl+a")
	cmd := r.BindingTimeoutCommand()
	if cmd == nil {
		t.Fatal("no timer")
	}
	if left := time.Until(r.prefix.deadline); left > 20*time.Millisecond {
		t.Fatalf("leader waits %v", left)
	}
	if handled, err := r.HandleBindingTimeout(runCommands(cmd)[0].(BindingTimeoutMsg)); !handled || err != nil {
		t.Fatalf("timer: handled=%v err=%v", handled, err)
	}
	if !reflect.DeepEqual(stream.sent, [][]string{{"ctrl+a"}}) || r.Mode.Name != "terminal" {
		t.Fatalf("sent %v mode %q", stream.sent, r.Mode.Name)
	}

	r, stream = leaderTimeoutFixture(t, 5*time.Millisecond, `{"terminal-leader-timeout": "1h"}`)
	press(t, r, "ctrl+a")
	time.Sleep(30 * time.Millisecond)
	press(t, r, "x")
	if !reflect.DeepEqual(stream.sent, [][]string{{"ctrl+a", "x"}}) {
		t.Fatalf("sent %v", stream.sent)
	}
	if err := r.Settings().Set(TerminalLeaderTimeoutSetting, "-1s"); err == nil {
		t.Fatal("a negative timeout accepted")
	}
}

// Without the setting, and with the framework's declaration unset, the
// leader keeps BindingTimeout; an application's own declaration of 0 is
// its default.
func TestTerminalLeaderTimeoutDefaults(t *testing.T) {
	expires := func(t *testing.T, r *Runtime, stream *fakeStream) {
		t.Helper()
		press(t, r, "ctrl+a")
		cmd := r.BindingTimeoutCommand()
		if cmd == nil {
			t.Fatal("no timer")
		}
		if handled, err := r.HandleBindingTimeout(runCommands(cmd)[0].(BindingTimeoutMsg)); !handled || err != nil {
			t.Fatalf("timer: handled=%v err=%v", handled, err)
		}
		if !reflect.DeepEqual(stream.sent, [][]string{{"ctrl+a"}}) {
			t.Fatalf("sent %v", stream.sent)
		}
	}
	t.Run("undeclared", func(t *testing.T) {
		r, _, stream := terminalFixture(t)
		r.BindingTimeout = 5 * time.Millisecond
		if r.Settings().find(TerminalLeaderTimeoutSetting) != nil {
			t.Fatal("declared without EnsureTerminalLeader")
		}
		expires(t, r, stream)
	})
	t.Run("framework default", func(t *testing.T) {
		r, stream := leaderTimeoutFixture(t, 5*time.Millisecond, "")
		v, err := r.Settings().Get(TerminalLeaderTimeoutSetting)
		if err != nil || v.Type != SettingDuration || v.Default != "1s" || v.Set {
			t.Fatalf("setting %+v %v", v, err)
		}
		expires(t, r, stream)
	})
	t.Run("application default 0", func(t *testing.T) {
		r, _ := leaderTimeoutFixture(t, 5*time.Millisecond, "", Setting{Name: TerminalLeaderTimeoutSetting, Type: SettingDuration, Default: "0s"})
		press(t, r, "ctrl+a")
		waitPastTimer(t, r, 30*time.Millisecond)
	})
}
