package ochazuke

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// memOverlay is an application's backend: its rows in memory, counted.
type memOverlay struct {
	rows    map[string]map[string]*OverlayRow
	changes int
}

func (m *memOverlay) Rows() (map[string]map[string]*OverlayRow, error) {
	out := map[string]map[string]*OverlayRow{}
	for noun, rows := range m.rows {
		out[noun] = map[string]*OverlayRow{}
		for key, row := range rows {
			out[noun][key] = row.clone()
		}
	}
	return out, nil
}

func (m *memOverlay) Change(noun, key string, change func(*OverlayRow) bool) (*OverlayRow, map[string]map[string]*OverlayRow, error) {
	m.changes++
	return ApplyOverlayChange(m.rows, noun, key, change).clone(), nil, nil
}

// overlay.store: app writes overlay.json beside settings.json, family
// writes ofam/overlay.json beside the config folder, and an application's
// backend (SetOverlayBackend) is the default and holds the rows itself.
func TestOverlayStoreValues(t *testing.T) {
	for _, value := range []string{OverlayStoreApp, OverlayStoreFamily, "mine"} {
		base := t.TempDir()
		dir := filepath.Join(base, "app")
		r := NewRuntime()
		r.Config().Dir = dir
		mem := &memOverlay{rows: map[string]map[string]*OverlayRow{}}
		if value == "mine" {
			if err := r.SetOverlayBackend("mine", mem); err != nil {
				t.Fatal(err)
			}
		} else if err := r.Settings().Set(OverlayStoreSetting, value); err != nil {
			t.Fatal(err)
		}
		r.defineFacts(factsSpec{Noun: "fact", Key: "id", StableKeys: true, Extra: []PropSpec{{Name: "id"}, {Name: "colour"}},
			List: func(Ctx, ListQuery) ([]Thing, error) { return []Thing{{"id": "f1", "colour": "green"}}, nil }})
		mustRun(t, r, Request{Noun: "fact", Verb: "set", Args: Arguments{"id": "f1", "prop": "colour", "value": "red"}})
		if got, _ := r.Execute(Request{Noun: "fact", Verb: "get", Args: Arguments{"id": "f1", "prop": "colour"}}); got != "red" {
			t.Fatalf("%s: colour %v", value, got)
		}
		app, _ := os.ReadFile(filepath.Join(dir, OverlayFile))
		family, _ := os.ReadFile(filepath.Join(base, FamilyFolder, OverlayFile))
		held := map[string]bool{OverlayStoreApp: strings.Contains(string(app), "red"), OverlayStoreFamily: strings.Contains(string(family), "red"), "mine": mem.changes == 1}
		for where, has := range held {
			if has != (where == value) {
				t.Fatalf("%s: kept in %s %v (app %q, family %q, backend %d writes)", value, where, has, app, family, mem.changes)
			}
		}
		if value == "mine" && mem.rows["fact"]["f1"].Changes["colour"].Original != "green" {
			t.Fatalf("the backend's row keeps no original: %+v", mem.rows["fact"]["f1"])
		}
	}
}

// overlay.conflict: after the source changes a value the user changed,
// keep-mine shows the user's value and take-source the source's; either
// way the row keeps the original as last seen, and OverlayChanges says the
// original changed and gives the new one.
func TestOverlayConflictValues(t *testing.T) {
	for _, value := range []string{OverlayKeepMine, OverlayTakeSource} {
		r := NewRuntime()
		colour := "green"
		r.defineFacts(factsSpec{Noun: "fact", Key: "id", Extra: []PropSpec{{Name: "id"}, {Name: "colour"}},
			List: func(Ctx, ListQuery) ([]Thing, error) { return []Thing{{"id": "f1", "colour": colour}}, nil }})
		if err := r.Settings().Set(OverlayConflictSetting, value); err != nil {
			t.Fatal(err)
		}
		mustRun(t, r, Request{Noun: "fact", Verb: "set", Args: Arguments{"id": "f1", "prop": "colour", "value": "red"}})
		if got, _ := r.Execute(Request{Noun: "fact", Verb: "get", Args: Arguments{"id": "f1", "prop": "colour"}}); got != "red" {
			t.Fatalf("%s: before the source changed: %v", value, got)
		}
		colour = "blue" // the source changes it
		want := map[string]string{OverlayKeepMine: "red", OverlayTakeSource: "blue"}[value]
		if got, _ := r.Execute(Request{Noun: "fact", Verb: "get", Args: Arguments{"id": "f1", "prop": "colour"}}); got != want {
			t.Fatalf("%s: get %v, want %s", value, got, want)
		}
		changes, err := r.OverlayChanges(Ctx{Runtime: r}, "fact", "f1")
		if err != nil || len(changes) != 1 {
			t.Fatalf("%s: changes %+v %v", value, changes, err)
		}
		ch := changes[0]
		shown := map[string]string{OverlayKeepMine: OverlayShownYours, OverlayTakeSource: OverlayShownSource}[value]
		if ch.Yours != "red" || ch.Original != "green" || ch.Current != "blue" || !ch.OriginalChanged || ch.Shown != shown || ch.By == nil {
			t.Fatalf("%s: change %+v", value, ch)
		}
		if row := r.overlayRow("fact", "f1"); row == nil || row.Props["colour"] != "red" {
			t.Fatalf("%s: a read dropped the value: %+v", value, row)
		}
	}
}
