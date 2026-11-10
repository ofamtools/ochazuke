package ochazuke

import (
	"strings"
	"testing"
)

func emptyIndexFixture(t *testing.T, required, paged bool) (*Runtime, *Model, *Collection) {
	t.Helper()
	r := NewRuntime()
	r.Config().Disable()
	store := DocStore(NewMemDocTable(r, "parcel"), "parcel")
	if !paged {
		store.Query = nil
	}
	r.Define("parcel").Props(
		PropSpec{Name: "id", Key: true},
		PropSpec{Name: "title", Role: RoleLabel, Parameter: Parameter{Required: required}},
		PropSpec{Name: "archived", Role: RoleArchive, Parameter: Parameter{Type: SettingBoolean}},
	).Store(store)
	if _, err := r.AddPage("work", "Work", func(*Runtime, int, int) Block { return Text("work") }); err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	m.Init()
	m.Width, m.Height = 100, 30
	if err := r.OpenIndex("parcel", ""); err != nil {
		t.Fatal(err)
	}
	return r, m, r.List(IndexPageID("parcel") + "/list")
}

func TestEmptyIndexGeneratedCreate(t *testing.T) {
	for _, tc := range []struct {
		name            string
		required, paged bool
	}{
		{name: "windowed form", required: true, paged: true},
		{name: "windowed immediate", paged: true},
		{name: "whole list form", required: true},
		{name: "whole list immediate"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, _, list := emptyIndexFixture(t, tc.required, tc.paged)
			if r.Focus != list.id || list.Len() != 0 {
				t.Fatalf("empty index: focus %q, rows %d", r.Focus, list.Len())
			}
			for _, inst := range r.Instances {
				if inst.Noun == "parcel" {
					t.Fatalf("empty index fabricated a parcel: %+v", inst)
				}
			}
			if handled, err := r.Dispatch(MacroKey{Key: "n"}); err != nil || !handled {
				t.Fatalf("n on an empty index: handled %v, error %v", handled, err)
			}
			if tc.required {
				if r.TopModal() != ArgsFormID || r.Focus != ArgsFormID+"/title" || list.Len() != 0 {
					t.Fatalf("new asks for the required title without creating: modal %q, focus %q, rows %d", r.TopModal(), r.Focus, list.Len())
				}
				typeKeys(t, r, "First parcel")
				formKeys(t, r, "enter")
			}
			things, err := r.Nouns["parcel"].Store.List(Ctx{Runtime: r, Context: r.Context()}, ListQuery{})
			if err != nil || len(things) != 1 || list.Len() != 1 || r.TopModal() != "" {
				t.Fatalf("new persisted and refilled: things %v, error %v, rows %d, modal %q", things, err, list.Len(), r.TopModal())
			}
			if tc.required && things[0]["title"] != "First parcel" {
				t.Fatalf("created title %v", things[0]["title"])
			}
			item, selected := list.Selected()
			if !selected || item.Data.(Thing)["id"] != things[0]["id"] || r.Focus != item.ID || r.Instances[item.ID].Noun != "parcel" {
				t.Fatalf("new selects its real row: %+v, selected %v, focus %q", item, selected, r.Focus)
			}
		})
	}
}

func TestEmptyIndexEffectiveKeys(t *testing.T) {
	r, _, list := emptyIndexFixture(t, false, true)
	r.Define("parcel").Verb("refresh", func(c Ctx) Result { return Done(c.Instance.ID) }, "Refresh parcels").
		Mode("normal").Bind("x", Do("parcel.refresh").Hinted("refresh parcels"))
	listed := map[string]string{}
	for _, b := range r.ListBindings(r.Focus) {
		listed[b.Keys] = b.Request.Key()
		if b.Keys == "n" && b.Request.Target != "" {
			t.Fatalf("empty context must not invent a target: %+v", b)
		}
	}
	for key, want := range map[string]string{"n": "parcel.create", "x": "parcel.refresh", "j": "ochazuke.collection.move", "z A": "ochazuke.collection.show-archived"} {
		if listed[key] != want {
			t.Errorf("binding.list %q = %q, want %q", key, listed[key], want)
		}
	}
	found := false
	for _, row := range BindingHelp(r, r.Focus) {
		found = found || row.Key == "n" && row.Request == "parcel.create" && row.Noun == "parcel"
	}
	if !found {
		t.Fatal("help omits the empty index's generated creation key")
	}
	if hints := BindingHints(r, r.Focus); !strings.Contains(hints, "x refresh parcels") {
		t.Fatalf("empty row noun's visible binding absent from hints: %s", hints)
	}
	// Help describes its origin beneath its own modal, not that modal's row noun.
	formKeys(t, r, "?")
	if r.TopModal() != helpID {
		t.Fatalf("help modal %q", r.TopModal())
	}
	found = false
	for _, item := range r.List(helpListID).Items() {
		if row, ok := item.Data.(HelpRow); ok && row.Key == "n" && row.Request == "parcel.create" {
			found = true
		}
	}
	if !found {
		t.Fatal("open help must retain the creation key from its empty origin")
	}
	if err := r.Pop(); err != nil {
		t.Fatal(err)
	}
	formKeys(t, r, "z", "A")
	if !list.ShowsArchived() || r.Focus != list.id || list.Len() != 0 {
		t.Fatalf("collection keys still work: archived %v, focus %q, rows %d", list.ShowsArchived(), r.Focus, list.Len())
	}
	// A populated list reads its real row's own overrides, not an empty context.
	mustRun(t, r, Request{Noun: "parcel", Verb: "create", Args: Arguments{"title": "Real parcel"}})
	item, _ := list.Selected()
	r.Define("app").Verb("inspect", func(c Ctx) Result { c.Info(c.Instance.ID); return Done(nil) }, "Inspect")
	if err := r.Bind(Binding{Instance: item.ID, Mode: "normal", Keys: "x", Request: Do("app.inspect").Request}); err != nil {
		t.Fatal(err)
	}
	sink := &RecordingSink{}
	r.UI().Attach(sink)
	formKeys(t, r, "x")
	if sink.LastInfo() != item.ID {
		t.Fatalf("populated row override lost precedence: %q", sink.LastInfo())
	}
}

func TestEmptyIndexDeclaredKeysWin(t *testing.T) {
	r, _, list := emptyIndexFixture(t, false, true)
	r.Define("app").Verb("inspect", func(c Ctx) Result {
		c.Info("collection override")
		return Done(nil)
	}, "Inspect")
	if err := r.Bind(Binding{Instance: list.id, Mode: "normal", Keys: "n", Request: Do("app.inspect").Request}); err != nil {
		t.Fatal(err)
	}
	sink := &RecordingSink{}
	r.UI().Attach(sink)
	formKeys(t, r, "n")
	if sink.LastInfo() != "collection override" || list.Len() != 0 {
		t.Fatalf("declared collection key must beat generated new: info %q, rows %d", sink.LastInfo(), list.Len())
	}
}

func TestEmptyIndexSubjectAndOptOut(t *testing.T) {
	r, _, list := emptyIndexFixture(t, false, true)
	formKeys(t, r, "v")
	if r.TopModal() != ArgsFormID || r.Focus != ArgsFormID+"/id" || list.Len() != 0 {
		t.Fatalf("show must ask for a real subject: modal %q, focus %q, rows %d", r.TopModal(), r.Focus, list.Len())
	}
	if err := r.Pop(); err != nil {
		t.Fatal(err)
	}
	if err := r.Settings().Set(MissingOpensFormSetting, false); err != nil {
		t.Fatal(err)
	}
	handled, err := r.Dispatch(MacroKey{Key: "v"})
	if need := needed(err); !handled || need == nil || need.Param != "id" || r.TopModal() != "" {
		t.Fatalf("show without forms refuses with its missing subject: handled %v, error %v", handled, err)
	}
	if err := r.Settings().Set(NounDefaultsSetting, false); err != nil {
		t.Fatal(err)
	}
	if handled, err := r.Dispatch(MacroKey{Key: "n"}); err != nil || handled {
		t.Fatalf("keys.noun-defaults off: handled %v, error %v", handled, err)
	}
	for _, b := range r.ListBindings(r.Focus) {
		if b.Keys == "n" && b.Request.Key() == "parcel.create" {
			t.Fatal("disabled generated key still offered")
		}
	}
	if err := r.Settings().Set(NounDefaultsSetting, true); err != nil {
		t.Fatal(err)
	}
	formKeys(t, r, "n")
	if list.Len() != 1 {
		t.Fatal("re-enabling noun defaults must restore creation")
	}
}

func TestEmptyListRowNounKeysAndMotions(t *testing.T) {
	r, m, a, _ := motionPage(t)
	r.Config().Disable()
	at := ""
	r.Define("row").Verb("refresh", func(c Ctx) Result {
		at = c.Instance.ID + ":" + c.Instance.Noun
		return Done(nil)
	}, "Refresh rows").Mode("normal").Bind("x", Do("row.refresh"))
	a.SetFilter("no-match")
	formKeys(t, r, "x")
	if at != "a:ochazuke.collection" || a.Shown() != 0 || a.Len() != 40 {
		t.Fatalf("filtered row noun answers at the collection: %q, shown %d, rows %d", at, a.Shown(), a.Len())
	}
	a.SetFilter("")
	if err := a.SetItems(nil); err != nil {
		t.Fatal(err)
	}
	formKeys(t, r, "x")
	if at != "a:ochazuke.collection" {
		t.Fatalf("remembered row noun answers without an instance: %q", at)
	}
	_ = m.View()
	formKeys(t, r, "j")
	if r.Focus != "b1" {
		t.Fatalf("empty collection must still spill: focus %q", r.Focus)
	}
	formKeys(t, r, "k")
	if r.Focus != "a" {
		t.Fatalf("motion returns to the empty collection: focus %q", r.Focus)
	}
	declared, err := r.AddList("declared", nil, Parent("p"), RowNoun("row"))
	if err != nil {
		t.Fatal(err)
	}
	if err := declared.Focus(); err != nil {
		t.Fatal(err)
	}
	formKeys(t, r, "x")
	if at != "declared:ochazuke.collection" {
		t.Fatalf("declared row noun answers without rows: %q", at)
	}
	// A modal is a boundary: the empty collection beneath it lends no keys.
	if _, err := r.OpenModal("dialog", Payload{}); err != nil {
		t.Fatal(err)
	}
	if handled, err := r.Dispatch(MacroKey{Key: "x"}); err != nil || handled {
		t.Fatalf("empty row context leaked into a modal: handled %v, error %v", handled, err)
	}
}
