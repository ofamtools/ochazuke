package ochazuke

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// scaffoldFixture is an application with two stored nouns: task (label,
// archive role, a path prop) and note (label, an any-noun about prop), on
// a Work page, with one task.
func scaffoldFixture(t *testing.T) (*Runtime, *Model) {
	t.Helper()
	r := NewRuntime()
	r.Config().Disable()
	r.Define("task").Props(
		PropSpec{Name: "id", Key: true},
		PropSpec{Name: "title", Role: RoleLabel},
		PropSpec{Name: "archived", Role: RoleArchive, Parameter: Parameter{Type: SettingBoolean}},
		PropSpec{Name: "file", Parameter: Parameter{Type: PropPath}},
	).Store(DocStore(NewMemDocTable(r, "task"), "task"))
	r.Define("note").Props(
		PropSpec{Name: "id", Key: true},
		PropSpec{Name: "title", Role: RoleLabel},
		PropSpec{Name: "about", Role: RoleAbout, Parameter: Parameter{Noun: AnyNoun}},
	).Store(DocStore(NewMemDocTable(r, "note"), "note"))
	if _, err := r.AddPage("work", "Work", func(*Runtime, int, int) Block { return Text("work") }); err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	m.Init()
	m.Width, m.Height = 120, 40
	mustRun(t, r, Request{Noun: "task", Verb: "create", Args: Arguments{"title": "Buy milk"}})
	return r, m
}

func onlyTask(t *testing.T, r *Runtime) Thing {
	t.Helper()
	things, err := r.Nouns["task"].Store.List(Ctx{Runtime: r, Context: r.Context()}, ListQuery{Archived: true})
	if err != nil || len(things) == 0 {
		t.Fatalf("tasks %v %v", things, err)
	}
	return things[0]
}

// Every stored noun gets the scaffold verbs from the generic table, each
// where its needs are met: show, restore, duplicate (alias dup), yank,
// history and annotate (note can be about anything, so both nouns have it).
func TestScaffoldVerbsOnEveryStoredNoun(t *testing.T) {
	r, _ := scaffoldFixture(t)
	for _, key := range []string{"task.show", "task.restore", "task.duplicate", "task.yank", "task.history", "task.annotate", "note.show", "note.duplicate", "note.annotate"} {
		op, ok := r.Operation(key)
		if !ok || op.Unavailable != "" {
			t.Fatalf("%s: %v %q", key, ok, op.Unavailable)
		}
	}
	if _, ok := r.Operation("note.restore"); ok {
		t.Fatal("note has no archive role, so no restore")
	}
	if a, ok := r.ActionAlias("task.dup"); !ok || a.Action != "task.duplicate" {
		t.Fatalf("dup alias: %+v %v", a, ok)
	}
	// A prop named like a scaffold verb keeps the word: no verb.
	r.Define("log").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "history"}).Store(DocStore(NewMemDocTable(r, "log"), "log"))
	if op, ok := r.Operation("log.history"); ok && op.Standard != "" && op.Unavailable == "" {
		t.Fatal("log's history prop should keep the word")
	}
}

func TestScaffoldVerbsRun(t *testing.T) {
	r, _ := scaffoldFixture(t)
	id := onlyTask(t, r)["id"]
	mustRun(t, r, Request{Noun: "task", Verb: "archive", Args: Arguments{"id": id}})
	mustRun(t, r, Request{Noun: "task", Verb: "restore", Args: Arguments{"id": id}})
	if onlyTask(t, r)["archived"] == true {
		t.Fatal("restore left it archived")
	}
	mustRun(t, r, Request{Noun: "task", Verb: "archive", Args: Arguments{"id": id}})
	copy := mustRun(t, r, Request{Noun: "task", Verb: "dup", Args: Arguments{"id": id, "title": "Buy oat milk"}}).(map[string]any)
	if copy["title"] != "Buy oat milk" || copy["archived"] == true || copy["id"] == id {
		t.Fatalf("copy %v", copy)
	}
	if got := mustRun(t, r, Request{Noun: "task", Verb: "yank", Args: Arguments{"id": id}}); got != "Buy milk" {
		t.Fatalf("yank %v", got)
	}
	if got := mustRun(t, r, Request{Noun: "task", Verb: "yank", Args: Arguments{"id": id, "as": "ref"}}); got != "task:"+id.(string) {
		t.Fatalf("yank ref %v", got)
	}
	rows := mustRun(t, r, Request{Noun: "task", Verb: "history", Args: Arguments{"id": id}}).([]map[string]any)
	if len(rows) < 3 {
		t.Fatalf("history of the task: %d entries %v", len(rows), rows)
	}
	mustRun(t, r, Request{Noun: "task", Verb: "annotate", Args: Arguments{"id": id, "text": "oat is fine"}})
	notes, _ := r.Nouns["note"].Store.List(Ctx{Runtime: r, Context: r.Context()}, ListQuery{})
	if len(notes) != 1 || notes[0]["about"] != "task:"+id.(string) || notes[0]["title"] != "oat is fine" {
		t.Fatalf("notes %v", notes)
	}
	// show answers the record; a program moves nobody's page.
	page := r.Page
	if got := mustRun(t, r, Request{Noun: "task", Verb: "show", Args: Arguments{"id": id}}).(map[string]any); got["title"] != "Buy milk" || r.Page != page {
		t.Fatalf("show %v on %s", got, r.Page)
	}
}

// The generated key block: on an index row, d a archives, z A shows the
// archived rows, d r restores, g d duplicates, y y yanks, d d archives a
// live thing with no question; the delete of an archived one asks one
// question naming what goes with it.
func TestGeneratedNounKeys(t *testing.T) {
	r, _ := scaffoldFixture(t)
	id := onlyTask(t, r)["id"].(string)
	mustRun(t, r, Request{Noun: "note", Verb: "create", Args: Arguments{"title": "see", "about": "task:" + id}})
	if err := r.OpenIndex("task", id); err != nil {
		t.Fatal(err)
	}
	row := IndexPageID("task") + "/list/" + id
	if r.Focus != row {
		t.Fatalf("focus %s", r.Focus)
	}
	press(t, r, "d", "a")
	if onlyTask(t, r)["archived"] != true {
		t.Fatal("d a did not archive")
	}
	press(t, r, "z", "A")
	forceFocus(r, row)
	press(t, r, "d", "r")
	if onlyTask(t, r)["archived"] == true {
		t.Fatal("d r did not restore")
	}
	forceFocus(r, row)
	sink := &RecordingSink{}
	r.UI().Attach(sink)
	press(t, r, "g", "d")
	if things, _ := r.Nouns["task"].Store.List(Ctx{Runtime: r, Context: r.Context()}, ListQuery{Archived: true}); len(things) != 2 {
		t.Fatalf("g d made no copy: %v (%s)", things, sink.LastError())
	}
	forceFocus(r, row)
	press(t, r, "y", "y")
	if y, _ := r.readYank(""); y.Text != "Buy milk" {
		t.Fatalf("y y yanked %q", y.Text)
	}
	press(t, r, "d", "d")
	if onlyTask(t, r)["archived"] != true || r.TopModal() != "" {
		t.Fatalf("d d on a live thing archives with no question (modal %q)", r.TopModal())
	}
	op, _ := r.Operation("task.delete")
	prompt := op.ConfirmFor(r, r.Context(), Request{Noun: "task", Verb: "delete", Args: Arguments{"id": id, "archive-first": true}}, DefaultDeleteConfirm)
	if !strings.Contains(prompt, "1 note name it by about") {
		t.Fatalf("delete prompt %q", prompt)
	}
	rows := r.RowsOf("task", "normal")
	keys := map[string]string{}
	for _, b := range rows {
		keys[b.Keys] = b.Request.Key()
		if b.Generated && b.Source != NounDefaultsSource {
			t.Fatalf("source %s", b.Source)
		}
	}
	for k, want := range map[string]string{"n": "task.create", "r": "task.edit", "v": "task.show", "d a": "task.archive", "d r": "task.restore", "d d": "task.delete", "g d": "task.duplicate", "y y": "task.yank", "y p": "task.yank-path", "R": "task.reveal"} {
		if keys[k] != want {
			t.Errorf("%s: %q, want %s", k, keys[k], want)
		}
	}
	if r.rowsOn("note", "d r") {
		t.Error("note has no restore, so no d r")
	}
}

func (r *Runtime) rowsOn(noun, keys string) bool {
	for _, b := range r.RowsOf(noun, "normal") {
		if b.Keys == keys {
			return true
		}
	}
	return false
}

// A declared row on the walk wins the key and the generated row steps
// aside, whatever its guard: an application row on the page for d (a
// prefix of d a), and a user-style row on the list for y y. Off, none.
func TestGeneratedNounKeysStepAside(t *testing.T) {
	r, _ := scaffoldFixture(t)
	id := onlyTask(t, r)["id"].(string)
	if err := r.OpenIndex("task", id); err != nil {
		t.Fatal(err)
	}
	list := IndexPageID("task") + "/list"
	if err := r.Bind(Binding{Mode: "normal", Instance: list, Keys: "y y", Request: Do("ochazuke.collection.mark").Request}); err != nil {
		t.Fatal(err)
	}
	if err := r.Bind(Binding{Mode: "normal", Noun: "task", Keys: "d d", Request: Request{Noun: "task", Verb: "show"}}); err != nil {
		t.Fatal(err)
	}
	listed := map[string]string{}
	for _, b := range r.ListBindings(r.Focus) {
		listed[b.Keys] = b.Request.Key()
	}
	if listed["y y"] != "ochazuke.collection.mark" || listed["d d"] != "task.show" {
		t.Fatalf("declared rows should win: %v", listed)
	}
	press(t, r, "d", "d")
	if onlyTask(t, r)["archived"] == true {
		t.Fatal("the generated d d answered over the noun's own row")
	}
	if err := r.Settings().Set(NounDefaultsSetting, false); err != nil {
		t.Fatal(err)
	}
	for _, b := range r.RowsOf("task", "normal") {
		if b.Generated {
			t.Fatalf("keys.noun-defaults off left %s", b.Keys)
		}
	}
}

// The show form of an index page has the relation sections under it (the
// notes about the task) and the history last; enter on a related row
// follows it: the note's own index, its row selected.
func TestShowFormSectionsAndFollow(t *testing.T) {
	r, m := scaffoldFixture(t)
	id := onlyTask(t, r)["id"].(string)
	mustRun(t, r, Request{Noun: "note", Verb: "create", Args: Arguments{"title": "see the shelf", "about": "task:" + id}})
	mustRun(t, r, Request{Noun: "task", Verb: "set", Args: Arguments{"id": id, "props": map[string]any{"title": "Buy oat milk"}}})
	if err := r.OpenIndex("task", id); err != nil {
		t.Fatal(err)
	}
	frame := ansi.Strip(m.View().Content)
	for _, want := range []string{"Notes", "see the shelf", "History", "task.set"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("frame lacks %q:\n%s", want, frame)
		}
	}
	rel := r.ShowFormOf(IndexPageID("task") + "/show").Related()
	var noteRow string
	for _, s := range rel.sections {
		if s.ref.Noun == "note" {
			for _, it := range s.list.Items() {
				if it.Noun == "note" {
					noteRow = it.ID
				}
			}
		}
	}
	if noteRow == "" {
		t.Fatal("no note row in the sections")
	}
	forceFocus(r, noteRow)
	press(t, r, "enter")
	if r.Page != IndexPageID("note") {
		t.Fatalf("enter on a related row went to %s", r.Page)
	}
}
