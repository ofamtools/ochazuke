package ochazuke

import (
	"fmt"
	"image/color"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke/keytest"
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
	opWithArg := r.Operations["app.cache-test"]
	opWithArg.Parameters = map[string]Parameter{"n": {Type: "integer"}}
	r.Operations[opWithArg.Key()] = opWithArg
	check := func() {
		t.Helper()
		got := HintBindings(r, r.Focus)
		want := hintBindingsUncached(r, r.Focus, groupLevelRows(r.scanRowsUncached(r.walk(r.Focus), r.Mode, r.Focus, false)))
		if !reflect.DeepEqual(got, want) {
			t.Fatal("cached hints disagree with the live resolver")
		}
		if got, want := r.levelRows(r.Focus), r.scanRowsUncached(r.walk(r.Focus), r.Mode, r.Focus, false); !reflect.DeepEqual(got, want) {
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
	check()
	r.Nouns["app"].Guards["cache-test"] = "cache-test"
	check()
	if err := r.Bind(Binding{Noun: "app", Mode: "normal", Keys: "x", Request: Request{Noun: "app", Verb: "cache-test", Args: Arguments{"n": 2}}}); err != nil {
		t.Fatal(err)
	}
	check()
	check()
	// Public table edits must invalidate too, including nested arguments.
	for i := range r.Nouns["app"].Bindings["normal"] {
		b := &r.Nouns["app"].Bindings["normal"][i]
		if b.Keys == "x" {
			b.Request.Args["n"] = 3
		}
	}
	check()
	op := r.Operations["app.cache-test"]
	op.Motion = true
	r.Operations[op.Key()] = op
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
	if err := r.Alias("thing", "long.thing"); err != nil {
		t.Fatal(err)
	}
	if err := r.Bind(Binding{Noun: "app", Mode: "normal", Keys: "x", Request: Request{Noun: "long.thing", Verb: "open"}}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"thing.open", "long.thing.open"} {
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
		r.DefineOwn("thing")
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
