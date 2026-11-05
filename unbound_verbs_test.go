package ochazuke

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"ofam.dev/ochazuke/keytest"
)

// unboundFixture is helpFixture's widget with two verbs no row binds: ping
// (plain) and wipe (asks first).
func unboundFixture(t *testing.T) (*Runtime, *Model, *[]string) {
	t.Helper()
	t.Parallel()
	r := NewRuntime()
	var ran []string
	noop := func(Ctx) Result { return Done(nil) }
	r.Define("widget").
		Verb("open", noop, "Open the widget").
		Verb("ping", func(Ctx) Result { ran = append(ran, "ping"); return Done(nil) }, "Ping the widget").
		Verb("wipe", func(Ctx) Result { ran = append(ran, "wipe"); return Done(nil) }, "Wipe the widget").Confirm("Wipe it?").Destructive().
		Mode("normal").
		Bind("enter", Do("widget.open"))
	if _, err := r.AddPage("main", "Main", func(r *Runtime, w, h int) Block { return Text(r.List("widgets").View(w, h)) }); err != nil {
		t.Fatal(err)
	}
	list, err := r.AddList("widgets", []ListItem{{ID: "w1", Label: "one", Noun: "widget"}}, Parent("main"))
	if err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	if err := r.OpenPage("main"); err != nil {
		t.Fatal(err)
	}
	if err := list.Focus(); err != nil {
		t.Fatal(err)
	}
	m.Width, m.Height = 80, 24
	return r, m, &ran
}

func unboundOps(rows []HelpRow) []string {
	var out []string
	for _, row := range rows {
		if row.Unbound {
			out = append(out, row.Operation)
		}
	}
	return out
}

// The help lists the focused noun's verbs that no row binds, key blank, in
// the noun's "unbound" section; bound verbs are not repeated there.
func TestHelpListsUnboundVerbsOfTheFocus(t *testing.T) {
	r, _, _ := unboundFixture(t)
	rows := BindingHelp(r, "w1")
	if got := unboundOps(rows); !slices.Equal(got, []string{"widget.ping", "widget.wipe"}) {
		t.Fatalf("unbound: %v", got)
	}
	for _, row := range rows {
		if row.Unbound && (row.Key != "" || row.Section != "widget · unbound" || row.Noun != "widget") {
			t.Fatalf("row %+v", row)
		}
		if row.Unbound && row.Operation == "widget.wipe" && !strings.Contains(row.Description, "asks first") {
			t.Fatalf("wipe description %q", row.Description)
		}
	}
	// The noun's own keys stay first: the unbound block follows them.
	if rows[0].Operation != "widget.open" {
		t.Fatalf("first row %+v", rows[0])
	}
	if err := r.Settings().Set(UnboundVerbsSetting, false); err != nil {
		t.Fatal(err)
	}
	if got := unboundOps(BindingHelp(r, "w1")); len(got) != 0 {
		t.Fatalf("setting off still lists %v", got)
	}
}

// A verb bound anywhere on the walk, in any table, is not unbound; binding
// one takes it off the list.
func TestUnboundVerbsFollowTheBindings(t *testing.T) {
	r, _, _ := unboundFixture(t)
	if err := r.Bind(Binding{Noun: "widget", Mode: "normal", Keys: "p", Request: Request{Noun: "widget", Verb: "ping"}}); err != nil {
		t.Fatal(err)
	}
	if got := unboundOps(BindingHelp(r, "w1")); !slices.Equal(got, []string{"widget.wipe"}) {
		t.Fatalf("unbound: %v", got)
	}
}

func selectHelpRow(t *testing.T, r *Runtime, operation string) {
	t.Helper()
	tree := r.Tree(helpTreeID)
	for _, n := range tree.Nodes() {
		if row, ok := n.Data.(HelpRow); ok && row.Unbound && row.Operation == operation {
			if err := tree.Select(n.ID); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatalf("no unbound row %s", operation)
}

// enter on an unbound row closes the help and runs the verb on the thing it
// opened over; one that asks first asks, as its key would.
func TestEnterRunsAnUnboundVerb(t *testing.T) {
	r, m, ran := unboundFixture(t)
	keytest.Press(m, "?")
	selectHelpRow(t, r, "widget.ping")
	keytest.Press(m, "enter")
	if r.TopModal() == helpID || !slices.Equal(*ran, []string{"ping"}) {
		t.Fatalf("modal %q, ran %v, status %q", r.TopModal(), *ran, m.Status)
	}
	keytest.Press(m, "?")
	selectHelpRow(t, r, "widget.wipe")
	keytest.Press(m, "enter")
	if len(*ran) != 1 || r.TopModal() == "" {
		t.Fatalf("wipe ran without asking: ran %v, modal %q", *ran, r.TopModal())
	}
	keytest.Press(m, "y")
	if !slices.Equal(*ran, []string{"ping", "wipe"}) {
		t.Fatalf("after confirming: %v", *ran)
	}
}

// enter runs any operation row, bound or not, as one rule; a row with
// children (a prefix) folds instead and runs nothing.
func TestEnterRunsABoundRowAndFoldsAPrefix(t *testing.T) {
	r, m, ran := unboundFixture(t)
	for _, b := range []Binding{
		{Noun: "widget", Mode: "normal", Keys: "p", Request: Request{Noun: "widget", Verb: "ping"}},
		{Noun: "widget", Mode: "normal", Keys: "g a", Request: Request{Noun: "widget", Verb: "ping"}},
		{Noun: "widget", Mode: "normal", Keys: "g b", Request: Request{Noun: "widget", Verb: "ping"}},
	} {
		if err := r.Bind(b); err != nil {
			t.Fatal(err)
		}
	}
	selectKey := func(key string) {
		t.Helper()
		tree := r.Tree(helpTreeID)
		for _, n := range tree.Nodes() {
			if row, ok := n.Data.(HelpRow); ok && row.Key == key && row.Operation != "" || ok && row.Key == key && row.Group {
				if err := tree.Select(n.ID); err != nil {
					t.Fatal(err)
				}
				return
			}
		}
		t.Fatalf("no row for %q", key)
	}
	keytest.Press(m, "?")
	selectKey("g")
	keytest.Press(m, "enter")
	if r.TopModal() != helpID || len(*ran) != 0 {
		t.Fatalf("enter on a prefix: modal %q ran %v", r.TopModal(), *ran)
	}
	selectKey("p")
	keytest.Press(m, "enter")
	if r.TopModal() == helpID || !slices.Equal(*ran, []string{"ping"}) {
		t.Fatalf("enter on a bound row: modal %q ran %v", r.TopModal(), *ran)
	}
}

// bindings.bind writes the row to the user's file, loads it, and the key
// runs the verb; a key that answers something else needs replace, and the
// old file is kept as .bak.
func TestBindingsBindWritesTheUserFile(t *testing.T) {
	r, m, ran := unboundFixture(t)
	dir := t.TempDir()
	r.Config().Dir = dir
	if _, err := r.Execute(Request{Noun: "ochazuke.bindings", Verb: "bind", Args: Arguments{"key": "p", "action": "widget.ping"}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, BindingsFile))
	if err != nil || !strings.Contains(string(data), `"key": "p"`) || !strings.Contains(string(data), "widget.ping") {
		t.Fatalf("file %q, %v", data, err)
	}
	keytest.Press(m, "p")
	if !slices.Equal(*ran, []string{"ping"}) {
		t.Fatalf("p ran %v, status %q", *ran, m.Status)
	}
	if got := unboundOps(BindingHelp(r, "w1")); !slices.Equal(got, []string{"widget.wipe"}) {
		t.Fatalf("unbound after bind: %v", got)
	}
	// enter is widget.open: taking it needs replace.
	q := Request{Noun: "ochazuke.bindings", Verb: "bind", Args: Arguments{"key": "enter", "action": "widget.wipe"}}
	if _, err := r.Execute(q); err != nil || r.TopModal() == "" || !strings.Contains(m.View().Content, "enter already runs widget.open here; bind anyway?") {
		t.Fatalf("no question: %v top %q", err, r.TopModal())
	}
	keytest.Press(m, "n")
	q.Args["replace"] = true
	if _, err := r.Execute(q); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, BindingsFile+".bak")); err != nil {
		t.Fatal(err)
	}
	// A bad key is refused before anything is written.
	before, _ := os.ReadFile(filepath.Join(dir, BindingsFile))
	if _, err := r.Execute(Request{Noun: "ochazuke.bindings", Verb: "bind", Args: Arguments{"key": "", "action": "widget.ping"}}); err == nil {
		t.Fatal("empty key accepted")
	}
	if after, _ := os.ReadFile(filepath.Join(dir, BindingsFile)); string(after) != string(before) {
		t.Fatal("file changed by a refused bind")
	}
}

// b on an unbound help row waits for the next key and binds it to the
// selected verb: the file is written, the key runs the verb and the help
// lists it no more. esc cancels.
func TestHelpBindCapturesTheNextKey(t *testing.T) {
	r, m, ran := unboundFixture(t)
	r.Config().Dir = t.TempDir()
	keytest.Press(m, "?")
	selectHelpRow(t, r, "widget.ping")
	keytest.Press(m, "b")
	if r.Instances[ArgsFormID] != nil || r.Mode.Name != "help_bind_pending" {
		t.Fatalf("mode %q, form %v", r.Mode.Name, r.Instances[ArgsFormID] != nil)
	}
	keytest.Press(m, "esc")
	if r.Mode.Name == "help_bind_pending" || r.TopModal() != helpID {
		t.Fatalf("after esc: mode %q top %q", r.Mode.Name, r.TopModal())
	}
	if _, err := os.Stat(filepath.Join(r.Config().Dir, BindingsFile)); err == nil {
		t.Fatal("esc wrote the file")
	}
	keytest.Press(m, "b")
	keytest.Press(m, "p")
	if got := unboundOps(BindingHelp(r, "w1")); !slices.Equal(got, []string{"widget.wipe"}) {
		t.Fatalf("unbound after the key: %v (status %q, top %q)", got, m.Status, r.TopModal())
	}
	keytest.Press(m, "esc")
	keytest.Press(m, "p")
	if !slices.Equal(*ran, []string{"ping"}) {
		t.Fatalf("p ran %v", *ran)
	}
}

// A key the walk at the focus already answers, at any level, is not taken
// over without a yes: the question names what answers it now. j and esc
// are the page's, not the widget's own table.
func TestHelpBindAsksBeforeTakingAnAnsweredKey(t *testing.T) {
	r, m, ran := unboundFixture(t)
	r.Config().Dir = t.TempDir()
	r.Define("widget").Mode("normal").Bind("x", Do("widget.ping"))
	keytest.Press(m, "?")
	selectHelpRow(t, r, "widget.wipe")
	keytest.Press(m, "b")
	keytest.Press(m, "j")
	if top := r.TopModal(); top == helpID || top == "" {
		t.Fatalf("no confirm: top %q", top)
	} else if view := m.View().Content; !strings.Contains(view, "j already runs list.move here; bind anyway?") {
		t.Fatalf("the question does not name what answers j now:\n%s", view)
	}
	if _, err := os.Stat(filepath.Join(r.Config().Dir, BindingsFile)); err == nil {
		t.Fatal("the file was written before the yes")
	}
	keytest.Press(m, "n")
	if _, err := os.Stat(filepath.Join(r.Config().Dir, BindingsFile)); err == nil {
		t.Fatal("no wrote the file")
	}
	keytest.Press(m, "b")
	keytest.Press(m, "j")
	keytest.Press(m, "y")
	if data, err := os.ReadFile(filepath.Join(r.Config().Dir, BindingsFile)); err != nil || !strings.Contains(string(data), `"key": "j"`) {
		t.Fatalf("yes did not bind j: %q %v", data, err)
	}
	_ = ran
	// Every route asks: the verb itself opens the same question, and
	// replace skips it.
	for _, keys := range []string{"j", "esc", ":", "Q"} {
		if _, err := r.Execute(Request{Noun: "ochazuke.bindings", Verb: "bind", Args: Arguments{"key": keys, "action": "widget.wipe"}}); err != nil {
			t.Fatalf("%s: %v", keys, err)
		}
		if r.TopModal() == "" || !strings.Contains(m.View().Content, keys+" already runs") {
			t.Fatalf("%s: no question (top %q)", keys, r.TopModal())
		}
		keytest.Press(m, "n")
	}
	_ = ran
	if _, err := r.Execute(Request{Noun: "ochazuke.bindings", Verb: "bind", Args: Arguments{"key": "esc", "action": "widget.wipe", "replace": true}}); err != nil || r.TopModal() != helpID {
		t.Fatalf("replace asked: %v top %q", err, r.TopModal())
	}
}

// A form opened over a modal that fails to run stays open with what was
// typed and one plain sentence (the general rule, not the bind form's).
func TestFormOverAModalReopensOnAFailedRun(t *testing.T) {
	r, m, _ := unboundFixture(t)
	r.Config().Dir = t.TempDir()
	keytest.Press(m, "?")
	r.Register(Operation{Verb: "fail", Noun: "widget", Parameters: map[string]Parameter{"key": {Type: "string", Required: true}}, Run: func(*Runtime, Context, Arguments) (any, error) {
		return nil, errors.New("j already runs list.move here")
	}})
	q := Request{Noun: "widget", Verb: "fail"}
	if _, err := r.OpenArgsForm(q, func(q Request) error {
		_, err := r.ExecuteAt(r.Context(), q)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	keytest.Type(m, "j")
	keytest.Press(m, "enter")
	if r.TopModal() != ArgsFormID {
		t.Fatalf("the form did not stay: top %q status %q", r.TopModal(), m.Status)
	}
	ed := r.RecordEditor(ArgsFormID)
	if got := ed.Values()["key"]; got != "j" {
		t.Fatalf("typed key lost: %v", got)
	}
	if strings.Contains(m.Status, "\n") || strings.Contains(m.Status, "modal is open") {
		t.Fatalf("status %q", m.Status)
	}
}

// An agent runs an unbound verb under its right, as at a key: a destructive
// verb is refused headless, with nothing run.
func TestRunUnboundKeepsAgentRights(t *testing.T) {
	r, _, ran := unboundFixture(t)
	a := testAgent("p")
	restore := r.BindActor(a, false)
	defer restore()
	err := r.RunUnbound("w1", Request{Noun: "widget", Verb: "wipe", Actor: &a})
	var refused *RefusedError
	if !errors.As(err, &refused) || len(*ran) != 0 {
		t.Fatalf("err %v ran %v", err, *ran)
	}
}

// A row requesting list.apply {verb: ping} binds ping: the operation
// declares which argument names the verb it forwards (Parameter.Forwards),
// so the unbound list counts it with no check of the operation's name.
func TestUnboundVerbsCountAForwardedVerb(t *testing.T) {
	r, _, _ := unboundFixture(t)
	row := Binding{Noun: "widget", Mode: "normal", Keys: "p", Request: Request{Noun: "ochazuke.list", Verb: "apply", Args: Arguments{"verb": "ping"}}}
	if err := r.Bind(row); err != nil {
		t.Fatal(err)
	}
	if got := unboundOps(BindingHelp(r, "w1")); !slices.Equal(got, []string{"widget.wipe"}) {
		t.Fatalf("unbound: %v", got)
	}
}

// The scope setting decides whose verbs list: focus the focused noun's
// own, chain the nouns out to the page, all also the framework's. A bound
// verb is in none of them.
func TestUnboundScopesListOnlyUnboundVerbs(t *testing.T) {
	r, _, _ := unboundFixture(t)
	r.Define("widget").Mode("normal").Bind("p", Do("widget.ping"))
	count := func(scope string) (own, framework, bound int) {
		if err := r.Settings().Set(UnboundScopeSetting, scope); err != nil {
			t.Fatal(err)
		}
		for _, row := range BindingHelp(r, "w1") {
			switch {
			case !row.Unbound:
			case row.Operation == "widget.ping":
				bound++
			case row.Noun == "widget":
				own++
			default:
				framework++
			}
		}
		return
	}
	for _, scope := range []string{UnboundFocus, UnboundChain, UnboundAll} {
		own, _, bound := count(scope)
		if own != 1 || bound != 0 {
			t.Fatalf("scope %s: own %d, bound listed %d", scope, own, bound)
		}
	}
	if _, framework, _ := count(UnboundFocus); framework != 0 {
		t.Fatalf("focus listed %d other verbs", framework)
	}
}

// A bind refreshes the open help, which keeps what the reader folded: the
// state belongs to the row, not to its place in the list.
func TestHelpRefreshKeepsFoldState(t *testing.T) {
	r, m, _ := unboundFixture(t)
	r.Config().Dir = t.TempDir()
	for _, keys := range []string{"g a", "g b"} {
		if err := r.Bind(Binding{Noun: "widget", Mode: "normal", Keys: keys, Request: Request{Noun: "widget", Verb: "open"}}); err != nil {
			t.Fatal(err)
		}
	}
	keytest.Press(m, "?")
	tree := r.Tree(helpTreeID)
	group := ""
	for _, n := range tree.Nodes() {
		if row, ok := n.Data.(HelpRow); ok && row.Group && row.Key == "g" {
			group = n.ID
		}
	}
	if group == "" {
		t.Fatal("no g prefix row")
	}
	if err := tree.SetCollapsed(group, true); err != nil {
		t.Fatal(err)
	}
	folded := func() map[string]bool {
		out := map[string]bool{}
		for _, n := range tree.Nodes() {
			if row, ok := n.Data.(HelpRow); ok {
				out[row.Section+"|"+row.Key+"|"+row.Operation] = tree.Collapsed(n.ID)
			}
		}
		return out
	}
	before := folded()
	if _, err := r.Execute(Request{Noun: "ochazuke.bindings", Verb: "bind", Args: Arguments{"key": "p", "action": "widget.ping"}}); err != nil {
		t.Fatal(err)
	}
	after := folded()
	for id, was := range before {
		if now, ok := after[id]; ok && now != was {
			t.Fatalf("row %q folded %v before the bind, %v after", id, was, now)
		}
	}
}

func sendFixture(t *testing.T) (*Runtime, *Model, *[]Arguments) {
	t.Helper()
	r, m, _ := unboundFixture(t)
	var got []Arguments
	r.Register(Operation{Verb: "send", Noun: "widget", Kind: KindApplication, Description: "Send the widget", Parameters: map[string]Parameter{
		"to":   {Type: "string", Required: true, Description: "Where to"},
		"note": {Type: "string", Description: "A note"},
	}, Positional: []string{"to", "note"}, Result: "JSON value", Run: func(_ *Runtime, _ Context, a Arguments) (any, error) {
		got = append(got, a)
		return map[string]any{"sent": a["to"]}, nil
	}})
	return r, m, &got
}

// A required argument no source fills opens the form from the help's run
// (enter), and the form's run goes through the same path as a key's; the
// verb's data answer is shown, not dropped.
func TestHelpRunAsksForARequiredArgument(t *testing.T) {
	r, m, got := sendFixture(t)
	sink := &RecordingSink{}
	r.UI().Attach(sink)
	keytest.Press(m, "?")
	selectHelpRow(t, r, "widget.send")
	keytest.Press(m, "enter")
	if r.TopModal() != ArgsFormID {
		t.Fatalf("no form: top %q status %q", r.TopModal(), m.Status)
	}
	keytest.Type(m, "mars")
	keytest.Press(m, "enter")
	if len(*got) != 1 || (*got)[0]["to"] != "mars" {
		t.Fatalf("ran %v (status %q, top %q)", *got, m.Status, r.TopModal())
	}
	if !slices.Contains(sink.Notices(), `widget.send: {"sent":"mars"}`) {
		t.Fatalf("the answer was not shown: %q", sink.Notices())
	}
}

// a opens the form for any verb with parameters besides its target, even
// when every required one is filled; a verb without parameters has no a.
func TestHelpArgsOpensTheFormForAVerbWithParameters(t *testing.T) {
	r, m, got := sendFixture(t)
	keytest.Press(m, "?")
	selectHelpRow(t, r, "widget.ping")
	keytest.Press(m, "a")
	if r.TopModal() != helpID {
		t.Fatalf("a on a verb without parameters: top %q", r.TopModal())
	}
	selectHelpRow(t, r, "widget.send")
	keytest.Press(m, "a")
	if r.TopModal() != ArgsFormID {
		t.Fatalf("no form: top %q", r.TopModal())
	}
	keytest.Type(m, "venus")
	keytest.Press(m, "enter")
	if len(*got) != 1 || (*got)[0]["to"] != "venus" {
		t.Fatalf("ran %v", *got)
	}
}

// results.show-data off leaves the answer unshown.
func TestHelpRunDataAnswerCanBeTurnedOff(t *testing.T) {
	r, m, _ := sendFixture(t)
	if err := r.Settings().Set(DataResultsSetting, false); err != nil {
		t.Fatal(err)
	}
	sink := &RecordingSink{}
	r.UI().Attach(sink)
	keytest.Press(m, "?")
	selectHelpRow(t, r, "widget.send")
	keytest.Press(m, "a")
	keytest.Type(m, "mars")
	keytest.Press(m, "enter")
	if slices.ContainsFunc(sink.Notices(), func(n string) bool { return strings.Contains(n, "sent") }) {
		t.Fatalf("notices %q", sink.Notices())
	}
}

// Plumbing parameters (a list's id, a row item, the confirmed flag) are
// never form fields: a confirmed field would skip the verb's confirm. A
// verb with only plumbing has no args form (and no a in the help).
func TestPlumbingParametersAreNeverFormFields(t *testing.T) {
	r, _, _ := unboundFixture(t)
	fields, err := ArgsFields(r.Operations["ochazuke.list.apply"])
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fields {
		if f.Name == "confirmed" || f.Name == "id" || f.Name == "args" {
			t.Fatalf("form field %q", f.Name)
		}
	}
	if len(fields) != 1 || fields[0].Name != "verb" {
		t.Fatalf("fields %v", fields)
	}
	if got := askableParameters(r.Operations["ochazuke.list.mark"]); len(got) != 0 {
		t.Fatalf("list.mark asks for %v", got)
	}
}

// An args form that cannot open (one is open already) is a plain refusal,
// not a silent run of the verb.
func TestAskUnboundRefusesWhenTheFormCannotOpen(t *testing.T) {
	r, _, got := sendFixture(t)
	if _, err := r.OpenArgsForm(Request{Noun: "widget", Verb: "send"}, func(Request) error { return nil }); err != nil {
		t.Fatal(err)
	}
	err := r.AskUnbound("w1", Request{Noun: "widget", Verb: "send"})
	if err == nil || len(*got) != 0 || !strings.Contains(err.Error(), "cannot open the form") {
		t.Fatalf("err %v ran %v", err, *got)
	}
}

// A modal covered by a confirm keeps its own focus's hints: the help's, not
// the page's under it.
func TestCoveredHelpKeepsItsOwnHints(t *testing.T) {
	r, m, _ := unboundFixture(t)
	r.Config().Dir = t.TempDir()
	keytest.Press(m, "?")
	if _, err := r.Execute(Request{Noun: "ochazuke.bindings", Verb: "bind", Args: Arguments{"key": "enter", "action": "widget.wipe"}}); err != nil {
		t.Fatal(err)
	}
	if r.TopModal() == helpID || r.TopModal() == "" {
		t.Fatalf("no confirm: %q", r.TopModal())
	}
	hints := BindingHints(r, r.ModalFocus(helpID))
	if !strings.Contains(hints, "S") || strings.Contains(hints, "widget.open") {
		t.Fatalf("covered help hints: %q", hints)
	}
}
