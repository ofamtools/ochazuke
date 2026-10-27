package storymatrix

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/unixsuperhero/ochazuke"
	"github.com/unixsuperhero/ochazuke/examples/demo"
	"github.com/unixsuperhero/ochazuke/grid"
	"github.com/unixsuperhero/ochazuke/story"
)

const mapPath = "../../cmd/o-story/components.json"
const storiesDir = "../../cmd/o-story/stories"

func loadMap(t *testing.T) (*Map, []byte) {
	t.Helper()
	data, err := os.ReadFile(mapPath)
	if err != nil {
		t.Fatal(err)
	}
	m, err := ParseMap(data)
	if err != nil {
		t.Fatal(err)
	}
	return m, data
}

// TestComponentMapIsComplete: every noun the framework registers (every
// defined noun and every builtin operation's noun of FrameworkRuntime) is
// mapped to exactly one component, and the map names nothing the
// framework lacks.
func TestComponentMapIsComplete(t *testing.T) {
	m, _ := loadMap(t)
	r, err := FrameworkRuntime()
	if err != nil {
		t.Fatal(err)
	}
	if problems := m.CheckRuntime(r, FixtureNouns); len(problems) > 0 {
		t.Fatal(strings.Join(problems, "\n"))
	}
	if _, err := m.Derive(r, FixtureInstances); err != nil {
		t.Fatal(err)
	}
}

// TestFrameworkRuntimeIsEnough: the minimal runtime registers every
// builtin noun the demo application (with the grid) and every core story
// runtime register, so mapping it maps them all.
func TestFrameworkRuntimeIsEnough(t *testing.T) {
	m, _ := loadMap(t)
	r, err := FrameworkRuntime()
	if err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{}
	for _, n := range Universe(r, FixtureNouns) {
		known[n] = true
	}
	for _, c := range m.Components {
		for _, k := range c.Kinds {
			known[k] = true
		}
	}
	builtinNouns := func(r *ochazuke.Runtime) []string {
		var out []string
		for _, op := range r.Operations {
			if op.Builtin {
				out = append(out, op.Noun)
			}
		}
		return out
	}
	d := demo.New()
	grid.Register(d.Runtime)
	for _, n := range builtinNouns(d.Runtime) {
		if !known[n] {
			t.Errorf("the demo registers builtin noun %q the framework runtime lacks", n)
		}
	}
	stories, err := story.LoadDir(storiesDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range stories {
		if s.Base != "" {
			continue
		}
		ss, err := story.Build(s, story.Options{Mono: true})
		if err != nil {
			t.Fatalf("%s: %v", s.Name, err)
		}
		for _, n := range builtinNouns(ss.R) {
			if !known[n] {
				t.Errorf("%s: builtin noun %q the framework runtime lacks", s.Name, n)
			}
		}
		ss.Close()
	}
}

// TestMapRejectsOverlapAndGaps: a noun in two components fails to parse;
// a framework noun in none is reported.
func TestMapRejectsOverlapAndGaps(t *testing.T) {
	m, data := loadMap(t)
	twice := strings.Replace(string(data), `"nouns": ["focus"]`, `"nouns": ["focus", "list"]`, 1)
	if twice == string(data) {
		t.Fatal("fixture edit did not apply")
	}
	if _, err := ParseMap([]byte(twice)); err == nil || !strings.Contains(err.Error(), `noun "list" is mapped to`) {
		t.Fatalf("overlap: %v", err)
	}
	c := m.Component("focus")
	c.Nouns = nil
	r, err := FrameworkRuntime()
	if err != nil {
		t.Fatal(err)
	}
	problems := m.CheckRuntime(r, FixtureNouns)
	if !slices.Contains(problems, `noun "focus" is mapped to no component`) {
		t.Fatalf("gap not reported: %v", problems)
	}
}

// TestAreasExist: every story area belongs to a component, and every area
// a component names is a folder.
func TestAreasExist(t *testing.T) {
	m, _ := loadMap(t)
	entries, err := os.ReadDir(storiesDir)
	if err != nil {
		t.Fatal(err)
	}
	claimed := map[string]bool{}
	for _, c := range m.Components {
		for _, a := range c.Areas {
			claimed[a] = true
			if _, err := os.Stat(filepath.Join(storiesDir, a)); err != nil {
				t.Errorf("%s: area %q: %v", c.ID, a, err)
			}
		}
	}
	for _, e := range entries {
		if e.IsDir() && !claimed[e.Name()] {
			t.Errorf("story area %q belongs to no component", e.Name())
		}
	}
}

// TestMatrixIsByteStable: two builds from scratch give the same bytes.
func TestMatrixIsByteStable(t *testing.T) {
	_, data := loadMap(t)
	render := func() (string, string) {
		_, mx, err := Load(data)
		if err != nil {
			t.Fatal(err)
		}
		var text, js bytes.Buffer
		if err := mx.WriteText(&text); err != nil {
			t.Fatal(err)
		}
		if err := mx.WriteJSON(&js); err != nil {
			t.Fatal(err)
		}
		return text.String(), js.String()
	}
	t1, j1 := render()
	t2, j2 := render()
	if t1 != t2 || j1 != j2 {
		t.Fatal("matrix output differs between two runs")
	}
	ids := strings.Split(strings.TrimSpace(t1), "\n")
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			t.Fatalf("cell %s appears twice", id)
		}
		seen[id] = true
		if strings.ContainsAny(id, " \n\t*?[]\\") || strings.Count(id, "/") != 2 {
			t.Fatalf("cell id %q is not three glob-safe segments", id)
		}
	}
}

// covered checks a covering array: no row matches an exclude, the full
// params' product is there (less excluded rows), and every pair a valid
// row could hold appears.
func covered(t *testing.T, name string, params []Param, full int, excludes []Exclude, rows [][]int) {
	t.Helper()
	matches := func(e Exclude, get func(string) (string, bool)) bool {
		for a, v := range e.Match {
			got, ok := get(a)
			if !ok || got != v {
				return false
			}
		}
		return true
	}
	for _, r := range rows {
		get := func(a string) (string, bool) {
			i := index(params, a)
			if i < 0 {
				return "", false
			}
			return params[i].Values[r[i]], true
		}
		for _, e := range excludes {
			if matches(e, get) {
				t.Fatalf("%s: row %v matches exclude %v", name, r, e.Match)
			}
		}
	}
	for i := 0; i < len(params); i++ {
		for k := i + 1; k < len(params); k++ {
			for vi := range params[i].Values {
				for vk := range params[k].Values {
					get := func(a string) (string, bool) {
						switch a {
						case params[i].Name:
							return params[i].Values[vi], true
						case params[k].Name:
							return params[k].Values[vk], true
						}
						return "", false
					}
					impossible := false
					for _, e := range excludes {
						if matches(e, get) {
							impossible = true
						}
					}
					if impossible {
						continue
					}
					found := false
					for _, r := range rows {
						if r[i] == vi && r[k] == vk {
							found = true
							break
						}
					}
					if !found {
						t.Fatalf("%s: pair %s=%s %s=%s is covered by no row", name, params[i].Name, params[i].Values[vi], params[k].Name, params[k].Values[vk])
					}
				}
			}
		}
	}
	want := 1
	for _, p := range params[:min(full, len(params))] {
		want *= len(p.Values)
	}
	if full >= 2 && len(excludes) == 0 && len(rows) < want {
		t.Fatalf("%s: %d rows, fewer than the full product %d", name, len(rows), want)
	}
}

// TestPairwiseCoversEveryPair is the property check: synthetic params
// with and without excludes, and every action of two real components.
func TestPairwiseCoversEveryPair(t *testing.T) {
	vals := func(prefix string, n int) []string {
		var out []string
		for i := range n {
			out = append(out, fmt.Sprintf("%s%d", prefix, i))
		}
		return out
	}
	params := []Param{{"a", vals("a", 2)}, {"b", vals("b", 3)}, {"c", vals("c", 4)}, {"d", vals("d", 5)}, {"e", vals("e", 3)}, {"f", vals("f", 6)}}
	for full := 0; full <= 3; full++ {
		rows := Combos(params, full, nil)
		covered(t, fmt.Sprintf("full=%d", full), params, full, nil, rows)
		if full == 0 && len(rows) >= 2*3*4*5*3*6 {
			t.Fatalf("pairwise did not reduce: %d rows", len(rows))
		}
	}
	excl := []Exclude{
		{Match: map[string]string{"a": "a0", "d": "d1"}, Why: "t"},
		{Match: map[string]string{"c": "c3", "f": "f0"}, Why: "t"},
		{Match: map[string]string{"b": "b2", "e": "e0", "f": "f5"}, Why: "t"},
	}
	covered(t, "excludes", params, 2, excl, Combos(params, 2, excl))
	if a, b := Combos(params, 2, excl), Combos(params, 2, excl); fmt.Sprint(a) != fmt.Sprint(b) {
		t.Fatal("Combos is not deterministic")
	}

	m, data := loadMap(t)
	_, mx, err := Load(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"notices", "detail", "panes"} {
		c := m.Component(id)
		var cc ComponentCells
		for _, x := range mx.Components {
			if x.ID == id {
				cc = x
			}
		}
		keyModes := []string{}
		for _, a := range cc.Actions {
			if a.Channel == ChannelKey {
				keyModes = append(keyModes, a.Modes...)
			}
		}
		keyModes = m.inAxisOrder("mode", keyModes)
		excludes := append(slices.Clone(m.Exclude), c.Exclude...)
		for _, a := range cc.Actions {
			if a.Channel == ChannelJourney {
				continue
			}
			params, full := m.params(c, a, keyModes)
			covered(t, id+"/"+a.ID, params, full, excludes, Combos(params, full, excludes))
		}
	}
}

// TestCoversParseAndMatch: covers is optional, parses, rejects a bad
// pattern; globs match ids and leading segments.
func TestCoversParseAndMatch(t *testing.T) {
	base := `{"name": "x", "area": "tables", "page": {"lists": [{"id": "a", "rows": 2}]}%s}`
	s, err := story.Parse([]byte(fmt.Sprintf(base, "")), "tables/x.json")
	if err != nil || s.Covers != nil {
		t.Fatalf("no covers: %v %v", s, err)
	}
	s, err = story.Parse([]byte(fmt.Sprintf(base, `, "covers": ["tables/list.move/*", "tables/~hosted"]`)), "tables/x.json")
	if err != nil || len(s.Covers) != 2 {
		t.Fatalf("covers: %v %v", s, err)
	}
	if _, err := story.Parse([]byte(fmt.Sprintf(base, `, "covers": ["tables/[x"]`)), "tables/x.json"); err == nil {
		t.Fatal("a malformed pattern parsed")
	}
	if _, err := story.Parse([]byte(fmt.Sprintf(base, `, "cover": ["x"]`)), "tables/x.json"); err == nil {
		t.Fatal("an unknown field parsed")
	}
	id := "tables/list.move(delta=1)/mode=normal,placement=page"
	for pattern, want := range map[string]bool{
		id:                                   true,
		"tables/list.move(delta=1)/*":        true,
		"tables/list.move*/*placement=page":  true,
		"tables/list.move(delta=1)":          true,
		"tables/*":                           true,
		"tables":                             true,
		"tables/list.move/*":                 false,
		"lists/*":                            false,
		"tables/list.move(delta=1)/*=modal*": false,
	} {
		if got := Matches(pattern, id); got != want {
			t.Errorf("Matches(%q) = %v, want %v", pattern, got, want)
		}
	}
}

// TestCoverageCounts runs the coverage on a fixture map and inventory.
func TestCoverageCounts(t *testing.T) {
	m, err := ParseMap([]byte(`{
  "axes": {"order": ["to", "mode", "focus", "size"], "full": ["to", "mode", "focus"],
           "values": {"mode": ["normal", "insert"], "focus": ["focused", "unfocused"], "size": ["narrow", "wide"]}},
  "modes": {"normal": "normal", "insert": "insert"},
  "channels": {"key": ["mode", "focus", "size"], "hosted": ["focus"], "spill": ["to"]},
  "exclude": [{"mode": "insert", "focus": "unfocused", "why": "insert is the focus's"}],
  "components": [
    {"id": "alpha", "nouns": ["a"], "areas": ["alpha"], "neighbours": ["beta"], "journeys": [{"id": "round-trip"}]},
    {"id": "beta", "nouns": ["b"], "areas": ["beta", "shared"], "neighbours": [], "axes": {"size": ["wide"]}, "journeys": []}
  ]}`))
	if err != nil {
		t.Fatal(err)
	}
	inv := &Inventory{Actions: map[string][]Action{
		"alpha": {{ID: "a.go", Channel: ChannelKey, Modes: []string{"normal", "insert"}}, {ID: "~hosted", Channel: ChannelHosted}, {ID: "~spill", Channel: ChannelSpill}, {ID: "~journey", Channel: ChannelJourney, Operation: "round-trip"}},
		"beta":  {{ID: "b.go", Channel: ChannelKey, Modes: []string{"normal"}}},
	}}
	mx := m.Build(inv)
	// alpha: a.go is mode x focus (4, less insert+unfocused = 3) with
	// size paired in: unfocused sits on one row, so pairing it with both
	// sizes and insert with both takes 5 rows; ~hosted 2; ~spill 1; the
	// journey 1 = 9. beta: b.go normal x 2 focus x 1 size = 2.
	var text bytes.Buffer
	_ = mx.WriteText(&text)
	if got := mx.Total(); got != 11 {
		t.Fatalf("total %d, want 11:\n%s", got, text.String())
	}
	mk := func(area, name string, covers ...string) *story.Story {
		return &story.Story{Name: name, Area: area, Covers: covers}
	}
	stories := []*story.Story{
		mk("alpha", "one", "alpha/a.go/*"),
		mk("alpha", "two"),
		mk("shared", "three", "alpha/~journey/round-trip", "beta/b.go/mode=normal,focus=focused,size=wide", "beta/b.og/*"),
		mk("other", "four", "gamma/*"),
	}
	rows := mx.Coverage(stories, true)
	want := []string{
		"alpha cells=9 covered=6 missing=3 stories=2 untagged=[alpha/two] typos=[]",
		"beta cells=2 covered=1 missing=1 stories=1 untagged=[] typos=[shared/three: beta/b.og/*]",
	}
	for i, r := range rows {
		got := fmt.Sprintf("%s cells=%d covered=%d missing=%d stories=%d untagged=%v typos=%v", r.Component, r.Cells, r.Covered, r.Missing, r.Stories, r.Untagged, r.Typos)
		if got != want[i] {
			t.Errorf("got  %s\nwant %s", got, want[i])
		}
		if len(r.MissingCells) != r.Missing {
			t.Errorf("%s: %d missing cells listed, %d counted", r.Component, len(r.MissingCells), r.Missing)
		}
	}
	if typos := mx.Typos(stories); !slices.Equal(typos, []string{"other/four: gamma/*", "shared/three: beta/b.og/*"}) {
		t.Errorf("typos %v", typos)
	}
}
