package ochazuke

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// recorder registers verb.test operations that append their verb to calls.
func recorder(t *testing.T, r *Runtime, verbs ...string) *[]string {
	t.Helper()
	var calls []string
	for _, verb := range verbs {
		r.Register(Operation{Verb: verb, Noun: "test", Run: func(_ *Runtime, _ Context, _ Arguments) (any, error) {
			calls = append(calls, verb)
			return nil, nil
		}})
	}
	return &calls
}

func TestDefineBuilderBindsVerbsActionsAndRows(t *testing.T) {
	r := NewRuntime()
	calls := recorder(t, r, "open", "close")
	b := r.Define("panel").Verbs("inspect").
		Verb("toggle", func(*Runtime, Context, Arguments) (any, error) { return "toggled", nil }).
		Action("app.quit", func(*Runtime, Context, Arguments) (any, error) { return "panel", nil })
	b.Mode("normal").Bind("o", Do("test.open")).Bind("o", Do("test.close")).Pattern(Digit, Do("test.open")).Unbind("o")
	noun := r.Noun("panel")
	if noun == nil || !reflect.DeepEqual(noun.Verbs, []string{"inspect", "toggle"}) || r.Operations["panel.toggle"].Run == nil || noun.Actions["app.quit"] == nil {
		t.Fatalf("builder did not declare verbs, operations and actions: %+v", noun)
	}
	if rows := noun.Bindings["normal"]; len(rows) != 1 || rows[0].Pattern != Digit || rows[0].Noun != "panel" || rows[0].Mode != "normal" {
		t.Fatalf("Bind must replace by key and Unbind remove: %+v", rows)
	}
	r.Instance("p", Kind("panel"))
	if err := r.SetFocus("p"); err != nil {
		t.Fatal(err)
	}
	if v, _ := r.Execute(Request{Verb: "quit", Noun: "app"}); v != "panel" || r.Quit {
		t.Fatal("noun action must override the operation while on the stack")
	}
	r.Dispatch(MacroKey{Key: "7"})
	if !reflect.DeepEqual(*calls, []string{"open"}) {
		t.Fatalf("pattern row did not run: %v", *calls)
	}
	// Define on an existing noun returns the same table.
	if r.Define("panel").Noun() != noun {
		t.Fatal("Define must return the existing noun")
	}
}

// The walk: focus outward, instance rows above noun rows at one level, the
// mode table above the any-mode table, exact above class above any.
func TestResolveWalksStackInnerToOuter(t *testing.T) {
	r := NewRuntime()
	calls := recorder(t, r, "app-normal", "app-any", "page-any", "page-normal", "row-insert", "row-instance", "any-row", "alpha-row")
	r.Instance("page")
	r.Instance("list", Parent("page"))
	r.Instance("row", Parent("list"))
	r.Define("app").Mode("normal").Bind("x", Do("test.app-normal")).Mode("").Bind("y", Do("test.app-any"))
	r.Define("page").Mode("").Bind("x", Do("test.page-any")).Mode("normal").Bind("y", Do("test.page-normal")).Pattern(Any, Do("test.any-row")).Pattern(Alpha, Do("test.alpha-row"))
	r.Define("row").Mode("insert").Bind("x", Do("test.row-insert"))
	if err := r.SetFocus("row"); err != nil {
		t.Fatal(err)
	}
	press := func(keys ...string) {
		t.Helper()
		for _, key := range keys {
			if _, err := r.Dispatch(MacroKey{Key: key}); err != nil {
				t.Fatalf("%s: %v", key, err)
			}
		}
	}
	press("x", "y", "z", "3")
	if want := []string{"page-any", "page-normal", "alpha-row", "any-row"}; !reflect.DeepEqual(*calls, want) {
		t.Fatalf("precedence: got %v; want %v", *calls, want)
	}
	// An instance row sits above its noun's rows at the same level.
	if err := r.Bind(Binding{Instance: "row", Mode: "normal", Keys: "x", Request: Request{Verb: "row-instance", Noun: "test"}}); err != nil {
		t.Fatal(err)
	}
	*calls = nil
	press("x")
	r.Mode = ModeState{Name: "insert"}
	press("x")
	if want := []string{"row-instance", "row-insert"}; !reflect.DeepEqual(*calls, want) {
		t.Fatalf("instance and mode rows: got %v; want %v", *calls, want)
	}
	listed := map[string]string{}
	for _, b := range r.ListBindings("row") {
		listed[b.Keys+b.Pattern] = b.Request.Verb
	}
	if listed["x"] != "row-insert" || listed["y"] != "app-any" || listed["any"] != "" {
		t.Fatalf("bindings.list in insert mode: %v", listed)
	}
	if got := r.Ancestry("row"); !reflect.DeepEqual(got, []string{"row", "list", "page", "app"}) {
		t.Fatalf("ancestry %v", got)
	}
	if r.Ancestry("missing") != nil {
		t.Fatal("unknown instance has no ancestry")
	}
}

// Pending modes are exclusive and one-shot; the editor noun is the fallback
// after app, so the any-mode ctrl+c row still quits from normal mode but
// nothing outside operator_pending answers while an operator waits.
func TestPendingModesAreExclusiveAndOneShot(t *testing.T) {
	r, in := editorTestRuntime(t, "one two", true)
	calls := recorder(t, r, "any")
	r.Define("app").Mode("").Bind("w", Do("test.any"))
	editorTestKeys(t, r, "d", "w")
	if string(in.Text) != "two" || len(*calls) != 0 || r.Mode.Name != "normal" {
		t.Fatalf("operator_pending leaked: text=%q calls=%v mode=%s", in.Text, *calls, r.Mode.Name)
	}
	editorTestKeys(t, r, "w")
	if len(*calls) != 1 {
		t.Fatal("any-mode row must answer in a base mode")
	}
	// A failing row still leaves the pending mode.
	editorTestKeys(t, r, "q")
	if _, err := r.Dispatch(MacroKey{Key: "tab"}); err == nil || r.Mode.Name != "normal" {
		t.Fatalf("invalid register must error and leave macro_pending: %v %s", err, r.Mode.Name)
	}
	// Counts chain through operators and g prefixes with one payload.
	editorTestKeys(t, r, "u", "2", "d", "g")
	if got := r.Mode; got.Name != "operator_g_pending" || got.Payload.Op != "d" || got.Payload.OpCount != 2 || got.Payload.Return != "normal" {
		t.Fatalf("payload chain: %+v", got)
	}
	editorTestKeys(t, r, "esc")
	if r.Mode.Name != "normal" || in.Count() != 0 {
		t.Fatalf("esc did not cancel: %s %d", r.Mode.Name, in.Count())
	}
}

// A key sequence waits in a prefix mode on the focused instance; the timer
// restores the base mode and runs the completed match.
func TestPrefixModeTimeoutRestoresBaseMode(t *testing.T) {
	r := NewRuntime()
	r.BindingTimeout = time.Hour
	calls := recorder(t, r, "g", "gg")
	r.Define("app").Mode("normal").Bind("g", Do("test.g")).Bind("g g", Do("test.gg"))
	r.Dispatch(MacroKey{Key: "g"})
	if got := r.Mode; got.Name != "normal g" || got.Payload.Prefix != "g" || got.Payload.Request == nil || r.Pending != "g" {
		t.Fatalf("prefix mode: %+v pending=%q", got, r.Pending)
	}
	// The continuations and the generated cancel row ("g esc" → mode.leave).
	if rows := r.ListBindings("app"); len(rows) != 2 || rows[0].Keys != "g esc" || rows[0].Request.Key() != "mode.leave" || rows[1].Keys != "g g" {
		t.Fatalf("bindings.list in a prefix mode lists the continuations: %+v", rows)
	}
	r.prefix.deadline = time.Now().Add(-time.Second)
	if handled, err := r.HandleBindingTimeout(BindingTimeoutMsg{runtime: r, version: r.bindingVersion}); !handled || err != nil {
		t.Fatalf("timeout: %v %v", handled, err)
	}
	if r.Mode.Name != "normal" || !reflect.DeepEqual(*calls, []string{"g"}) || r.Pending != "" {
		t.Fatalf("timeout did not restore: %s %v", r.Mode.Name, *calls)
	}
}

// Recording is a shadow mode on the app noun: keys are appended and then
// dispatched in the shadowed mode; q stops only from a quiet normal mode.
func TestMacroRecordingShadowMode(t *testing.T) {
	r, in := editorTestRuntime(t, "", true)
	editorTestKeys(t, r, "q", "a")
	if r.Registers().Macro.Recording != "a" || r.Mode.Name != "normal" {
		t.Fatalf("recording did not start: %+v", r.Registers().Macro)
	}
	editorTestKeys(t, r, "i", "q", "esc", "2", "q")
	if string(in.Text) != "q" || r.Registers().Macro.Recording != "a" {
		t.Fatalf("q in insert or count_pending must record and re-dispatch: text=%q %+v", in.Text, r.Registers().Macro)
	}
	editorTestKeys(t, r, "esc", "q")
	want := []MacroKey{{Key: "i"}, {Key: "q"}, {Key: "esc"}, {Key: "2"}, {Key: "q"}, {Key: "esc"}}
	if r.Registers().Macro.Recording != "" || !reflect.DeepEqual(r.Registers().Named["a"].Keys, want) {
		t.Fatalf("recorded %+v; want %+v", r.Registers().Named["a"].Keys, want)
	}
	editorTestKeys(t, r, "@", "a")
	if string(in.Text) != "qq" {
		t.Fatalf("replay (not re-recorded): %q", in.Text)
	}
}

// leader_pending has the same shape in rows alone: an Enter row with the
// leader as its prefix, and an any row that sends the prefix and the key
// (Join) and returns to the shadowed mode because the mode is one-shot. The
// terminal noun binds exactly this; here it is the mechanism on a bare noun.
func TestLeaderPendingShadowShape(t *testing.T) {
	r := NewRuntime()
	var forwarded []string
	calls := recorder(t, r, "new-window")
	r.Register(Operation{Verb: "forward", Noun: "pane", Parameters: map[string]Parameter{"keys": {Type: "string", Required: true}}, Run: func(_ *Runtime, _ Context, a Arguments) (any, error) {
		forwarded = append(forwarded, StringArg(a, "keys", ""))
		return nil, nil
	}})
	alone := Request{Verb: "forward", Noun: "pane", Args: Arguments{"keys": "ctrl+b"}}
	r.Define("term").Mode("normal").
		Bind("ctrl+b", Enter("leader_pending", Payload{Prefix: "ctrl+b", Request: &alone})).
		Pattern(Any, Do("pane.forward").FromKey("keys")).
		Mode("leader_pending").
		Bind("c", Do("test.new-window")).
		Pattern(Any, Do("pane.forward").Join("keys", SourcePrefix, SourceKey))
	r.Instance("pane", Kind("term"))
	if err := r.SetFocus("pane"); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"x", "ctrl+b", "c", "ctrl+b", "z", "y"} {
		if _, err := r.Dispatch(MacroKey{Key: key}); err != nil {
			t.Fatalf("%s: %v", key, err)
		}
	}
	if !reflect.DeepEqual(forwarded, []string{"x", "ctrl+b z", "y"}) || !reflect.DeepEqual(*calls, []string{"new-window"}) || r.Mode.Name != "normal" {
		t.Fatalf("leader: forwarded=%v calls=%v mode=%s", forwarded, *calls, r.Mode.Name)
	}
	// The Enter row armed the sequence timer; expiry forwards the leader alone.
	if _, err := r.Dispatch(MacroKey{Key: "ctrl+b"}); err != nil || r.Pending != "ctrl+b" || r.Mode.Payload.Prefix != "ctrl+b" {
		t.Fatalf("leader alone: err=%v pending=%q payload=%+v", err, r.Pending, r.Mode.Payload)
	}
	r.prefix.deadline = time.Now().Add(-time.Second)
	if handled, err := r.HandleBindingTimeout(BindingTimeoutMsg{runtime: r, version: r.bindingVersion}); !handled || err != nil {
		t.Fatalf("timer: handled=%v err=%v", handled, err)
	}
	if !reflect.DeepEqual(forwarded[3:], []string{"ctrl+b"}) || r.Mode.Name != "normal" || r.Pending != "" {
		t.Fatalf("timed-out leader: forwarded=%v mode=%s pending=%q", forwarded, r.Mode.Name, r.Pending)
	}
}

// Rows from a file replace, add and unbind defaults on nouns and instances.
func TestLoadBindingsRowsReplaceAddAndUnbind(t *testing.T) {
	r, in := editorTestRuntime(t, "abc", false)
	calls := recorder(t, r, "hello", "digit")
	rows, err := r.LoadBindings([]byte(`[
		{"noun": "app", "mode": "normal", "keys": "ctrl+t", "request": {"verb": "hello", "noun": "test"}},
		{"noun": "editor", "mode": "normal", "keys": "x", "request": null},
		{"noun": "editor", "mode": "insert", "keys": "esc", "request": {"verb": "hello", "noun": "test"}},
		{"instance": "editor", "mode": "normal", "pattern": "digit-0", "request": {"verb": "digit", "noun": "test"}}
	]`))
	if err != nil || len(rows) != 4 {
		t.Fatal(err)
	}
	editorTestKeys(t, r, "ctrl+t", "x", "5", "0", "i", "esc")
	if !reflect.DeepEqual(*calls, []string{"hello", "digit", "hello"}) || string(in.Text) != "abc" || in.Mode != "insert" {
		t.Fatalf("rows: calls=%v text=%q mode=%s", *calls, in.Text, in.Mode)
	}
	for _, bad := range []string{
		`[{"noun": "nowhere", "keys": "x", "request": {"verb": "quit", "noun": "app"}}]`,
		`[{"noun": "app", "keys": "x", "request": {"verb": "nope", "noun": "app"}}]`,
		`[{"noun": "app", "pattern": "letters", "request": {"verb": "quit", "noun": "app"}}]`,
		`[{"noun": "app", "keys": "x", "mode": "unknown", "request": {"verb": "quit", "noun": "app"}}]`,
		`[{"noun": "app", "keys": "x", "request": {"verb": "move", "noun": "input"}}]`,
		`[{"noun": "app", "keys": "x", "request": {"verb": "move", "noun": "input", "args": {"motion": "left"}}, "from": {"steps": "nowhere"}}]`,
	} {
		if _, err := r.LoadBindings([]byte(bad)); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}

// Acceptance: every Readline and Vim key the editor answers to appears in
// bindings.list for its mode on a plain focused input (rows of a guarded
// verb answer only where the guard holds).
func TestListBindingsListsEveryEditorKey(t *testing.T) {
	r, in := editorTestRuntime(t, "abc", true)
	editor := r.Noun("editor")
	for mode, rows := range editor.Bindings {
		want := map[string]bool{}
		for _, b := range rows {
			if r.VerbGuard(b.Request.Key()) != "" {
				continue // a guarded verb answers only where its guard holds (tab opens completions)
			}
			want[b.Keys+"\x00"+b.Pattern] = true
		}
		if r.pendingModes[mode] {
			r.enterMode(mode, Payload{Op: "d"})
		} else {
			in.storeMode(mode)
		}
		got := map[string]bool{}
		for _, b := range r.ListBindings(in.ID) {
			got[b.Keys+"\x00"+b.Pattern] = true
		}
		for key := range want {
			if !got[key] {
				t.Fatalf("mode %s: %q missing from bindings.list", mode, strings.ReplaceAll(key, "\x00", "/"))
			}
		}
		in.storeMode("normal")
	}
	in.storeMode("insert")
	for _, key := range []string{"ctrl+a", "ctrl+w", "alt+f", "<printable>", "<paste>"} {
		if !slices.ContainsFunc(r.ListBindings(in.ID), func(b Binding) bool { return b.Keys == key || "<"+b.Pattern+">" == key }) {
			t.Fatalf("readline key %q not listed", key)
		}
	}
}

// Rows are instances of their nouns, any instance is a target, and the mode
// lives on instances.
func TestListRowsAreInstancesOfTheirNouns(t *testing.T) {
	r, list, input, items := listFixture(t)
	r.Register(Operation{Verb: "inspect", Noun: "item", Run: func(*Runtime, Context, Arguments) (any, error) { return "application", nil }})
	r.Define("a").Action("item.inspect", func(*Runtime, Context, Arguments) (any, error) { return "noun a", nil }).
		Mode("normal").Bind("x", Do("app.quit"))
	for _, item := range items {
		inst := r.Instances[item.ID]
		if inst == nil || inst.Noun != item.Noun || inst.Parent != "sidebar" {
			t.Fatalf("row %s is not an instance of its noun: %+v", item.ID, inst)
		}
	}
	if err := list.Focus(); err != nil {
		t.Fatal(err)
	}
	if got := r.Context().Path; !reflect.DeepEqual(got, []string{"app", "page", "sidebar", "one"}) {
		t.Fatalf("row path %v", got)
	}
	if value, _ := r.Execute(Request{Verb: "inspect", Noun: "item"}); value != "noun a" {
		t.Fatalf("row action must come from its noun: %v", value)
	}
	keys := func(id string) []string {
		var out []string
		for _, b := range r.ListBindings(id) {
			if b.Request.Noun == "app" {
				out = append(out, b.Keys)
			}
		}
		return out
	}
	if got := keys("one"); !slices.Contains(got, "x") {
		t.Fatalf("row bindings must come from its noun: %v", got)
	}
	// A per-instance override sits above the noun's rows.
	if err := r.Bind(Binding{Instance: "one", Mode: "normal", Keys: "x", Request: Request{Verb: "ignore", Noun: "key"}}); err != nil {
		t.Fatal(err)
	}
	for _, b := range r.ListBindings("one") {
		if b.Keys == "x" && b.Request.Noun != "key" {
			t.Fatalf("instance rows must beat the noun's: %+v", b)
		}
	}
	if err := list.SetItems(items[:2]); err != nil {
		t.Fatal(err)
	}
	if r.Instances["three"] != nil || r.Instances["one"].Bindings == nil {
		t.Fatal("SetItems removes dropped rows and keeps retained instances")
	}
	// Any instance is a target: row, list, page and input share one addressing.
	for target, want := range map[string]Context{
		"two":     {Path: []string{"app", "page", "sidebar", "two"}},
		"sidebar": {Path: []string{"app", "page", "sidebar"}},
		"page":    {Path: []string{"app", "page"}},
		"note":    {Path: []string{"app", "page", "editor"}, InputID: "note"},
	} {
		var got Context
		r.Register(Operation{Verb: "where-" + target, Noun: "test", Run: func(_ *Runtime, c Context, _ Arguments) (any, error) { got = c; return nil, nil }})
		if _, err := r.Execute(Request{Verb: "where-" + target, Noun: "test", Target: target}); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("target %s: %+v; want %+v", target, got, want)
		}
	}
	if _, err := r.Execute(Request{Verb: "quit", Noun: "app", Target: "nowhere"}); err == nil {
		t.Fatal("unknown target accepted")
	}
	if _, err := r.Execute(Request{Verb: "select", Noun: "context", Args: Arguments{"instance": "two"}}); err != nil || r.Context().Path[3] != "two" {
		t.Fatalf("context.select instance: %v %+v", err, r.Context())
	}
	if err := r.SetFocus(input.ID); err != nil || r.Context().InputID != "note" {
		t.Fatalf("Focus input: %v", err)
	}
	if err := r.SetFocus("sidebar"); err != nil || r.Context().InputID != "" || r.Context().Path[2] != "sidebar" {
		t.Fatalf("Focus list: %v %+v", err, r.Context())
	}
	snapshot := r.Snapshot()
	if noun, _ := JSONPointer(snapshot, "/lists/sidebar/items/1/noun"); noun != "b" {
		t.Fatalf("list rows show their noun: %v", noun)
	}
	if noun, _ := JSONPointer(snapshot, "/instances/two/noun"); noun != "b" {
		t.Fatalf("/instances/<id>/noun: %v", noun)
	}
	if state, err := JSONPointer(snapshot, "/instances/two/state"); err == nil {
		t.Fatalf("an instance holds no mode: /instances/<id>/state = %v", state)
	}
}

// Multi-selection fan-out: a verb over marked rows runs <row noun>.verb per
// row, skipping and reporting rows whose noun lacks the verb.
func TestMultiSelectionFanOut(t *testing.T) {
	r, list, _, _ := listFixture(t)
	var opened []string
	r.Define("a").Verb("open", func(_ *Runtime, c Context, _ Arguments) (any, error) {
		opened = append(opened, focusOf(c))
		return nil, nil
	})
	r.Define("b").Verb("open", func(_ *Runtime, c Context, _ Arguments) (any, error) {
		opened = append(opened, focusOf(c))
		return nil, nil
	})
	if err := list.Focus(); err != nil {
		t.Fatal(err)
	}
	editorTestKeys(t, r, "space", "j", "space", "j", "space")
	result, err := r.Execute(Request{Verb: "apply", Noun: "list", Args: Arguments{"verb": "open"}})
	if err != nil {
		t.Fatal(err)
	}
	report := result.(map[string]any)
	if !reflect.DeepEqual(opened, []string{"one", "two"}) || !reflect.DeepEqual(report["applied"], []string{"one", "two"}) || !reflect.DeepEqual(report["skipped"], []string{"three: c.open is not registered"}) {
		t.Fatalf("fan-out: opened=%v report=%v", opened, report)
	}
	editorTestKeys(t, r, "space")
	if marked := list.Marked(); len(marked) != 2 || marked[1].ID != "two" {
		t.Fatalf("toggle mark: %v", marked)
	}
}

func TestModeIsGlobal(t *testing.T) {
	r, list, input, _ := listFixture(t)
	if err := r.DefineMode("review", false); err != nil {
		t.Fatal(err)
	}
	if err := list.Focus(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(Request{Verb: "set", Noun: "mode", Args: Arguments{"mode": "review"}}); err != nil {
		t.Fatal(err)
	}
	if r.Mode.Name != "review" {
		t.Fatalf("mode.set must write Runtime.Mode: %+v", r.Mode)
	}
	// Another row of the same list is the same focus stop: the mode stays.
	list.Move(1)
	if r.Mode.Name != "review" {
		t.Fatalf("a move inside one stop must keep the mode: %+v", r.Mode)
	}
	// A pending mode returns to the base mode it shadowed.
	if _, err := r.Execute(Request{Verb: "enter", Noun: "mode", Args: Arguments{"mode": "macro_pending"}}); err != nil {
		t.Fatal(err)
	}
	if r.Mode.Name != "macro_pending" || r.Mode.Payload.Return != "review" {
		t.Fatalf("mode %+v", r.Mode)
	}
	editorTestKeys(t, r, "esc")
	if r.Mode.Name != "review" {
		t.Fatalf("leave must restore the base mode: %+v", r.Mode)
	}
	// Focus moving to another stop resets the mode to normal; closing the
	// modal restores the interaction state beneath it.
	if err := r.Push("dialog", "dialog"); err != nil {
		t.Fatal(err)
	}
	if r.Mode.Name != "normal" {
		t.Fatalf("a modal on another stop starts in normal: %+v", r.Mode)
	}
	if err := r.Pop(); err != nil {
		t.Fatal(err)
	}
	if r.Mode.Name != "review" {
		t.Fatalf("pop restores the mode the modal opened over: %+v", r.Mode)
	}
	// A pinned noun sets its mode on focus.
	if err := r.SetFocus(input.ID); err != nil {
		t.Fatal(err)
	}
	if r.Mode.Name != "insert" || input.Mode != "insert" {
		t.Fatalf("prompt focus must pin insert: %+v %q", r.Mode, input.Mode)
	}
	other := NewInput("other-input", "ab", false)
	if err := r.AddInput(other, Parent("editor")); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFocus("other-input"); err != nil {
		t.Fatal(err)
	}
	if r.Mode.Name != "normal" {
		t.Fatalf("an unpinned input starts in normal: %+v", r.Mode)
	}
	// While focused, the input's editing mode is the global mode.
	for _, keys := range [][]string{{"i"}, {"esc"}, {"v"}, {"esc", "i", "x", "esc", "u"}} {
		editorTestKeys(t, r, keys...)
		if r.Mode.Name != other.Mode {
			t.Fatalf("after %v: mode %q, input %q", keys, r.Mode.Name, other.Mode)
		}
	}
}

// A count typed before a key sequence reaches the row the sequence runs:
// 3gg is line 3 (the prefix mode carries Count and OpCount).
func TestSequencePrefixCarriesCount(t *testing.T) {
	r := NewRuntime()
	r.BindingTimeout = time.Hour
	var lines []int
	r.Define("test").Verb("line", func(_ *Runtime, _ Context, a Arguments) (any, error) {
		lines = append(lines, IntArg(a, "line", -1))
		return nil, nil
	})
	op := r.Operations["test.line"]
	op.Parameters = map[string]Parameter{"line": {Type: "integer"}}
	r.Operations["test.line"] = op
	row := Do("test.line").FromPayload("line", SourceLine)
	r.Define("app").Mode("normal").Bind("g g", row).Mode("count_pending").Bind("g g", row)
	for _, key := range []string{"3", "g", "g", "g", "g"} {
		if _, err := r.Dispatch(MacroKey{Key: key}); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(lines, []int{3, 0}) || r.Mode.Name != "normal" {
		t.Fatalf("lines %v mode %+v", lines, r.Mode)
	}
}

// A row that matched at an outer level addresses that level's instance
// when the operation is the level's own, so a confirm names it.
func TestRowAddressesItsLevelInstance(t *testing.T) {
	r := NewRuntime()
	var targets []string
	r.Define("row").Verb("drop", func(_ *Runtime, c Context, _ Arguments) (any, error) {
		targets = append(targets, focusOf(c))
		return nil, nil
	}).Confirm("Drop {{id}}?").Mode("normal").Bind("d d", Do("row.drop")).Bind("m", Do("mode.set", Arguments{"mode": "insert"}))
	r.Instance("r1", Kind("row"))
	r.Instance("r1:c0", Parent("r1"), Kind("cell"))
	if err := r.SetFocus("r1:c0"); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"d", "d"} {
		if _, err := r.Dispatch(MacroKey{Key: key}); err != nil {
			t.Fatal(err)
		}
	}
	if inst := r.Instances["confirm"]; inst == nil || inst.Payload.Prompt != "Drop r1?" {
		t.Fatalf("confirm %+v", inst)
	}
	if _, err := r.Dispatch(MacroKey{Key: "y"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(targets, []string{"r1"}) || r.Focus != "r1:c0" {
		t.Fatalf("targets %v focus %q", targets, r.Focus)
	}
	// A shared operation bound on the level still addresses the focus.
	if _, err := r.Dispatch(MacroKey{Key: "m"}); err != nil || r.Mode.Name != "insert" {
		t.Fatalf("mode.set from the row level: %v %+v", err, r.Mode)
	}
}

// Extends on Define: a mode reads its own rows, then its parent's, including
// rows bound on the parent later; a row of its own wins.
func TestDefineExtendsInheritsTables(t *testing.T) {
	r := NewRuntime()
	calls := recorder(t, r, "base", "own", "late")
	if err := r.DefineMode("visual-row", false); err != nil {
		t.Fatal(err)
	}
	r.Define("app").Mode("visual-block").Bind("h", Do("test.base")).Bind("x", Do("test.base"))
	r.Define("app").Mode("visual-row").Extends("visual-block").Bind("x", Do("test.own"))
	r.Define("app").Mode("visual-block").Bind("l", Do("test.late"))
	r.Mode = ModeState{Name: "visual-row"}
	for _, key := range []string{"h", "x", "l"} {
		if _, err := r.Dispatch(MacroKey{Key: key}); err != nil {
			t.Fatal(err)
		}
	}
	if want := []string{"base", "own", "late"}; !reflect.DeepEqual(*calls, want) {
		t.Fatalf("calls %v, want %v", *calls, want)
	}
	if n := r.Noun("app"); n.Extends["visual-row"] != "visual-block" {
		t.Fatalf("extends %v", n.Extends)
	}
}

// In code the guard is part of the identity too: Bind then When places the
// row under its guard (the unguarded row it first replaced comes back),
// rows on the same keys with different guards coexist, and Unbind and
// UnbindGuarded each remove one.
func TestBuilderRowIdentityIncludesTheGuard(t *testing.T) {
	r := NewRuntime()
	recorder(t, r, "alpha", "beta", "gamma")
	r.DefineGuard("flag", func(*Runtime, KeyEvent) bool { return true })
	b := r.Define("widget").Mode("normal")
	rows := func() []string {
		var out []string
		for _, row := range r.Nouns["widget"].Bindings["normal"] {
			out = append(out, row.Keys+"/"+row.When+"/"+row.Request.Key())
		}
		return out
	}
	b.Bind("a", Do("test.beta")).Bind("a", Do("test.alpha")).When("flag")
	if got := rows(); !reflect.DeepEqual(got, []string{"a/flag/test.alpha", "a//test.beta"}) {
		t.Fatalf("Bind then When: %v", got)
	}
	b.Bind("a", Do("test.gamma").Guard("flag"))
	if got := rows(); !reflect.DeepEqual(got, []string{"a/flag/test.gamma", "a//test.beta"}) {
		t.Fatalf("a guarded row replaces the guarded row: %v", got)
	}
	b.Bind("a", Do("test.alpha"))
	if got := rows(); !reflect.DeepEqual(got, []string{"a/flag/test.gamma", "a//test.alpha"}) {
		t.Fatalf("an unguarded row replaces the unguarded row: %v", got)
	}
	b.UnbindGuarded("a", "flag")
	if got := rows(); !reflect.DeepEqual(got, []string{"a//test.alpha"}) {
		t.Fatalf("UnbindGuarded: %v", got)
	}
	b.Bind("a", Do("test.gamma")).When("flag").Unbind("a")
	if got := rows(); !reflect.DeepEqual(got, []string{"a/flag/test.gamma"}) {
		t.Fatalf("Unbind: %v", got)
	}
}
