package inspect

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func loadCatalog(t *testing.T, name string) *Catalog {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	var c Catalog
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return &c
}

func fixtureData(t *testing.T) *Data {
	t.Helper()
	host, old := loadCatalog(t, "host.json"), loadCatalog(t, "old.json")
	var actions []UserAction
	b, err := os.ReadFile("testdata/actions.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &actions); err != nil {
		t.Fatal(err)
	}
	// The older tool is listed first: the newer ochazuke must still win.
	srcs := []Source{
		{Tool: ToolInfo{Name: "o-ext", Ochazuke: "v0.9.0", Role: "extension"}, Catalog: old},
		{Tool: ToolInfo{Name: "host", Ochazuke: "v0.20.0", Role: "host"}, Catalog: host},
	}
	return Merge(srcs, map[string][]string{"host": {"ext"}}, actions, "host")
}

func index(d *Data) (map[string]*Noun, map[string]*Verb) {
	ns, vs := map[string]*Noun{}, map[string]*Verb{}
	for _, n := range d.Nouns {
		ns[n.ID] = n
	}
	for _, v := range d.Verbs {
		vs[v.ID] = v
	}
	return ns, vs
}

func TestParamsKeepOrder(t *testing.T) {
	c := loadCatalog(t, "host.json")
	var got []string
	for _, p := range c.Nouns[1].Verbs[1].Params {
		got = append(got, p.Name)
	}
	if want := []string{"project", "scratch", "task"}; !equal(got, want) {
		t.Fatalf("param order %v, want %v", got, want)
	}
}

func TestFrameworkNounsDeduplicated(t *testing.T) {
	d := fixtureData(t)
	nouns, verbs := index(d)

	list := nouns["ochazuke:ochazuke.list"]
	if list == nil {
		t.Fatal("ochazuke.list missing")
	}
	if list.Owner != "ochazuke" || list.NS != "ochazuke." {
		t.Fatalf("ochazuke.list owner/ns = %s/%s", list.Owner, list.NS)
	}
	if !equal(sorted(list.Tools), []string{"host", "o-ext"}) {
		t.Fatalf("ochazuke.list tools = %v", list.Tools)
	}
	// The bare "list" of the older catalog is not a separate noun.
	if _, ok := nouns["o-ext:list"]; ok {
		t.Fatal("o-ext:list should fold into ochazuke.list")
	}
	move := verbs["ochazuke:ochazuke.list#move"]
	if move == nil || move.Owner != "ochazuke" || !equal(sorted(move.Tools), []string{"host", "o-ext"}) {
		t.Fatalf("move = %+v", move)
	}
	if move.Description != "" || len(move.Params) != 1 {
		t.Fatalf("move should come from the newest ochazuke (host), got desc %q params %v", move.Description, move.Params)
	}
	legacy := verbs["ochazuke:ochazuke.list#legacy"]
	if legacy == nil || !equal(legacy.Tools, []string{"o-ext"}) {
		t.Fatalf("legacy (older ochazuke only) = %+v", legacy)
	}
	// Exactly one noun per framework name.
	count := 0
	for _, n := range d.Nouns {
		if n.Name == "ochazuke.list" || n.Name == "list" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("%d list nouns, want 1", count)
	}
	// A tool's own verb on a framework noun keeps the tool as owner.
	if c := verbs["ochazuke:ochazuke.terminal#copy"]; c == nil || c.Owner != "host" {
		t.Fatalf("terminal.copy = %+v", c)
	}
	if !equal(nouns["ochazuke:ochazuke.terminal"].Aliases, []string{"terminal"}) {
		t.Fatalf("aliases = %v", nouns["ochazuke:ochazuke.terminal"].Aliases)
	}
}

func TestAppNounsStayWithTheirTool(t *testing.T) {
	d := fixtureData(t)
	nouns, verbs := index(d)
	// o-ext reuses "settings" for its own kind (no builtin verbs): an app noun.
	if s := nouns["o-ext:settings"]; s == nil || s.Owner != "o-ext" || s.NS != "app" {
		t.Fatalf("o-ext settings = %+v", s)
	}
	// Each tool keeps its own app noun; quit is builtin but the app's.
	for _, id := range []string{"host:app", "o-ext:app"} {
		if nouns[id] == nil {
			t.Fatalf("%s missing", id)
		}
	}
	if q := verbs["host:app#quit"]; q == nil || !q.Builtin || q.Owner != "host" {
		t.Fatalf("host app.quit = %+v", q)
	}
	// Mirrors: host's ext-agent is o-ext's agent.
	if m := nouns["host:ext-agent"]; m == nil || m.NS != "ext:ext" || m.MirrorOf != "o-ext:agent" {
		t.Fatalf("ext-agent = %+v", m)
	}
	// Positional index carried onto the param.
	a := verbs["host:pane#associate"]
	if a.Params[0].Positional != 0 || a.Params[1].Positional != -1 {
		t.Fatalf("positional = %+v", a.Params)
	}
}

func TestBindingsResolve(t *testing.T) {
	d := fixtureData(t)
	byTool := map[string][]KeyRow{}
	for _, k := range d.Keys {
		byTool[k.Tool] = append(byTool[k.Tool], k)
	}
	if len(byTool["host"]) != 3 || len(byTool["o-ext"]) != 3 {
		t.Fatalf("rows per tool: host %d, o-ext %d", len(byTool["host"]), len(byTool["o-ext"]))
	}
	find := func(tool, keys string) KeyRow {
		for _, k := range byTool[tool] {
			if k.Keys == keys {
				return k
			}
		}
		t.Fatalf("%s %s not found", tool, keys)
		return KeyRow{}
	}
	if k := find("o-ext", "j"); k.Scope != "ochazuke:ochazuke.list" || k.VerbID != "ochazuke:ochazuke.list#move" {
		t.Fatalf("old bare list row = %+v", k)
	}
	if k := find("host", "enter"); k.VerbID != "host:pane#attach" {
		t.Fatalf("enter = %+v", k)
	}
	// Requests naming no verb stay unresolved (o-ext's settings.undo, host's pane.gone).
	if k := find("o-ext", "u"); k.VerbID != "" || k.Scope != "o-ext:settings" {
		t.Fatalf("settings.undo = %+v", k)
	}
	if k := find("host", "x"); k.VerbID != "" {
		t.Fatalf("pane.gone = %+v", k)
	}
}

func TestUserActions(t *testing.T) {
	d := fixtureData(t)
	nouns, verbs := index(d)
	if len(d.Actions) != 2 {
		t.Fatalf("actions = %v", d.Actions)
	}
	v := verbs["host:pane#open-log"]
	if v == nil || !v.User || v.Owner != "user" || v.Confirm != "Open?" || v.Command != "open {{data.log}}" {
		t.Fatalf("open-log = %+v", v)
	}
	if w := nouns["host:widget"]; w == nil || !w.UserOnly {
		t.Fatalf("widget = %+v", w)
	}
}

func TestToolCounts(t *testing.T) {
	d := fixtureData(t)
	var fw, host *ToolInfo
	for i := range d.Tools {
		switch d.Tools[i].Name {
		case "ochazuke":
			fw = &d.Tools[i]
		case "host":
			host = &d.Tools[i]
		}
	}
	if fw == nil || fw.Version != "v0.20.0" || fw.Nouns != 2 {
		t.Fatalf("framework tool = %+v", fw)
	}
	// list.move, list.pick, list.legacy, terminal.scroll
	if fw.Verbs != 4 {
		t.Fatalf("framework verbs = %d", fw.Verbs)
	}
	if host.Bindings != 3 || host.BindingSource != "catalog" {
		t.Fatalf("host = %+v", host)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sorted(a []string) []string {
	out := append([]string(nil), a...)
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// A host's mount of an extension's noun (cli.MountNouns) has the props but
// no store kind: the page still draws its props.
func TestMergeKeepsAMountedNounsProps(t *testing.T) {
	host := &Catalog{Name: "host", Nouns: []CatNoun{{Name: "ext-task", Props: []Prop{{Name: "id", Key: true}, {Name: "title", Role: "label"}},
		Verbs: []CatVerb{{Name: "set", Kind: "application", Standard: "set"}}}}}
	d := Merge([]Source{{Tool: ToolInfo{Name: "host", Role: RoleHost}, Catalog: host}}, nil, nil, "host")
	nouns, _ := index(d)
	n := nouns["host:ext-task"]
	if n == nil || len(n.Props) != 2 || n.Kind != "" {
		t.Fatalf("mounted noun %+v (nouns %v)", n, nouns)
	}
}
