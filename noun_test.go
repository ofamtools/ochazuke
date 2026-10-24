package ochazuke

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"ochazuke/internal/keytest"
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
		Action("quit.app", func(*Runtime, Context, Arguments) (any, error) { return "panel", nil })
	b.Mode("normal").Bind("o", Do("open.test")).Bind("o", Do("close.test")).Pattern(Digit, Do("open.test")).Unbind("o")
	noun := r.Noun("panel")
	if noun == nil || !reflect.DeepEqual(noun.Verbs, []string{"inspect", "toggle"}) || r.Operations["toggle.panel"].Run == nil || noun.Actions["quit.app"] == nil {
		t.Fatalf("builder did not declare verbs, operations and actions: %+v", noun)
	}
	if rows := noun.Bindings["normal"]; len(rows) != 1 || rows[0].Pattern != Digit || rows[0].Noun != "panel" || rows[0].Mode != "normal" {
		t.Fatalf("Bind must replace by key and Unbind remove: %+v", rows)
	}
	r.Instance("p", Kind("panel"))
	if err := r.Focus("p"); err != nil {
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
	r.Define("app").Mode("normal").Bind("x", Do("app-normal.test")).Mode("").Bind("y", Do("app-any.test"))
	r.Define("page").Mode("").Bind("x", Do("page-any.test")).Mode("normal").Bind("y", Do("page-normal.test")).Pattern(Any, Do("any-row.test")).Pattern(Alpha, Do("alpha-row.test"))
	r.Define("row").Mode("insert").Bind("x", Do("row-insert.test"))
	if err := r.Focus("row"); err != nil {
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
	r.Instances["row"].State = "insert"
	press("x")
	if want := []string{"row-instance", "row-insert"}; !reflect.DeepEqual(*calls, want) {
		t.Fatalf("instance and mode rows: got %v; want %v", *calls, want)
	}
	listed := map[string]string{}
	for _, b := range r.ListBindings("row") {
		listed[b.Keys+b.Pattern] = b.Request.Verb
	}
	if listed["x"] != "row-insert" || listed["y"] != "app-any" || listed["any"] != "" {
		t.Fatalf("list.bindings in insert mode: %v", listed)
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
	r.Define("app").Mode("").Bind("w", Do("any.test"))
	editorTestKeys(t, r, "d", "w")
	if string(in.Text) != "two" || len(*calls) != 0 || r.Mode(r.Context()) != "normal" {
		t.Fatalf("operator_pending leaked: text=%q calls=%v mode=%s", in.Text, *calls, r.Mode(r.Context()))
	}
	editorTestKeys(t, r, "w")
	if len(*calls) != 1 {
		t.Fatal("any-mode row must answer in a base mode")
	}
	// A failing row still leaves the pending mode.
	editorTestKeys(t, r, "q")
	if _, err := r.Dispatch(MacroKey{Key: "tab"}); err == nil || r.Mode(r.Context()) != "normal" {
		t.Fatalf("invalid register must error and leave macro_pending: %v %s", err, r.Mode(r.Context()))
	}
	// Counts chain through operators and g prefixes with one payload.
	editorTestKeys(t, r, "u", "2", "d", "g")
	if got := r.modeState(in.ID); got.Name != "operator_g_pending" || got.Payload.Op != "d" || got.Payload.OpCount != 2 || got.Payload.Return != "normal" {
		t.Fatalf("payload chain: %+v", got)
	}
	editorTestKeys(t, r, "esc")
	if r.Mode(r.Context()) != "normal" || in.Count() != 0 {
		t.Fatalf("esc did not cancel: %s %d", r.Mode(r.Context()), in.Count())
	}
}

// A key sequence waits in a prefix mode on the focused instance; the timer
// restores the base mode and runs the completed match.
func TestPrefixModeTimeoutRestoresBaseMode(t *testing.T) {
	r := NewRuntime()
	r.BindingTimeout = time.Hour
	calls := recorder(t, r, "g", "gg")
	r.Define("app").Mode("normal").Bind("g", Do("g.test")).Bind("g g", Do("gg.test"))
	r.Instances["app"].State = "normal"
	r.Dispatch(MacroKey{Key: "g"})
	if got := r.modeState("app"); got.Name != "normal g" || got.Payload.Prefix != "g" || got.Payload.Request == nil || r.Pending != "g" {
		t.Fatalf("prefix mode: %+v pending=%q", got, r.Pending)
	}
	if rows := r.ListBindings("app"); len(rows) != 1 || rows[0].Keys != "g g" {
		t.Fatalf("list.bindings in a prefix mode lists the continuations: %+v", rows)
	}
	r.prefix.deadline = time.Now().Add(-time.Second)
	if handled, err := r.HandleBindingTimeout(BindingTimeoutMsg{runtime: r, version: r.bindingVersion}); !handled || err != nil {
		t.Fatalf("timeout: %v %v", handled, err)
	}
	if r.modeState("app").Name != "normal" || !reflect.DeepEqual(*calls, []string{"g"}) || r.Pending != "" {
		t.Fatalf("timeout did not restore: %s %v", r.modeState("app").Name, *calls)
	}
}

// Recording is a shadow mode on the app noun: keys are appended and then
// dispatched in the shadowed mode; q stops only from a quiet normal mode.
func TestMacroRecordingShadowMode(t *testing.T) {
	r, in := editorTestRuntime(t, "", true)
	editorTestKeys(t, r, "q", "a")
	if r.Macro.Recording != "a" || r.Mode(r.Context()) != "normal" {
		t.Fatalf("recording did not start: %+v", r.Macro)
	}
	editorTestKeys(t, r, "i", "q", "esc", "2", "q")
	if string(in.Text) != "q" || r.Macro.Recording != "a" {
		t.Fatalf("q in insert or count_pending must record and re-dispatch: text=%q %+v", in.Text, r.Macro)
	}
	editorTestKeys(t, r, "esc", "q")
	want := []MacroKey{{Key: "i"}, {Key: "q"}, {Key: "esc"}, {Key: "2"}, {Key: "q"}, {Key: "esc"}}
	if r.Macro.Recording != "" || !reflect.DeepEqual(r.Registers["a"].Keys, want) {
		t.Fatalf("recorded %+v; want %+v", r.Registers["a"].Keys, want)
	}
	editorTestKeys(t, r, "@", "a")
	if string(in.Text) != "qq" {
		t.Fatalf("replay (not re-recorded): %q", in.Text)
	}
}

// leader_pending has the same shape: a shadow mode whose any row forwards
// the prefix and key and restores the shadowed mode. The terminal arrives in
// phase 7; this is the mechanism.
func TestLeaderPendingShadowShape(t *testing.T) {
	r := NewRuntime()
	var forwarded []string
	calls := recorder(t, r, "new-window")
	r.DefineMode("leader_pending", true)
	r.Register(Operation{Verb: "forward", Noun: "pane", Parameters: map[string]Parameter{"keys": {Type: "string", Required: true}}, Run: func(_ *Runtime, _ Context, a Arguments) (any, error) {
		forwarded = append(forwarded, StringArg(a, "keys", ""))
		return nil, nil
	}})
	r.Define("term").Mode("normal").
		Bind("ctrl+b", Func(func(r *Runtime, ev KeyEvent) error {
			shadow := ev.Mode
			r.enterMode(r.Instances[ev.Focus], "leader_pending", Payload{Shadow: &shadow})
			return nil
		})).
		Pattern(Any, Do("forward.pane").FromKey("keys")).
		Mode("leader_pending").
		Bind("c", Do("new-window.test")).
		Pattern(Any, Func(func(r *Runtime, ev KeyEvent) error {
			inst := r.Instances[ev.Focus]
			shadow := *inst.Payload.Shadow
			r.leaveMode(inst)
			_, err := r.Execute(Request{Verb: "forward", Noun: "pane", Args: Arguments{"keys": "ctrl+b " + ev.Key.Key}})
			if shadow.Name != inst.State {
				inst.State = shadow.Name
			}
			return err
		}))
	r.Instance("pane", Kind("term"))
	if err := r.Focus("pane"); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"x", "ctrl+b", "c", "ctrl+b", "z", "y"} {
		if _, err := r.Dispatch(MacroKey{Key: key}); err != nil {
			t.Fatalf("%s: %v", key, err)
		}
	}
	if !reflect.DeepEqual(forwarded, []string{"x", "ctrl+b z", "y"}) || !reflect.DeepEqual(*calls, []string{"new-window"}) || r.Mode(r.Context()) != "normal" {
		t.Fatalf("leader: forwarded=%v calls=%v mode=%s", forwarded, *calls, r.Mode(r.Context()))
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
// list.bindings for its mode on a plain focused input.
func TestListBindingsListsEveryEditorKey(t *testing.T) {
	r, in := editorTestRuntime(t, "abc", true)
	editor := r.Noun("editor")
	for mode, rows := range editor.Bindings {
		want := map[string]bool{}
		for _, b := range rows {
			want[b.Keys+"\x00"+b.Pattern] = true
		}
		if r.pendingModes[mode] {
			r.enterMode(in.inst, mode, Payload{Op: "d"})
		} else {
			in.storeMode(mode)
		}
		got := map[string]bool{}
		for _, b := range r.ListBindings(in.ID) {
			got[b.Keys+"\x00"+b.Pattern] = true
		}
		for key := range want {
			if !got[key] {
				t.Fatalf("mode %s: %q missing from list.bindings", mode, strings.ReplaceAll(key, "\x00", "/"))
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

func TestSnapshotExposesNounsAndInstances(t *testing.T) {
	r := NewDemo().Runtime
	rows, err := JSONPointer(r.Snapshot(), "/nouns/playground/bindings/normal")
	if err != nil {
		t.Fatal(err)
	}
	if list, ok := rows.([]any); !ok || len(list) == 0 {
		t.Fatalf("playground normal rows: %#v", rows)
	}
	parent, err := JSONPointer(r.Snapshot(), "/instances/directory/parent")
	if err != nil || parent != "agent" {
		t.Fatalf("instance parent %v, %v", parent, err)
	}
	if page, _ := JSONPointer(r.Snapshot(), "/page"); page != "overview" {
		t.Fatalf("/page = %v", page)
	}
}

// assertPageInvariant: Runtime.Page is an ancestor of the base frame's focus.
func assertPageInvariant(t *testing.T, r *Runtime, step string) {
	t.Helper()
	if r.Page == "" {
		return
	}
	if !slices.Contains(r.Ancestry(r.Frames[0].Focus), r.Page) {
		t.Fatalf("%s: page %q is not an ancestor of base focus %q", step, r.Page, r.Frames[0].Focus)
	}
}

// The demo session the phase 3 equivalence test drove: pages, fields, nested
// modals, yank history, palette, picker and sequences, every step green and
// the page invariant holding.
func TestDemoKeyboardSessionStaysGreen(t *testing.T) {
	m := NewDemo()
	r := m.Runtime
	r.BindingTimeout = time.Hour
	step := func(label string, run func()) {
		t.Helper()
		run()
		if m.Status != "" {
			t.Fatalf("%s: %s", label, m.Status)
		}
		assertPageInvariant(t, r, label)
	}
	press := func(keys ...string) {
		t.Helper()
		for _, key := range keys {
			step("key "+key, func() { keytest.Press(m, key) })
		}
	}
	execute := func(verb, noun string, args Arguments) {
		t.Helper()
		step(verb+"."+noun, func() {
			if _, err := r.Execute(Request{Verb: verb, Noun: noun, Args: args}); err != nil {
				t.Fatalf("%s.%s: %v", verb, noun, err)
			}
		})
	}
	press("ctrl+j", "i", "h", "i", "esc", "y", "y", "tab", "tab", "shift+tab", "ctrl+x")
	press("ctrl+n", "i", "x", "esc", "ctrl+n", "ctrl+g", "ctrl+g")
	press("ctrl+y", "esc")
	execute("open", "palette", nil)
	press("p", "a")
	execute("close", "palette", nil)
	press("?", "ctrl+g")
	execute("focus", "input", Arguments{"id": "directory"})
	press("ctrl+o")
	execute("close", "picker", nil)
	press("g")
	execute("select", "context", Arguments{"path": `["app","playground","agent"]`})
	press("ctrl+j", "j", "k", "ctrl+j", "t", "d", "ctrl+j", "ctrl+k", "ctrl+k")
	execute("open", "page", Arguments{"id": "playground"})
	execute("push", "modal", Arguments{"id": "draft", "input": "dialog"})
	execute("pop", "modal", nil)
	if string(r.Inputs["directory"].Text) != "hi/tmp" || r.Registers["\""].Yank.Text != "hi/tmp\n" {
		t.Fatalf("session state: text=%q yank=%q", r.Inputs["directory"].Text, r.Registers["\""].Yank.Text)
	}
}

// Rows are instances of their nouns, any instance is a target, and the mode
// lives on instances.
func TestListRowsAreInstancesOfTheirNouns(t *testing.T) {
	r, list, input, items := listFixture(t)
	r.Register(Operation{Verb: "inspect", Noun: "item", Run: func(*Runtime, Context, Arguments) (any, error) { return "application", nil }})
	r.Define("a").Action("inspect.item", func(*Runtime, Context, Arguments) (any, error) { return "noun a", nil }).
		Mode("normal").Bind("x", Do("quit.app"))
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
		t.Fatalf("select.context instance: %v %+v", err, r.Context())
	}
	if err := r.Focus(input.ID); err != nil || r.Context().InputID != "note" {
		t.Fatalf("Focus input: %v", err)
	}
	if err := r.Focus("sidebar"); err != nil || r.Context().InputID != "" || r.Context().Path[2] != "sidebar" {
		t.Fatalf("Focus list: %v %+v", err, r.Context())
	}
	snapshot := r.Snapshot()
	if noun, _ := JSONPointer(snapshot, "/lists/sidebar/items/1/noun"); noun != "b" {
		t.Fatalf("list rows show their noun: %v", noun)
	}
	if noun, _ := JSONPointer(snapshot, "/instances/two/noun"); noun != "b" {
		t.Fatalf("/instances/<id>/noun: %v", noun)
	}
	if state, err := JSONPointer(snapshot, "/instances/two/state"); err != nil || state != "" {
		t.Fatalf("/instances/<id>/state: %v %v", state, err)
	}
}

// Multi-selection fan-out: a verb over marked rows runs verb.<row noun> per
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
	if !reflect.DeepEqual(opened, []string{"one", "two"}) || !reflect.DeepEqual(report["applied"], []string{"one", "two"}) || !reflect.DeepEqual(report["skipped"], []string{"three: open.c is not registered"}) {
		t.Fatalf("fan-out: opened=%v report=%v", opened, report)
	}
	editorTestKeys(t, r, "space")
	if marked := list.Marked(); len(marked) != 2 || marked[1].ID != "two" {
		t.Fatalf("toggle mark: %v", marked)
	}
}

func TestModeLivesOnInstances(t *testing.T) {
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
	row := r.Context()
	if r.Instances["one"].State != "review" || r.Mode(row) != "review" {
		t.Fatalf("set.mode must write the leaf instance: %+v", r.Instances["one"])
	}
	list.Move(1)
	if r.Mode(r.Context()) != "normal" {
		t.Fatal("another row does not inherit a sibling's state")
	}
	// A pending mode entered on an inheriting leaf returns to inheriting.
	editorTestKeys(t, r, "q")
	if r.Mode(r.Context()) != "macro_pending" {
		t.Fatalf("mode %s", r.Mode(r.Context()))
	}
	editorTestKeys(t, r, "esc")
	if r.Instances["two"].State != "" || r.Mode(r.Context()) != "normal" {
		t.Fatalf("leave must restore the raw state: %q %s", r.Instances["two"].State, r.Mode(r.Context()))
	}
	// A state higher on the stack is inherited by instances without one.
	r.Instances["page"].State = "review"
	if r.Mode(r.Context()) != "review" || r.Mode(Context{Path: []string{"app", "page"}}) != "review" || r.Mode(Context{Path: []string{"app"}}) != "normal" {
		t.Fatal("mode inherits from the nearest ancestor with a state")
	}
	// Mode reads the context it is given, not the top frame.
	if err := r.Push(&Frame{ID: "dialog", Focus: "dialog"}); err != nil {
		t.Fatal(err)
	}
	if r.Mode(r.Context()) != "normal" || r.Mode(row) != "review" {
		t.Fatalf("Mode(c) must follow c: top %q, row %q", r.Mode(r.Context()), r.Mode(row))
	}
	if err := r.Pop(); err != nil {
		t.Fatal(err)
	}
	// An input's instance is the source of its mode and Input.Mode mirrors it.
	if err := r.Focus(input.ID); err != nil {
		t.Fatal(err)
	}
	if r.Instances["note"].State != "insert" || r.Mode(r.Context()) != "insert" {
		t.Fatalf("prompt input instance: %+v", r.Instances["note"])
	}
	other := NewInput("other-input", "ab", false)
	if err := r.AddInput(other, Parent("editor")); err != nil {
		t.Fatal(err)
	}
	if err := r.Focus("other-input"); err != nil {
		t.Fatal(err)
	}
	for _, keys := range [][]string{{"i"}, {"esc"}, {"v"}, {"esc", "i", "x", "esc", "u"}} {
		editorTestKeys(t, r, keys...)
		if r.Instances["other-input"].State != other.Mode || r.Mode(r.Context()) != other.Mode {
			t.Fatalf("after %v: instance %q, mirror %q", keys, r.Instances["other-input"].State, other.Mode)
		}
	}
}

func TestInputModeMirrorsInstanceInDemo(t *testing.T) {
	m := NewDemo()
	r := m.Runtime
	for _, key := range []string{"ctrl+j", "i", "h", "esc", "v", "l", "esc", "ctrl+n", "i", "x", "esc", "ctrl+g", "2", "d"} {
		keytest.Press(m, key)
		for id, in := range r.Inputs {
			inst := r.Instances[id]
			pending := r.pendingModes[inst.State] || inst.Payload.Prefix != ""
			if inst == nil || !pending && inst.State != in.Mode || pending && inst.Payload.Return != in.Mode {
				t.Fatalf("after %s: input %s mode %q, instance %+v", key, id, in.Mode, inst)
			}
		}
	}
}
