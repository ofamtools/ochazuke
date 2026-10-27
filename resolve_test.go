package ochazuke

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/unixsuperhero/ochazuke/internal/keytest"
)

// A capture row is consulted outer-first before the inner-first walk, so an
// inner any row does not swallow it. The row comes from config with
// "capture": true.
func TestCaptureRowBeatsInnerAnyRow(t *testing.T) {
	r := NewRuntime()
	calls := recorder(t, r, "any", "quit")
	r.Instance("panel")
	if err := r.SetFocus("panel"); err != nil {
		t.Fatal(err)
	}
	r.Define("panel").Mode("normal").Pattern(Any, Do("test.any"))
	press(t, r, "ctrl+c")
	if r.Quit || len(*calls) != 1 {
		t.Fatalf("without a capture row the any row takes ctrl+c: quit=%v calls=%v", r.Quit, *calls)
	}
	var row Binding
	if err := json.Unmarshal([]byte(`{"noun": "app", "keys": "ctrl+c", "capture": true, "request": {"verb": "quit", "noun": "test"}}`), &row); err != nil {
		t.Fatal(err)
	}
	if err := r.Bind(row); err != nil {
		t.Fatal(err)
	}
	press(t, r, "ctrl+c")
	if got := *calls; len(got) != 2 || got[1] != "quit" {
		t.Fatalf("captured ctrl+c: %v", got)
	}
	press(t, r, "x")
	if got := *calls; len(got) != 3 || got[2] != "any" {
		t.Fatalf("other keys still reach the any row: %v", got)
	}
	listed := false
	for _, b := range r.ListBindings(r.Focus) {
		listed = listed || b.Keys == "ctrl+c" && b.Capture
	}
	if !listed {
		t.Fatal("bindings.list shows the capture row")
	}
}

// Capture runs outer-first: the app's capture row answers before an inner
// level's capture row for the same key; a non-capture app row does not.
func TestCaptureIsOuterFirst(t *testing.T) {
	r := NewRuntime()
	calls := recorder(t, r, "outer", "inner", "plain")
	r.Instance("panel")
	if err := r.SetFocus("panel"); err != nil {
		t.Fatal(err)
	}
	r.Define("panel").Mode("normal").Bind("x", Do("test.inner").Captured())
	r.Define("app").Mode("normal").Bind("x", Do("test.plain"))
	press(t, r, "x")
	r.Define("app").Mode("normal").Bind("x", Do("test.outer").Captured())
	press(t, r, "x")
	if got := *calls; len(got) != 2 || got[0] != "inner" || got[1] != "outer" {
		t.Fatalf("capture order: %v", got)
	}
}

// The model's ctrl+c is a capture row guarded by no-modal: it quits past an
// inner any row, inside the palette the palette still closes, and in the
// terminal's passthrough mode there is no capture pass, so ctrl+c (and
// ctrl+c under the leader) reaches the pane and the app keeps running.
func TestModelCtrlCCapturedOutsideModals(t *testing.T) {
	r, _, stream := terminalFixture(t)
	m := NewModel(r)
	for _, b := range r.ListBindings(r.Focus) {
		if b.Keys == "ctrl+c" {
			t.Fatalf("an attached terminal lists ctrl+c as %s", b.Request.Key())
		}
	}
	keytest.Press(m, "ctrl+c")
	if r.Quit || !reflect.DeepEqual(stream.sent, [][]string{{"ctrl+c"}}) {
		t.Fatalf("terminal focus: quit=%v sent=%v", r.Quit, stream.sent)
	}
	keytest.Press(m, "ctrl+a")
	keytest.Press(m, "ctrl+c")
	if r.Quit || !reflect.DeepEqual(stream.sent[1:], [][]string{{"ctrl+a", "ctrl+c"}}) || r.Mode.Name != "terminal" {
		t.Fatalf("leader then ctrl+c: quit=%v sent=%v mode=%s", r.Quit, stream.sent, r.Mode.Name)
	}
	if err := m.Palette.Open(); err != nil {
		t.Fatal(err)
	}
	for _, b := range r.ListBindings(r.Focus) {
		if b.Keys == "ctrl+c" && b.Request.Key() != "ochazuke.palette.close" {
			t.Fatalf("inside the palette ctrl+c lists %s", b.Request.Key())
		}
	}
	keytest.Press(m, "ctrl+c")
	if m.Palette.IsOpen() || r.Quit {
		t.Fatalf("palette ctrl+c closes it: open=%v quit=%v", m.Palette.IsOpen(), r.Quit)
	}
	if err := r.SetFocus("console"); err != nil {
		t.Fatal(err)
	}
	listed := false
	for _, b := range r.ListBindings(r.Focus) {
		listed = listed || b.Keys == "ctrl+c" && b.Capture && b.Request.Key() == "app.quit"
	}
	keytest.Press(m, "ctrl+c")
	if !listed || !r.Quit || len(stream.sent) != 2 {
		t.Fatalf("detached: listed=%v quit=%v sent=%v", listed, r.Quit, stream.sent)
	}
}

// The top modal is a resolution boundary: a key inside a modal opened over
// a panel never reaches the panel's (or the page's) rows, while app rows
// still answer.
func TestModalIsResolutionBoundary(t *testing.T) {
	r, _ := focusFixture(t)
	pokes := 0
	r.Register(Operation{Verb: "poke", Noun: "panel", Run: func(*Runtime, Context, Arguments) (any, error) { pokes++; return nil, nil }})
	calls := recorder(t, r, "app")
	r.Define("panel").Mode("normal").Bind("z", Do("panel.poke"))
	r.Define("ochazuke.page").Mode("normal").Bind("y", Do("panel.poke"))
	r.Define("app").Mode("normal").Bind("v", Do("test.app"))
	if err := r.SetMode("normal"); err != nil {
		t.Fatal(err)
	}
	press(t, r, "z")
	if pokes != 1 {
		t.Fatalf("without a modal the panel row answers: %d", pokes)
	}
	if _, err := r.OpenModal("ask", Payload{Prompt: "?"}); err != nil {
		t.Fatal(err)
	}
	if got := r.Ancestry(r.Focus); !slices.Contains(got, "panel") {
		t.Fatalf("the modal stays parented over the panel: %v", got)
	}
	for _, key := range []string{"z", "y"} {
		if _, err := r.Dispatch(MacroKey{Key: key}); err != nil {
			t.Fatal(err)
		}
	}
	if pokes != 1 {
		t.Fatalf("a key inside the modal bubbled beneath it: %d", pokes)
	}
	press(t, r, "v")
	if len(*calls) != 1 {
		t.Fatal("app rows still answer inside a modal")
	}
	for _, b := range r.ListBindings(r.Focus) {
		if b.Noun == "panel" || b.Noun == "ochazuke.page" {
			t.Fatalf("bindings.list shows a row beneath the modal: %+v", b)
		}
	}
}

// sequenceFixture focuses a panel inside app with a long timeout; inner
// rows sit on the panel noun, outer rows on app.
func sequenceFixture(t *testing.T) (*Runtime, *[]string) {
	t.Helper()
	r := NewRuntime()
	r.BindingTimeout = time.Hour
	calls := recorder(t, r, "inner", "outer")
	r.Instance("panel")
	if err := r.SetFocus("panel"); err != nil {
		t.Fatal(err)
	}
	return r, calls
}

func expire(t *testing.T, r *Runtime) {
	t.Helper()
	r.prefix.deadline = time.Now().Add(-time.Second)
	if err := r.expirePrefix(); err != nil {
		t.Fatal(err)
	}
}

// Sequences scan the whole stack per key (typeahead): a longer sequence at
// any level waits, the innermost exact match is the fallback.
func TestSequencesScanTheWholeStack(t *testing.T) {
	// inner x + outer x x: x waits, x x runs outer, timeout runs inner x.
	r, calls := sequenceFixture(t)
	r.Define("panel").Mode("normal").Bind("x", Do("test.inner"))
	r.Define("app").Mode("normal").Bind("x x", Do("test.outer"))
	press(t, r, "x")
	if r.Pending != "x" || len(*calls) != 0 {
		t.Fatalf("x waits: pending %q calls %v", r.Pending, *calls)
	}
	press(t, r, "x")
	press(t, r, "x")
	expire(t, r)
	if got := *calls; !reflect.DeepEqual(got, []string{"outer", "inner"}) {
		t.Fatalf("x x then x timeout: %v", got)
	}

	// inner g x + outer exact g: timeout runs outer g.
	r, calls = sequenceFixture(t)
	r.Define("panel").Mode("normal").Bind("g x", Do("test.inner"))
	r.Define("app").Mode("normal").Bind("g", Do("test.outer"))
	press(t, r, "g")
	expire(t, r)
	press(t, r, "g", "x")
	if got := *calls; !reflect.DeepEqual(got, []string{"outer", "inner"}) {
		t.Fatalf("g timeout, then g x: %v", got)
	}

	// inner g x + outer g g: g g runs outer.
	r, calls = sequenceFixture(t)
	r.Define("panel").Mode("normal").Bind("g x", Do("test.inner"))
	r.Define("app").Mode("normal").Bind("g g", Do("test.outer"))
	press(t, r, "g", "g", "g", "x")
	if got := *calls; !reflect.DeepEqual(got, []string{"outer", "inner"}) {
		t.Fatalf("g g then g x: %v", got)
	}
}

// A row whose operation is guarded off does not answer: the walk goes on to
// the next level, as the listings hide it.
func TestGuardedOffRowFallsThrough(t *testing.T) {
	r, calls := sequenceFixture(t)
	r.DefineGuard("never", func(*Runtime, KeyEvent) bool { return false })
	guardVerb(r, "test.inner", "never")
	r.Define("panel").Mode("normal").Bind("d", Do("test.inner"))
	r.Define("app").Mode("normal").Bind("d", Do("test.outer"))
	press(t, r, "d")
	if got := *calls; !reflect.DeepEqual(got, []string{"outer"}) {
		t.Fatalf("d: %v", got)
	}
	if _, err := r.Execute(Request{Verb: "inner", Noun: "test"}); err == nil {
		t.Fatal("a direct call still gets the refusal")
	}
}

// SortBindings orders the full export: mode, then noun (a noun's rows
// before an instance's of that name), then the request's verb (no verb
// last), then keys (pattern rows after key rows); equal rows keep order.
func TestSortBindingsOrdersModeNounVerbKeys(t *testing.T) {
	row := func(mode, noun, instance, verb, keys, pattern, when string) Binding {
		return Binding{Mode: mode, Noun: noun, Instance: instance, Request: Request{Verb: verb, Noun: "x"}, Keys: keys, Pattern: pattern, When: when}
	}
	rows := []Binding{
		row("normal", "list", "", "move", "j", "", ""),
		row("insert", "editor", "", "type", "", "printable", ""),
		row("normal", "app", "", "quit", "q", "", ""),
		row("normal", "", "app", "beta", "b", "", ""),
		row("normal", "app", "", "", "z", "", ""),
		row("normal", "app", "", "alpha", "", "digit", ""),
		row("normal", "app", "", "alpha", "b", "", "second"),
		row("normal", "app", "", "alpha", "b", "", "first"),
		row("", "app", "", "zap", "a", "", ""),
		row("insert", "editor", "", "type", "a", "", ""),
	}
	SortBindings(rows)
	var got []string
	for _, b := range rows {
		got = append(got, strings.Join([]string{b.Mode, b.Noun, b.Instance, b.Request.Verb, b.Keys, b.Pattern, b.When}, "|"))
	}
	want := []string{
		"|app||zap|a||",
		"insert|editor||type|a||",
		"insert|editor||type||printable|",
		"normal|app||alpha|b||second",
		"normal|app||alpha|b||first",
		"normal|app||alpha||digit|",
		"normal|app||quit|q||",
		"normal|app|||z||",
		"normal||app|beta|b||",
		"normal|list||move|j||",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("order\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
