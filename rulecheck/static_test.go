package rulecheck_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"ofam.dev/ochazuke/rulecheck"
)

// The syntax checks' fixture: bad holds one violation of each, clean
// the look-alikes each check leaves out (a path join, a clock time, a
// message, a URL, a hook that does its side effect).
var staticFixture = map[string]string{
	"bad/a.go": `package app

import (
	"cmp"
	"strings"

	"ofam.dev/ochazuke"
)

const paneNoun = "pane"

type App struct {
	r  *ochazuke.Runtime
	pv *paneUI
}

func (a *App) setup() {
	a.r.Define(paneNoun).Mode("normal").Bind("x", ochazuke.Do("pane.kill"))
}

func rowID(session, id string) string { return "pane:" + session + "/" + id }

func cut(s string) (string, bool) { return strings.CutPrefix(s, "pane:") }

func pick(p P) string { return cmp.Or(p.SessionID, p.Session) }

func (a *App) store() ochazuke.Store {
	return ochazuke.Store{
		List: func(c ochazuke.Ctx, q ochazuke.ListQuery) ([]ochazuke.Thing, error) {
			a.refresh()
			return nil, nil
		},
	}
}

func (a *App) refresh() { a.pv.dirty = true }

func (a *App) hooks() {
	_ = ochazuke.DocStore(nil, "x", ochazuke.DocHooks{BeforeWrite: a.before})
}

func (a *App) before(c ochazuke.Ctx, w *ochazuke.DocWrite) error {
	return ochazuke.NothingErr("kept", "nothing to write")
}
`,
	"clean/b.go": `package app

import (
	"fmt"

	"ofam.dev/ochazuke"
)

func join(dir, name string) string { return dir + "/" + name }

func clock(h, m int) string { return fmt.Sprintf("%d:%02d", h, m) }

func msg(id string) string { return "cannot open " + id + ": gone" }

func link(host string) string { return "https://" + host }

func hooks(d *Daemon) ochazuke.DocHooks {
	return ochazuke.DocHooks{AfterWrite: func(c ochazuke.Ctx, key string, t ochazuke.Thing) { d.Rename(key) }}
}
`,
}

func writeFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for name, text := range staticFixture {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func keys(found []rulecheck.Finding) []string {
	var out []string
	for _, f := range found {
		out = append(out, strings.Join(f.Fields, " "))
	}
	slices.Sort(out)
	return out
}

// Each syntax check reports bad's violation, leaves clean's look-alikes
// out, and fails on a baseline row nothing matches any more.
func TestStaticChecks(t *testing.T) {
	root := writeFixture(t)
	cases := []struct {
		id    string
		width int
		run   func(*rulecheck.Source) []rulecheck.Finding
		want  []string
	}{
		{"declarations", rulecheck.DeclarationsWidth, rulecheck.Declarations,
			[]string{`bad/a.go App.setup "pane"`}},
		{"gokeys", rulecheck.GoKeysWidth, rulecheck.GoKeys,
			[]string{`bad/a.go App.setup "x"`}},
		{"stringids", rulecheck.StringIDsWidth, rulecheck.StringIDs,
			[]string{"bad/a.go cut prefix-split pane:", "bad/a.go pick id-or-name SessionID Session", "bad/a.go rowID join /", "bad/a.go rowID prefix-join pane:"}},
		{"storeliterals", rulecheck.StoreLiteralsWidth, rulecheck.StoreLiterals,
			[]string{"bad/a.go App.store"}},
		{"storeui", rulecheck.StoreUIWidth, rulecheck.StoreUI,
			[]string{"bad/a.go App.store Store.List via App.refresh: pv"}},
		{"hookrefusal", rulecheck.HookRefusalWidth, rulecheck.HookRefusals,
			[]string{"bad/a.go App.before DocHooks.BeforeWrite NothingErr"}},
		{"nounlines", rulecheck.NounLinesWidth, func(src *rulecheck.Source) []rulecheck.Finding {
			return rulecheck.NounLineFindings(rulecheck.NounLines(src, []string{"pane"}))
		}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			bad, err := rulecheck.LoadSource(root, []string{"bad"})
			if err != nil {
				t.Fatal(err)
			}
			clean, err := rulecheck.LoadSource(root, []string{"clean"})
			if err != nil {
				t.Fatal(err)
			}
			found := tc.run(bad)
			if tc.want != nil {
				if got := slices.Compact(keys(found)); !slices.Equal(got, tc.want) {
					t.Fatalf("bad: got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
				}
			} else if len(found) == 0 {
				t.Fatal("bad: nothing found")
			}
			if got := tc.run(clean); len(got) > 0 {
				t.Fatalf("clean: %v", keys(got))
			}
			// A baseline row the code no longer reaches fails as stale.
			path := filepath.Join(t.TempDir(), tc.id+".txt")
			gone := strings.Repeat("gone\t", tc.width) + "1\n"
			if err := os.WriteFile(path, []byte(gone), 0o644); err != nil {
				t.Fatal(err)
			}
			msg := failure(func(tb testing.TB) { rulecheck.CheckFindings(tb, tc.id, path, tc.width, tc.id, nil) })
			if !strings.Contains(msg, "is not found any more: remove the row") {
				t.Fatalf("stale row: %q", msg)
			}
		})
	}
}

// NounLines counts a function whole for each noun it names: by a literal
// (rowID's and cut's "pane:"), a constant holding one (setup's paneNoun);
// pick names no pane, and the struct's paneUI field is not in a function.
func TestNounLines(t *testing.T) {
	src, err := rulecheck.LoadSource(writeFixture(t), []string{"bad"})
	if err != nil {
		t.Fatal(err)
	}
	got := rulecheck.NounLines(src, []string{"pane"})
	if len(got) != 1 || got[0].Funcs != 3 || got[0].Lines != 5 {
		t.Fatalf("got %+v, want pane in bad/a.go: 3 functions, 5 lines (setup 3, rowID 1, cut 1)", got)
	}
	if n := len(rulecheck.NounLineFindings(got)); n != 5 {
		t.Fatalf("%d findings, want one per line", n)
	}
}

// failure runs fn with a testing.TB whose Fatalf stops fn and returns the
// message ("" when fn passes).
func failure(fn func(testing.TB)) (msg string) {
	tb := &recorder{}
	defer func() {
		if r := recover(); r != nil && r != errStop {
			panic(r)
		}
		msg = tb.msg
	}()
	fn(tb)
	return
}

var errStop = fmt.Errorf("stop")

type recorder struct {
	testing.TB
	msg string
}

func (r *recorder) Helper()                   {}
func (r *recorder) Logf(string, ...any)       {}
func (r *recorder) Fatalf(f string, a ...any) { r.msg = fmt.Sprintf(f, a...); panic(errStop) }
