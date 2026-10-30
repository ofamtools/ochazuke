package ochazuke

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

// Lane S7: the framework's own nouns on the standard verbs. ochazuke.setting
// is a collection of the declared settings (get, list, watch standard; the
// :set verbs named), ochazuke.page an instances noun of the pages (get,
// list, set, watch standard; page.hide and page.show set's aliases).

// The setting noun's things are the declared settings, keyed by name, each
// with its type, default, value, enum, description, section, owner and
// origin; list filters on them and get reads one prop or the record.
func TestSettingThingsAndProps(t *testing.T) {
	r, _, _ := settingsFixture(t)
	if got := r.Noun("setting").StoreKind(); got != Collection {
		t.Fatalf("setting kind %q", got)
	}
	var props []string
	for _, p := range r.Props("setting") {
		props = append(props, p.Name)
		if !p.Key && p.writable() {
			t.Fatalf("setting prop %s is writable", p.Name)
		}
	}
	if want := []string{"name", "type", "default", "value", "enum", "description", "section", "owner", "group", "origin"}; !reflect.DeepEqual(props, want) {
		t.Fatalf("setting props %v", props)
	}
	rec := paletteRequest(t, r, "get", "setting", Arguments{"name": "theme"}).(map[string]any)
	want := map[string]any{"name": "theme", "type": "string", "default": "dark", "value": "dark", "enum": []string{"dark", "light", "mono"},
		"description": "Colour theme", "section": "general", "owner": "app", "group": "", "origin": "default"}
	if !reflect.DeepEqual(rec, want) {
		t.Fatalf("setting get theme %#v", rec)
	}
	if got := paletteRequest(t, r, "get", "setting", Arguments{"name": "rename.select", "prop": "owner"}); got != "ochazuke" {
		t.Fatalf("rename.select owner %v", got)
	}
	if got := paletteRequest(t, r, "get", "setting", Arguments{"name": "rename.select", "prop": "section"}); got != "lists" {
		t.Fatalf("rename.select section %v", got)
	}
	// A setting set this session reads origin session; written, file.
	paletteRequest(t, r, "set", "setting", Arguments{"name": "scrolloff", "value": "5", "scope": ScopeFile})
	rec = paletteRequest(t, r, "get", "setting", Arguments{"name": "scrolloff", "props": "value,origin"}).(map[string]any)
	if rec["value"] != 5 || rec["origin"] != OriginFile {
		t.Fatalf("scrolloff after a file write %#v", rec)
	}
	rows := paletteRequest(t, r, "list", "setting", Arguments{"where": "owner=app", "props": "type"}).([]map[string]any)
	var ids []string
	for _, row := range rows {
		ids = append(ids, row["id"].(string))
	}
	if want := []string{"scrolloff", "theme", "wrap", "debounce", "ratio"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("setting list where owner=app %v", ids)
	}
	if rows[0]["type"] != "integer" {
		t.Fatalf("row %#v", rows[0])
	}
	all := paletteRequest(t, r, "list", "setting", nil).([]map[string]any)
	if len(all) != len(r.Settings().List()) {
		t.Fatalf("setting list has %d rows, %d settings declared", len(all), len(r.Settings().List()))
	}
	if _, err := r.Execute(Request{Noun: "setting", Verb: "get", Args: Arguments{"name": "nope"}}); err == nil || !strings.Contains(err.Error(), "unknown setting") {
		t.Fatalf("get of an unknown setting: %v", err)
	}
}

// The named :set verbs win over the standard ones: setting.set, show and
// reset are the :set plan's (no Standard mark), the store writes nothing,
// so no standard set, create or delete is generated, and :set is unchanged.
func TestNamedSettingSetStillWins(t *testing.T) {
	r, _, _ := settingsFixture(t)
	for _, verb := range []string{"set", "show", "reset", "toggle", "cycle"} {
		op, ok := r.Operations["ochazuke.setting."+verb]
		if !ok || op.Standard != "" {
			t.Fatalf("setting.%s: ok %v standard %q", verb, ok, op.Standard)
		}
	}
	for _, verb := range []string{"get", "list", "watch"} {
		if op := r.Operations["ochazuke.setting."+verb]; op.Standard != verb {
			t.Fatalf("setting.%s standard %q", verb, op.Standard)
		}
	}
	for _, verb := range []string{"create", "delete", "rename", "duplicate"} {
		if _, ok := r.Operations["ochazuke.setting."+verb]; ok {
			t.Fatalf("setting.%s is registered", verb)
		}
	}
	if _, ok := paletteRequest(t, r, "set", "setting", Arguments{"name": "theme", "value": "light"}).(SettingWritten); !ok {
		t.Fatal("setting set did not answer the named verb's SettingWritten")
	}
	if v := paletteRequest(t, r, "get", "setting", Arguments{"name": "theme", "prop": "value"}); v != "light" {
		t.Fatalf("value after setting.set %v", v)
	}
	// :set still reads settings and the one-value nouns.
	r.Settings().Set("theme", "dark")
	paletteRequest(t, r, "apply", "settings", Arguments{"line": "theme=mono"})
	if v := r.Settings().String("theme"); v != "mono" {
		t.Fatalf(":set theme=mono left %q", v)
	}
}

// On the Settings page the focused row is the subject: setting get with no
// name reads the row's setting (its Data is the setting's name).
func TestSettingGetFromTheFocusedRow(t *testing.T) {
	r, m, _ := settingsPageFixture(t)
	pressKeys(m, "ctrl+o")
	if err := r.SetFocus(settingRowPrefix + "wrap"); err != nil {
		t.Fatal(err)
	}
	if got := paletteRequest(t, r, "get", "setting", Arguments{"prop": "name"}); got != "wrap" {
		t.Fatalf("setting get on the wrap row read %v", got)
	}
}

// A watch of settings sees a setting change however it is made (here the
// Go API, which is no change request).
func TestSettingWatchFollowsChanges(t *testing.T) {
	r, _, _ := settingsFixture(t)
	var rec watchRecorder
	w, err := r.Watch("setting", Arguments{"name": "wrap"}, rec.add)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()
	wantEvents(t, rec.take(), "added wrap")
	if err := r.Settings().Set("wrap", true); err != nil {
		t.Fatal(err)
	}
	wantEvents(t, rec.take(), "modified wrap")
}

// The page noun is an instances noun: its things are the registered pages
// in order, whatever noun their instances have, with id, title, hidden and
// active; hidden is the one writable prop.
func TestPageThings(t *testing.T) {
	r, _ := pageHideFixture(t)
	if got := r.Noun("page").StoreKind(); got != Instances {
		t.Fatalf("page kind %q", got)
	}
	rows := paletteRequest(t, r, "list", "page", nil).([]map[string]any)
	if len(rows) != 3 || rows[0]["id"] != "alpha" || rows[0]["label"] != "ALPHA" || rows[0]["active"] != true || rows[1]["hidden"] != false {
		t.Fatalf("page list %v", rows)
	}
	// The current page is the subject when the id is omitted.
	if got := paletteRequest(t, r, "get", "page", Arguments{"prop": "id"}); got != "alpha" {
		t.Fatalf("page get on the current page %v", got)
	}
	if _, err := r.Execute(Request{Noun: "page", Verb: "get", Args: Arguments{"id": "alpha-editor"}}); err == nil || !strings.Contains(err.Error(), `unknown page "alpha-editor"`) {
		t.Fatalf("get of an instance that is no page: %v", err)
	}
	paletteRequest(t, r, "set", "page", Arguments{"id": "beta", "prop": "hidden", "value": "true"})
	if !r.PageHidden("beta") {
		t.Fatal("page set beta hidden true left beta in the tabs")
	}
	paletteRequest(t, r, "set", "page", Arguments{"id": "beta", "prop": "hidden", "toggle": true})
	if r.PageHidden("beta") {
		t.Fatal("toggle left beta hidden")
	}
	for _, prop := range []string{"title", "active", "id"} {
		if _, err := r.Execute(Request{Noun: "page", Verb: "set", Args: Arguments{"id": "beta", "prop": prop, "value": "x"}}); err == nil {
			t.Fatalf("page set %s was written", prop)
		}
	}
}

// page.hide and page.show are aliases of page.set with hidden preset: the
// old names stay reachable (actions, bindings, the CLI) and run the
// standard set, whose availability keeps the old rule.
func TestPageHideShowAreAliasesOfSet(t *testing.T) {
	r, _ := pageHideFixture(t)
	for name, value := range map[string]bool{"ochazuke.page.hide": true, "ochazuke.page.show": false} {
		a, ok := r.ActionAlias(name)
		if !ok || a.Action != "ochazuke.page.set" || a.Args["prop"] != "hidden" || a.Args["value"] != value {
			t.Fatalf("%s: %#v %v", name, a, ok)
		}
		if _, ok := r.Operations[name]; ok {
			t.Fatalf("%s is still an operation", name)
		}
		op, inv, ok := r.ResolveAction(name)
		if !ok || op.Key() != "ochazuke.page.set" || !inv.Alias {
			t.Fatalf("ResolveAction(%s) = %s %#v", name, op.Key(), inv)
		}
	}
	if aliases := r.aliasNames("ochazuke.page.set"); !slices.Contains(aliases, "page.hide") || !slices.Contains(aliases, "page.show") {
		t.Fatalf("set's aliases %v", aliases)
	}
	// Availability: hide is not offered for a hidden page, show not for
	// one in the tabs; without an id both are.
	hide := Request{Noun: "page", Verb: "hide", Args: Arguments{"id": "beta"}}
	show := Request{Noun: "page", Verb: "show", Args: Arguments{"id": "beta"}}
	avail := func(q Request) bool { return r.Available(r.CanonicalRequest(q), r.Focus, r.Mode) }
	if !avail(hide) || avail(show) {
		t.Fatalf("beta in the tabs: hide %v show %v", avail(hide), avail(show))
	}
	paletteRequest(t, r, "hide", "page", Arguments{"id": "beta"})
	if avail(hide) || !avail(show) {
		t.Fatalf("beta hidden: hide %v show %v", avail(hide), avail(show))
	}
	if !avail(Request{Noun: "page", Verb: "hide"}) || !avail(Request{Noun: "page", Verb: "show"}) {
		t.Fatal("hide and show with no id are not offered")
	}
	// A binding row may still name them.
	r.Define("app").Mode("normal").Bind("H", Do("ochazuke.page.hide"))
	if _, err := r.ValidateBindings(); err != nil {
		t.Fatalf("a row naming page.hide: %v", err)
	}
}

// An instances store with no Get or List of its own reads the noun's live
// instances: the key is the instance id, the label prop the instance's
// label, other props its Data record; the focus inside an instance makes it
// the subject; an id that is no live instance of the noun is unknown.
func TestInstancesStoreReadsLiveInstances(t *testing.T) {
	r := NewRuntime()
	r.Define("widget").Props(
		PropSpec{Name: "id", Key: true},
		PropSpec{Name: "name", Role: RoleLabel, ReadOnly: true},
		PropSpec{Name: "size", ReadOnly: true, Parameter: Parameter{Type: SettingInteger}},
	).Store(Store{Kind: Instances})
	w1 := r.Instance("w1", Kind("widget"))
	w1.Label, w1.Data = "First", map[string]any{"size": 3}
	r.Instance("w1-inner", Parent("w1"))
	r.Instance("w2", Kind("widget"))
	r.Instance("other")
	for _, verb := range []string{"get", "list", "watch"} {
		if _, ok := r.Operations["widget."+verb]; !ok {
			t.Fatalf("widget.%s is not generated", verb)
		}
	}
	if _, ok := r.Operations["widget.set"]; ok {
		t.Fatal("widget.set without Patch")
	}
	rows := paletteRequest(t, r, "list", "widget", nil).([]map[string]any)
	if len(rows) != 2 || rows[0]["id"] != "w1" || rows[0]["label"] != "First" || rows[0]["size"] != 3 || rows[1]["id"] != "w2" {
		t.Fatalf("widget list %v", rows)
	}
	if err := r.SetFocus("w1-inner"); err != nil {
		t.Fatal(err)
	}
	if got := paletteRequest(t, r, "get", "widget", Arguments{"prop": "name"}); got != "First" {
		t.Fatalf("widget get in w1 %v", got)
	}
	if _, err := r.Execute(Request{Noun: "widget", Verb: "get", Args: Arguments{"id": "other"}}); err == nil || !strings.Contains(err.Error(), `unknown widget "other"`) {
		t.Fatalf("get of another noun's instance: %v", err)
	}
}
