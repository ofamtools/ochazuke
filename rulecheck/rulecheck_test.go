package rulecheck_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/rulecheck"
)

// fixture is the Needs you shape: a tree with a pane-like row (tile;
// "pane" is the framework's own noun) and, drawn under
// it, a part-noun row that binds only enter and y (matomeru's pane-child,
// 2026-11-13). extra is a bindings file loaded on top (a redirect row).
func fixture(t *testing.T, own []string, extra string) *ochazuke.Runtime {
	t.Helper()
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	noop := func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) }
	tile := r.Define("tile").Verb("open", noop, "Open the tile").Verb("yank", noop, "Yank its id").
		Verb("kill", noop, "Kill the tile").Verb("pin", noop, "Pin the tile").Mode("normal")
	tile.Bind("enter", ochazuke.Do("tile.open")).Bind("y", ochazuke.Do("tile.yank")).
		Bind("x", ochazuke.Do("tile.kill")).Bind("r", ochazuke.Do("tile.pin"))
	child := r.Define("subagent").Kind(ochazuke.NounKindPart).DefaultKeys(false).
		Verb("open", noop, "Open the subagent").Verb("yank", noop, "Yank its id").Mode("normal")
	child.Bind("enter", ochazuke.Do("subagent.open")).Bind("y", ochazuke.Do("subagent.yank"))
	for _, k := range own {
		child.Bind(k, ochazuke.Do("subagent.open"))
	}
	if extra != "" {
		if err := r.LoadDefaultBindings("test/extra.json", []byte(extra)); err != nil {
			t.Fatal(err)
		}
	}
	r.Instance("p")
	if _, err := r.AddTree("p/t", []ochazuke.TreeNode{
		{ID: "tile1", Label: "tile", Noun: "tile"},
		{ID: "sub1", Parent: "tile1", Label: "subagent", Noun: "subagent"},
	}, ochazuke.Parent("p")); err != nil {
		t.Fatal(err)
	}
	return r
}

const redirect = `{"bindings": [{"context": "subagent/normal", "bindings": [
	{"key": "unbound", "action": "ochazuke.key.redirect", "args": {"to": "parent"}, "hidden": true}]}]}`

// R1 reports the keys a tile row answers that its subagent row does not
// (x, r), drops the one the subagent binds itself, and reports nothing
// once the subagent redirects its unbound keys to its parent tile; a
// reason row covers a finding.
func TestDeadKeys(t *testing.T) {
	cases := []struct {
		name  string
		own   []string
		extra string
		want  []string // "row noun key sibling"
	}{
		{name: "missing", want: []string{"subagent r tile", "subagent x tile"}},
		{name: "own key", own: []string{"x"}, want: []string{"subagent r tile"}},
		{name: "redirected", extra: redirect},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := fixture(t, tc.own, tc.extra)
			units := 0
			found, cov := rulecheck.DeadKeys(r, func(n int) bool { units += n; return true })
			var got []string
			for _, f := range found {
				if f.Fields[2] != "normal" {
					t.Errorf("mode %q in %v: only normal rows are bound", f.Fields[2], f.Fields)
				}
				got = append(got, f.Fields[1]+" "+f.Fields[3]+" "+f.Fields[4])
			}
			slices.Sort(got)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("findings %v, want %v", got, tc.want)
			}
			if cov.Mixed != 1 || units != 2 {
				t.Fatalf("coverage %+v, %d units: want one mixed list walked once per row noun", cov, units)
			}
		})
	}
	t.Run("reason row", func(t *testing.T) {
		found, _ := rulecheck.DeadKeys(fixture(t, []string{"x"}, ""), func(int) bool { return true })
		base := rulecheck.Baseline{}
		for _, f := range found {
			base[f.Key()] = rulecheck.Row{Fields: f.Fields, Count: 1, Reason: "tiles pin, subagents do not"}
		}
		if res := rulecheck.Compare(found, base); len(res.New)+len(res.Stale) != 0 || res.Legit != 1 {
			t.Fatalf("a reason row must cover its finding: %+v", res)
		}
	})
}

// RULECHECK_UPDATE=1 lowers a debt row and removes a row nothing matches,
// and never adds a row for a new finding.
func TestBaselineOnlyShrinks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules", "x.txt")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# x\na\tb\t3\nc\td\t1\twhy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	found := []rulecheck.Finding{{Fields: []string{"a", "b"}}, {Fields: []string{"a", "b"}}}
	t.Setenv(rulecheck.UpdateEnv, "1")
	rulecheck.CheckFindings(t, "x", path, 2, "x", found)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != "# x\na\tb\t2\n" {
		t.Fatalf("shrunk baseline %q", got)
	}
	res := rulecheck.Compare(append(found, rulecheck.Finding{Fields: []string{"e", "f"}, Message: "new"}), rulecheck.Baseline{})
	if len(res.New) != 2 || len(res.Shrunk) != 0 {
		t.Fatalf("an empty baseline grew: %+v", res)
	}
}

// Check runs the checks in the table's order as subtests and skips one
// turned off with its reason.
func TestCheckRunsSubtests(t *testing.T) {
	off := map[string]string{"names": "fixture", "refusals": "fixture", "lexicon": "fixture", "nouns": "fixture"}
	rulecheck.Check(t, rulecheck.Config{Runtime: fixture(t, nil, redirect), Baselines: t.TempDir(), Off: off})
	var ids []string
	for _, c := range rulecheck.Checks() {
		ids = append(ids, c.ID)
	}
	if got := strings.Join(ids, " "); got != "deadkeys names refusals lexicon nouns" {
		t.Fatalf("checks %q", got)
	}
}
