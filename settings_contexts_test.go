package ochazuke

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func nounRef(key string) Ref { return Ref{Noun: ContextNoun, Key: key} }

// A setting's points are its row's, then its section's; an owner's section
// without contexts takes the every-owner declaration's.
func TestSettingPointsMergeRowAndSection(t *testing.T) {
	r := NewRuntime()
	if err := r.DeclareSettingSection(SettingSection{ID: "tiles", Contexts: []Ref{nounRef("tile"), nounRef("app")}}); err != nil {
		t.Fatal(err)
	}
	if err := r.DeclareSetting(Setting{Name: "tiles.wide", Type: SettingBoolean, Default: false, Contexts: []Ref{{Noun: ContextPage, Key: "ctx"}, nounRef("tile")}}); err != nil {
		t.Fatal(err)
	}
	want := []SettingPoint{{Ref{ContextPage, "ctx"}, PointFromRow}, {nounRef("tile"), PointFromRow}, {nounRef("app"), PointFromSection}}
	if got := r.Settings().Points("tiles.wide"); !reflect.DeepEqual(got, want) {
		t.Fatalf("points %+v", got)
	}
	shown, err := r.Execute(Request{Noun: "ochazuke.setting", Verb: "show", Args: Arguments{"name": "tiles.wide", "verbose": true}})
	if err != nil {
		t.Fatal(err)
	}
	if got := shown.(SettingShown); !reflect.DeepEqual(got.Points, want) || !strings.Contains(got.Line, "applies at ochazuke.page:ctx, ochazuke.noun:tile, ochazuke.noun:app") {
		t.Fatalf("show %+v", got)
	}
}

// setting check warns on a point that names nothing or no context noun,
// and on a section whose settings have no point; the setting still loads.
func TestSettingCheckWarnsOnUnknownContexts(t *testing.T) {
	r := NewRuntime()
	if err := r.DeclareSetting(
		Setting{Name: "tiles.wide", Type: SettingBoolean, Default: false, Contexts: []Ref{nounRef("no-such-noun"), {Noun: "tile", Key: "a"}}},
		Setting{Name: "loose.one", Type: SettingBoolean, Default: false},
	); err != nil {
		t.Fatal(err)
	}
	got, err := r.Execute(Request{Noun: "ochazuke.setting", Verb: "check"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"warning: setting tiles.wide: context ochazuke.noun:no-such-noun names nothing here (kept)",
		"warning: setting tiles.wide: context tile:a: tile is not a context noun (ochazuke.instance, ochazuke.mode, ochazuke.noun, ochazuke.page)",
		"warning: section loose of app: 1 settings have no context point, so they show at no level of a walk: loose.one",
	}
	var mine []string
	for _, line := range got.([]string) {
		if strings.Contains(line, "tiles.wide") || strings.Contains(line, "loose") {
			mine = append(mine, line)
		}
	}
	if !slices.Equal(mine, want) {
		t.Fatalf("check %q", got)
	}
	if pts := r.Settings().Points("tiles.wide"); len(pts) != 2 {
		t.Fatalf("an unknown point still loads: %+v", pts)
	}
}

// setting.list {at} places each setting at the innermost walk level one of
// its points matches, in walk order, the settings on no level last.
func TestSettingListAtPlacesOnTheWalk(t *testing.T) {
	r := helpWalkFixture(t, helpWalkRows)
	if err := r.DeclareSetting(
		Setting{Name: "t.off", Type: SettingBoolean, Default: false, Contexts: []Ref{nounRef(FrameworkNamespace + ".palette")}},
		Setting{Name: "t.app", Type: SettingBoolean, Default: false, Contexts: []Ref{nounRef(AppNoun)}},
		Setting{Name: "t.page", Type: SettingBoolean, Default: false, Contexts: []Ref{{Noun: ContextPage, Key: "ctx"}}},
		Setting{Name: "t.panel", Type: SettingBoolean, Default: false, Contexts: []Ref{{Noun: ContextInstance, Key: "panel"}}},
		Setting{Name: "t.both", Type: SettingBoolean, Default: false, Contexts: []Ref{{Noun: ContextPage, Key: "ctx"}, nounRef("tile")}},
		Setting{Name: "t.mode", Type: SettingBoolean, Default: false, Contexts: []Ref{{Noun: ContextMode, Key: "normal"}}},
		Setting{Name: "t.insert", Type: SettingBoolean, Default: false, Contexts: []Ref{{Noun: ContextMode, Key: "insert"}}},
	); err != nil {
		t.Fatal(err)
	}
	list, err := r.Execute(Request{Noun: "ochazuke.setting", Verb: "list", Args: Arguments{"at": r.Focus, "where": "owner=app"}})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(AnswerRows(list))
	var rows []struct {
		Name  string `json:"name"`
		Level int    `json:"level"`
		Label string `json:"level_label"`
		Role  string `json:"level_role"`
	}
	_ = json.Unmarshal(data, &rows)
	var got []string
	for _, row := range rows {
		got = append(got, strings.Join([]string{row.Name, row.Label, row.Role}, " "))
	}
	want := []string{
		"t.both tile focus", "t.mode tile focus",
		"t.panel panel container",
		"t.page ochazuke.page page",
		"t.app app app",
		"t.off not on this walk ", "t.insert not on this walk ",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("list at %s:\n%s", r.Focus, strings.Join(got, "\n"))
	}
}
