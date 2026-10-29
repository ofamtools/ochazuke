package ochazuke

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// completionRig is a focused input in insert mode with completion
// triggers whose searches record their queries; messages the runtime
// schedules are collected, not delivered, so a test delivers them in the
// order it wants.
type completionRig struct {
	t       *testing.T
	r       *Runtime
	in      *Input
	c       *Completer
	queries []string
	ctxs    []context.Context
	pending []tea.Msg
}

var completionFiles = []string{"src/main.go", "src/main_test.go", "src/model.go", "README.md"}

// filesSearch answers the files holding the query.
func filesSearch(_ context.Context, q string) ([]string, error) {
	var out []string
	for _, f := range completionFiles {
		if strings.Contains(f, q) {
			out = append(out, f)
		}
	}
	return out, nil
}

func newCompletionRig(t *testing.T, text string, multiline bool, extra ...CompletionTrigger) *completionRig {
	t.Helper()
	g := &completionRig{t: t, r: NewRuntime()}
	record := func(search CompletionSearch) CompletionSearch {
		return func(ctx context.Context, q string) ([]Suggestion, error) {
			g.queries = append(g.queries, q)
			g.ctxs = append(g.ctxs, ctx)
			return search(ctx, q)
		}
	}
	triggers := []CompletionTrigger{{Trigger: "@", Search: record(SearchStrings(filesSearch))}}
	for _, tr := range extra {
		if tr.Search != nil {
			tr.Search = record(tr.Search)
		}
		triggers = append(triggers, tr)
	}
	g.in = NewInput("prompt-line", text, multiline)
	if _, err := g.r.AddPage("chat", "Chat", func(r *Runtime, w, h int) Block {
		b := InputBlock(g.in, max(1, w-2), 1)
		return b.Offset(2, 0).WithText("> " + b.Text)
	}); err != nil {
		t.Fatal(err)
	}
	if err := g.r.AddInput(g.in, Parent("chat"), Completions(triggers...)); err != nil {
		t.Fatal(err)
	}
	if err := g.r.OpenPage("chat"); err != nil {
		t.Fatal(err)
	}
	if err := g.r.SetFocus(g.in.ID); err != nil {
		t.Fatal(err)
	}
	g.in.SetMode("insert")
	g.in.Cursor = len(g.in.Text)
	g.c = g.r.Completer()
	g.c.Debounce = time.Nanosecond
	g.drain()
	return g
}

// drain runs what the runtime scheduled (as the Model would after every
// event) and keeps the messages.
func (g *completionRig) drain() {
	var run func(tea.Cmd)
	run = func(cmd tea.Cmd) {
		if cmd == nil {
			return
		}
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				run(c)
			}
			return
		}
		g.pending = append(g.pending, msg)
	}
	run(g.r.PendingCommands())
}

// keys dispatches keys one at a time, draining after each.
func (g *completionRig) keys(keys ...string) {
	g.t.Helper()
	for _, k := range keys {
		if _, err := g.r.Dispatch(MacroKey{Key: k}); err != nil {
			g.t.Fatalf("key %s: %v", k, err)
		}
		g.drain()
	}
}

// typeText types text key by key.
func (g *completionRig) typeText(text string) {
	g.t.Helper()
	for _, ch := range text {
		k := string(ch)
		if ch == ' ' {
			k = "space"
		}
		g.keys(k)
	}
}

// deliver hands one message to the runtime and drains.
func (g *completionRig) deliver(msg tea.Msg) {
	g.t.Helper()
	if _, err := g.r.Deliver(msg); err != nil {
		g.t.Fatalf("deliver %T: %v", msg, err)
	}
	g.drain()
}

// take removes and returns the pending messages of one kind.
func take[T any](g *completionRig) []T {
	var out []T
	var rest []tea.Msg
	for _, msg := range g.pending {
		if v, ok := msg.(T); ok {
			out = append(out, v)
		} else {
			rest = append(rest, msg)
		}
	}
	g.pending = rest
	return out
}

// pause delivers every pending pause timer.
func (g *completionRig) pause() {
	for _, tick := range take[CompletionDebounceMsg](g) {
		g.deliver(tick)
	}
}

// answers delivers every pending search answer in finish order.
func (g *completionRig) answers() {
	for _, done := range take[Completion](g) {
		g.deliver(done)
	}
}

// settle runs the pause and the searches.
func (g *completionRig) settle() { g.pause(); g.answers() }

func (g *completionRig) shown() []string {
	var out []string
	for _, s := range g.c.Suggestions() {
		out = append(out, s.Text)
	}
	return out
}

func TestCompletionOffersSuggestionsWhileTyping(t *testing.T) {
	g := newCompletionRig(t, "", false)
	g.typeText("Please review @src/ma")
	if g.c.Open() {
		t.Fatal("the list opened before the pause")
	}
	g.settle()
	if !g.c.Open() || g.r.Mode.Name != "insert" {
		t.Fatalf("open %v mode %s", g.c.Open(), g.r.Mode.Name)
	}
	if got := g.shown(); !slices.Equal(got, []string{"src/main.go", "src/main_test.go"}) {
		t.Fatalf("suggestions %v", got)
	}
	tok, _ := g.c.Token()
	if tok.Trigger != "@" || tok.Query != "src/ma" {
		t.Fatalf("token %+v", tok)
	}
	// Typing keeps editing the same input while the list is open.
	g.typeText("i")
	if string(g.in.Text) != "Please review @src/mai" || g.r.Focus != g.in.ID {
		t.Fatalf("text %q focus %s", string(g.in.Text), g.r.Focus)
	}
}

func TestCompletionDebounceAsksOnceForTheLastQuery(t *testing.T) {
	g := newCompletionRig(t, "", false)
	g.typeText("@src")
	ticks := take[CompletionDebounceMsg](g)
	if len(ticks) != 4 {
		t.Fatalf("%d pause timers for 4 keys", len(ticks))
	}
	for _, tick := range ticks {
		g.deliver(tick)
	}
	g.answers()
	if !slices.Equal(g.queries, []string{"src"}) {
		t.Fatalf("searches %q, want one for the last query", g.queries)
	}
	if tick, ok := g.c.DebounceTick(); ok {
		t.Fatalf("a pause timer is still current: %+v", tick)
	}
}

func TestCompletionDropsAnswersOlderThanTheShownOne(t *testing.T) {
	g := newCompletionRig(t, "", false)
	g.typeText("@src/m")
	g.pause()
	older := take[Completion](g)
	g.typeText("o")
	g.pause()
	newer := take[Completion](g)
	if len(older) != 1 || len(newer) != 1 || !slices.Equal(g.queries, []string{"src/m", "src/mo"}) {
		t.Fatalf("searches %q", g.queries)
	}
	// The newer answer lands first; the older one, late, is dropped.
	g.deliver(newer[0])
	if got := g.shown(); !slices.Equal(got, []string{"src/model.go"}) {
		t.Fatalf("after the newer answer: %v", got)
	}
	if g.ctxs[0].Err() == nil {
		t.Fatal("the older search was not cancelled once a newer answer showed")
	}
	g.deliver(older[0])
	if got := g.shown(); !slices.Equal(got, []string{"src/model.go"}) {
		t.Fatalf("a stale answer replaced the newer one: %v", got)
	}
}

func TestCompletionShowsAnOlderAnswerUntilTheNewerLands(t *testing.T) {
	g := newCompletionRig(t, "", false)
	g.typeText("@src/m")
	g.pause()
	older := take[Completion](g)
	g.typeText("o")
	g.pause()
	newer := take[Completion](g)
	// In order: the older answer shows (nothing newer is on screen), then
	// the newer one replaces it.
	g.deliver(older[0])
	if got := g.shown(); len(got) != 3 || !g.c.Searching() {
		t.Fatalf("older answer: %v searching %v", got, g.c.Searching())
	}
	g.deliver(newer[0])
	if got := g.shown(); !slices.Equal(got, []string{"src/model.go"}) || g.c.Searching() {
		t.Fatalf("newer answer: %v searching %v", got, g.c.Searching())
	}
}

func TestCompletionAcceptReplacesTheTokenAsOneUndoStep(t *testing.T) {
	g := newCompletionRig(t, "", false)
	g.typeText("Please review @src/ma")
	g.settle()
	g.keys("ctrl+n", "ctrl+n", "tab")
	if got := string(g.in.Text); got != "Please review @src/main_test.go" || g.in.Cursor != len(g.in.Text) {
		t.Fatalf("after accept %q cursor %d", got, g.in.Cursor)
	}
	if g.c.Open() || g.r.Mode.Name != "insert" {
		t.Fatalf("open %v mode %s after accept", g.c.Open(), g.r.Mode.Name)
	}
	// The accepted text does not reopen the list by itself.
	g.pause()
	if g.c.Open() {
		t.Fatal("the list reopened after accept")
	}
	g.keys("esc", "u")
	if got := string(g.in.Text); got != "Please review @src/ma" {
		t.Fatalf("u undid to %q, want the text before the accept", got)
	}
	g.keys("ctrl+r")
	if got := string(g.in.Text); got != "Please review @src/main_test.go" {
		t.Fatalf("ctrl+r redid to %q", got)
	}
}

func TestCompletionMidTokenQueriesAndReplacesTheWholeToken(t *testing.T) {
	g := newCompletionRig(t, "see @src/main now", false)
	g.in.Cursor = strings.Index(string(g.in.Text), "in now")
	g.drain()
	if g.c.Open() {
		t.Fatal("moving into a token opened the list")
	}
	// Tab on a token with the list closed asks at once.
	g.keys("tab")
	if !g.c.Open() || len(take[CompletionDebounceMsg](g)) != 0 {
		t.Fatal("tab did not open the list without a pause")
	}
	g.answers()
	if !slices.Equal(g.queries, []string{"src/main"}) {
		t.Fatalf("query %q, want the whole token", g.queries)
	}
	g.keys("down", "down", "enter")
	if got := string(g.in.Text); got != "see @src/main_test.go now" {
		t.Fatalf("after accept %q", got)
	}
}

func TestCompletionLongestTriggerWins(t *testing.T) {
	single := SearchStrings(func(context.Context, string) ([]string, error) { return []string{"single"}, nil })
	double := SearchStrings(func(context.Context, string) ([]string, error) { return []string{"Double Note"}, nil })
	g := newCompletionRig(t, "", false, CompletionTrigger{Trigger: "[", Search: single}, CompletionTrigger{Trigger: "[[", Search: double})
	g.typeText("[[no")
	g.settle()
	if got := g.shown(); !slices.Equal(got, []string{"Double Note"}) || !slices.Equal(g.queries, []string{"no"}) {
		t.Fatalf("suggestions %v queries %q", got, g.queries)
	}
	g.keys("tab")
	if got := string(g.in.Text); got != "[[Double Note" {
		t.Fatalf("accepted %q", got)
	}
}

func TestCompletionErrorShowsInTheList(t *testing.T) {
	failing := func(context.Context, string) ([]Suggestion, error) { return nil, errors.New("index unavailable") }
	g := newCompletionRig(t, "", false, CompletionTrigger{Trigger: "$", Search: failing})
	m := NewModel(g.r)
	m.Appearance.Theme = "mono"
	m.Width, m.Height = 40, 8
	g.typeText("$sk")
	g.settle()
	if !g.c.Open() || g.c.Err() == nil || len(g.c.Suggestions()) != 0 {
		t.Fatalf("open %v err %v", g.c.Open(), g.c.Err())
	}
	frame := ansi.Strip(m.View().Content)
	if !strings.Contains(frame, "! index unavailable") {
		t.Fatalf("the error is not in the list:\n%s", frame)
	}
	if m.Status != "" {
		t.Fatalf("the error went to the status line too: %q", m.Status)
	}
	// Nothing to accept: enter and tab do not take a row (tab asks again).
	g.keys("tab")
	if string(g.in.Text) != "$sk" {
		t.Fatalf("text %q", string(g.in.Text))
	}
}

func TestCompletionEscapeDismissesAndStaysInInsert(t *testing.T) {
	g := newCompletionRig(t, "", false)
	g.typeText("@src")
	g.settle()
	g.keys("esc")
	if g.c.Open() || g.r.Mode.Name != "insert" || string(g.in.Text) != "@src" {
		t.Fatalf("after esc: open %v mode %s text %q", g.c.Open(), g.r.Mode.Name, string(g.in.Text))
	}
	// A late answer for the dismissed list is dropped.
	g.typeText("/")
	g.pause()
	late := take[Completion](g)
	g.keys("esc")
	g.deliver(late[0])
	if g.c.Open() {
		t.Fatal("an answer reopened a dismissed list")
	}
	// Typing in the token asks again; a second esc leaves insert as usual.
	g.typeText("m")
	g.settle()
	if !g.c.Open() {
		t.Fatal("typing did not reopen the list")
	}
	g.keys("esc", "esc")
	if g.c.Open() || g.r.Mode.Name != "normal" {
		t.Fatalf("open %v mode %s", g.c.Open(), g.r.Mode.Name)
	}
}

func TestCompletionLeavingTheTokenCloses(t *testing.T) {
	g := newCompletionRig(t, "", true)
	g.typeText("@src")
	g.settle()
	// Enter with nothing highlighted is the input's: a new line.
	g.keys("enter")
	if string(g.in.Text) != "@src\n" || g.c.Open() {
		t.Fatalf("text %q open %v", string(g.in.Text), g.c.Open())
	}
	g.typeText("@R")
	g.settle()
	g.keys("space")
	if g.c.Open() {
		t.Fatal("whitespace after the token left the list open")
	}
}

func TestCompletionRowsAreAnInnerLevel(t *testing.T) {
	g := newCompletionRig(t, "", false)
	listed := func() []string {
		var out []string
		for _, b := range g.r.ListBindings(g.in.ID) {
			if b.Request.Noun == CompletionNoun {
				out = append(out, b.Keys+" "+b.Request.Key())
			}
		}
		slices.Sort(out)
		return out
	}
	if got := listed(); len(got) != 0 {
		t.Fatalf("completion rows listed with no token: %v", got)
	}
	g.typeText("@s")
	if got := listed(); !slices.Equal(got, []string{"tab ochazuke.completion.open"}) {
		t.Fatalf("closed list on a token: %v", got)
	}
	g.settle()
	got := listed()
	for _, want := range []string{"esc ochazuke.completion.dismiss", "tab ochazuke.completion.accept", "enter ochazuke.completion.accept", "ctrl+n ochazuke.completion.move", "up ochazuke.completion.move"} {
		if !slices.Contains(got, want) {
			t.Fatalf("%q missing from %v", want, got)
		}
	}
	if hints := BindingHints(g.r, g.in.ID); !strings.Contains(hints, "tab completion.accept") || !strings.Contains(hints, "esc completion.dismiss") {
		t.Fatalf("footer hints %q", hints)
	}
	if _, err := g.r.ValidateBindings(); err != nil {
		t.Fatal(err)
	}
}

func TestCompletionOverlayDrawsUnderTheToken(t *testing.T) {
	g := newCompletionRig(t, "", false)
	m := NewModel(g.r)
	m.Appearance.Theme = "mono"
	m.Width, m.Height = 50, 12
	g.typeText("review @src/ma")
	g.settle()
	g.keys("ctrl+n")
	view := m.View()
	lines := strings.Split(ansi.Strip(view.Content), "\n")
	row := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "review @src/ma") })
	if row < 0 || row+3 >= len(lines) {
		t.Fatalf("no input line:\n%s", strings.Join(lines, "\n"))
	}
	column := func(line, text string) int {
		i := strings.Index(line, text)
		if i < 0 {
			return -1
		}
		return ansi.StringWidth(line[:i])
	}
	token := column(lines[row], "src/ma")
	first := lines[row+2]
	if at := column(first, "src/main.go"); at != token {
		t.Fatalf("suggestion at column %d, token at %d:\n%s", at, token, strings.Join(lines, "\n"))
	}
	if !strings.Contains(lines[row+3], "src/main_test.go") {
		t.Fatalf("second row:\n%s", strings.Join(lines, "\n"))
	}
	if view.Cursor == nil || view.Cursor.Y != row {
		t.Fatalf("the cursor moved off the input: %+v", view.Cursor)
	}
	if !strings.Contains(view.Content, "\x1b[7m") {
		t.Fatal("the highlighted row is not reversed")
	}
}

// DropTrigger: accepting replaces the trigger too, so only the
// suggestion's text stays; the default keeps it.
func TestCompletionDropTrigger(t *testing.T) {
	g := newCompletionRig(t, "", false, CompletionTrigger{Trigger: "\\", DropTrigger: true, Search: SearchStrings(func(context.Context, string) ([]string, error) {
		return []string{"λ"}, nil
	})})
	g.typeText("x \\la")
	g.settle()
	g.keys("tab")
	if string(g.in.Text) != "x λ" {
		t.Fatalf("text %q", string(g.in.Text))
	}
	g.typeText(" @READ")
	g.settle()
	g.keys("tab")
	if string(g.in.Text) != "x λ @README.md" {
		t.Fatalf("the default keeps the trigger: %q", string(g.in.Text))
	}
}

// Boundaries: a trigger's token also ends at its boundary characters, so
// a jq "." after "|" or "(" completes; whitespace stays a boundary; a
// trigger without boundaries is as before.
func TestCompletionTriggerBoundaries(t *testing.T) {
	jq := []CompletionTrigger{{Trigger: ".", Boundaries: "|(),"}, {Trigger: "@"}}
	for _, tc := range []struct{ marked, token, query string }{
		{".a|.b^", ".b", "b"},
		{"map(.na^me)", ".name", "name"},
		{".x, .y^", ".y", "y"},
		{"a @fi^le", "@file", "file"},
	} {
		text, cursor := caretAt(tc.marked)
		tok, _, ok := FindCompletionTokenIn(text, cursor, jq)
		if !ok || string(text[tok.Start:tok.End]) != tc.token || tok.Query != tc.query {
			t.Fatalf("%q: %+v %v", tc.marked, tok, ok)
		}
	}
	// "@" has no boundaries: "a|@file" is one token that does not start
	// with it.
	text, cursor := caretAt("a|@fi^")
	if _, _, ok := FindCompletionTokenIn(text, cursor, jq); ok {
		t.Fatal("a trigger without boundaries split at |")
	}
	if err := validTriggers([]CompletionTrigger{{Trigger: "(", Boundaries: "(", Search: SearchStrings(filesSearch)}}); err == nil {
		t.Fatal("a trigger holding its own boundary is refused")
	}
}

// SearchAt is given the whole text and the cursor besides the query, so a
// source can complete a document path before the token.
func TestCompletionSearchAtSeesTheText(t *testing.T) {
	var got []CompletionQuery
	g := newCompletionRig(t, "", false, CompletionTrigger{Trigger: ".", Boundaries: "|", SearchAt: func(_ context.Context, q CompletionQuery) ([]Suggestion, error) {
		got = append(got, q)
		return []Suggestion{{Text: "name"}}, nil
	}})
	g.typeText(".items[]|.na")
	g.settle()
	if len(got) == 0 {
		t.Fatal("SearchAt was not asked")
	}
	last := got[len(got)-1]
	if last.Query != "na" || last.Text != ".items[]|.na" || last.Cursor != 12 || last.Token.Start != 9 {
		t.Fatalf("query %+v", last)
	}
	g.keys("tab")
	if string(g.in.Text) != ".items[]|.name" {
		t.Fatalf("text %q", string(g.in.Text))
	}
}

// caretAt is text with ^ marking the cursor (| is jq's pipe here).
func caretAt(marked string) ([]rune, int) {
	i := strings.Index(marked, "^")
	return []rune(marked[:i] + marked[i+1:]), len([]rune(marked[:i]))
}
