package ochazuke

import (
	"fmt"
	"image/color"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"ofam.dev/ochazuke/keytest"
)

func TestRenderCoalescesAndFlushesLastFrame(t *testing.T) {
	m, calls := renderFixture(t)
	now := time.Unix(100, 0)
	m.Now = func() time.Time { return now }
	m.View()
	start := *calls
	for range 100 {
		m.Update(struct{}{}) // an arbitrary host message may change its domain state
		m.Info = "latest"
		m.View()
	}
	if *calls != start || !m.redraw.dirty || m.redraw.pending.IsZero() {
		t.Fatal("burst did not share a pending frame")
	}
	now = m.redraw.pending
	m.Update(renderFrameMsg{m, now})
	if view := m.View(); !strings.Contains(ansi.Strip(view.Content), "latest") || *calls != start+1 {
		t.Fatal("final burst state was not flushed")
	}
	if m.redraw.dirty || !m.redraw.pending.IsZero() {
		t.Fatal("render timer remains active at idle")
	}
	// Input is never delayed behind the background frame budget.
	m.Update(keytest.Msg("j", false, false))
	m.View()
	if *calls != start+2 {
		t.Fatal("key was delayed")
	}
	if err := m.Runtime.Settings().Set(RenderMaxFPSSetting, 0); err != nil {
		t.Fatal(err)
	}
	for range 5 {
		m.Update(SettingsPollMsg{})
		m.View()
	}
	if *calls != start+7 {
		t.Fatal("render.max-fps=0 did not restore per-message rendering")
	}
}

func TestRenderKeySurvivesBatchedUpdatesAndClockReset(t *testing.T) {
	m, calls := renderFixture(t)
	m.View()
	start := *calls
	now := time.Unix(100, 0) // a headless driver installs its clock after build
	m.Now = func() time.Time { return now }
	m.Update(struct{}{})
	m.View()
	if *calls != start+1 || !m.redraw.pending.IsZero() {
		t.Fatal("backward clock scheduled an unbounded timer")
	}
	m.Update(keytest.Msg("j", false, false))
	m.Update(struct{}{})
	m.View()
	if *calls != start+2 {
		t.Fatal("background update hid an unrendered input change")
	}
}

func TestRenderTerminalOwnsReusedStreamBuffer(t *testing.T) {
	m, _ := renderFixture(t)
	term, _ := m.Runtime.AddTerminal("pane", newFakeStream(), Parent("page"))
	m.Runtime.PageOf("page").Render = func(_ *Runtime, w, h int) Block { return term.ViewBlock(w, h) }
	lines := []string{"first"}
	term.acceptFrame(TerminalFrame{Lines: lines})
	lines[0] = "second"
	if term.Frame().Lines[0] != "first" {
		t.Fatal("stream buffer changed the accepted frame")
	}
	m.View()
	m.Update(Completion{Ticket: m.Runtime.jobs[m.Runtime.jobSeq].ticket(), Value: TerminalFrame{Lines: lines}})
	if !m.redraw.dirty || term.Frame().Lines[0] != "second" {
		t.Fatal("reused buffer was mistaken for an unchanged frame")
	}
}

func TestRenderDuplicateTerminalFrameAndStaleCompletion(t *testing.T) {
	m, calls := renderFixture(t)
	term, err := m.Runtime.AddTerminal("pane", newFakeStream(), Parent("page"))
	if err != nil {
		t.Fatal(err)
	}
	frame := TerminalFrame{Lines: []string{"unchanged"}}
	term.frame = frame
	m.View()
	start := *calls
	done := Completion{Ticket: m.Runtime.jobs[m.Runtime.jobSeq].ticket(), Value: frame}
	m.Update(done)
	m.View()
	if *calls != start {
		t.Fatal("identical terminal frame recomposed the page")
	}
	m.Update(done) // already settled
	m.View()
	if *calls != start {
		t.Fatal("stale completion recomposed the page")
	}
	// Application callbacks must run and draw even for an identical payload.
	ticket, _, _ := m.Runtime.Issue("app", "", "", Ctx{})
	m.Runtime.jobs[ticket.Job].then = func(Ctx, any, error) error { m.Info = "callback"; return nil }
	m.Update(Completion{Ticket: ticket})
	if !m.redraw.dirty {
		t.Fatal("application completion was treated as an unchanged stream frame")
	}
}

func TestRenderStylesInvalidateByValues(t *testing.T) {
	m, _ := renderFixture(t)
	t.Setenv("NO_COLOR", "")
	accent := &color.RGBA{R: 100, G: 180, A: 255}
	checks := []func(){
		func() { m.Appearance.Theme = "light" },
		func() { m.Appearance.Density = "compact" },
		func() { m.Appearance.Accent = accent },
		func() { accent.R = 230 },
		func() { m.Appearance.SetTone("accent", ToneStyle{Fg: SchemeColor{"#ff0000", "#ff0000"}}) },
		func() { _ = m.Runtime.Settings().Set(ModalDimSetting, 30) },
		func() { m.Runtime.SetMode("insert") },
		func() { t.Setenv("NO_COLOR", "1") },
	}
	for i, change := range checks {
		m.Styles()
		change()
		r := m.Runtime
		want := NewStyles(*m.Appearance).WithDims(r.dimSetting(ModalDimSetting, DefaultModalDim), r.dimSetting(DimUnfocusedSetting, DefaultDimUnfocused)).WithBackdropHighlights(r.Settings().String(ModalBackdropHighlightsSetting) != "plain").WithChrome(r.ChromeLevel(), r.modeRoles())
		got := m.Styles()
		if got.Panel.Render("panel") != want.Panel.Render("panel") || got.Badge.Render("badge") != want.Badge.Render("badge") || got.Tone("accent").Render("tone") != want.Tone("accent").Render("tone") {
			t.Fatalf("styles differ after change %d", i)
		}
	}
}

func TestRenderHintsTrackArbitraryGuardsAndBindingEdits(t *testing.T) {
	m, _ := renderFixture(t)
	r := m.Runtime
	shown := false
	r.DefineGuard("cache-test", func(*Runtime, KeyEvent) bool { return shown })
	r.Define("app").Verb("cache-test", func(Ctx) Result { return Done(nil) }).When("cache-test").Mode("normal").Bind("x", Do("app.cache-test"))
	opWithArg := r.Operations.At(Key("app.cache-test"))
	opWithArg.Parameters = map[string]Parameter{"n": {Type: "integer"}}
	r.Operations.Set(opWithArg)
	check := func() {
		t.Helper()
		got := HintBindings(r, r.Focus)
		want := hintBindingsUncached(r, r.Focus, groupLevelRows(r.scanRowsUncached(r.walk(r.Focus), r.Mode, r.Focus, nil)))
		if !reflect.DeepEqual(got, want) {
			t.Fatal("cached hints disagree with the live resolver")
		}
		if got, want := r.levelRows(r.Focus), r.scanRowsUncached(r.walk(r.Focus), r.Mode, r.Focus, nil); !reflect.DeepEqual(got, want) {
			t.Fatal("cached binding walk differs")
		}
	}
	check()
	check()
	shown = true
	check()
	check()
	shown = false
	check()
	r.DefineGuard("cache-test", func(*Runtime, KeyEvent) bool { return true })
	check()
	r.DefineGuard("cache-other", func(*Runtime, KeyEvent) bool { return false })
	r.Nouns["app"].Guards["cache-test"] = "cache-other"
	r.InvalidateBindings()
	check()
	r.Nouns["app"].Guards["cache-test"] = "cache-test"
	r.InvalidateBindings()
	check()
	if err := r.Bind(Binding{Noun: "app", Mode: "normal", Keys: "x", Request: Request{Noun: "app", Verb: "cache-test", Args: Arguments{"n": 2}}}); err != nil {
		t.Fatal(err)
	}
	check()
	check()
	// Public table edits invalidate once the host says so, including nested
	// arguments.
	for i := range r.Nouns["app"].Bindings["normal"] {
		b := &r.Nouns["app"].Bindings["normal"][i]
		if b.Keys == "x" {
			b.Request.Args["n"] = 3
		}
	}
	r.InvalidateBindings()
	check()
	op := r.Operations.At(Key("app.cache-test"))
	op.Motion = true
	r.Operations.Set(op)
	check()
	_ = r.Settings().Set(HintsShowHiddenSetting, true)
	check()
	r.SetMode("insert")
	check()
}

func TestRenderHelpAndHintsFollowAliasShadowing(t *testing.T) {
	m, _ := renderFixture(t)
	r := m.Runtime
	r.Define("long.thing").Verb("open", func(Ctx) Result { return Done(nil) })
	if err := r.Alias("cached", "long.thing"); err != nil {
		t.Fatal(err)
	}
	if err := r.Bind(Binding{Noun: "app", Mode: "normal", Keys: "x", Request: Request{Noun: "long.thing", Verb: "open"}}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"cached.open", "long.thing.open"} {
		if got := BindingHints(r, r.Focus); !strings.Contains(got, want) {
			t.Fatalf("hints lack %s: %s", want, got)
		}
		found := false
		for _, row := range BindingHelp(r, r.Focus) {
			if row.Key == "x" {
				found = row.Request == want
			}
		}
		if !found {
			t.Fatalf("help lacks %s", want)
		}
		r.DefineOwn("cached")
	}
	if err := r.AliasAction("long.thing.show", "long.thing.open"); err != nil {
		t.Fatal(err)
	}
	for _, wantAlias := range []bool{true, false} {
		for _, row := range BindingHelp(r, r.Focus) {
			if row.Key == "x" && (len(row.Aliases) > 0) != wantAlias {
				t.Fatal("removed action alias survived in cached help")
			}
		}
		r.removeAlias("long.thing.show")
	}
}

func TestRenderChildIndexTracksPublicParentAndRegistryEdits(t *testing.T) {
	r := NewRuntime()
	a := r.Instance("a")
	b := r.Instance("b")
	child := r.Instance("child", Parent("a"))
	if got := r.childIndex()["a"]; len(got) != 1 || got[0] != child {
		t.Fatal("initial index")
	}
	child.Parent = "b"
	if len(r.childIndex()["a"]) != 0 || r.childIndex()["b"][0] != child {
		t.Fatal("reparent did not invalidate")
	}
	delete(r.Instances, child.ID)
	if len(r.childIndex()["b"]) != 0 {
		t.Fatal("direct deletion did not invalidate")
	}
	r.Instances[child.ID] = child
	r.Instance("later", Parent("b"))
	if got := r.childIndex()["b"]; len(got) != 2 || got[0] != child {
		t.Fatal("registration order changed")
	}
	_, _ = a, b
}

func TestRenderViewportMatchesUncachedWindows(t *testing.T) {
	m, _ := renderFixture(t)
	for _, text := range []string{"abc", "a long line wrapping past the available viewport", "界 é 👩‍💻\nsecond\nthird\nfourth", "\x1b[31mred\x1b[0m\n"} {
		for _, width := range []int{4, 12, 80} {
			for _, height := range []int{1, 3} {
				for _, offset := range []int{0, 1, 2} {
					m.scroll["probe"] = offset
					view := m.viewport("probe", text, width, height)
					want := view.View()
					if got := m.viewCache.text("probe", view); got != want {
						t.Fatalf("cached viewport %dx%d offset %d differs", width, height, offset)
					}
					if got := m.viewCache.text("probe", view); got != want {
						t.Fatal("warm viewport differs")
					}
				}
			}
		}
	}
}

func FuzzRenderChromeWidth(f *testing.F) {
	for _, s := range []string{"ASCII", "", "a\tb", "a\nb", "界 é 👩‍💻", "\x1b[31mred\x1b[0m", "\x1b]8;;https://example.test\x1b\\link\x1b]8;;\x1b\\"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if got, want := chromeWidth(s), ansi.StringWidth(s); got != want {
			t.Fatalf("width(%q) = %d, want %d", s, got, want)
		}
	})
}

func BenchmarkChildIndex(b *testing.B) {
	r := NewRuntime()
	for i := range 1000 {
		r.Instance(fmt.Sprint(i), Parent("app"))
	}
	for _, cached := range []bool{false, true} {
		b.Run(fmt.Sprint(cached), func(b *testing.B) {
			r.childIndex()
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if cached {
					r.childIndex()
					continue
				}
				// Original childIndex implementation, retained only to measure
				// the same registry with and without rebuilding/sorting it.
				out := map[string][]*Instance{}
				for _, inst := range r.Instances {
					if inst.Parent != inst.ID {
						out[inst.Parent] = append(out[inst.Parent], inst)
					}
				}
				for _, kids := range out {
					sort.Slice(kids, func(i, j int) bool { return kids[i].seq < kids[j].seq })
				}
			}
		})
	}
}

// A read whose cells are unchanged but whose stream Status changed (a
// process exiting after its last output) redraws: the application may show
// that state outside the pane, in a title or a row, drawn or hidden.
func TestRenderTerminalStatusChangeRedraws(t *testing.T) {
	for _, drawn := range []bool{true, false} {
		t.Run(fmt.Sprintf("drawn=%v", drawn), func(t *testing.T) {
			m, _ := renderFixture(t)
			now := time.Unix(100, 0)
			m.Now = func() time.Time { return now }
			term, err := m.Runtime.AddTerminal("pane", newFakeStream(), Parent("page"))
			if err != nil {
				t.Fatal(err)
			}
			status := "running" // what the application reads from its stream
			m.Runtime.PageOf("page").Render = func(_ *Runtime, w, h int) Block {
				title := "pane · " + status
				if !drawn {
					return Text(title)
				}
				return Text(title + "\n" + term.ViewBlock(w, h-1).Text)
			}
			lines := []string{"hi"}
			term.acceptFrame(TerminalFrame{Lines: lines, Status: "running"})
			m.View()
			now = now.Add(time.Millisecond) // inside the frame budget: coalesced
			status = "exited 0"
			_, cmd := m.Update(Completion{Ticket: m.Runtime.jobs[m.Runtime.jobSeq].ticket(), Value: TerminalFrame{Lines: lines, Status: "exited 0"}})
			m.View()
			if !m.redraw.dirty || m.redraw.pending.IsZero() || cmd == nil {
				t.Fatal("a status change was taken for an unchanged frame: no frame scheduled")
			}
			now = m.redraw.pending
			m.Update(renderFrameMsg{m, now})
			if s := ansi.Strip(m.View().Content); !strings.Contains(s, "pane · exited 0") {
				t.Fatalf("the exit never showed:\n%s", s)
			}
		})
	}
}

// A background completion that changes visible rows inside the frame
// budget is held for the frame tick the same Update returns, never dropped:
// the next composed frame shows it with render.max-fps at its default.
func TestRenderBackgroundCompletionShowsInNextFrame(t *testing.T) {
	m, _ := renderFixture(t)
	if fps := m.Runtime.Settings().Int(RenderMaxFPSSetting); fps != 30 {
		t.Fatalf("render.max-fps = %d, want the default 30", fps)
	}
	now := time.Unix(100, 0)
	m.Now = func() time.Time { return now }
	l := m.Runtime.List("rows")
	ticket, _, err := m.Runtime.Issue("rows", "", "", Ctx{})
	if err != nil {
		t.Fatal(err)
	}
	m.Runtime.jobs[ticket.Job].then = func(_ Ctx, v any, _ error) error {
		return l.SetItems([]ListItem{{ID: "three", Noun: "app", Label: v.(string)}})
	}
	m.View()
	now = now.Add(time.Millisecond)
	_, cmd := m.Update(Completion{Ticket: ticket, Value: "Loaded"})
	if cmd == nil || m.redraw.pending.IsZero() {
		t.Fatal("the completion scheduled no frame")
	}
	m.View() // the loop's View after the coalesced Update may reuse the last frame
	now = m.redraw.pending
	m.Update(renderFrameMsg{m, now})
	if s := ansi.Strip(m.View().Content); !strings.Contains(s, "Loaded") {
		t.Fatalf("the loaded rows never showed:\n%s", s)
	}
}

// Each input of the binding walk, written through its API, moves the render
// stamp, and the cached walk, hints and help then agree with the live
// resolver; none is served stale.
func TestRenderStampInvalidatesOnEveryInput(t *testing.T) {
	m, _ := renderFixture(t)
	r := m.Runtime
	r.DefineGuard("stamp-on", func(*Runtime, KeyEvent) bool { return true })
	r.Define("app").Verb("stamp", func(Ctx) Result { return Done(nil) }).Mode("normal").Bind("x", Do("app.stamp"))
	agree := func(name string) {
		t.Helper()
		live := r.scanRowsUncached(r.walk(r.Focus), r.Mode, r.Focus, nil)
		if got := r.levelRows(r.Focus); !reflect.DeepEqual(got, live) {
			t.Fatalf("%s: cached walk differs from the live resolver", name)
		}
		if got, want := HintBindings(r, r.Focus), hintBindingsUncached(r, r.Focus, groupLevelRows(live)); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: cached hints differ from the live resolver", name)
		}
		HintCells(r, r.Focus)
		BindingHelp(r, r.Focus)
	}
	agree("start")
	steps := []struct {
		name  string
		write func()
	}{
		{"bind", func() {
			_ = r.Bind(Binding{Noun: "app", Mode: "normal", Keys: "ctrl+y", Request: Request{Noun: "app", Verb: "stamp"}})
		}},
		{"builder bind", func() { r.Define("app").Mode("normal").Bind("ctrl+u", Do("app.stamp")) }},
		{"builder unbind", func() { r.Define("app").Mode("normal").Unbind("ctrl+u") }},
		{"availability", func() { r.SetAvailability("app.stamp", "stamp-on") }},
		{"operation", func() {
			op := r.Operations.At(Key("app.stamp"))
			op.Motion = true
			r.Operations.Set(op)
		}},
		{"new noun", func() { r.Define("stamp-noun").Verb("v", func(Ctx) Result { return Done(nil) }) }},
		{"mode", func() { r.SetMode("insert") }},
		{"in-place edit", func() {
			r.Nouns["app"].Bindings["normal"][0].Hint = "edited"
			r.InvalidateBindings()
		}},
	}
	for _, step := range steps {
		before := r.renderCache.stamp(r)
		step.write()
		if r.renderCache.stamp(r) == before {
			t.Fatalf("%s: the render stamp did not move", step.name)
		}
		agree(step.name)
	}
}

// TestRenderCachesStandardRowWalks: a walk holding standard verbs' and
// prop rows (every store noun has them) is read once while nothing
// changes, and a change of what their availability reads, the noun's store
// or its props, reads it again with no further call.
func TestRenderCachesStandardRowWalks(t *testing.T) {
	m, _ := renderFixture(t)
	r := m.Runtime
	list := func(Ctx, ListQuery) ([]Thing, error) { return []Thing{{"id": "a"}}, nil }
	get := func(Ctx, string) (Thing, error) { return Thing{"id": "a"}, nil }
	r.Define("stamp-thing").Props(PropSpec{Name: "id", Key: true}).Store(Store{List: list})
	if err := r.Bind(Binding{Noun: "app", Mode: "normal", Keys: "alt+g", Request: Request{Noun: "stamp-thing", Verb: "get"}}); err != nil {
		t.Fatal(err)
	}
	// A prop row naming a prop the noun does not have yet (Bind refuses
	// one, a host's table may hold it).
	app := r.Nouns["app"]
	app.Bindings["normal"] = append(app.Bindings["normal"], Binding{Keys: "alt+t", Request: Request{Noun: "stamp-thing", Prop: "title", Toggle: true}})
	r.InvalidateBindings()
	hinted := func(name, keys string, want bool) {
		t.Helper()
		r.levelRows(r.Focus)
		first := r.renderCache.lastScan
		if r.levelRows(r.Focus); first == nil || r.renderCache.lastScan != first {
			t.Fatalf("%s: a second read with nothing changed walked the rows again", name)
		}
		live := r.scanRowsUncached(r.walk(r.Focus), r.Mode, r.Focus, nil)
		hints := HintBindings(r, r.Focus)
		if want := hintBindingsUncached(r, r.Focus, groupLevelRows(live)); !reflect.DeepEqual(hints, want) {
			t.Fatalf("%s: cached hints differ from the live resolver", name)
		}
		if got := slices.ContainsFunc(hints, func(b Binding) bool { return b.Keys == keys }); got != want {
			t.Fatalf("%s: %s hinted %v, want %v", name, keys, got, want)
		}
	}
	hinted("store without get", "alt+g", false)
	r.Define("stamp-thing").Store(Store{List: list, Get: get})
	hinted("store with get", "alt+g", true)
	hinted("no title prop", "alt+t", false)
	r.Define("stamp-thing").Props(PropSpec{Name: "title", Parameter: Parameter{Type: SettingBoolean}})
	hinted("title prop", "alt+t", true)
}

// TestRenderBindingHelpOncePerFrame: inside a frame BindingHelp reads each
// focus once however often the page asks (a sentence of keys per action),
// and the next frame reads again, so a row bound between frames shows.
func TestRenderBindingHelpOncePerFrame(t *testing.T) {
	r := NewRuntime()
	r.Config().Disable()
	m := NewModel(r)
	var drawn [][]HelpRow
	if _, err := r.AddPage("page", "Page", func(r *Runtime, _, _ int) Block {
		drawn = append(drawn, BindingHelp(r, r.Focus), BindingHelp(r, r.Focus), BindingHelp(r, "app"))
		return Text("page")
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("page"); err != nil {
		t.Fatal(err)
	}
	r.Define("app").Verb("once", func(Ctx) Result { return Done(nil) })
	has := func(rows []HelpRow, key string) bool {
		return slices.ContainsFunc(rows, func(h HelpRow) bool { return h.Key == key })
	}
	m.View()
	if n := len(r.renderCache.helpFrame.rows); n != 2 {
		t.Fatalf("a frame asking for 2 focuses read %d", n)
	}
	if !reflect.DeepEqual(drawn[0], drawn[1]) || !reflect.DeepEqual(drawn[0], r.bindingHelpIn(r.liveResolution(r.Focus))) {
		t.Fatal("the frame's answers differ from a live reading")
	}
	if err := r.Bind(Binding{Noun: "app", Mode: "normal", Keys: "alt+o", Request: Request{Noun: "app", Verb: "once"}}); err != nil {
		t.Fatal(err)
	}
	if !has(BindingHelp(r, r.Focus), "alt+o") {
		t.Fatal("between frames BindingHelp kept the frame's answer")
	}
	drawn = nil
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.View()
	if len(drawn) == 0 || !has(drawn[0], "alt+o") {
		t.Fatal("the next frame kept the last frame's answer")
	}
}

// TestRenderHelpPageRowsFollowTheirInputs: the help page's stored rows,
// read once and kept between opens, are read again after each thing they
// depend on moves: a table (edited in place), the registry (a description), the alias
// names, and an instance holding rows of its own going away.
func TestRenderHelpPageRowsFollowTheirInputs(t *testing.T) {
	m, _ := renderFixture(t)
	r := m.Runtime
	r.Define("app").Verb("stored", func(Ctx) Result { return Done(nil) }, "first words")
	r.Instance("side", Parent("app"))
	if err := r.Bind(Binding{Instance: "side", Mode: "normal", Keys: "alt+s", Request: Request{Noun: "app", Verb: "stored"}}); err != nil {
		t.Fatal(err)
	}
	agree := func(name string) {
		t.Helper()
		walk := r.HelpWalkAt(r.Focus, r.Mode, nil)
		got := r.HelpRows(walk)
		r.renderCache.stored = helpStoredMemo{}
		if want := r.HelpRows(walk); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: kept help rows differ from a fresh reading", name)
		}
	}
	agree("start")
	r.HelpRows(r.HelpWalkAt(r.Focus, r.Mode, nil))
	for _, step := range []struct {
		name  string
		write func()
	}{
		{"table edit", func() {
			r.Instances["side"].Bindings[0].Hint = "edited"
			r.InvalidateBindings()
		}},
		{"description", func() {
			op := r.Operations.At(Key("app.stored"))
			op.Description = "other words"
			r.Operations.Set(op)
		}},
		{"alias", func() { _ = r.AddActionAlias(ActionAlias{Alias: "kept", Action: "app.stored"}) }},
		{"bound instance removed", func() { _ = r.RemoveInstance("side") }},
	} {
		r.HelpRows(r.HelpWalkAt(r.Focus, r.Mode, nil)) // fill the memo
		step.write()
		agree(step.name)
	}
}
