package ochazuke

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"ofam.dev/ochazuke/retain"
)

// retentionFixture is a runtime with temp data and state folders, one file
// row (backups, keep 1) over three dated files and one record row (old
// chores, delete after 4 days) over a chore noun, and a fake remover that
// records what it is asked and removes nothing.
func retentionFixture(t *testing.T) (*Runtime, *[]string, *MemDocTable) {
	t.Helper()
	r := NewRuntime()
	c := r.Config()
	c.Disable()
	c.DataDir, c.StateDir = t.TempDir(), t.TempDir()
	backups := filepath.Join(c.DataDir, "backups")
	if err := os.MkdirAll(backups, 0o700); err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"2026-11-15.tar.gz", "2026-11-16.tar.gz", "2026-11-17.tar.gz"} {
		p := filepath.Join(backups, name)
		if err := os.WriteFile(p, []byte("data"), 0o600); err != nil {
			t.Fatal(err)
		}
		at := time.Now().Add(time.Duration(i-3) * time.Hour)
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
	}
	table := NewMemDocTable(r, "chore")
	r.Define("chore").Props(
		PropSpec{Name: "id", Key: true, CreateOnly: true},
		PropSpec{Name: "title", Role: RoleLabel},
		PropSpec{Name: "archived", Role: RoleArchive, Parameter: Parameter{Type: SettingBoolean}},
	).Store(DocStore(table, "chore"))
	old := time.Now().Add(-10 * 24 * time.Hour).UTC().Format(time.RFC3339)
	_ = table.Put("c1", Thing{"id": "c1", "title": "old", "archived": true, "updated": old})
	_ = table.Put("c2", Thing{"id": "c2", "title": "new", "archived": true, "updated": time.Now().UTC().Format(time.RFC3339)})
	_ = table.Put("c3", Thing{"id": "c3", "title": "open", "archived": false, "updated": old})
	if err := r.DeclareRetention(
		RetentionRow{Name: "backups", Paths: []string{"{data}/backups/????-??-??.tar.gz"}, Keep: 1, MinKeep: 1},
		RetentionRow{Name: "old-chores", Noun: "chore", Where: "archived=true", Verb: "delete", Days: 4},
	); err != nil {
		t.Fatal(err)
	}
	asked := &[]string{}
	r.SetRetentionRemover(func(p string) error { *asked = append(*asked, filepath.Base(p)); return nil })
	return r, asked, table
}

// TestRetentionRuleVerbs: list is the dry run, set writes the row's
// setting, apply removes files through the remover and things through the
// row's verb, and log reads one line per removal.
func TestRetentionRuleVerbs(t *testing.T) {
	r, asked, table := retentionFixture(t)
	v, err := r.Execute(Request{Noun: "retention-rule", Verb: "get", Args: Arguments{"name": "backups"}})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := ThingOf(v); fmt.Sprint(got["would_remove"]) != "2" || len(*asked) != 0 {
		t.Fatalf("the dry run lists two copies and removes nothing: %v asked %v", got, *asked)
	}
	if _, err := r.Execute(Request{Noun: "retention-rule", Verb: "set", Args: Arguments{"name": "backups", "props": map[string]any{"keep": 2}}}); err != nil {
		t.Fatal(err)
	}
	if r.Settings().Int("retention.backups.keep") != 2 {
		t.Fatal("set keep writes retention.backups.keep")
	}
	v, err = r.Execute(Request{Noun: "retention-rule", Verb: "apply"})
	if err != nil {
		t.Fatal(err)
	}
	runs := v.([]RetentionRun)
	if !slices.Equal(*asked, []string{"2026-11-15.tar.gz"}) || runs[0].Removed != 1 {
		t.Fatalf("keep 2 removes the oldest copy only: %v %+v", *asked, runs[0])
	}
	if _, ok, _ := table.Get("c1"); ok || runs[1].Removed != 1 {
		t.Fatalf("the old archived chore is deleted through chore.delete: %+v", runs[1])
	}
	if _, ok, _ := table.Get("c3"); !ok {
		t.Fatal("a chore outside where stays")
	}
	logged, err := r.Execute(Request{Noun: "retention-rule", Verb: "log", Args: Arguments{"name": "old-chores"}})
	if entries, _ := logged.([]retain.Entry); err != nil || len(entries) != 1 || entries[0].Noun != "chore" || entries[0].Key != "c1" {
		t.Fatalf("one log line per removal, the thing as {noun, key}: %v %v", logged, err)
	}
}

// TestRetentionAgentApply: an agent may apply (the owner: "agents can
// trim"); files go, but a thing's removal still goes through the verb's own
// right, and delete is never an agent's.
func TestRetentionAgentApply(t *testing.T) {
	r, asked, table := retentionFixture(t)
	defer r.BindActor(Actor{Kind: ActorAgent, Harness: "test", Pane: "p", Session: "s"}, false)()
	v, err := r.Execute(Request{Noun: "retention-rule", Verb: "apply"})
	if err != nil {
		t.Fatal(err)
	}
	runs := v.([]RetentionRun)
	if len(*asked) != 2 || runs[0].Removed != 2 {
		t.Fatalf("an agent's apply trims files: %v", *asked)
	}
	if _, ok, _ := table.Get("c1"); !ok || runs[1].Removed != 0 || !strings.Contains(runs[1].Items[0].Error, "agents never delete") {
		t.Fatalf("an agent's apply cannot delete a thing around chore.delete's right: %+v", runs[1])
	}
}

// TestRetentionAuto: one test per retention.auto value. On, RetentionDue
// runs every row once a UTC day; off, it runs nothing.
func TestRetentionAuto(t *testing.T) {
	for _, on := range []bool{true, false} {
		r, asked, _ := retentionFixture(t)
		if err := r.Settings().Set(RetentionAutoSetting, on); err != nil {
			t.Fatal(err)
		}
		if err := r.Settings().Set(RetentionFirstRunSetting, false); err != nil {
			t.Fatal(err)
		}
		first, second := r.RetentionDue(), r.RetentionDue()
		if on && (len(first) != 2 || len(second) != 0 || len(*asked) != 2) {
			t.Fatalf("on: each row once a day: %d then %d, asked %v", len(first), len(second), *asked)
		}
		if !on && (first != nil || len(*asked) != 0) {
			t.Fatalf("off: nothing runs: %v", *asked)
		}
	}
}

// TestRetentionFirstRun: one test per retention.first-run-ask value. On: the
// automatic run holds a row's first removal of more than its keep (backups
// keep 1 would remove 2), records it on the row and removes it only once
// the row is applied by hand; from then on automatic runs remove. Off: the
// automatic run removes at once.
func TestRetentionFirstRun(t *testing.T) {
	for _, ask := range []bool{true, false} {
		r, asked, _ := retentionFixture(t)
		if err := r.Settings().Set(RetentionFirstRunSetting, ask); err != nil {
			t.Fatal(err)
		}
		runs := r.RetentionDue()
		if !ask {
			if !slices.Equal(*asked, []string{"2026-11-16.tar.gz", "2026-11-15.tar.gz"}) || runs[0].Held {
				t.Fatalf("off: the first run removes: %v %+v", *asked, runs[0])
			}
			continue
		}
		if len(*asked) != 0 || !runs[0].Held || runs[0].Remove != 2 || runs[1].Held {
			t.Fatalf("on: the large first removal is held, the small one is not: %v %+v", *asked, runs)
		}
		v, _ := r.Execute(Request{Noun: "retention-rule", Verb: "get", Args: Arguments{"name": "backups"}})
		if got, _ := ThingOf(v); !strings.Contains(fmt.Sprint(got["held"]), "retention-rule apply backups") || got["approved"] != "" {
			t.Fatalf("on: the row shows what it holds: %v", got)
		}
		if _, err := r.Execute(Request{Noun: "retention-rule", Verb: "apply", Args: Arguments{"name": "backups", "after-write": true}}); err != nil || len(*asked) != 0 {
			t.Fatalf("on: a run after a write (after-write) still holds: %v %v", *asked, err)
		}
		if _, err := r.Execute(Request{Noun: "retention-rule", Verb: "apply", Args: Arguments{"name": "backups"}}); err != nil || len(*asked) != 2 {
			t.Fatalf("on: apply by hand removes at once: %v %v", *asked, err)
		}
		v, _ = r.Execute(Request{Noun: "retention-rule", Verb: "get", Args: Arguments{"name": "backups"}})
		if got, _ := ThingOf(v); got["held"] != "" || got["approved"] == "" {
			t.Fatalf("on: applied by hand, the row holds nothing and is approved: %v", got)
		}
	}
}

// TestRetentionStoreRow: a store row's Expire counts (the dry run: the
// row's would_remove and its one removals line) and removes (apply); with
// retention.first-run-ask on, an automatic run holds the first large
// removal of a store row like any row, and removes at once when the row
// was declared with the owner's decision (Approved). Each run is one log
// line with the count.
func TestRetentionStoreRow(t *testing.T) {
	r := NewRuntime()
	c := r.Config()
	c.Disable()
	c.DataDir, c.StateDir = t.TempDir(), t.TempDir()
	// A fake store of 10 entries, 6 of them older than 7 days.
	stores := map[string]*[]RetentionCut{}
	row := func(name, approved string) RetentionRow {
		cuts := &[]RetentionCut{}
		stores[name] = cuts
		left := 10
		return RetentionRow{Name: name, Days: 7, MinKeep: 1, Approved: approved, Expire: func(cut RetentionCut) (RetentionCount, error) {
			*cuts = append(*cuts, cut)
			n := 0
			if cut.Days == 7 && left == 10 {
				n = 6
			}
			if !cut.DryRun {
				left -= n
			}
			return RetentionCount{Count: left, Bytes: int64(left) * 100, Remove: n, RemoveBytes: int64(n) * 100, What: fmt.Sprintf("%d entries", n)}, nil
		}}
	}
	if err := r.DeclareRetention(row("held", ""), row("approved", "owner 2026-11-17: clear it")); err != nil {
		t.Fatal(err)
	}
	v, err := r.Execute(Request{Noun: "retention-rule", Verb: "get", Args: Arguments{"name": "held"}})
	got, _ := ThingOf(v)
	if err != nil || got["kind"] != "store" || fmt.Sprint(got["would_remove"]) != "6" || fmt.Sprint(got["removals"]) != "[6 entries (about 600B): older than 7 days]" {
		t.Fatalf("the dry run counts through Expire: %v %v", got, err)
	}
	for _, cut := range *stores["held"] {
		if !cut.DryRun || cut.Before.IsZero() {
			t.Fatalf("a dry run asks a dry cut with its cutoff: %+v", cut)
		}
	}
	runs := r.RetentionDue()
	if len(runs) != 2 || !runs[0].Held || runs[0].Removed != 0 || runs[1].Held || runs[1].Removed != 6 || runs[1].Freed != 600 {
		t.Fatalf("held waits for the owner, approved removes at once: %+v", runs)
	}
	v, _ = r.Execute(Request{Noun: "retention-rule", Verb: "get", Args: Arguments{"name": "approved"}})
	if got, _ := ThingOf(v); got["approved"] != "owner 2026-11-17: clear it" || got["held"] != "" {
		t.Fatalf("the declared decision shows as approved: %v", got)
	}
	logged, err := r.Execute(Request{Noun: "retention-rule", Verb: "log"})
	if entries, _ := logged.([]retain.Entry); err != nil || len(entries) != 1 || entries[0].Rule != "approved" || entries[0].Count != 6 || entries[0].Label != "6 entries" {
		t.Fatalf("one log line for the cut, with its count: %v %v", logged, err)
	}
}
