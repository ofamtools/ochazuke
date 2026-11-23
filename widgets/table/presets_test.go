package table

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPresetStoreCRUDAndIsolationByTableID(t *testing.T) {
	store := NewPresetStore()

	if err := store.Save("", Preset{Name: "bad"}); err == nil {
		t.Fatal("expected error for empty tableID")
	}
	if err := store.Save("tasks.inbox", Preset{Name: ""}); err == nil {
		t.Fatal("expected error for empty preset name")
	}

	p1 := Preset{
		Name:    "active",
		Filter:  "kind:go",
		Sort:    []SortKey{{Field: "size", Dir: SortDesc}, {Field: "name", Dir: SortAsc}},
		Group:   "kind",
		Default: true,
	}
	p2 := Preset{
		Name:   "small",
		Filter: "md",
		Sort:   []SortKey{{Field: "size", Dir: SortAsc}},
	}
	if err := store.Save("files.main", p1); err != nil {
		t.Fatalf("Save p1: %v", err)
	}
	if err := store.Save("files.main", p2); err != nil {
		t.Fatalf("Save p2: %v", err)
	}
	// Separate tableID (even for same record kind) stays completely independent.
	if err := store.Save("files.sidebar", Preset{Name: "active", Filter: "README", Default: true}); err != nil {
		t.Fatalf("Save sidebar: %v", err)
	}

	if got := store.List("files.main"); len(got) != 2 || got[0].Name != "active" || got[1].Name != "small" {
		t.Fatalf("List(files.main) = %+v", got)
	}
	if got := store.List("files.sidebar"); len(got) != 1 || got[0].Filter != "README" {
		t.Fatalf("List(files.sidebar) = %+v", got)
	}

	// Default switching enforces at most one default per tableID.
	def, ok := store.Default("files.main")
	if !ok || def.Name != "active" {
		t.Fatalf("Default = (%+v, %v), want active", def, ok)
	}
	if !store.SetDefault("files.main", "small") {
		t.Fatal("SetDefault(small) failed")
	}
	if store.SetDefault("files.main", "missing") {
		t.Fatal("SetDefault(missing) should return false")
	}
	def, ok = store.Default("files.main")
	if !ok || def.Name != "small" {
		t.Fatalf("Default after switch = (%+v, %v), want small", def, ok)
	}
	if list := store.List("files.main"); list[0].Default || !list[1].Default {
		t.Fatalf("only one default should be set: %+v", list)
	}

	// Clearing default via empty name.
	if !store.SetDefault("files.main", "") {
		t.Fatal("clearing default should return true")
	}
	if _, ok := store.Default("files.main"); ok {
		t.Fatal("expected no default after clear")
	}

	// Delete removes preset and cleans up empty tableID.
	if !store.Delete("files.main", "active") || store.Delete("files.main", "active") {
		t.Fatal("Delete return mismatch")
	}
	if got := len(store.List("files.main")); got != 1 {
		t.Fatalf("remaining count = %d, want 1", got)
	}
	if !store.Delete("files.main", "small") {
		t.Fatal("Delete small failed")
	}
	if got := store.TableIDs(); strings.Join(got, ",") != "files.sidebar" {
		t.Fatalf("TableIDs = %v, want [files.sidebar]", got)
	}
}

func TestPresetCaptureApplyAndJSONRoundTrip(t *testing.T) {
	src := sample()
	src.Query = "go"
	src.FilterField = "kind"
	src.MatchMode = MatchFuzzy
	src.CycleSort("size")
	src.AddSort("name") // size asc, name asc
	src.AddSort("size") // size desc, name asc
	src.GroupField = "kind"

	store := NewPresetStore()
	captured := store.Capture(src, "go-by-size")
	captured.Default = true
	if err := store.Save("proj.files", captured); err != nil {
		t.Fatalf("Save captured: %v", err)
	}

	// Embed inside a settings.json-like payload and round-trip.
	type settingsDoc struct {
		Theme        string      `json:"theme"`
		TablePresets PresetStore `json:"tablePresets"`
	}
	blob, err := json.Marshal(settingsDoc{Theme: "ochazuke-dark", TablePresets: *store})
	if err != nil {
		t.Fatalf("Marshal settings: %v", err)
	}
	if !strings.Contains(string(blob), `"dir":"desc"`) || !strings.Contains(string(blob), `"proj.files"`) {
		t.Fatalf("unexpected JSON: %s", string(blob))
	}

	var decoded settingsDoc
	if err := json.Unmarshal(blob, &decoded); err != nil {
		t.Fatalf("Unmarshal settings: %v", err)
	}

	// Apply to a fresh table and verify identical filtered/sorted/grouped output.
	dst := sample()
	if !decoded.TablePresets.Apply(dst, "proj.files", "go-by-size") {
		t.Fatal("Apply returned false")
	}
	if dst.Query != "go" || dst.FilterField != "kind" || dst.MatchMode != MatchFuzzy || dst.GroupField != "kind" {
		t.Fatalf("applied filter/group mismatch: %+v", dst)
	}
	if got := ids(dst.Filtered()); got != ids(src.Filtered()) {
		t.Fatalf("filtered rows = %s, want %s", got, ids(src.Filtered()))
	}
	if len(dst.SortKeys) != 2 || dst.SortKeys[0] != (SortKey{Field: "size", Dir: SortDesc}) || dst.SortKeys[1] != (SortKey{Field: "name", Dir: SortAsc}) {
		t.Fatalf("applied SortKeys = %+v", dst.SortKeys)
	}

	// ApplyDefault works on another fresh table.
	dst2 := sample()
	if !decoded.TablePresets.ApplyDefault(dst2, "proj.files") {
		t.Fatal("ApplyDefault returned false")
	}
	if got := ids(dst2.Filtered()); got != ids(src.Filtered()) {
		t.Fatalf("ApplyDefault rows = %s, want %s", got, ids(src.Filtered()))
	}

	// SortDir UnmarshalJSON also accepts integer values.
	var numericDir SortDir
	if err := json.Unmarshal([]byte(`2`), &numericDir); err != nil || numericDir != SortDesc {
		t.Fatalf("numeric SortDir unmarshal = (%v, %v), want SortDesc", numericDir, err)
	}
}
