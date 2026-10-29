package ochazuke

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// seqTimeoutFixture is terminalFixture with BindingTimeout cleared (so the
// sequence-timeout setting decides), a config directory with file as
// settings.json when not empty, the declarations given, then the
// framework's (EnsureTerminalLeader, Model.Init), and a recording sink.
func seqTimeoutFixture(t *testing.T, file string, declare ...Setting) (*Runtime, *fakeStream, *RecordingSink) {
	t.Helper()
	r, _, stream := terminalFixture(t)
	r.BindingTimeout = 0
	dir := t.TempDir()
	r.Config().Dir = dir
	if file != "" {
		writeConfig(t, dir, SettingsFile, file)
	}
	if err := r.DeclareSetting(declare...); err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	sink := &RecordingSink{}
	r.EnsureTerminalLeader()
	m.Init()
	r.UI().Attach(sink)
	return r, stream, sink
}

// waitsForever: no timer is scheduled, a stale timer message does
// nothing, and the sequence is still pending.
func waitsForever(t *testing.T, r *Runtime, mode, pending string) {
	t.Helper()
	if cmd := r.BindingTimeoutCommand(); cmd != nil {
		t.Fatal("a timer was scheduled")
	}
	if _, ok := r.BindingTimeoutTick(); ok {
		t.Fatal("a tick is due")
	}
	time.Sleep(5 * time.Millisecond)
	if handled, err := r.HandleBindingTimeout(BindingTimeoutMsg{runtime: r, version: r.bindingVersion}); handled || err != nil {
		t.Fatalf("timer message: handled=%v err=%v", handled, err)
	}
	if r.Mode.Name != mode || r.Pending != pending {
		t.Fatalf("expired: mode %q pending %q", r.Mode.Name, r.Pending)
	}
}

// The defaults are the embedded rows: sequence-timeout 0, sequence-unknown
// pass, terminal-leader ctrl+a; the framework declares them.
func TestSequenceSettingsDefaultsAreEmbedded(t *testing.T) {
	r, _, _ := seqTimeoutFixture(t, "")
	for name, want := range map[string]any{SequenceTimeoutSetting: "0s", SequenceUnknownSetting: SequencePass, TerminalLeaderSetting: "ctrl+a"} {
		v, err := r.Settings().Get(name)
		if err != nil || v.Default != want || v.Value != want || v.Set {
			t.Fatalf("%s: %+v %v", name, v, err)
		}
	}
	if err := r.Settings().Set(SequenceTimeoutSetting, "-1s"); err == nil {
		t.Fatal("a negative timeout accepted")
	}
	if err := r.Settings().Set(SequenceUnknownSetting, "drop"); err == nil {
		t.Fatal("an unknown sequence-unknown accepted")
	}
	// A bare runtime (no settings declared) reads the same defaults.
	bare := NewRuntime()
	if bare.sequenceTimeout() != 0 || bare.swallowsUnknown() || bare.TerminalLeader() != "ctrl+a" {
		t.Fatalf("bare: timeout %v swallow %v leader %q", bare.sequenceTimeout(), bare.swallowsUnknown(), bare.TerminalLeader())
	}
}

// With the default, every prefix waits for the next key: a sequence's
// prefix and the terminal leader alike. A focus change still clears it.
func TestSequenceTimeoutZeroWaitsForEveryPrefix(t *testing.T) {
	r, stream, _ := seqTimeoutFixture(t, "")
	calls := recorder(t, r, "top")
	r.Define("app").Mode("normal").Bind("g g", Do("test.top"))
	press(t, r, "ctrl+a")
	waitsForever(t, r, "leader_pending", "ctrl+a")
	press(t, r, "d")
	if r.Focus != "console" || r.Mode.Name != "normal" {
		t.Fatalf("detach: focus %q mode %q", r.Focus, r.Mode.Name)
	}
	press(t, r, "g")
	waitsForever(t, r, "normal g", "g")
	press(t, r, "g")
	if !reflect.DeepEqual(*calls, []string{"top"}) || r.Pending != "" {
		t.Fatalf("g g: calls %v pending %q", *calls, r.Pending)
	}
	attach := Request{Verb: "attach", Noun: "ochazuke.terminal", Args: Arguments{"id": "pane"}}
	if _, err := r.Execute(attach); err != nil {
		t.Fatal(err)
	}
	press(t, r, "ctrl+a")
	if err := r.SetFocus("console"); err != nil {
		t.Fatal(err)
	}
	if r.Pending != "" || r.Mode.Name != "normal" || len(stream.sent) != 0 {
		t.Fatalf("focus moved: pending %q mode %q sent %v", r.Pending, r.Mode.Name, stream.sent)
	}
}

// A positive sequence-timeout expires every prefix: the leader sends
// itself alone, g runs its shorter binding. No branch picks a mode's own
// timeout.
func TestSequenceTimeoutPositiveExpiresEveryPrefix(t *testing.T) {
	r, stream, _ := seqTimeoutFixture(t, `{"sequence-timeout": "20ms"}`)
	calls := recorder(t, r, "go", "top")
	r.Define("app").Mode("normal").Bind("g", Do("test.go")).Bind("g g", Do("test.top"))
	expire := func() {
		t.Helper()
		cmd := r.BindingTimeoutCommand()
		if cmd == nil {
			t.Fatal("no timer")
		}
		if left := time.Until(r.prefix.deadline); left > 20*time.Millisecond {
			t.Fatalf("waits %v", left)
		}
		if handled, err := r.HandleBindingTimeout(runCommands(cmd)[0].(BindingTimeoutMsg)); !handled || err != nil {
			t.Fatalf("timer: handled=%v err=%v", handled, err)
		}
	}
	press(t, r, "ctrl+a")
	expire()
	if !reflect.DeepEqual(stream.sent, [][]string{{"ctrl+a"}}) || r.Mode.Name != "terminal" {
		t.Fatalf("leader: sent %v mode %q", stream.sent, r.Mode.Name)
	}
	press(t, r, "ctrl+a", "d", "g")
	expire()
	if !reflect.DeepEqual(*calls, []string{"go"}) {
		t.Fatalf("g: calls %v", *calls)
	}
	// BindingTimeout, set in code, stands in for the setting.
	r.BindingTimeout = time.Hour
	press(t, r, "g")
	if left := time.Until(r.prefix.deadline); left < time.Minute {
		t.Fatalf("BindingTimeout ignored: %v", left)
	}
}

// An unbound key after a prefix, with sequence-unknown pass: the prefix
// ends (its shorter binding runs: the leader sends itself) and the key is
// handled as if typed alone.
func TestSequenceUnknownPassHandsTheKeyOn(t *testing.T) {
	r, stream, _ := seqTimeoutFixture(t, "")
	calls := recorder(t, r, "go", "top", "x")
	press(t, r, "ctrl+a", "x")
	if !reflect.DeepEqual(stream.sent, [][]string{{"ctrl+a"}, {"x"}}) || r.Mode.Name != "terminal" || r.Pending != "" {
		t.Fatalf("leader x: sent %v mode %q pending %q", stream.sent, r.Mode.Name, r.Pending)
	}
	press(t, r, "ctrl+a", "d")
	r.Define("app").Mode("normal").Bind("g", Do("test.go")).Bind("g g", Do("test.top")).Bind("x", Do("test.x"))
	press(t, r, "g", "x")
	if !reflect.DeepEqual(*calls, []string{"go", "x"}) || r.Pending != "" || r.Mode.Name != "normal" {
		t.Fatalf("g x: calls %v pending %q mode %q", *calls, r.Pending, r.Mode.Name)
	}
}

// With sequence-unknown swallow, the prefix and the key are dropped with
// a notice; nothing reaches the pane or runs.
func TestSequenceUnknownSwallowDropsBoth(t *testing.T) {
	r, stream, sink := seqTimeoutFixture(t, `{"sequence-unknown": "swallow"}`)
	calls := recorder(t, r, "go", "top", "x")
	press(t, r, "ctrl+a")
	if outcome := pressOutcome(t, r, "x"); outcome != KeyConsumed {
		t.Fatalf("outcome %v", outcome)
	}
	if len(stream.sent) != 0 || r.Mode.Name != "terminal" || r.Pending != "" {
		t.Fatalf("leader x: sent %v mode %q pending %q", stream.sent, r.Mode.Name, r.Pending)
	}
	msgs := sink.Messages()
	if len(msgs) == 0 || msgs[len(msgs)-1].Text != "ctrl+a x is not bound" {
		t.Fatalf("messages %+v", msgs)
	}
	press(t, r, "ctrl+a", "ctrl+a")
	if !reflect.DeepEqual(stream.sent, [][]string{{"ctrl+a"}}) {
		t.Fatalf("leader twice: sent %v", stream.sent)
	}
	press(t, r, "ctrl+a", "d")
	r.Define("app").Mode("normal").Bind("g", Do("test.go")).Bind("g g", Do("test.top")).Bind("x", Do("test.x"))
	press(t, r, "g", "x")
	if len(*calls) != 0 || r.Pending != "" || r.Mode.Name != "normal" {
		t.Fatalf("g x: calls %v pending %q mode %q", *calls, r.Pending, r.Mode.Name)
	}
}

// The leader is an ordinary row whose key names the terminal-leader
// setting: the framework's rows, an application's Go row and a user's
// bindings.json row naming {terminal-leader} all follow the setting, and a
// row that names ctrl+a literally stays on ctrl+a.
func TestLeaderRowsNameTheSetting(t *testing.T) {
	r, stream, _ := seqTimeoutFixture(t, "")
	calls := recorder(t, r, "mine", "lit")
	if err := r.Bind(
		Binding{Noun: "ochazuke.terminal", Mode: "leader_pending", Keys: "m", Request: Request{Noun: "test", Verb: "mine"}},
		Binding{Noun: "app", Mode: "normal", Keys: "{terminal-leader}", Request: Request{Noun: "test", Verb: "mine"}, Hint: "go {terminal-leader}"},
		Binding{Noun: "app", Mode: "normal", Keys: "ctrl+a", Request: Request{Noun: "test", Verb: "lit"}},
	); err != nil {
		t.Fatal(err)
	}
	if err := r.Settings().Set(TerminalLeaderSetting, "ctrl+b"); err != nil {
		t.Fatal(err)
	}
	if r.TerminalLeader() != "ctrl+b" {
		t.Fatalf("leader %q", r.TerminalLeader())
	}
	press(t, r, "ctrl+a")
	if !reflect.DeepEqual(stream.sent, [][]string{{"ctrl+a"}}) || r.Mode.Name != "terminal" {
		t.Fatalf("ctrl+a is input now: sent %v mode %q", stream.sent, r.Mode.Name)
	}
	press(t, r, "ctrl+b")
	if r.Mode.Name != "leader_pending" || r.Pending != "ctrl+b" {
		t.Fatalf("ctrl+b: mode %q pending %q", r.Mode.Name, r.Pending)
	}
	press(t, r, "ctrl+b")
	press(t, r, "ctrl+b", "m")
	if !reflect.DeepEqual(stream.sent, [][]string{{"ctrl+a"}, {"ctrl+b"}}) || !reflect.DeepEqual(*calls, []string{"mine"}) {
		t.Fatalf("sent %v calls %v", stream.sent, *calls)
	}
	hint := ""
	for _, b := range r.Nouns["ochazuke.terminal"].Bindings["terminal"] {
		if b.Keys == "ctrl+b" {
			hint = b.Hint
		}
	}
	if hint != "leader: d detaches, ctrl+b sends ctrl+b" {
		t.Fatalf("hint %q", hint)
	}
	press(t, r, "ctrl+b", "d")
	*calls = nil
	press(t, r, "ctrl+b", "ctrl+a")
	if !reflect.DeepEqual(*calls, []string{"mine", "lit"}) {
		t.Fatalf("app rows: calls %v", *calls)
	}
	for _, b := range r.Nouns["app"].Bindings["normal"] {
		if b.Keys == "ctrl+b" && b.Hint != "go ctrl+b" {
			t.Fatalf("app hint %q", b.Hint)
		}
	}
	if err := r.Bind(Binding{Noun: "app", Mode: "normal", Keys: "{no-such-setting}", Request: Request{Noun: "test", Verb: "mine"}}); err == nil || !strings.Contains(err.Error(), "no-such-setting") {
		t.Fatalf("unknown setting in keys: %v", err)
	}
}

// A user's bindings.json row may name the setting too.
func TestUserBindingsNameTheLeaderSetting(t *testing.T) {
	r, _, _ := terminalFixture(t)
	dir := t.TempDir()
	r.Config().Dir = dir
	writeConfig(t, dir, SettingsFile, `{"terminal-leader": "alt+x"}`)
	writeConfig(t, dir, "bindings.json", `[{"noun": "ochazuke.terminal", "mode": "terminal", "keys": "{terminal-leader}", "request": {"noun": "ochazuke.terminal", "verb": "detach"}}]`)
	m := NewModel(r)
	r.EnsureTerminalLeader()
	m.Init()
	if problems := r.Config().BindingProblems(); len(problems) != 0 {
		t.Fatalf("problems %v", problems)
	}
	press(t, r, "alt+x")
	if r.Focus != "console" {
		t.Fatalf("alt+x: focus %q mode %q", r.Focus, r.Mode.Name)
	}
}

// terminal-leader-timeout is a deprecated name of sequence-timeout for one
// release: settings.json, get, set, reset and a declaration under it reach
// sequence-timeout, each warning once.
func TestTerminalLeaderTimeoutIsADeprecatedAlias(t *testing.T) {
	r, stream, sink := seqTimeoutFixture(t, `{"terminal-leader-timeout": "20ms"}`)
	s := r.Settings()
	if d := s.Duration(SequenceTimeoutSetting); d != 20*time.Millisecond {
		t.Fatalf("sequence-timeout %v", d)
	}
	if !problemMentions(s.Problems(), "deprecated \"terminal-leader-timeout\"") {
		t.Fatalf("problems %v", s.Problems())
	}
	v, err := s.Get("terminal-leader-timeout")
	if err != nil || v.Name != SequenceTimeoutSetting || v.Value != "20ms" {
		t.Fatalf("get old name: %+v %v", v, err)
	}
	press(t, r, "ctrl+a")
	if cmd := r.BindingTimeoutCommand(); cmd == nil {
		t.Fatal("the alias's timeout is not the leader's")
	}
	press(t, r, "ctrl+a")
	if !reflect.DeepEqual(stream.sent, [][]string{{"ctrl+a"}}) {
		t.Fatalf("sent %v", stream.sent)
	}
	if err := s.Set("terminal-leader-timeout", "1s"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(r.Config().Dir, SettingsFile))
	if err != nil || !strings.Contains(string(data), `"sequence-timeout": "1s"`) || strings.Contains(string(data), "terminal-leader-timeout") {
		t.Fatalf("file %s %v", data, err)
	}
	if problemMentions(s.Problems(), "deprecated") {
		t.Fatalf("the write did not take the problem back: %v", s.Problems())
	}
	if err := s.Reset("terminal-leader-timeout"); err != nil || s.Duration(SequenceTimeoutSetting) != 0 {
		t.Fatalf("reset: %v %v", err, s.Duration(SequenceTimeoutSetting))
	}
	warnings := 0
	for _, msg := range sink.Messages() {
		if msg.Kind == UINotice && strings.Contains(msg.Text, `setting "terminal-leader-timeout" is deprecated`) {
			warnings++
		}
	}
	if warnings != 1 {
		t.Fatalf("warnings %d", warnings)
	}

	// An application declaring the old name declares sequence-timeout
	// with its default.
	r2, _, _ := seqTimeoutFixture(t, "", Setting{Name: "terminal-leader-timeout", Type: SettingDuration, Default: "3s"})
	v, err = r2.Settings().Get(SequenceTimeoutSetting)
	if err != nil || v.Default != "3s" {
		t.Fatalf("declared under the old name: %+v %v", v, err)
	}
	for _, one := range r2.Settings().Declared() {
		if one.Name == "terminal-leader-timeout" {
			t.Fatal("the old name is declared")
		}
	}
}

func problemMentions(problems []string, text string) bool {
	for _, p := range problems {
		if strings.Contains(p, text) {
			return true
		}
	}
	return false
}
