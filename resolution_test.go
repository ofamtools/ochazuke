package ochazuke

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

// interactionSnapshot is everything a query must leave as it found it: the
// live mode, focus and modal chain with each modal's saved return, the
// page and the input's state, the mode generation, the pending sequence
// and its timer, and the binding and help caches.
type interactionSnapshot struct {
	Mode            ModeState
	Focus, Page     string
	Modals          []string
	Returns         []string
	ReturnModes     []ModeState
	Remembered      string
	ModeGeneration  uint64
	BindingVersion  uint64
	Pending         string
	Prefix          pendingPrefix
	InputText       string
	InputCursor     int
	InputMode       string
	CacheScans      []*bindingScan
	CacheLast       *bindingScan
	CacheCollecting *bindingScan
	CacheHints      hintMemo
	CacheHelp       int
	CacheGeneration uint64
}

func snapshotInteraction(r *Runtime, input string) interactionSnapshot {
	s := interactionSnapshot{
		Mode: cloneModeState(r.Mode), Focus: r.Focus, Page: r.Page, Modals: slices.Clone(r.modals),
		ModeGeneration: r.modeGeneration, BindingVersion: r.bindingVersion, Pending: r.Pending, Prefix: r.prefix,
		CacheScans: slices.Clone(r.renderCache.scans), CacheLast: r.renderCache.lastScan, CacheCollecting: r.renderCache.collecting,
		CacheHints: r.renderCache.hints, CacheHelp: len(r.renderCache.help), CacheGeneration: r.renderCache.generation,
	}
	for _, id := range r.modals {
		back, mode := r.ModalReturn(id)
		s.Returns = append(s.Returns, back)
		s.ReturnModes = append(s.ReturnModes, mode)
	}
	if page := r.PageOf(r.Page); page != nil {
		s.Remembered = page.Remembered
	}
	if in := r.Input(input); in != nil {
		s.InputText, s.InputCursor, s.InputMode = string(in.Text), in.Cursor, in.Mode
	}
	return s
}

func assertUntouched(t *testing.T, what string, before, after interactionSnapshot) {
	t.Helper()
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("%s changed the runtime:\nbefore %+v\nafter  %+v", what, before, after)
	}
}

// pureQueryFixture is a page with an input, a focusable panel and a pad
// pinned to insert mode (its stop is read in insert), focus on the panel in
// normal mode.
func pureQueryFixture(t *testing.T) *Runtime {
	t.Helper()
	r, _, _ := helpInsertFixture(t)
	r.Define("widget").Focusable()
	r.Instance("panel", Kind("widget"), Parent("p"))
	r.Define("pad").Focusable().Pin("insert")
	r.Instance("pad", Kind("pad"), Parent("p"))
	if err := r.SetFocus("panel"); err != nil {
		t.Fatal(err)
	}
	if err := r.SetMode("normal"); err != nil {
		t.Fatal(err)
	}
	return r
}

// The queries read a hypothetical focus and mode through their own
// resolution context: StopBindings, TypingShadows and help refreshed at its
// origin leave the live mode, focus, modals and saved returns, the page,
// the input, the mode generation, a pending sequence and its timer, and
// the binding and help caches exactly as they were. Also while a sequence
// waits (the timer it armed still expires it) and with a chooser above the
// help.
func TestResolveAtDoesNotMutateRuntime(t *testing.T) {
	t.Run("stops and typing", func(t *testing.T) {
		r := pureQueryFixture(t)
		r.ListBindings(r.Focus) // a warm cache entry the queries must not touch
		before := snapshotInteraction(r, "note")
		if stops := r.StopBindings(); len(stops) < 3 {
			t.Fatalf("stops %+v", stops)
		}
		assertUntouched(t, "StopBindings", before, snapshotInteraction(r, "note"))
		r.TypingShadows()
		assertUntouched(t, "TypingShadows", before, snapshotInteraction(r, "note"))
	})
	t.Run("during a pending sequence", func(t *testing.T) {
		r := pureQueryFixture(t)
		r.BindingTimeout = 3600e9
		r.Define("app").Mode("normal").Bind("x x", Do("ochazuke.focus.next"))
		press(t, r, "x")
		if r.Pending != "x" || !r.pending() {
			t.Fatalf("x waits: pending %q mode %+v", r.Pending, r.Mode)
		}
		tick := BindingTimeoutMsg{runtime: r, version: r.bindingVersion, due: true}
		before := snapshotInteraction(r, "note")
		stops := r.StopBindings()
		r.TypingShadows()
		assertUntouched(t, "queries during a sequence", before, snapshotInteraction(r, "note"))
		if stops[1].Stop != "panel" || stops[1].Mode != r.Mode.Name {
			t.Fatalf("the current stop reads the pending mode: %+v", stops[1])
		}
		if ran, err := r.HandleBindingTimeout(tick); !ran || err != nil || r.Pending != "" {
			t.Fatalf("the sequence's own timer still expires it: ran %v err %v pending %q", ran, err, r.Pending)
		}
	})
	t.Run("help under a chooser", func(t *testing.T) {
		r := pureQueryFixture(t)
		if _, err := r.Execute(Do("ochazuke.bindings.help").Request); err != nil {
			t.Fatal(err)
		}
		h, ok := r.Instances[helpID].Component.(*helpState)
		if !ok {
			t.Fatalf("help component %T", r.Instances[helpID].Component)
		}
		if _, err := r.OpenChooser(ChooserSpec{Kind: "k", Rows: []ChooserRow{{ID: "a", Label: "alpha"}}}); err != nil {
			t.Fatal(err)
		}
		origin, mode := r.ModalReturn(helpID)
		before := snapshotInteraction(r, "note")
		rows := r.bindingHelpIn(queryResolution(origin, mode, nil))
		assertUntouched(t, "help's query", before, snapshotInteraction(r, "note"))
		if err := h.refreshBindings(); err != nil {
			t.Fatal(err)
		}
		after := snapshotInteraction(r, "note")
		// The refresh redraws the help's own tree (its rows are the output);
		// the interaction around it stays.
		before.CacheScans, after.CacheScans, before.CacheLast, after.CacheLast = nil, nil, nil, nil
		before.CacheHints, after.CacheHints = hintMemo{}, hintMemo{}
		before.CacheHelp, after.CacheHelp, before.CacheGeneration, after.CacheGeneration = 0, 0, 0, 0
		assertUntouched(t, "help refresh", before, after)
		if !reflect.DeepEqual(h.here, rows) {
			t.Fatalf("refresh reads the help's origin:\n%+v\nwant %+v", h.here, rows)
		}
	})
}

// A query's guards read its hypothetical interaction from the event
// (KeyEvent.Mode, Runtime.EventFocus, Runtime.EventModals), while the
// runtime's own fields and accessors stay the live interaction throughout:
// owner question 5 of the mode-ownership plan.
func TestResolveAtUsesEventContext(t *testing.T) {
	setup := func(t *testing.T) (*Runtime, *[]seen) {
		r := pureQueryFixture(t)
		var log []seen
		r.DefineGuard("probe", func(r *Runtime, ev KeyEvent) bool {
			log = append(log, seen{r.Mode.Name, r.Focus, ev.Mode.Name, r.EventFocus(ev), r.EventTopModal(ev), len(r.modals)})
			return r.EventFocus(ev) == "note"
		})
		for _, mode := range []string{"normal", "insert"} {
			r.Define("app").Mode(mode).Bind("ctrl+y", Binding{Request: Request{Noun: "ochazuke.focus", Verb: "next"}, When: "probe"})
		}
		return r, &log
	}
	t.Run("stops", func(t *testing.T) {
		r, log := setup(t)
		stops := r.StopBindings()
		var note StopBindings
		for _, s := range stops {
			if s.Stop == "note" {
				note = s
			}
		}
		if note.Mode == "" {
			t.Fatalf("no stop for the input: %+v", stops)
		}
		checkLive(t, *log, "normal", "panel", 0)
		if !slices.ContainsFunc(*log, func(s seen) bool { return s.eventFocus == "pad" && s.eventMode == "insert" }) {
			t.Fatalf("the pinned stop is read in its pinned mode: %+v", *log)
		}
		for _, at := range []string{"note", "panel", "app"} {
			if !slices.ContainsFunc(*log, func(s seen) bool { return s.eventFocus == at }) {
				t.Fatalf("no reading at the %s stop: %+v", at, *log)
			}
		}
		if !slices.ContainsFunc(*log, func(s seen) bool { return s.eventFocus == "note" && s.eventMode == note.Mode }) {
			t.Fatalf("the input's stop is read in its own mode: %+v", *log)
		}
	})
	t.Run("typing", func(t *testing.T) {
		r, log := setup(t)
		// A printable row the guard lets answer only at the input: the check
		// judges it at the input in insert mode, and reports it.
		r.Define("app").Mode("insert").Bind("~", Binding{Request: Request{Noun: "ochazuke.focus", Verb: "next"}, When: "probe"})
		shadows := r.TypingShadows()
		checkLive(t, *log, "normal", "panel", 0)
		if !slices.ContainsFunc(*log, func(s seen) bool { return s.eventFocus == "note" && s.eventMode == "insert" }) {
			t.Fatalf("the typing check reads each focus and mode on the event: %+v", *log)
		}
		if !slices.ContainsFunc(shadows, func(line string) bool {
			return strings.Contains(line, "focus note mode insert") && strings.Contains(line, `"~"`)
		}) {
			t.Fatalf("the guarded row shadows typing at the input: %q", shadows)
		}
	})
	t.Run("help at its origin", func(t *testing.T) {
		r, log := setup(t)
		if err := r.SetFocus("note"); err != nil {
			t.Fatal(err)
		}
		if _, err := r.Execute(Do("ochazuke.bindings.help").Request); err != nil {
			t.Fatal(err)
		}
		if _, err := r.OpenChooser(ChooserSpec{Kind: "k", Rows: []ChooserRow{{ID: "a", Label: "alpha"}}}); err != nil {
			t.Fatal(err)
		}
		h := r.Instances[helpID].Component.(*helpState)
		liveMode, liveFocus := r.Mode.Name, r.Focus
		*log = nil
		if err := h.refreshBindings(); err != nil {
			t.Fatal(err)
		}
		checkLive(t, *log, liveMode, liveFocus, 2)
		if len(*log) == 0 {
			t.Fatal("the guard was not read")
		}
		for _, s := range *log {
			if s.eventFocus != "note" || s.eventTop != "" {
				t.Fatalf("help reads its origin with no modal above it: %+v", s)
			}
		}
		found := false
		for _, row := range h.here {
			found = found || row.Key == "ctrl+y"
		}
		if !found {
			t.Fatalf("the guarded row answers at the origin: %+v", h.here)
		}
	})
}

// seen is one reading of the probe guard: what the runtime says (live)
// and what the event says (the query's interaction).
type seen struct {
	liveMode, liveFocus, eventMode, eventFocus, eventTop string
	liveModals                                           int
}

// checkLive fails when a guard saw anything but the live interaction
// through the runtime itself.
func checkLive(t *testing.T, log []seen, mode, focus string, modals int) {
	t.Helper()
	for _, s := range log {
		if s.liveMode != mode || s.liveFocus != focus || s.liveModals != modals {
			t.Fatalf("a guard saw a borrowed interaction on the runtime: %+v (live %s at %s, %d modals)", s, mode, focus, modals)
		}
	}
}
