package config

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
)

func TestStripJSONC(t *testing.T) {
	src := `{
  // line
  "url": "http://x.y/*not*/ // keep", /* block
  spans */ "a": [1, 2, /* c */ ],
  "b": {"c": 1,},
}`
	var v map[string]any
	if err := json.Unmarshal(StripJSONC([]byte(src)), &v); err != nil {
		t.Fatal(err)
	}
	if v["url"] != "http://x.y/*not*/ // keep" {
		t.Fatalf("url %v", v["url"])
	}
	if !reflect.DeepEqual(v["a"], []any{1.0, 2.0}) {
		t.Fatalf("a %v", v["a"])
	}
	if got := string(StripJSONC([]byte(`["a\"//b",]`))); got != `["a\"//b"]` {
		t.Fatal(got)
	}
}

var modes = []core.Mode{{Name: "normal"}}

func TestMergeBindingsUnbind(t *testing.T) {
	defs := []core.Binding{{Mode: "normal", Keys: []string{"x"}, Command: "motion.down"}, {Mode: "normal", Keys: []string{"y"}, Command: "motion.up"}}
	over, err := LoadBindings([]byte(`[
  // drop x, remap y
  {"mode":"normal","keys":["x"],"unbind":true},
  {"mode":"normal","keys":["y"],"command":"motion.top"},
]`))
	if err != nil {
		t.Fatal(err)
	}
	km, err := core.NewKeymap(modes, MergeBindings(defs, over))
	if err != nil {
		t.Fatal(err)
	}
	chain := []core.ContextFrame{{Scope: "app"}}
	res := func(key string) core.Command {
		r := km.Resolve(core.KeyQuery{Mode: "normal", Key: key, Chain: chain})
		if r.Command == nil {
			return core.Command{}
		}
		return *r.Command
	}
	if c := res("x"); c.ID != "" {
		t.Fatalf("x should be unbound, got %q", c.ID)
	}
	if c := res("y"); c.ID != "motion.top" {
		t.Fatalf("y: %q", c.ID)
	}
	if _, err := LoadBindings([]byte(`{"bindings":[{"keys":["a"],"command":"app.quit"}]}`)); err != nil {
		t.Fatal(err)
	}
}

func TestCheckBindings(t *testing.T) {
	bs := []core.Binding{
		{Mode: "normal", Keys: []string{"a"}, Command: "app.quit"},
		{Mode: "normal", Keys: []string{"a"}, Command: "app.quit"},
		{Mode: "normal", Keys: []string{"b"}, Command: "nope.cmd"},
		{Mode: "normal", Keys: []string{"c"}, Command: "app.quit", When: "a &&"},
		{Mode: "ghost", Keys: []string{"d"}, Command: "app.quit"},
		{Mode: "normal", Command: "app.quit"},
		{Mode: "normal", Keys: []string{"e"}, Command: "custom.cmd"},
	}
	diags := CheckBindings(modes, bs, core.CommandSpec{ID: "custom.cmd"})
	got := map[int]string{}
	for _, d := range diags {
		got[d.Index] += d.Severity + ":" + d.Message + ";"
	}
	for i, frag := range map[int]string{1: "shadowed", 2: "unknown command", 3: "bad when", 4: "unknown mode", 5: "needs keys"} {
		if !strings.Contains(got[i], frag) {
			t.Errorf("binding %d: %q lacks %q", i, got[i], frag)
		}
	}
	if got[0] != "" || got[6] != "" {
		t.Fatalf("unexpected diagnostics %v", got)
	}
}

func TestLoadTheme(t *testing.T) {
	base := ui.DefaultDarkTheme()
	th, err := LoadTheme([]byte(`{"roles":{"accent":"#ff0000"}, // c
 "tones":{"mine":{"fg":"accent","bold":true}},}`), base)
	if err != nil {
		t.Fatal(err)
	}
	if th.Roles["accent"] != "#ff0000" || th.Roles["text"] != base.Roles["text"] || !th.Tones["mine"].Bold || th.Tones["error"] != base.Tones["error"] {
		t.Fatal("merge")
	}
	if th.Version != base.Version+1 || base.Roles["accent"] == "#ff0000" || th.Name != base.Name || !th.Dark {
		t.Fatal("version/immutability")
	}
}

func TestSettings(t *testing.T) {
	s, err := LoadSettings([]byte(`{"a": true, "n": 4, "f": 1.5, "s": "x", "l": ["p","q"],}`), map[string]any{"n": 1, "d": "def", "a": false})
	if err != nil {
		t.Fatal(err)
	}
	if !s.Bool("a", false) || s.Int("n", 0) != 4 || s.Float("f", 0) != 1.5 || s.String("s", "") != "x" || s.String("d", "") != "def" {
		t.Fatal("getters")
	}
	if !reflect.DeepEqual(s.Strings("l", nil), []string{"p", "q"}) || s.Int("missing", 9) != 9 || s.Bool("s", true) != true {
		t.Fatal("fallbacks")
	}
	if v, ok := s.Raw("d"); !ok || v != "def" || len(s.Merged()) != 6 {
		t.Fatalf("raw/merged %v", s.Merged())
	}
	if _, err := LoadSettings([]byte(`[1]`), nil); err == nil {
		t.Fatal("want error")
	}
}

func TestLoadView(t *testing.T) {
	in := ui.ViewNode{Kind: ui.KindVStack, Padding: [4]int{1, 0, 1, 0}, Children: []ui.ViewNode{
		{ID: "t", Kind: ui.KindText, Props: map[string]any{"text": "hi"}},
	}}
	b, _ := json.Marshal(in)
	out, err := LoadView(b)
	if err != nil {
		t.Fatal(err)
	}
	if out.Kind != ui.KindVStack || out.Padding != in.Padding || out.Children[0].Props["text"] != "hi" {
		t.Fatalf("%+v", out)
	}
	if _, err := LoadView([]byte(`{"kind":"vstack","children":[{"id":"x"}]}`)); err == nil {
		t.Fatal("want missing kind error")
	}
}
