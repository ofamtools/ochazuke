package lexiconcheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

func registry(keys ...string) *ochazuke.Operations {
	ops := ochazuke.NewOperations(map[string]*ochazuke.Noun{})
	for _, k := range keys {
		ops.Register(ochazuke.Key(k), "", "")
	}
	return &ops
}

func rules(vs []Violation) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = v.Rule + " " + v.Subject
	}
	return out
}

func want(t *testing.T, got []Violation, rows ...string) {
	t.Helper()
	g := strings.Join(rules(got), "\n")
	if w := strings.Join(rows, "\n"); g != w {
		t.Fatalf("violations:\n%s\nwant:\n%s", g, w)
	}
}

func TestCleanRegistry(t *testing.T) {
	ops := registry("o-lane.task.define", "o-lane.task.state", "o-lane.plan.define", "ochazuke.status.get")
	want(t, Scan(ops, nil, nil))
}

func TestNounThatIsAnAction(t *testing.T) {
	ops := registry("x.list.focus", "x.pane.list", "x.copy.show", "x.pane.copy")
	want(t, Scan(ops, nil, nil), "noun-action copy", "noun-action list")
}

func TestCompoundNamesAreJudgedWhole(t *testing.T) {
	ops := registry("x.copy-mode.enter", "x.pane.copy", "x.history-entry.list", "x.pane.history")
	want(t, Scan(ops, nil, nil))
}

func TestSingularAndPluralPair(t *testing.T) {
	ops := registry("x.pane.show", "x.panes.group", "cmds.cmd.add")
	want(t, Scan(ops, nil, nil), "pair cmds/cmd", "pair panes/pane")
}

func TestPluralOnlyNounAndSingularLooking(t *testing.T) {
	ops := registry("x.events.list", "x.status.get", "x.hooks.show")
	want(t, Scan(ops, nil, nil), "plural-noun events", "plural-noun hooks")
	words := Words{"status": {Word: "status", Part: "noun", Reviewed: "yes", Note: "singular"}}
	want(t, Scan(registry("x.status.get"), words, nil), "unclassified get")
}

func TestNamespaceIsNotANounOrActionWord(t *testing.T) {
	ops := registry("lane.lane.define", "lane.plan.show", "task.task.add", "x.pane.show")
	want(t, Scan(ops, nil, nil), "namespace-word lane", "namespace-word task")
	want(t, Scan(ops, nil, map[string]string{"lane": "lane"}), "namespace-word task")
	want(t, Scan(registry("list.thing.show", "x.pane.list"), nil, nil), "namespace-word list")
}

func TestKeysHaveAtMostThreeParts(t *testing.T) {
	ops := registry("x.pane.show")
	ops.All["a.b.c.d"] = &ochazuke.Operation{Key: "a.b.c.d"}
	ops.Order = append(ops.Order, "a.b.c.d")
	want(t, Scan(ops, nil, nil), "parts a.b.c.d")
}

func TestWordListClassifiesWords(t *testing.T) {
	words, err := ParseWords("# c\nrun\taction\tseed\ntask\tnoun\tyes\nplan\tnoun\tyes\nshow\taction\tyes\nfetch\tnoun\tyes\n")
	if err != nil {
		t.Fatal(err)
	}
	ops := registry("x.run.show", "x.task.fetch", "x.thing.show")
	want(t, Scan(ops, words, nil), "unclassified thing", "word-part fetch", "word-part run")
}

func TestParseWordsErrors(t *testing.T) {
	for _, text := range []string{"a\tnoun\n", "a\tnoun\tseed\nb\tnoun\tseed\na\taction\tseed\n"} {
		if _, err := ParseWords(text); err == nil {
			t.Errorf("ParseWords(%q) succeeded", text)
		}
	}
}

func TestBaselineRatchet(t *testing.T) {
	ops := registry("x.list.focus", "x.pane.list")
	found := Scan(ops, nil, nil)
	base, err := ParseBaseline(Baseline{"noun-action\tlist": true, "noun-action\tgone": true}.Format())
	if err != nil {
		t.Fatal(err)
	}
	res := Compare(found, base)
	if len(res.New) != 0 || res.Known != 1 || len(res.Stale) != 1 || res.Stale[0] != "noun-action\tgone" {
		t.Fatalf("Compare = %+v", res)
	}
	res = Compare(found, Baseline{})
	if len(res.New) != 1 || res.New[0].Subject != "list" {
		t.Fatalf("new violation not found: %+v", res)
	}
}

type fakeT struct {
	testing.TB
	failed string
	logs   []string
}

func (f *fakeT) Helper()                   {}
func (f *fakeT) Log(a ...any)              { f.logs = append(f.logs, "log") }
func (f *fakeT) Logf(s string, a ...any)   { f.logs = append(f.logs, s) }
func (f *fakeT) Fatalf(s string, a ...any) { f.failed = s; panic(f) }

func runCheck(cfg Config) (msg string) {
	f := &fakeT{}
	defer func() {
		if r := recover(); r != nil {
			if r != f {
				panic(r)
			}
			msg = f.failed
		}
	}()
	Check(f, cfg)
	return ""
}

func TestCheckFailsOnNewAndStaleAndSeeds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "base.txt")
	ops := registry("x.list.focus", "x.pane.list")
	t.Setenv(UpdateEnv, "")
	if runCheck(Config{Operations: ops, Baseline: path}) == "" {
		t.Fatal("a new violation passed")
	}
	t.Setenv(UpdateEnv, "seed")
	if msg := runCheck(Config{Operations: ops, Baseline: path}); msg != "" {
		t.Fatalf("seed run failed: %s", msg)
	}
	t.Setenv(UpdateEnv, "")
	if msg := runCheck(Config{Operations: ops, Baseline: path}); msg != "" {
		t.Fatalf("seeded baseline failed: %s", msg)
	}
	fixed := registry("x.list.focus", "x.pane.show")
	if runCheck(Config{Operations: fixed, Baseline: path}) == "" {
		t.Fatal("a stale row passed")
	}
	t.Setenv(UpdateEnv, "1")
	if msg := runCheck(Config{Operations: fixed, Baseline: path}); msg != "" {
		t.Fatalf("shrink run failed: %s", msg)
	}
	b, _ := os.ReadFile(path)
	if strings.Contains(string(b), "noun-action") {
		t.Fatalf("baseline not shrunk:\n%s", b)
	}
}
