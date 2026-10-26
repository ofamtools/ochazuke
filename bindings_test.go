package ochazuke

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/unixsuperhero/ochazuke/internal/keytest"
)

func timedBindings(t *testing.T, keys ...string) (*Runtime, *[]string) {
	t.Helper()
	r := NewRuntime()
	r.BindingTimeout = time.Hour
	var calls []string
	for _, key := range keys {
		r.Register(Operation{Verb: key, Noun: "test", Run: func(_ *Runtime, _ Context, _ Arguments) (any, error) {
			calls = append(calls, key)
			return nil, nil
		}})
		r.Define("app").Mode("").Bind(key, Do("test."+key))
	}
	return r, &calls
}

func bindingKey(t *testing.T, r *Runtime, key string) bool {
	t.Helper()
	handled, err := r.HandleBinding(key)
	if err != nil {
		t.Fatal(err)
	}
	return handled
}

func expireBinding(t *testing.T, r *Runtime) {
	t.Helper()
	r.prefix.deadline = time.Now().Add(-time.Second)
	handled, err := r.HandleBindingTimeout(BindingTimeoutMsg{runtime: r, version: r.bindingVersion})
	if !handled || err != nil {
		t.Fatalf("timeout handled=%v err=%v", handled, err)
	}
}

func TestAmbiguousBindingsChooseLongestCompletedMatch(t *testing.T) {
	for _, tc := range []struct {
		name    string
		keys    []string
		timeout bool
		want    string
	}{
		{"single timeout", []string{"g"}, true, "g"},
		{"double timeout", []string{"g", "g"}, true, "g g"},
		{"triple complete", []string{"g", "g", "g"}, false, "g g g"},
		{"single mismatch runs the match then x", []string{"g", "x"}, false, "g x"},
		{"double mismatch runs the match then x", []string{"g", "g", "x"}, false, "g g x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, calls := timedBindings(t, "g", "g g", "g g g", "x")
			for i, key := range tc.keys {
				if !bindingKey(t, r, key) {
					t.Fatalf("unhandled %q", key)
				}
				if i < len(tc.keys)-1 && len(*calls) != 0 {
					t.Fatalf("executed too early: %v", *calls)
				}
			}
			if tc.timeout {
				if len(*calls) != 0 {
					t.Fatalf("did not defer: %v", *calls)
				}
				expireBinding(t, r)
			}
			want := []string{tc.want}
			if tc.want == "g x" || tc.want == "g g x" {
				want = []string{strings.TrimSuffix(tc.want, " x"), "x"}
			}
			if !reflect.DeepEqual(*calls, want) || r.Pending != "" {
				t.Fatalf("calls=%v pending=%q", *calls, r.Pending)
			}
		})
	}
}

func TestBindingPrefixRetainsEarlierFallback(t *testing.T) {
	r, calls := timedBindings(t, "g", "g g g")
	bindingKey(t, r, "g")
	bindingKey(t, r, "g")
	expireBinding(t, r)
	if !reflect.DeepEqual(*calls, []string{"g"}) {
		t.Fatalf("calls=%v", *calls)
	}
}

func TestBindingTimeoutDoesNotReplayResolvedOrCancelledAction(t *testing.T) {
	r, calls := timedBindings(t, "g", "g g", "g g g")
	bindingKey(t, r, "g")
	old := BindingTimeoutMsg{runtime: r, version: r.bindingVersion}
	bindingKey(t, r, "g")
	if handled, err := r.HandleBindingTimeout(old); handled || err != nil {
		t.Fatal("old timer resolved new prefix")
	}
	if r.Pending != "g g" || len(*calls) != 0 {
		t.Fatalf("pending=%q calls=%v", r.Pending, *calls)
	}
	current := BindingTimeoutMsg{runtime: r, version: r.bindingVersion}
	bindingKey(t, r, "esc")
	if handled, _ := r.HandleBindingTimeout(current); handled {
		t.Fatal("cancelled timer executed")
	}
	bindingKey(t, r, "g")
	bindingKey(t, r, "g")
	current = BindingTimeoutMsg{runtime: r, version: r.bindingVersion}
	bindingKey(t, r, "g")
	if handled, _ := r.HandleBindingTimeout(current); handled {
		t.Fatal("resolved timer executed")
	}
	if !reflect.DeepEqual(*calls, []string{"g g g"}) {
		t.Fatalf("calls=%v", *calls)
	}
}

func TestPureBindingPrefixesExpireOrRetryWithoutFallback(t *testing.T) {
	r, calls := timedBindings(t, "g g", "x")
	bindingKey(t, r, "g")
	expireBinding(t, r)
	if r.Pending != "" || len(*calls) != 0 {
		t.Fatalf("pending=%q calls=%v", r.Pending, *calls)
	}
	bindingKey(t, r, "g")
	bindingKey(t, r, "x")
	if !reflect.DeepEqual(*calls, []string{"x"}) {
		t.Fatalf("calls=%v", *calls)
	}
}

func TestLateKeyStartsFreshAfterExpiredFallback(t *testing.T) {
	r, calls := timedBindings(t, "g", "g g", "x")
	bindingKey(t, r, "g")
	r.prefix.deadline = time.Now().Add(-time.Second)
	bindingKey(t, r, "x")
	if !reflect.DeepEqual(*calls, []string{"g", "x"}) {
		t.Fatalf("calls=%v", *calls)
	}
}

func TestContextChangesCancelDeferredBindings(t *testing.T) {
	for _, change := range []string{"frame", "mode"} {
		t.Run(change, func(t *testing.T) {
			r, calls := timedBindings(t, "g", "g g")
			bindingKey(t, r, "g")
			msg := BindingTimeoutMsg{runtime: r, version: r.bindingVersion}
			if change == "frame" {
				if err := r.Push("modal", "app"); err != nil {
					t.Fatal(err)
				}
			} else {
				r.Mode = ModeState{Name: "insert"}
			}
			r.prefix.deadline = time.Now().Add(-time.Second)
			r.HandleBindingTimeout(msg)
			if r.Pending != "" || len(*calls) != 0 {
				t.Fatalf("pending=%q calls=%v", r.Pending, *calls)
			}
		})
	}
}

func TestModelExecutesTimedBindingOnEventLoop(t *testing.T) {
	r, calls := timedBindings(t, "g", "g g")
	r.BindingTimeout = time.Millisecond
	m := NewModel(r)
	cmd := keytest.Type(m, "g")
	if cmd == nil || len(*calls) != 0 {
		t.Fatalf("missing timer or premature action: %v", *calls)
	}
	msg := cmd()
	if len(*calls) != 0 {
		t.Fatal("timer executed action outside event loop")
	}
	m.Update(msg)
	if !reflect.DeepEqual(*calls, []string{"g"}) || r.Pending != "" {
		t.Fatalf("calls=%v pending=%q", *calls, r.Pending)
	}
}

// A row that names an operation by the pre-noun.verb spelling still binds
// for one release: it is rewritten to the registered key and reported by
// ValidateBindings, through the builder, Bind and the JSON loader alike.
func TestOldVerbNounSpellingIsRewrittenOnce(t *testing.T) {
	r := NewRuntime()
	r.Define("app").Mode("normal").Bind("u", Do("undo.input"))
	if err := r.Bind(Binding{Noun: "app", Mode: "normal", Keys: "ctrl+r", Request: Request{Verb: "input", Noun: "redo"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.LoadBindings([]byte(`[{"noun":"app","mode":"normal","keys":"x","request":{"verb":"input","noun":"delete"}}]`)); err != nil {
		t.Fatal(err)
	}
	keys := map[string]string{}
	for _, row := range r.Noun("app").Bindings["normal"] {
		if row.Keys == "u" || row.Keys == "ctrl+r" || row.Keys == "x" {
			keys[row.Keys] = row.Request.Key()
		}
	}
	want := map[string]string{"u": "input.undo", "ctrl+r": "input.redo", "x": "input.delete"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("rows = %v, want %v", keys, want)
	}
	warnings, err := r.ValidateBindings()
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 3 || !strings.Contains(warnings[0], `"undo.input"`) || !strings.Contains(warnings[0], `"input.undo"`) {
		t.Fatalf("warnings = %q", warnings)
	}
	if err := r.Bind(Binding{Noun: "app", Keys: "q", Request: Request{Verb: "nothing", Noun: "input"}}); err == nil || !strings.Contains(err.Error(), `unknown operation "input.nothing"`) {
		t.Fatalf("unknown operation error = %v", err)
	}
}

// FromRow passes the acted-on row's Data as arguments: a map key, a
// struct's JSON field, a nested field, the whole value (JSON-shaped, an
// object argument); a row bound on the
// list reads the focused row; a missing field leaves the argument out.
// ValidateBindings accepts row and row:<field> and rejects a bad field.
func TestFromRowPassesRowData(t *testing.T) {
	type tree struct {
		Path string `json:"path"`
		Repo struct {
			Name string `json:"name"`
		} `json:"repo"`
	}
	r := NewRuntime()
	var got []Arguments
	record := func(c Ctx) Result { got = append(got, c.Args); return Done(nil) }
	r.Define("worktree").Verb("open", record)
	op := r.Operations["worktree.open"]
	op.Parameters = map[string]Parameter{"path": {Type: "string"}, "repo": {Type: "string"}, "row": {Type: "object"}}
	r.Operations["worktree.open"] = op
	onList := Do("worktree.open").FromRow("path", "path").FromRow("row", "")
	onList.Noun, onList.Mode, onList.Keys = "list", "normal", "O"
	if err := r.Bind(
		Binding{Noun: "worktree", Mode: "normal", Keys: "o", Request: Do("worktree.open").Request, From: map[string]string{"path": "row:path", "repo": "row:repo.name"}},
		onList,
	); err != nil {
		t.Fatal(err)
	}
	r.Instance("page", Kind("page"))
	var st tree
	st.Path, st.Repo.Name = "/src/b", "beta"
	list, err := r.AddList("trees", []ListItem{
		{ID: "a", Label: "a", Noun: "worktree", Data: map[string]any{"path": "/src/a", "repo": map[string]any{"name": "alpha"}}},
		{ID: "b", Label: "b", Noun: "worktree", Data: st},
		{ID: "c", Label: "c", Noun: "worktree"},
	}, Parent("page"))
	if err != nil {
		t.Fatal(err)
	}
	if err := list.Focus(); err != nil {
		t.Fatal(err)
	}
	press := func(key string) {
		t.Helper()
		if _, err := r.Dispatch(MacroKey{Key: key}); err != nil {
			t.Fatal(err)
		}
	}
	press("o")
	list.Move(1)
	press("o")
	press("O")
	list.Move(1)
	press("o")
	want := []Arguments{
		{"path": "/src/a", "repo": "alpha"},
		{"path": "/src/b", "repo": "beta"},
		{"path": "/src/b", "row": map[string]any{"path": "/src/b", "repo": map[string]any{"name": "beta"}}},
		{},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("arguments\n got %#v\nwant %#v", got, want)
	}
	if _, err := r.LoadBindings([]byte(`[{"noun":"worktree","mode":"normal","keys":"P","request":{"noun":"worktree","verb":"open"},"from":{"path":"row:path"}}]`)); err != nil {
		t.Fatal(err)
	}
	list.Move(-2)
	press("P")
	if last := got[len(got)-1]; !reflect.DeepEqual(last, Arguments{"path": "/src/a"}) {
		t.Fatalf("binding file row: %#v", last)
	}
	if _, err := r.ValidateBindings(); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"row:", "row:a..b", "rows:path"} {
		err := r.Bind(Binding{Noun: "worktree", Mode: "normal", Keys: "p", Request: Do("worktree.open").Request, From: map[string]string{"path": source}})
		if err == nil || !strings.Contains(err.Error(), "unknown argument source") {
			t.Fatalf("source %q: %v", source, err)
		}
	}
}
