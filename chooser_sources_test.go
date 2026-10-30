package ochazuke

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/unixsuperhero/ochazuke/keytest"
)

// landLoads runs the commands a key queued and feeds the messages that
// come back within a moment (job completions) to the model, again for
// what those queue; a timer that does not fire soon is left alone.
func landLoads(m *Model, cmd tea.Cmd) {
	for range 10 {
		if cmd == nil {
			return
		}
		var next []tea.Cmd
		for _, msg := range quickMsgs(cmd) {
			_, out := m.Update(msg)
			next = append(next, out)
		}
		cmd = tea.Batch(next...)
	}
}

// quickMsgs runs cmd (a batch: each child) and keeps the messages that
// arrive within 300ms.
func quickMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	select {
	case msg := <-ch:
		if batch, ok := msg.(tea.BatchMsg); ok {
			var out []tea.Msg
			for _, child := range batch {
				out = append(out, quickMsgs(child)...)
			}
			return out
		}
		if _, ok := msg.(Completion); ok {
			return []tea.Msg{msg}
		}
		return nil
	case <-time.After(300 * time.Millisecond):
		return nil
	}
}

func pressLand(m *Model, keys ...string) {
	for _, k := range keys {
		landLoads(m, keytest.Press(m, k))
	}
}

func typeText(m *Model, text string) {
	landLoads(m, keytest.Type(m, text))
}

func rowsSource(name string, labels ...string) ChooserSource {
	return ChooserSource{Name: name, Load: func(string) Result {
		rows := make([]ChooserRow, len(labels))
		for i, l := range labels {
			rows[i] = ChooserRow{ID: name + ":" + l, Label: l}
		}
		return Done(rows)
	}}
}

func labels(rows []ChooserRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Label
	}
	return out
}

func sourcesFixture(t *testing.T, kind string, sources ...ChooserSource) (*Runtime, *Model, *[]string) {
	t.Helper()
	r := NewRuntime()
	m := NewModel(r)
	m.Width, m.Height = 100, 30
	m.Appearance.Theme = "mono"
	var picked []string
	open := func() {
		if _, err := r.OpenChooser(ChooserSpec{Kind: kind, Title: "Pick", Sources: sources, Choose: func(row ChooserRow) Result {
			picked = append(picked, row.ID)
			return Done(nil)
		}}); err != nil {
			t.Fatal(err)
		}
	}
	open()
	r.Define("test").Verb("reopen", func(Ctx) Result { open(); return Done(nil) })
	return r, m, &picked
}

// ctrl+o and alt+o cycle the sources, wrapping; the query carries over
// and the title names the current source in brackets and the others.
func TestChooserSourcesCycleKeepingTheQuery(t *testing.T) {
	r, m, picked := sourcesFixture(t, "cyc", rowsSource("one", "alpha", "beta"), rowsSource("two", "alpine", "gamma"), rowsSource("three", "delta"))
	c := r.Chooser("cyc")
	if c.Source() != "one" || c.Title() != "Pick · [one] two three" {
		t.Fatalf("source %q title %q", c.Source(), c.Title())
	}
	typeText(m, "al")
	if got := labels(c.Matches()); !slices.Equal(got, []string{"alpha"}) {
		t.Fatalf("one al: %v", got)
	}
	pressLand(m, "ctrl+o")
	if c.Source() != "two" || c.Query() != "al" || !slices.Equal(labels(c.Matches()), []string{"alpine"}) {
		t.Fatalf("after ctrl+o: source %q query %q rows %v", c.Source(), c.Query(), labels(c.Matches()))
	}
	if !strings.Contains(screen(m), "[two]") {
		t.Fatalf("the title shows the current source:\n%s", screen(m))
	}
	pressLand(m, "ctrl+o", "ctrl+o")
	if c.Source() != "one" {
		t.Fatalf("ctrl+o wraps: %q", c.Source())
	}
	pressLand(m, "alt+o")
	if c.Source() != "three" {
		t.Fatalf("alt+o wraps back: %q", c.Source())
	}
	pressLand(m, "alt+o", "enter")
	if !slices.Equal(*picked, []string{"two:alpine"}) {
		t.Fatalf("picked %v", *picked)
	}
}

// A chooser with one source (or none) leaves ctrl+o alone: the guard
// keeps next-source unavailable.
func TestChooserOneSourceTabDoesNothing(t *testing.T) {
	r, m, _ := sourcesFixture(t, "solo", rowsSource("one", "alpha"))
	pressLand(m, "ctrl+o")
	if c := r.Chooser("solo"); c == nil || c.Source() != "one" {
		t.Fatal("ctrl+o changed a one-source chooser")
	}
}

// A Live source loads again for every edit of the query and its rows show
// as given (the chooser does not filter them).
func TestChooserLiveSourceSeesTheTypedText(t *testing.T) {
	var asked []string
	live := ChooserSource{Name: "live", Live: true, Load: func(q string) Result {
		asked = append(asked, q)
		return Done([]ChooserRow{{ID: "echo", Label: "you typed " + q}, {ID: "x", Label: "unrelated"}})
	}}
	r, m, _ := sourcesFixture(t, "live", live)
	typeText(m, "ab")
	if !slices.Equal(asked, []string{"", "a", "ab"}) {
		t.Fatalf("asked %q", asked)
	}
	if got := labels(r.Chooser("live").Matches()); !slices.Equal(got, []string{"you typed ab", "unrelated"}) {
		t.Fatalf("rows %v", got)
	}
}

// A Pending load runs off the loop: loading… meanwhile, then its rows; a
// newer load replaces an older one.
func TestChooserSourcePendingLoad(t *testing.T) {
	src := ChooserSource{Name: "slow", Live: true, Load: func(q string) Result {
		return Pending(func(context.Context) (any, error) {
			return []ChooserRow{{ID: q, Label: "row " + q}}, nil
		})
	}}
	r, m, _ := sourcesFixture(t, "slow", src)
	c := r.Chooser("slow")
	if !c.Loading() || !strings.Contains(screen(m), "loading…") {
		t.Fatalf("loading %v:\n%s", c.Loading(), screen(m))
	}
	typeText(m, "q")
	if c.Loading() || !slices.Equal(labels(c.Matches()), []string{"row q"}) {
		t.Fatalf("loading %v rows %v", c.Loading(), labels(c.Matches()))
	}
}

// The source last used is remembered per kind while chooser.remember-source
// is on, and forgotten when it is off.
func TestChooserRemembersTheSource(t *testing.T) {
	for _, on := range []bool{true, false} {
		r, m, _ := sourcesFixture(t, "mem", rowsSource("one", "a"), rowsSource("two", "b"))
		r.EnsureConfigSettings()
		if err := r.Settings().Set(ChooserRememberSourceSetting, on); err != nil {
			t.Fatal(err)
		}
		pressLand(m, "ctrl+o", "esc")
		if r.ChooserLastSource("mem") != "two" {
			t.Fatalf("last source %q", r.ChooserLastSource("mem"))
		}
		if _, err := r.Execute(Request{Noun: "test", Verb: "reopen"}); err != nil {
			t.Fatal(err)
		}
		want := "one"
		if on {
			want = "two"
		}
		if got := r.Chooser("mem").Source(); got != want {
			t.Fatalf("remember %v: reopened on %q, want %q", on, got, want)
		}
	}
}

// querySource defines files.complete, a choices source that declares a
// query parameter and answers with the query's completions.
func querySource(r *Runtime, asked *[]string) {
	r.Define("files").Verb("complete", func(ctx Ctx) Result {
		q, _ := ctx.Args[ChoicesQueryParameter].(string)
		*asked = append(*asked, q)
		return Done([]string{q, q + "-one", q + "-two"})
	})
	op := r.Operations["files.complete"]
	op.Parameters = map[string]Parameter{ChoicesQueryParameter: {Type: "string"}}
	r.Operations["files.complete"] = op
}

// A choices source that declares a query parameter sees the typed text:
// ChoicesFor passes it, the args check passes the value checked, and a
// source without the parameter is called with none.
func TestChoicesSeeTheTypedText(t *testing.T) {
	r := NewRuntime()
	var asked []string
	querySource(r, &asked)
	p := Parameter{Type: "string", Source: "files.complete"}
	if !r.ChoicesTakeQuery(p) {
		t.Fatal("files.complete declares query")
	}
	got, err := r.ChoicesFor(r.Context(), p, "ab")
	if err != nil || len(got) != 3 || got[1].ID != "ab-one" {
		t.Fatalf("choices %v %v", got, err)
	}
	r.Define("plain").Verb("list", func(ctx Ctx) Result {
		if len(ctx.Args) != 0 {
			t.Fatalf("a source without query got %v", ctx.Args)
		}
		return Done([]string{"x"})
	})
	if r.ChoicesTakeQuery(Parameter{Source: "plain.list"}) {
		t.Fatal("plain.list declares no query")
	}
	if _, err := r.ChoicesFor(r.Context(), Parameter{Source: "plain.list"}, "zz"); err != nil {
		t.Fatal(err)
	}
	r.Define("doc").Verb("open", func(Ctx) Result { return Done(nil) })
	op := r.Operations["doc.open"]
	op.Parameters = map[string]Parameter{"name": p}
	r.Operations["doc.open"] = op
	if _, err := r.Execute(Request{Noun: "doc", Verb: "open", Args: Arguments{"name": "q-two"}}); err != nil || !slices.Contains(asked, "q-two") {
		t.Fatalf("the checked value is the query: %v (asked %v)", err, asked)
	}
}

// In a form, the suggestions chooser of a field whose source sees the
// query loads again as the user types (completion as you type).
func TestFormSuggestionsFollowTheQuery(t *testing.T) {
	r, m, ed, writes := suggestFixture(t, "files.complete")
	var asked []string
	querySource(r, &asked)
	pressLand(m, "enter")
	if r.TopModal() != FormSuggestionsKind {
		t.Fatalf("top %q", r.TopModal())
	}
	typeText(m, "cfg")
	if !slices.Equal(asked, []string{"", "c", "cf", "cfg"}) {
		t.Fatalf("asked %q", asked)
	}
	if got := labels(r.Chooser(FormSuggestionsKind).Matches()); !slices.Equal(got[:3], []string{"cfg", "cfg-one", "cfg-two"}) {
		t.Fatalf("rows %v", got)
	}
	pressLand(m, "ctrl+n", "ctrl+n", "enter")
	if ed.Values()["model"] != "cfg-two" || !slices.Equal(*writes, []string{"write model=cfg-two"}) {
		t.Fatalf("values %v writes %v", ed.Values(), *writes)
	}
}

// The palette's word reaches a source that sees the query.
func TestPaletteChoicesSeeTheWord(t *testing.T) {
	r := NewRuntime()
	m := NewModel(r)
	var asked []string
	querySource(r, &asked)
	r.Define("doc").Verb("open", func(Ctx) Result { return Done(nil) })
	op := r.Operations["doc.open"]
	op.Parameters = map[string]Parameter{"name": {Type: "string", Required: true, Source: "files.complete"}}
	r.Operations["doc.open"] = op
	if err := m.Palette.Open(); err != nil {
		t.Fatal(err)
	}
	typeText(m, "doc open zz")
	m.View()
	if !slices.Contains(asked, "zz") {
		t.Fatalf("asked %q", asked)
	}
}
