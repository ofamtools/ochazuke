package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/unixsuperhero/ochazuke"
)

// stateTool is an extension with a state verb over rows it can change, and
// how often the verb ran.
type stateTool struct {
	mu    sync.Mutex
	rows  []Row
	runs  int
	dir   string
	fp    string // the Fingerprint's answer; "" declares none
	keep  int
	scope string
}

func newStateTool(t testing.TB, rows ...Row) *stateTool {
	return &stateTool{rows: rows, dir: t.TempDir()}
}

func (st *stateTool) set(rows ...Row) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.rows = rows
}

// run runs the state verb once in a fresh runtime (a new process) with flags.
func (st *stateTool) run(t testing.TB, flags ...string) (string, string) {
	t.Helper()
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	r.Define("status").Verb("state", func(ochazuke.Ctx) ochazuke.Result {
		st.mu.Lock()
		defer st.mu.Unlock()
		st.runs++
		out := make(Lines, len(st.rows))
		for i, row := range st.rows {
			out[i] = row
		}
		return ochazuke.Done(out)
	}, "every list's rows").State()
	op := r.Operations["status.state"]
	op.Parameters = map[string]ochazuke.Parameter{"fresh": {Type: "boolean"}, "project": {Type: "string"}}
	r.Operations["status.state"] = op
	var out, errOut bytes.Buffer
	sh := New(r, "tool", strings.NewReader(""), &out, &errOut)
	sh.StateCache = StateCache{Dir: st.dir, Keep: st.keep}
	if st.scope != "" {
		scope := st.scope
		sh.StateCache.Scope = func(ochazuke.Arguments) string { return scope }
	}
	if st.fp != "" {
		fp := st.fp
		sh.StateCache.Fingerprint = func(ochazuke.Arguments) (string, error) { return fp, nil }
	}
	if err := sh.Run(context.Background(), append([]string{"status", "state"}, flags...)); err != nil {
		t.Fatalf("status state %v: %v\n%s", flags, err, errOut.String())
	}
	return out.String(), errOut.String()
}

// host is what a host holds of an answer's lists: rows (ids and record
// hashes, in order) and each list's hash.
type host struct {
	cache  string
	rows   map[string][]ochazuke.ListItem // Data is the record hash
	hashes map[string]string
}

func newHost() *host {
	return &host{rows: map[string][]ochazuke.ListItem{}, hashes: map[string]string{}}
}

func (h *host) flags() []string { return StateFlags(h.cache, h.hashes) }

// apply files an answer as a host does and checks every list's hash.
func (h *host) apply(t testing.TB, a StateAnswer) {
	t.Helper()
	if !a.Delta {
		t.Fatal("the answer is not a delta answer")
	}
	h.cache = a.Cache
	by := map[string][]ochazuke.ListItem{}
	for i, row := range a.Rows {
		by[row.Noun] = append(by[row.Noun], ochazuke.ListItem{ID: row.ID, Label: row.Label, Data: a.Hashes[i]})
	}
	for _, l := range a.Lists {
		switch l.Answer {
		case AnswerUnchanged:
		case AnswerFull:
			h.rows[l.List] = by[l.List]
		case AnswerDelta:
			if h.hashes[l.List] != l.Base {
				t.Fatalf("%s: delta against %s, the host holds %s", l.List, l.Base, h.hashes[l.List])
			}
			rows, err := ochazuke.PatchItems(h.rows[l.List], ochazuke.ListPatch{Upsert: by[l.List], At: l.At, Remove: l.Removed})
			if err != nil {
				t.Fatal(err)
			}
			h.rows[l.List] = rows
		default:
			t.Fatalf("%s: answer %q", l.List, l.Answer)
		}
		var records []string
		for _, item := range h.rows[l.List] {
			records = append(records, item.Data.(string))
		}
		if got := ListHash(records); got != l.Hash || len(records) != l.Rows {
			t.Fatalf("%s (%s): applied hash %s / %d rows, the answer says %s / %d", l.List, l.Answer, got, len(records), l.Hash, l.Rows)
		}
		h.hashes[l.List] = l.Hash
	}
}

func decode(t testing.TB, out string) StateAnswer {
	t.Helper()
	a, err := DecodeState([]byte(out))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func answers0(a StateAnswer) map[string]string {
	out := map[string]string{}
	for _, l := range a.Lists {
		out[l.List] = l.Answer
	}
	return out
}

func skillRows(n int, label func(i int) string) []Row {
	rows := make([]Row, n)
	for i := range rows {
		rows[i] = Row{Noun: "skill", ID: fmt.Sprintf("skill/s%d", i), Label: label(i), Data: map[string]any{"i": i}}
	}
	return rows
}

func plain(i int) string { return fmt.Sprint("skill ", i) }

func TestStateDeltaAnswers(t *testing.T) {
	agents := []Row{{Noun: "agent", ID: "agent/a1", Label: "ada"}, {Noun: "agent", ID: "agent/a2", Label: "bo"}}
	st := newStateTool(t, append(skillRows(5, plain), agents...)...)
	h := newHost()

	// Without --since the answer is the plain one, byte for byte.
	legacy, _ := st.run(t)
	if strings.Contains(legacy, `"list"`) || strings.Count(legacy, "\n") != 7 {
		t.Fatalf("a plain call must answer as before:\n%s", legacy)
	}

	// The first call: every list in full, with hashes; the row lines are
	// the plain answer's lines.
	out, _ := st.run(t, "--since=")
	a := decode(t, out)
	if a.Cache == "" || !slices.Equal(slices.Sorted(maps(answers0(a))), []string{"agent=full", "skill=full"}) {
		t.Fatalf("first answer = %+v", a.Lists)
	}
	for _, line := range strings.Split(strings.TrimSpace(legacy), "\n") {
		if !strings.Contains(out, line+"\n") {
			t.Fatalf("row line %s is not in the delta answer:\n%s", line, out)
		}
	}
	h.apply(t, a)

	// An unchanged poll: every list unchanged, no row lines.
	out, _ = st.run(t, h.flags()...)
	a = decode(t, out)
	if len(a.Rows) != 0 || answers0(a)["skill"] != AnswerUnchanged || answers0(a)["agent"] != AnswerUnchanged {
		t.Fatalf("unchanged poll = %+v, %d rows", a.Lists, len(a.Rows))
	}
	h.apply(t, a)

	// One record changed: exactly that row, at its index.
	rows := append(skillRows(5, func(i int) string {
		if i == 3 {
			return "renamed"
		}
		return plain(i)
	}), agents...)
	st.set(rows...)
	out, _ = st.run(t, h.flags()...)
	a = decode(t, out)
	skill, _ := a.List("skill")
	if skill.Answer != AnswerDelta || len(a.Rows) != 1 || a.Rows[0].ID != "skill/s3" || !slices.Equal(skill.At, []int{3}) {
		t.Fatalf("one change = %+v, rows %+v", skill, a.Rows)
	}
	if ag, _ := a.List("agent"); ag.Answer != AnswerUnchanged {
		t.Fatalf("the agents did not change: %+v", ag)
	}
	h.apply(t, a)

	// A removal and an addition at the top.
	added := Row{Noun: "skill", ID: "skill/new", Label: "new"}
	st.set(append(append([]Row{added}, rows[:1]...), rows[2:]...)...)
	out, _ = st.run(t, h.flags()...)
	a = decode(t, out)
	skill, _ = a.List("skill")
	if skill.Answer != AnswerDelta || !slices.Equal(skill.Removed, []string{"skill/s1"}) || !slices.Equal(skill.At, []int{0}) || len(a.Rows) != 1 {
		t.Fatalf("removal = %+v, rows %+v", skill, a.Rows)
	}
	h.apply(t, a)

	// A whole list gone: answered empty, so the host empties it.
	st.set(skillRows(2, plain)...)
	out, _ = st.run(t, h.flags()...)
	a = decode(t, out)
	if ag, ok := a.List("agent"); !ok || ag.Rows != 0 || ag.Answer == AnswerUnchanged {
		t.Fatalf("an emptied list must be answered: %+v", a.Lists)
	}
	h.apply(t, a)

	// A stale cache id: every list in full.
	stale := StateFlags("0123456789abcdef", h.hashes)
	a = decode(t, first(st.run(t, stale...)))
	if answers0(a)["skill"] != AnswerFull {
		t.Fatalf("a stale cache id must answer full: %+v", a.Lists)
	}
	// Other arguments are another scope, another cache id.
	b := decode(t, first(st.run(t, append(h.flags(), "--project", "elsewhere")...)))
	if b.Cache == a.Cache || answers0(b)["skill"] != AnswerFull {
		t.Fatalf("another scope must answer full under its own id: %s %+v", b.Cache, b.Lists)
	}

	// A reorder a patch cannot say is answered in full.
	h = newHost()
	st.set(skillRows(4, plain)...)
	h.apply(t, decode(t, first(st.run(t, h.flags()...))))
	r := skillRows(4, plain)
	r[1], r[2] = r[2], r[1]
	st.set(r...)
	a = decode(t, first(st.run(t, h.flags()...)))
	if answers0(a)["skill"] != AnswerFull {
		t.Fatalf("a swap must answer full: %+v", a.Lists)
	}
	h.apply(t, a)

	// --since refuses another format.
	if _, err := runErr(st, "--since=", "--format", "table"); err == nil {
		t.Fatal("--since with --format table must fail")
	}
}

func first(a, _ string) string { return a }

func maps(m map[string]string) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k, v := range m {
			if !yield(k + "=" + v) {
				return
			}
		}
	}
}

func runErr(st *stateTool, flags ...string) (string, error) {
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	r.Define("status").Verb("state", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(Lines{}) }, "rows").State()
	var out bytes.Buffer
	sh := New(r, "tool", strings.NewReader(""), &out, &out)
	sh.StateCache = StateCache{Dir: st.dir}
	err := sh.Run(context.Background(), append([]string{"status", "state"}, flags...))
	return out.String(), err
}

// Each list keeps at most Keep versions; asking against a pruned one is
// answered in full.
func TestStateCacheRetention(t *testing.T) {
	st := newStateTool(t, skillRows(3, plain)...)
	h := newHost()
	h.apply(t, decode(t, first(st.run(t, h.flags()...))))
	firstHashes := maps0(h.hashes)
	for v := 1; v <= 3; v++ {
		st.set(skillRows(3, func(i int) string { return fmt.Sprintf("v%d %d", v, i) })...)
		h.apply(t, decode(t, first(st.run(t, h.flags()...))))
	}
	files, _ := filepath.Glob(filepath.Join(st.dir, "*", "skill", "*"+versionExt))
	if len(files) != 2 {
		t.Fatalf("versions kept = %d (%v), want 2", len(files), files)
	}
	a := decode(t, first(st.run(t, StateFlags(h.cache, firstHashes)...)))
	if answers0(a)["skill"] != AnswerFull {
		t.Fatalf("a pruned version must answer full: %+v", a.Lists)
	}
	// Scopes are pruned too: a run per project keeps the newest 8.
	for i := range 12 {
		st.run(t, "--since=", "--project", fmt.Sprint(i))
	}
	scopes, _ := os.ReadDir(st.dir)
	if len(scopes) != defaultScopes {
		t.Fatalf("scopes kept = %d, want %d", len(scopes), defaultScopes)
	}
}

func maps0(m map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

// An unchanged fingerprint answers every list unchanged without running
// the verb; --fresh and a changed fingerprint run it.
func TestStateFingerprintSkipsTheVerb(t *testing.T) {
	st := newStateTool(t, skillRows(3, plain)...)
	st.fp = "inputs-v1"
	h := newHost()
	h.apply(t, decode(t, first(st.run(t, h.flags()...))))
	runs := st.runs
	a := decode(t, first(st.run(t, h.flags()...)))
	if st.runs != runs || answers0(a)["skill"] != AnswerUnchanged {
		t.Fatalf("an unchanged fingerprint ran the verb (%d -> %d) or answered %+v", runs, st.runs, a.Lists)
	}
	h.apply(t, a)
	st.run(t, append(h.flags(), "--fresh")...)
	if st.runs != runs+1 {
		t.Fatal("--fresh must run the verb")
	}
	st.fp = "inputs-v2"
	st.set(skillRows(4, plain)...)
	a = decode(t, first(st.run(t, h.flags()...)))
	if st.runs != runs+2 || answers0(a)["skill"] != AnswerDelta {
		t.Fatalf("a changed fingerprint must run the verb: %+v", a.Lists)
	}
	h.apply(t, a)
	// A host that holds other hashes is not answered from the fingerprint.
	b := decode(t, first(st.run(t, "--since=")))
	if answers0(b)["skill"] != AnswerFull {
		t.Fatalf("a host with nothing gets the lists: %+v", b.Lists)
	}
}

// Hosts polling one cache at once never see a corrupt version: every
// answer applies and verifies, and the cache ends with at most Keep
// versions and no temporary files.
func TestStateCacheConcurrentWriters(t *testing.T) {
	st := newStateTool(t)
	variants := make([][]Row, 4)
	for v := range variants {
		variants[v] = skillRows(200, func(i int) string {
			if i%50 == v {
				return fmt.Sprintf("v%d %d", v, i)
			}
			return plain(i)
		})
	}
	var wg sync.WaitGroup
	errs := make(chan string, 64)
	for w := range 6 {
		wg.Add(1)
		go func() {
			ft := &fakeT{}
			defer func() {
				if ft.failed != "" {
					errs <- ft.failed
				}
				wg.Done()
			}()
			h := newHost()
			for i := range 15 {
				// Each call is its own process with its own answer.
				tool := &stateTool{rows: variants[(w+i)%len(variants)], dir: st.dir}
				out, _ := tool.run(ft, h.flags()...)
				h.apply(ft, decode(ft, out))
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}
	st.set(variants[0]...)
	st.run(t, "--since=")
	files, _ := filepath.Glob(filepath.Join(st.dir, "*", "skill", "*"))
	for _, f := range files {
		if strings.HasPrefix(filepath.Base(f), tempPrefix) {
			t.Fatalf("a temporary file is left: %s", f)
		}
	}
	if len(files) > 2 {
		t.Fatalf("versions kept = %d, want at most 2", len(files))
	}
}

// fakeT is a testing.TB for goroutines: the first failure is kept.
type fakeT struct {
	testing.TB
	failed string
}

func (f *fakeT) Helper() {}
func (f *fakeT) Fatal(args ...any) {
	if f.failed == "" {
		f.failed = fmt.Sprint(args...)
	}
	runtime.Goexit()
}
func (f *fakeT) Fatalf(format string, args ...any) { f.Fatal(fmt.Sprintf(format, args...)) }

// A state verb answering off the loop (Pending) gets deltas too.
func TestStateDeltaFromAPendingVerb(t *testing.T) {
	dir := t.TempDir()
	run := func(label string, flags ...string) StateAnswer {
		r := ochazuke.NewRuntime()
		r.Config().Disable()
		r.Define("status").Verb("state", func(ochazuke.Ctx) ochazuke.Result {
			return ochazuke.Pending(func(context.Context) (any, error) {
				return Lines{Row{Noun: "skill", ID: "s1", Label: label}, Row{Noun: "skill", ID: "s2", Label: "two <b>"}}, nil
			})
		}, "rows").State()
		var out, errOut bytes.Buffer
		sh := New(r, "tool", strings.NewReader(""), &out, &errOut)
		sh.StateCache.Dir = dir
		if err := sh.Run(context.Background(), append([]string{"status", "state"}, flags...)); err != nil {
			t.Fatal(err, errOut.String())
		}
		return decode(t, out.String())
	}
	h := newHost()
	h.apply(t, run("one", h.flags()...))
	a := run("uno", h.flags()...)
	if l, _ := a.List("skill"); l.Answer != AnswerDelta || len(a.Rows) != 1 || a.Rows[0].Label != "uno" {
		t.Fatalf("pending delta = %+v %+v", a.Lists, a.Rows)
	}
	h.apply(t, a)
}
