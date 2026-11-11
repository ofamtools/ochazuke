package ochazuke

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

// relRuntime has projects with a relation-backed `directories` list (kind
// directory, ordered, one default) and a `notes` single prop (kind about,
// at most one), over an in-memory relation store.
func relRuntime(t *testing.T) (*Runtime, *MemRelations, *relMem) {
	t.Helper()
	r := NewRuntime()
	projects := &relMem{}
	projects.add("id", "hub", "name", "Hub")
	projects.add("id", "ops", "name", "Ops")
	st := projects.store()
	st.Confirm = map[string]string{"delete": ""}
	r.Define("directory").Props(PropSpec{Name: "path", Key: true})
	r.Define("project").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "name", Role: RoleLabel},
		PropSpec{Name: "directories", Relation: "directory", Parameter: Parameter{Type: SettingList, Items: &Parameter{Type: PropPath, Path: "dir", Noun: "directory"}}},
		PropSpec{Name: "about", Relation: "about", Parameter: Parameter{Type: SettingString, Noun: "project"}},
		PropSpec{Name: "dirs", Relation: "directory", Parameter: Parameter{Type: SettingList, Items: &Parameter{Type: SettingString, Noun: AnyNoun}}}).Store(st)
	for _, k := range []RelationKind{
		{Name: "directory", From: "project", To: "directory", Ordered: true, Exclusive: []string{"default"}, Prop: "directories", Inverse: "projects"},
		{Name: "about", From: "project", To: "project", MaxFrom: 1},
		{Name: "pair", From: "project", MaxTo: 1, OnFull: RelationKeep},
	} {
		if err := r.DeclareRelationKind(k); err != nil {
			t.Fatal(err)
		}
	}
	mem := NewMemRelations()
	r.SetRelationStore(mem)
	return r, mem, projects
}

func relGet(t *testing.T, r *Runtime, key, prop string) any {
	t.Helper()
	return mustRun(t, r, Request{Noun: "project", Verb: "get", Args: Arguments{"id": key, "prop": prop}})
}

func relDirs(t *testing.T, r *Runtime, key string) []string {
	t.Helper()
	v := relGet(t, r, key, "directories")
	out, _ := v.([]string)
	if out == nil {
		if a, ok := v.([]any); ok {
			for _, x := range a {
				out = append(out, x.(string))
			}
		}
	}
	return out
}

func TestRelationPropListSetAndOrder(t *testing.T) {
	t.Parallel()
	r, mem, _ := relRuntime(t)
	mustRun(t, r, Request{Noun: "project", Verb: "set", Args: Arguments{"id": "hub", "props": map[string]any{"directories": []any{"/a", "/b", "/c"}}}})
	if got := relDirs(t, r, "hub"); !reflect.DeepEqual(got, []string{"/a", "/b", "/c"}) {
		t.Fatalf("directories %v", got)
	}
	// Text and data survive a reorder and a removal; the first is the default.
	if _, err := r.Relate(Ctx{Runtime: r, actor: r.Actor()}, Relation{Kind: "directory", From: "project:hub", To: "directory:/b", Text: "tools", Data: map[string]any{"role": "x"}}); err != nil {
		t.Fatal(err)
	}
	mustRun(t, r, Request{Noun: "project", Verb: "set", Args: Arguments{"id": "hub", "props": map[string]any{"directories": []any{"/c", "/b"}}}})
	if got := relDirs(t, r, "hub"); !reflect.DeepEqual(got, []string{"/c", "/b"}) {
		t.Fatalf("reordered %v", got)
	}
	rels, _ := mem.Relations(RelationQuery{Kind: "directory", From: "project:hub"})
	if rels[1].Text != "tools" || rels[1].Data["role"] != "x" || len(rels) != 2 {
		t.Fatalf("relations %+v", rels)
	}
	// /a was the default; unrelated, the next by position then (/b) took it.
	if rels[0].Flag("default") || !rels[1].Flag("default") {
		t.Fatalf("default flag %+v", rels)
	}
}

func TestRelationPropItemEdits(t *testing.T) {
	t.Parallel()
	r, _, _ := relRuntime(t)
	for _, line := range []string{"directories+=/a", "directories+=/b", "directories+=/a", "directories-=/a", `directories+=["/x","/y"]`} {
		mustRun(t, r, Request{Noun: "project", Verb: "set", Args: Arguments{"id": "hub", "prop": line}})
	}
	if got := relDirs(t, r, "hub"); !reflect.DeepEqual(got, []string{"/b", "/x", "/y"}) {
		t.Fatalf("after edits %v", got)
	}
	// The same edit form works on any list prop, and refuses a non-list.
	if _, err := r.Execute(Request{Noun: "project", Verb: "set", Args: Arguments{"id": "hub", "prop": "name+=x"}}); err == nil {
		t.Fatal("a string prop took +=")
	}
}

func TestRelationDefaultFlagMovesAndPromotes(t *testing.T) {
	t.Parallel()
	r, mem, _ := relRuntime(t)
	c := Ctx{Runtime: r, actor: r.Actor()}
	for _, d := range []string{"/a", "/b", "/c"} {
		if _, err := r.Relate(c, Relation{Kind: "directory", From: "project:hub", To: "directory:" + d}); err != nil {
			t.Fatal(err)
		}
	}
	def := func() string {
		rels, _ := mem.Relations(RelationQuery{Kind: "directory", From: "project:hub"})
		var out []string
		for _, x := range rels {
			if x.Flag("default") {
				out = append(out, x.To)
			}
		}
		return strings.Join(out, ",")
	}
	if def() != "directory:/a" {
		t.Fatalf("first is the default: %q", def())
	}
	_, _ = r.Relate(c, Relation{Kind: "directory", From: "project:hub", To: "directory:/c", Data: map[string]any{"default": true}})
	if def() != "directory:/c" {
		t.Fatalf("flag moved: %q", def())
	}
	if _, err := r.Unrelate(c, "directory", "project:hub", "directory:/c"); err != nil {
		t.Fatal(err)
	}
	if def() != "directory:/a" {
		t.Fatalf("promoted: %q", def())
	}
}

func TestRelationUnrelateArchivesFirst(t *testing.T) {
	t.Parallel()
	r, mem, _ := relRuntime(t)
	c := Ctx{Runtime: r, actor: r.Actor()}
	_, _ = r.Relate(c, Relation{Kind: "directory", From: "project:hub", To: "directory:/a", Text: "keep me"})
	if _, err := r.Settings().SetSession(DeleteArchiveSetting, true); err != nil {
		t.Fatal(err)
	}
	mustRun(t, r, Request{Noun: "project", Verb: "unrelate", Args: Arguments{"id": "hub", "to": "directory:/a", "kind": "directory"}})
	if len(relDirs(t, r, "hub")) != 0 || mem.Len() != 1 {
		t.Fatalf("archived, not removed: %v, %d rows", relDirs(t, r, "hub"), mem.Len())
	}
	// Relating it again brings the archived row back with its text.
	_, _ = r.Relate(c, Relation{Kind: "directory", From: "project:hub", To: "directory:/a"})
	rels, _ := mem.Relations(RelationQuery{Kind: "directory"})
	if len(rels) != 1 || rels[0].Text != "keep me" || rels[0].Archived {
		t.Fatalf("restored %+v", rels)
	}
	// Unrelate twice: archived, then removed.
	for i := 0; i < 2; i++ {
		mustRun(t, r, Request{Noun: "project", Verb: "unrelate", Args: Arguments{"id": "hub", "to": "directory:/a", "kind": "directory"}})
	}
	if mem.Len() != 0 {
		t.Fatalf("second unrelate removes: %d rows", mem.Len())
	}
	// With the setting off a live relation is removed at once.
	_, _ = r.Settings().SetSession(DeleteArchiveSetting, false)
	_, _ = r.Relate(c, Relation{Kind: "directory", From: "project:hub", To: "directory:/b"})
	mustRun(t, r, Request{Noun: "project", Verb: "unrelate", Args: Arguments{"id": "hub", "to": "directory:/b", "kind": "directory"}})
	if mem.Len() != 0 {
		t.Fatalf("removed at once: %d rows", mem.Len())
	}
}

func TestRelationKindRules(t *testing.T) {
	t.Parallel()
	r, mem, _ := relRuntime(t)
	c := Ctx{Runtime: r, actor: r.Actor()}
	// A declared kind refuses an end outside its nouns.
	if _, err := r.Relate(c, Relation{Kind: "directory", From: "directory:/x", To: "directory:/y"}); err == nil {
		t.Fatal("wrong From noun accepted")
	}
	// max-from 1 replaces: setting about again leaves one.
	_, _ = r.Relate(c, Relation{Kind: "about", From: "project:hub", To: "project:ops"})
	_, _ = r.Relate(c, Relation{Kind: "about", From: "project:hub", To: "project:hub"})
	if got := relGet(t, r, "hub", "about"); got != "hub" {
		t.Fatalf("about %v", got)
	}
	// max-to 1 with keep: both stay, a notice says so.
	_, _ = r.Relate(c, Relation{Kind: "pair", From: "project:hub", To: "project:ops"})
	_, _ = r.Relate(c, Relation{Kind: "pair", From: "project:ops", To: "project:ops"})
	if rels, _ := mem.Relations(RelationQuery{Kind: "pair"}); len(rels) != 2 {
		t.Fatalf("keep: %+v", rels)
	}
	// Any word is a kind: an undeclared one has no rules.
	if _, err := r.Relate(c, Relation{Kind: "see-also", From: "task:1", To: "pane:%3", Text: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Relate(c, Relation{From: "task:1", To: "pane:%3"}); err != nil {
		t.Fatal(err)
	}
	if rels, _ := mem.Relations(RelationQuery{Kind: DefaultRelationKind}); len(rels) != 1 {
		t.Fatalf("default kind %+v", rels)
	}
	// An end without a noun is an argument error.
	if _, err := r.Relate(c, Relation{From: "nonoun", To: "a:b"}); err == nil {
		t.Fatal("end without noun accepted")
	}
}

func TestRelationVerbsShareOneHandlerOnBothEnds(t *testing.T) {
	t.Parallel()
	r, mem, _ := relRuntime(t)
	// Directory has no store, so only project has the verbs; the other end is
	// a parameter. From project: this is From. With `from`: this is To.
	mustRun(t, r, Request{Noun: "project", Verb: "relate", Args: Arguments{"id": "hub", "to": "directory:/a", "kind": "directory", "text": "main", "data": map[string]any{"role": "primary"}}})
	mustRun(t, r, Request{Noun: "project", Verb: "relate", Args: Arguments{"id": "ops", "from": "project:hub", "kind": "about"}})
	rels, _ := mem.Relations(RelationQuery{})
	if len(rels) != 2 || rels[0].Text != "main" || rels[0].Data["role"] != "primary" || rels[1].From != "project:hub" || rels[1].To != "project:ops" {
		t.Fatalf("relations %+v", rels)
	}
	if _, err := r.Execute(Request{Noun: "project", Verb: "relate", Args: Arguments{"id": "hub"}}); err == nil {
		t.Fatal("relate without the other end")
	}
	if _, err := r.Execute(Request{Noun: "project", Verb: "relate", Args: Arguments{"id": "hub", "to": "a:b", "from": "c:d"}}); err == nil {
		t.Fatal("relate with both ends")
	}
}

func TestRelationVerbsAbsentWithoutAStore(t *testing.T) {
	t.Parallel()
	r := NewRuntime()
	r.DefineOwn("thing").Props(PropSpec{Name: "id", Key: true}).Store((&relMem{}).store())
	if _, ok := r.Operation("thing.relate"); ok {
		t.Fatal("relate offered with no relation store")
	}
	r.SetRelationStore(NewMemRelations())
	if _, ok := r.Operation("thing.relate"); !ok {
		t.Fatal("relate missing once a store is set")
	}
	r.SetRelationStore(nil)
	if _, ok := r.Operation("thing.unrelate"); ok {
		t.Fatal("unrelate left after the store went")
	}
}

func TestRelationPropWithoutStoreReadsEmptyAndRefusesWrites(t *testing.T) {
	t.Parallel()
	r, _, _ := relRuntime(t)
	r.SetRelationStore(nil)
	if got := relDirs(t, r, "hub"); len(got) != 0 {
		t.Fatalf("no store reads %v", got)
	}
	if _, err := r.Execute(Request{Noun: "project", Verb: "set", Args: Arguments{"id": "hub", "props": map[string]any{"directories": []any{"/a"}}}}); err == nil {
		t.Fatal("write with no store succeeded")
	}
}

func TestRelationUndoIsOneStep(t *testing.T) {
	t.Parallel()
	r, mem, _ := relRuntime(t)
	NewModel(r)
	mustRun(t, r, Request{Noun: "project", Verb: "relate", Args: Arguments{"id": "hub", "to": "directory:/a", "kind": "directory"}})
	mustRun(t, r, Request{Noun: "project", Verb: "relate", Args: Arguments{"id": "hub", "to": "directory:/b", "kind": "directory"}})
	mustRun(t, r, Request{Noun: "ochazuke.history-entry", Verb: "undo"})
	if got := relDirs(t, r, "hub"); !reflect.DeepEqual(got, []string{"/a"}) {
		t.Fatalf("after undo %v", got)
	}
	// A prop write undoes through the thing's own change.
	mustRun(t, r, Request{Noun: "project", Verb: "set", Args: Arguments{"id": "hub", "props": map[string]any{"directories": []any{"/q", "/r"}}}})
	mustRun(t, r, Request{Noun: "ochazuke.history-entry", Verb: "undo"})
	if got := relDirs(t, r, "hub"); !reflect.DeepEqual(got, []string{"/a"}) {
		t.Fatalf("after prop undo %v (%d rows)", got, mem.Len())
	}
}

func TestDeleteDetachesRelations(t *testing.T) {
	t.Parallel()
	r, mem, _ := relRuntime(t)
	c := Ctx{Runtime: r, actor: r.Actor()}
	_, _ = r.Relate(c, Relation{Kind: "directory", From: "project:ops", To: "directory:/z", Text: "z"})
	_, _ = r.Relate(c, Relation{Kind: "about", From: "project:hub", To: "project:ops"})
	mustRun(t, r, Request{Noun: "project", Verb: "delete", Args: Arguments{"id": "ops"}})
	if live, _ := mem.Relations(RelationQuery{Thing: "project:ops"}); len(live) != 0 {
		t.Fatalf("still attached: %+v", live)
	}
	gone, _ := mem.Relations(RelationQuery{Detached: true})
	if len(gone) != 2 {
		t.Fatalf("detached %+v", gone)
	}
	for _, x := range gone {
		if x.FormerFrom != "project:ops" && x.FormerTo != "project:ops" {
			t.Fatalf("former end lost: %+v", x)
		}
	}
	// Undoing the delete brings the ends back.
	NewModel(r)
	mustRun(t, r, Request{Noun: "ochazuke.history-entry", Verb: "undo"})
	if live, _ := mem.Relations(RelationQuery{Thing: "project:ops"}); len(live) != 2 {
		t.Fatalf("undo delete: %+v", live)
	}
}

func TestReferrersListRelationProps(t *testing.T) {
	t.Parallel()
	r, _, _ := relRuntime(t)
	var got []string
	for _, f := range r.Referrers("directory") {
		got = append(got, f.ID()+"/"+f.Relation+"/"+map[bool]string{true: "out", false: "in"}[f.Outward])
	}
	// project.directories and project.dirs (any) point at directories; the
	// declared kind adds the outward section on the project.
	if !slices.Contains(got, "project.directories/directory/in") {
		t.Fatalf("referrers %v", got)
	}
	var onProject []string
	for _, f := range r.Referrers("project") {
		onProject = append(onProject, f.ID()+"/"+map[bool]string{true: "out", false: "in"}[f.Outward])
	}
	if !slices.Contains(onProject, "directory.directories/out") || !slices.Contains(onProject, "project.about/in") {
		t.Fatalf("project referrers %v", onProject)
	}
}

func TestRelationKindFactsAndCatalogRows(t *testing.T) {
	t.Parallel()
	r, _, _ := relRuntime(t)
	NewModel(r)
	got := mustRun(t, r, Request{Noun: relationKindNoun, Verb: "list"})
	rows := AnswerRows(got).([]map[string]any)
	if len(rows) != 3 || rows[0]["name"] != "directory" || rows[0]["inverse"] != "projects" {
		t.Fatalf("kinds %+v", rows)
	}
	if err := r.DeclareRelationKind(RelationKind{Name: "bad kind"}); err == nil {
		t.Fatal("a kind of two words")
	}
	if err := r.DeclareRelationKind(RelationKind{Name: "x", From: "nothing"}); err == nil {
		t.Fatal("a kind of an unknown noun")
	}
}
