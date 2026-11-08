package ochazuke

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
)

// copySource is a source store over a map that logs every call.
type copySource struct {
	order []string
	items map[string]Thing
	calls []string
}

func (s *copySource) store() Store {
	return Store{
		Get: func(_ Ctx, id string) (Thing, error) {
			s.calls = append(s.calls, "get "+id)
			if t, ok := s.items[id]; ok {
				return maps.Clone(t), nil
			}
			return nil, fmt.Errorf("no item %q", id)
		},
		List: func(Ctx, ListQuery) ([]Thing, error) {
			s.calls = append(s.calls, "list")
			var out []Thing
			for _, k := range s.order {
				if t, ok := s.items[k]; ok {
					out = append(out, maps.Clone(t))
				}
			}
			return out, nil
		},
		Patch: func(_ Ctx, id string, ch Thing) (Thing, error) {
			s.calls = append(s.calls, "set "+id+" "+strings.Join(slices.Sorted(maps.Keys(ch)), ","))
			maps.Copy(s.items[id], ch)
			return maps.Clone(s.items[id]), nil
		},
		Delete: func(_ Ctx, id string) error {
			s.calls = append(s.calls, "delete "+id)
			delete(s.items, id)
			return nil
		},
	}
}

// take is the calls made since the last take, but lists: a list of the
// copies asks the source what it has (the first read of a new thing).
func (s *copySource) take() []string {
	out := slices.DeleteFunc(s.calls, func(c string) bool { return c == "list" })
	s.calls = nil
	return out
}

func copyFixture(t *testing.T) (*Runtime, *copySource, *MemoryLocal, *RecordingSink) {
	t.Helper()
	src := &copySource{order: []string{"a", "b"}, items: map[string]Thing{
		"a": {"id": "a", "title": "Alpha", "branch": "main"},
		"b": {"id": "b", "title": "Beta", "branch": "dev"},
	}}
	local := NewMemoryLocal()
	r := NewRuntime()
	sink := &RecordingSink{}
	r.UI().Attach(sink)
	r.Define("item").Props(
		PropSpec{Name: "id", Key: true},
		PropSpec{Name: "title", Role: RoleLabel, Parameter: Parameter{Type: SettingString}},
		PropSpec{Name: "branch", Through: true, Parameter: Parameter{Type: SettingString}},
		PropSpec{Name: "note", Parameter: Parameter{Type: SettingString}},
		PropSpec{Name: "gone", Role: RoleArchive, Parameter: Parameter{Type: SettingBoolean}},
	).Store(CopyStore(src.store(), local))
	return r, src, local, sink
}

func runItem(t *testing.T, r *Runtime, verb string, args Arguments) any {
	t.Helper()
	v, err := r.Execute(Request{Noun: "item", Verb: verb, Args: args, Confirmed: true})
	if err != nil {
		t.Fatalf("item.%s %v: %v", verb, args, err)
	}
	return v
}

// The first read of a thing copies it in once; after that the copy is
// the thing and the source is not asked again.
func TestCopyStoreBackfillsOnce(t *testing.T) {
	r, src, local, _ := copyFixture(t)
	got := runItem(t, r, "get", Arguments{"id": "a"}).(map[string]any)
	if got["title"] != "Alpha" {
		t.Fatalf("get a = %v", got)
	}
	if calls := src.take(); !slices.Equal(calls, []string{"get a"}) {
		t.Fatalf("first get asked the source %v, want one get", calls)
	}
	copied, ok, _ := local.Get("a")
	if !ok || copied["created"] == nil || copied["updated"] == nil {
		t.Fatalf("the copy holds %v, want it with its own base times", copied)
	}
	src.items["a"]["title"] = "Changed at the source"
	got = runItem(t, r, "get", Arguments{"id": "a"}).(map[string]any)
	if got["title"] != "Alpha" {
		t.Fatalf("a second get read the source: %v", got)
	}
	if calls := src.take(); len(calls) != 0 {
		t.Fatalf("a second get asked the source %v", calls)
	}
	// A list copies in what it meets for the first time, once.
	runItem(t, r, "list", nil)
	runItem(t, r, "list", nil)
	if _, ok, _ := local.Get("b"); !ok {
		t.Fatal("list did not copy b in")
	}
	if got := runItem(t, r, "get", Arguments{"id": "a"}).(map[string]any); got["title"] != "Alpha" {
		t.Fatalf("a list overwrote the copy: %v", got)
	}
}

// set writes the copy alone, except the props declared Through, which go
// to the source's Patch too; create and archive are local.
func TestCopyStoreSetIsLocalExceptThrough(t *testing.T) {
	r, src, local, _ := copyFixture(t)
	runItem(t, r, "get", Arguments{"id": "a"})
	src.take()
	runItem(t, r, "set", Arguments{"id": "a", "props": map[string]any{"title": "Mine", "note": "x"}})
	if calls := src.take(); len(calls) != 0 {
		t.Fatalf("a local set called the source: %v", calls)
	}
	if src.items["a"]["title"] != "Alpha" {
		t.Fatalf("the source changed: %v", src.items["a"])
	}
	runItem(t, r, "set", Arguments{"id": "a", "props": map[string]any{"branch": "feature", "title": "Mine 2"}})
	if calls := src.take(); !slices.Equal(calls, []string{"set a branch"}) {
		t.Fatalf("a set with a Through prop called %v, want set a branch", calls)
	}
	if src.items["a"]["branch"] != "feature" || src.items["a"]["title"] != "Alpha" {
		t.Fatalf("source after the through set: %v", src.items["a"])
	}
	if c, _, _ := local.Get("a"); c["branch"] != "feature" || c["title"] != "Mine 2" {
		t.Fatalf("copy after the through set: %v", c)
	}
	runItem(t, r, "archive", Arguments{"id": "a"})
	made := runItem(t, r, "create", Arguments{"title": "New here"}).(map[string]any)
	if calls := src.take(); len(calls) != 0 {
		t.Fatalf("archive and create called the source: %v", calls)
	}
	if _, ok := src.items[fmt.Sprint(made["id"])]; ok {
		t.Fatal("create made a thing at the source")
	}
	// Through is data the schema checks: a key cannot write through.
	defer func() {
		if recover() == nil {
			t.Fatal("a Through key was accepted")
		}
	}()
	NewRuntime().Define("bad").Props(PropSpec{Name: "id", Key: true, Through: true})
}

// delete removes the copy and keeps a tombstone, so a list does not copy
// it in again; copies.delete source deletes at the source too; undo
// brings the copy back.
func TestCopyStoreDeleteTombstone(t *testing.T) {
	r, src, local, _ := copyFixture(t)
	runItem(t, r, "list", nil)
	src.take()
	runItem(t, r, "delete", Arguments{"id": "a"})
	if calls := src.take(); len(calls) != 0 {
		t.Fatalf("a local delete called the source: %v", calls)
	}
	stone, ok, _ := local.Get("a")
	if !ok || stone[CopyTombstone] != true {
		t.Fatalf("no tombstone: %v", stone)
	}
	page := runItem(t, r, "list", nil).(ListAnswer)
	for _, it := range page.Items {
		if it["id"] == "a" {
			t.Fatal("list copied a deleted thing in again")
		}
	}
	if _, err := r.Execute(Request{Noun: "item", Verb: "get", Args: Arguments{"id": "a"}}); err == nil {
		t.Fatal("get read a deleted thing")
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.history-entry", Verb: "undo"}); err != nil {
		t.Fatalf("undo: %v", err)
	}
	if got := runItem(t, r, "get", Arguments{"id": "a"}).(map[string]any); got["title"] != "Alpha" {
		t.Fatalf("undo did not bring the copy back: %v", got)
	}
	if _, err := r.Settings().SetSession(CopiesDeleteSetting, CopiesDeleteSource); err != nil {
		t.Fatal(err)
	}
	src.take()
	runItem(t, r, "delete", Arguments{"id": "b"})
	if calls := src.take(); !slices.Contains(calls, "delete b") {
		t.Fatalf("copies.delete source did not delete at the source: %v", calls)
	}
}

// refresh shows a plain diff of the copy against the source; a pick
// writes through set and is undone like any set; the row's mark follows.
func TestCopyRefreshDiffPickUndo(t *testing.T) {
	r, src, _, sink := copyFixture(t)
	if _, ok := r.Operations.Find(Key("item.refresh")); !ok {
		t.Fatal("a copy noun has no refresh")
	}
	if _, ok := r.Operations.Find(Key("note.refresh")); ok {
		t.Fatal("refresh on a noun without a copy")
	}
	l, err := r.AddList("items", nil, From("item"))
	if err != nil {
		t.Fatal(err)
	}
	src.items["a"]["title"] = "Alpha at the source"
	src.items["a"]["extra"] = "the source's own"
	runItem(t, r, "refresh", nil)
	e := r.RecordEditor(copyRefreshFormID)
	if e == nil {
		t.Fatalf("refresh opened no diff; notices %v", sink.Notices())
	}
	if len(e.fields) != 1 || e.fields[0].Label != "title" {
		t.Fatalf("diff fields %v, want title alone", e.fields)
	}
	text := r.renderDiff(Styles{}, copyDiffText([]copyDiff{{Key: "a", Label: "Alpha", Prop: "title", Local: "Alpha", Source: "Alpha at the source"}}), 80, 10)
	if !strings.Contains(text, "- title: Alpha") || !strings.Contains(text, "+ title: Alpha at the source") {
		t.Fatalf("plain diff:\n%s", text)
	}
	if !strings.HasSuffix(l.Items()[0].Label, copyMarkText) {
		t.Fatalf("no mark on the differing row: %q", l.Items()[0].Label)
	}
	if strings.HasSuffix(l.Items()[1].Label, copyMarkText) {
		t.Fatalf("a mark on a row that does not differ: %q", l.Items()[1].Label)
	}
	src.take()
	if res := e.spec.Save(RecordWrite{Values: Record{"d1": copyPickSource}, Changed: []string{"d1"}}); res.err != nil {
		t.Fatal(res.err)
	}
	if got := runItem(t, r, "get", Arguments{"id": "a"}).(map[string]any); got["title"] != "Alpha at the source" {
		t.Fatalf("pick source wrote %v", got)
	}
	if calls := src.take(); len(calls) != 0 {
		t.Fatalf("a pick of a local prop called the source: %v", calls)
	}
	if strings.HasSuffix(l.Items()[0].Label, copyMarkText) {
		t.Fatalf("the mark stayed after the pick: %q", l.Items()[0].Label)
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.history-entry", Verb: "undo"}); err != nil {
		t.Fatalf("undo: %v", err)
	}
	if got := runItem(t, r, "get", Arguments{"id": "a"}).(map[string]any); got["title"] != "Alpha" {
		t.Fatalf("undo left %v", got)
	}
	if !strings.HasSuffix(l.Items()[0].Label, copyMarkText) {
		t.Fatalf("the mark did not come back with the undo: %q", l.Items()[0].Label)
	}
	// The picks show what the copy holds; picking local after source puts
	// the copy's value back, and the mark with it.
	if v := r.copyPicks(r.Nouns["item"], e.spec.Fields, map[string]copyDiff{"d1": {Key: "a", Prop: "title", Local: "Alpha", Source: "Alpha at the source"}}); v["d1"] != copyPickLocal {
		t.Fatalf("pick after undo shows %q, want local", v["d1"])
	}
	if res := e.spec.Save(RecordWrite{Values: Record{"d1": copyPickSource}, Changed: []string{"d1"}}); res.err != nil {
		t.Fatal(res.err)
	}
	if res := e.spec.Save(RecordWrite{Values: Record{"d1": copyPickLocal}, Changed: []string{"d1"}}); res.err != nil {
		t.Fatal(res.err)
	}
	if got := runItem(t, r, "get", Arguments{"id": "a"}).(map[string]any); got["title"] != "Alpha" {
		t.Fatalf("pick local left %v", got)
	}
	if !strings.HasSuffix(l.Items()[0].Label, copyMarkText) {
		t.Fatalf("pick local did not bring the mark back: %q", l.Items()[0].Label)
	}
	// Refresh of one thing, nothing differing: a notice, no modal.
	if err := e.Leave(); err != nil {
		t.Fatal(err)
	}
	src.items["a"]["title"] = "Alpha"
	runItem(t, r, "refresh", Arguments{"id": "a"})
	if r.RecordEditor(copyRefreshFormID) != nil && r.TopModal() == copyRefreshFormID {
		t.Fatal("a refresh with no differences opened the diff")
	}
}

// copies.marks off shows no mark.
func TestCopyMarksSetting(t *testing.T) {
	r, src, _, _ := copyFixture(t)
	l, err := r.AddList("items", nil, From("item"))
	if err != nil {
		t.Fatal(err)
	}
	src.items["b"]["title"] = "Beta changed"
	runItem(t, r, "refresh", nil)
	if !strings.HasSuffix(l.Items()[1].Label, copyMarkText) {
		t.Fatalf("no mark: %q", l.Items()[1].Label)
	}
	if _, err := r.Settings().SetSession(CopiesMarksSetting, false); err != nil {
		t.Fatal(err)
	}
	if err := l.Refill(); err != nil {
		t.Fatal(err)
	}
	if strings.HasSuffix(l.Items()[1].Label, copyMarkText) {
		t.Fatalf("copies.marks off still marks: %q", l.Items()[1].Label)
	}
}

// A remote source is read in a job, never on the loop: refresh launches
// one, and the diff opens when it is answered.
func TestCopyRefreshRemoteSourceIsAJob(t *testing.T) {
	b := &remoteBox{items: map[string]Thing{"a": {"id": "a", "title": "A"}}}
	r := NewRuntime()
	r.UI().Attach(&RecordingSink{})
	r.Define("box").Props(
		PropSpec{Name: "id", Key: true},
		PropSpec{Name: "title", Role: RoleLabel, Parameter: Parameter{Type: SettingString}},
	).Store(CopyStore(b.store(), NewMemoryLocal()))
	if !r.Nouns["box"].Store.Remote {
		t.Fatal("a copy of a remote source is not remote")
	}
	if _, err := r.Execute(Request{Noun: "box", Verb: "get", Args: Arguments{"id": "a"}}); err != nil {
		t.Fatal(err)
	}
	pumpRemote(t, r)
	b.mu.Lock()
	b.items["a"]["title"] = "A at the source"
	b.calls = nil
	b.mu.Unlock()
	if _, err := r.Execute(Request{Noun: "box", Verb: "refresh"}); err != nil {
		t.Fatal(err)
	}
	if r.RecordEditor(copyRefreshFormID) != nil {
		t.Fatal("the diff opened before the source answered: it was read on the loop")
	}
	if !hasRunning(r) && len(r.commands) == 0 {
		t.Fatal("refresh of a remote source launched no job")
	}
	pumpRemote(t, r)
	b.mu.Lock()
	calls := slices.Clone(b.calls)
	b.mu.Unlock()
	if !slices.Equal(calls, []string{"list "}) {
		t.Fatalf("source calls %v, want one list", calls)
	}
	if r.RecordEditor(copyRefreshFormID) == nil {
		t.Fatal("the diff did not open when the job answered")
	}
}

// Over a remote source the verb's runs make the same calls whatever an
// earlier run wrote to the copy: a set of a thing not copied yet copies it
// in, writes its Through prop at the source once, and undo restores the
// value the copy had.
func TestCopyStoreRemoteRunsAlike(t *testing.T) {
	b := &remoteBox{items: map[string]Thing{"a": {"id": "a", "title": "A", "on": false}}}
	r := NewRuntime()
	r.UI().Attach(&RecordingSink{})
	r.Define("box").Props(
		PropSpec{Name: "id", Key: true},
		PropSpec{Name: "title", Role: RoleLabel, Through: true, Parameter: Parameter{Type: SettingString}},
		PropSpec{Name: "on", Parameter: Parameter{Type: SettingBoolean}},
	).Store(CopyStore(b.store(), NewMemoryLocal()))
	if _, err := r.Execute(Request{Noun: "box", Verb: "set", Args: Arguments{"id": "a", "props": map[string]any{"title": "B", "on": true}}}); err != nil {
		t.Fatal(err)
	}
	pumpRemote(t, r)
	b.mu.Lock()
	calls, item := slices.Clone(b.calls), maps.Clone(b.items["a"])
	b.mu.Unlock()
	if !slices.Equal(calls, []string{"get a", "set a"}) {
		t.Fatalf("source calls %v, want get a then set a", calls)
	}
	if item["title"] != "B" || item["on"] != false {
		t.Fatalf("source %v: only the Through prop is written there", item)
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.history-entry", Verb: "undo"}); err != nil {
		t.Fatal(err)
	}
	pumpRemote(t, r)
	got, _, _ := r.Nouns["box"].Store.copy.local.Get("a")
	if got["title"] != "A" || got["on"] != false {
		t.Fatalf("undo left the copy %v", got)
	}
}

// A Through prop the source does not take is kept in the copy: a plain
// notice (the write succeeded here) names the thing by its label as it
// was, once.
func TestCopyStoreSourceRefusalIsANotice(t *testing.T) {
	r, src, local, sink := copyFixture(t)
	runItem(t, r, "get", Arguments{"id": "a"})
	delete(src.items, "a")
	s := r.Nouns["item"].Store
	patch := s.copy.source.Patch
	s.copy.source.Patch = func(c Ctx, id string, ch Thing) (Thing, error) {
		if _, ok := src.items[id]; !ok {
			return nil, fmt.Errorf("the source no longer has it")
		}
		return patch(c, id, ch)
	}
	runItem(t, r, "set", Arguments{"id": "a", "props": map[string]any{"branch": "kept"}})
	if c, _, _ := local.Get("a"); c["branch"] != "kept" {
		t.Fatalf("the copy: %v", c)
	}
	want := "item Alpha: kept here, not written to the source: the source no longer has it"
	if !slices.Contains(sink.Notices(), want) {
		t.Fatalf("notices %q, want %q", sink.Notices(), want)
	}
	for _, m := range sink.Messages() {
		if m.Kind == UIError {
			t.Fatalf("an error: %q", m.Text)
		}
	}
}
