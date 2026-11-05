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

// enter on a bound row still folds or does nothing: only an unbound row
// runs.
func TestEnterOnABoundRowDoesNotRun(t *testing.T) {
	r, m, ran := unboundFixture(t)
	keytest.Press(m, "?")
	tree := r.Tree(helpTreeID)
	for _, n := range tree.Nodes() {
		if row, ok := n.Data.(HelpRow); ok && row.Operation == "widget.open" {
			_ = tree.Select(n.ID)
		}
	}
	keytest.Press(m, "enter")
	if r.TopModal() != helpID || len(*ran) != 0 {
		t.Fatalf("modal %q ran %v", r.TopModal(), *ran)
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
	if _, err := r.Execute(q); err == nil || !strings.Contains(err.Error(), "replace") {
		t.Fatalf("err %v", err)
	}
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

// b on an unbound help row opens the bind form with the action filled; the
// key typed there is written and the help lists the verb no more.
func TestHelpBindFormBindsTheSelectedVerb(t *testing.T) {
	r, m, _ := unboundFixture(t)
	r.Config().Dir = t.TempDir()
	keytest.Press(m, "?")
	selectHelpRow(t, r, "widget.ping")
	keytest.Press(m, "b")
	if r.Instances[ArgsFormID] == nil {
		t.Fatalf("no form; top %q status %q", r.TopModal(), m.Status)
	}
	keytest.Type(m, "p")
	keytest.Press(m, "enter")
	if got := unboundOps(BindingHelp(r, "w1")); !slices.Equal(got, []string{"widget.wipe"}) {
		t.Fatalf("unbound after the form: %v (status %q, top %q)", got, m.Status, r.TopModal())
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
