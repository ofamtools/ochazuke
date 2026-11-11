package ochazuke

import (
	"slices"
	"strings"
	"testing"
)

func TestFocusedActionsListTheFocusedNounsVerbs(t *testing.T) {
	t.Parallel()
	r, f := pathVerbFixture(t)
	r.Instance("n1", Kind("note"))
	noun, choices := r.FocusedActions("n1")
	if noun != "note" {
		t.Fatalf("noun %q", noun)
	}
	var ids []string
	for _, c := range choices {
		ids = append(ids, c.ID)
	}
	for _, want := range []string{"note.reveal", "note.yank-path", "note.open", "note.open-with", "note.get"} {
		if !slices.Contains(ids, want) {
			t.Errorf("%s missing from %v", want, ids)
		}
	}
	r.SetFocus("n1")
	ch, err := r.OpenFocusActions()
	if err != nil || ch == nil {
		t.Fatalf("open %v", err)
	}
	if len(f.runs) != 0 {
		t.Fatalf("listing launched %v", f.runs)
	}
}

func TestFocusedActionsPickerOptOut(t *testing.T) {
	t.Parallel()
	r, _ := pathVerbFixture(t)
	r.Instance("n1", Kind("note"))
	r.SetFocus("n1")
	if err := r.Settings().Set(FocusedPickerSetting, false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.OpenFocusActions(); err == nil {
		t.Fatal("actions.focused-picker=false must refuse")
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.focus", Verb: "act"}); err == nil {
		t.Fatal("the verb refuses too")
	}
}

func TestFocusedActionsURLVerbs(t *testing.T) {
	t.Parallel()
	r := NewRuntime()
	r.Config().Disable()
	r.Define("bookmark").Props(
		PropSpec{Name: "id", Key: true},
		PropSpec{Name: "url", Parameter: Parameter{Type: PropURL}},
	).Store(Store{Get: func(Ctx, string) (Thing, error) {
		return Thing{"id": "b1", "url": "https://example.test/a"}, nil
	}})
	r.Instance("b1", Kind("bookmark")).Data = Thing{"id": "b1"}
	_, choices := r.FocusedActions("b1")
	var values []string
	for _, c := range choices {
		if c.Noun == "bookmark" && slices.Contains([]string{"bookmark.open", "bookmark.open-with", "bookmark.reveal", "bookmark.yank-path"}, c.ID) {
			values = append(values, c.ID)
		}
	}
	if !slices.Equal(values, []string{"bookmark.open", "bookmark.yank-path"}) {
		t.Fatalf("URL actions %v, want bookmark.open and bookmark.yank-path", values)
	}
}

func TestFocusedActionsFollowEveryWalkLevel(t *testing.T) {
	r, _, _ := unboundFixture(t)
	r.Config().Disable()
	var ran []string
	record := func(c Ctx) Result {
		ran = append(ran, c.Invoked.Name+"@"+r.Focus)
		return Done(nil)
	}
	r.Define("ochazuke.page").Verb("inspect", record, "Inspect this page")
	r.Define("app").Verb("inspect", record, "Inspect this application")
	r.Define("toolkit.entry").Verb("launch", record, "Launch the focused entry")
	r.Define("elsewhere").Verb("unrelated", record, "Not on this walk")
	r.Define("ochazuke.collection").Mode("normal").Bind("x", Do("toolkit.entry.launch"))
	_, choices := r.FocusedActions("w1")
	byID := map[string]Choice{}
	var groups []string
	for _, c := range choices {
		if _, duplicate := byID[c.ID]; duplicate {
			t.Fatalf("duplicate action %s", c.ID)
		}
		byID[c.ID] = c
		if len(groups) == 0 || groups[len(groups)-1] != c.Noun {
			groups = append(groups, c.Noun)
		}
		if c.Cells[0] != c.ID {
			t.Errorf("action does not show its full name: %+v", c)
		}
	}
	for _, id := range []string{"widget.open", "widget.ping", "ochazuke.collection.move", "ochazuke.collection.filter", "ochazuke.page.inspect", "app.inspect", "toolkit.entry.launch"} {
		if _, ok := byID[id]; !ok {
			t.Errorf("missing %s", id)
		}
	}
	if _, ok := byID["elsewhere.unrelated"]; ok {
		t.Error("listed an unrelated noun outside the key walk")
	}
	if !slices.Equal(groups, []string{"widget", "ochazuke.collection", "ochazuke.page", "app"}) {
		t.Fatalf("origin groups %v", groups)
	}
	if c := byID["toolkit.entry.launch"]; c.Noun != "ochazuke.collection" || c.Cells[1] != "x" {
		t.Fatalf("foreign action origin/key %+v", c)
	}
	for _, id := range []string{"toolkit.entry.launch", "ochazuke.page.inspect", "app.inspect"} {
		ch, err := r.OpenFocusActions()
		if err != nil {
			t.Fatal(err)
		}
		ch.SetQuery(id)
		rows := ch.Matches()
		if len(rows) != 2 || !rows[0].Header || rows[1].ID != id || rows[1].Section != rows[0].Section {
			t.Fatalf("filtered sections for %s: %+v", id, rows)
		}
		formKeys(t, r, "enter")
	}
	if !slices.Equal(ran, []string{"toolkit.entry.launch@w1", "ochazuke.page.inspect@w1", "app.inspect@w1"}) {
		t.Fatalf("actions ran %v", ran)
	}
}

func TestFocusedActionsKeepNeedsAndAskAtOriginalRow(t *testing.T) {
	r, _, _ := unboundFixture(t)
	r.Config().Disable()
	r.DefineGuard("connected", func(*Runtime, KeyEvent) bool { return false })
	r.Define("widget").Verb("send", func(Ctx) Result {
		t.Fatal("listing must not execute a verb")
		return Done(nil)
	}).Params(map[string]Parameter{"message": {Type: "string", Required: true}}).When("connected").
		Mode("normal").Bind("s", Do("widget.send"))
	r.Register(Operation{Key: "widget.unavailable", Unavailable: "a writable store", Run: func(*Runtime, Context, Arguments) (any, error) {
		t.Fatal("listing must not execute an unavailable verb")
		return nil, nil
	}})
	var sent []string
	r.Define("widget").Verb("deliver", func(c Ctx) Result {
		sent = append(sent, c.Instance.ID+":"+StringArg(c.Args, "id", "")+":"+StringArg(c.Args, "message", ""))
		return Done(nil)
	}).Params(map[string]Parameter{
		"id":      {Type: "string", Required: true, From: "row:id"},
		"message": {Type: "string", Required: true},
	})
	r.Instances["w1"].Data = Thing{"id": "original"}
	_, choices := r.FocusedActions("w1")
	byID := map[string]Choice{}
	for _, c := range choices {
		byID[c.ID] = c
	}
	for _, need := range []string{"message", "connected"} {
		if c := byID["widget.send"]; !strings.Contains(c.Cells[2], need) || c.Cells[1] != "s" {
			t.Errorf("guarded action missing need/key: %+v", c)
		}
	}
	if c := byID["widget.unavailable"]; !strings.Contains(c.Label, "a writable store") {
		t.Fatalf("unavailable action missing its need: %+v", c)
	}
	if c := byID["widget.deliver"]; !strings.Contains(c.Label, "needs: message") || strings.Contains(c.Cells[2], "id;") {
		t.Fatalf("row source should fill id, not message: %+v", c)
	}
	ch, err := r.OpenFocusActions()
	if err != nil {
		t.Fatal(err)
	}
	ch.SetQuery("widget.deliver")
	formKeys(t, r, "enter")
	ed := r.RecordEditor(ArgsFormID)
	if ed == nil || ed.Values()["id"] != "original" {
		t.Fatalf("args form lost the original row: %v", ed)
	}
	typeKeys(t, r, "hello")
	formKeys(t, r, "enter")
	if !slices.Equal(sent, []string{"w1:original:hello"}) {
		t.Fatalf("submitted action ran %v", sent)
	}
}

func TestFocusedActionsUseTheCurrentViewerWalk(t *testing.T) {
	r, _, _ := unboundFixture(t)
	r.Config().Disable()
	var actedAt string
	r.Define("ochazuke.editor").Verb("inspect", func(c Ctx) Result {
		actedAt = c.Instance.ID
		return Done(nil)
	})
	in, err := r.OpenTextView("Document", "line one\nline two")
	if err != nil {
		t.Fatal(err)
	}
	ch, err := r.OpenFocusActions()
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, row := range ch.Matches() {
		ids[row.ID] = true
	}
	for _, id := range []string{"ochazuke.editor.inspect", "ochazuke.input.move", "ochazuke.modal.close"} {
		if !ids[id] {
			t.Errorf("viewer walk missing %s", id)
		}
	}
	if ids["widget.ping"] || ids["ochazuke.collection.move"] {
		t.Error("viewer actions crossed the modal boundary to the page beneath")
	}
	ch.SetQuery("ochazuke.editor.inspect")
	formKeys(t, r, "enter")
	if actedAt != in.ID || r.Focus != in.ID || r.TopModal() != TextViewID {
		t.Fatalf("viewer action acted at %q, focus %q, modal %q", actedAt, r.Focus, r.TopModal())
	}
}
