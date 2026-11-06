package namecheck

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var testVocab = Vocab{
	"set":    {"verb"},
	"insert": {"mode"},
	"task":   {"noun"},
	"left":   {"motion"},
	"name":   {"parameter"},
	"motion": {"parameter"},
}

// fixture writes files into a fresh directory and makes it the working
// directory, so file keys are plain names.
func fixture(t *testing.T, files map[string]string) {
	t.Helper()
	dir := t.TempDir()
	for name, text := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)
}

func scan(t *testing.T, ceiling int) *Report {
	t.Helper()
	rep, err := Scan(Config{Vocab: testVocab, Ceiling: ceiling})
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

// counts is "kind literal" -> count for the hits of a function.
func counts(rep *Report, fn string) map[string]int {
	m := map[string]int{}
	for _, h := range rep.Hits {
		if h.Func == fn {
			m[h.Kind+" "+h.Literal]++
		}
	}
	return m
}

func wantCounts(t *testing.T, got map[string]int, want map[string]int) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("hits = %v, want %v", got, want)
	}
}

const branches = `package p

const verbSet = "set"

type op struct{ Verb, Mode string }

func literal(o op) bool { return o.Verb == "set" }

func notEqual(o op) bool { return "insert" != o.Mode }

func constant(o op) bool { return o.Verb == verbSet }

func switched(o op) int {
	switch o.Mode {
	case "insert":
		return 1
	case "other", "left":
		return 2
	}
	return 0
}

func tagless(o op) bool {
	switch {
	case o.Verb == "task":
		return true
	}
	return false
}

func unknown(o op) bool { return o.Verb == "nothing-declared" }

var nouns = map[string]int{"task": 1, "other": 2}

type table map[string]int

var named = table{"set": 1}

func closure() func(op) bool { return func(o op) bool { return o.Mode == "insert" } }
`

func TestFindsComparisonCaseAndTableHits(t *testing.T) {
	fixture(t, map[string]string{"a.go": branches})
	rep := scan(t, 0)
	wantCounts(t, counts(rep, "literal"), map[string]int{"compare set": 1})
	wantCounts(t, counts(rep, "notEqual"), map[string]int{"compare insert": 1})
	wantCounts(t, counts(rep, "constant"), map[string]int{"compare set": 1})
	wantCounts(t, counts(rep, "switched"), map[string]int{"case insert": 1, "case left": 1})
	wantCounts(t, counts(rep, "tagless"), map[string]int{"compare task": 1})
	wantCounts(t, counts(rep, "unknown"), map[string]int{})
	wantCounts(t, counts(rep, "var nouns"), map[string]int{"table task": 1})
	wantCounts(t, counts(rep, "closure"), map[string]int{"compare insert": 1})
	// a literal of a named map type is a table only by its string keys
	wantCounts(t, counts(rep, "var named"), map[string]int{"table set": 1})
}

func TestDeclarationsAndTestFilesAreNotHits(t *testing.T) {
	fixture(t, map[string]string{
		"decl.go": `package p

type Parameter struct{ Type string }
type Arguments map[string]any
type Binding struct {
	Mode string
	Args Arguments
}

func Register(name string, params map[string]Parameter, h func(op string) bool) {}

var rows = []Binding{{Mode: "insert", Args: Arguments{"motion": "left"}}}

var params = map[string]Parameter{"name": {Type: "string"}}

func setup() {
	Register("task.set", map[string]Parameter{"name": {}, "motion": {}}, func(op string) bool { return true })
}
`,
		"a_test.go": `package p

func inATest(v string) bool { return v == "set" }
`,
	})
	rep := scan(t, 0)
	if len(rep.Hits) != 0 {
		t.Fatalf("declarations or test files counted: %+v", rep.Hits)
	}
}

func TestHandlerInsideARegisterCallStillCounts(t *testing.T) {
	fixture(t, map[string]string{"h.go": `package p

func Register(name string, h func(verb string) bool) {}

func setup() {
	Register("task.set", func(verb string) bool { return verb == "set" })
}
`})
	wantCounts(t, counts(scan(t, 0), "setup"), map[string]int{"compare set": 1})
}

func TestQualifiedConstantsResolveOnlyThroughImports(t *testing.T) {
	fixture(t, map[string]string{
		"c.go": `package p

const Insert = "insert"
`,
		"u.go": `package p

import "p/other"

func viaPackage(v string) bool { return v == other.Insert }

func viaField(o struct{ Insert string }, v string) bool { return v == o.Insert }
`,
	})
	rep := scan(t, 0)
	wantCounts(t, counts(rep, "viaPackage"), map[string]int{"compare insert": 1})
	wantCounts(t, counts(rep, "viaField"), map[string]int{})
}

// recorder is a testing.TB that records a failure instead of ending the test.
type recorder struct {
	testing.TB
	msg string
}

type stop struct{}

func (r *recorder) Helper()                      {}
func (r *recorder) Log(args ...any)              {}
func (r *recorder) Fatalf(f string, args ...any) { r.msg = fmt.Sprintf(f, args...); panic(stop{}) }
func (r *recorder) failure(fn func(testing.TB)) (msg string) {
	defer func() { msg = r.msg }()
	defer func() {
		if v := recover(); v != nil {
			if _, ok := v.(stop); !ok {
				panic(v)
			}
		}
	}()
	fn(r)
	return r.msg
}

func check(t *testing.T, baseline string) string {
	t.Helper()
	r := &recorder{TB: t}
	return r.failure(func(tb testing.TB) {
		Check(tb, Config{Vocab: testVocab, Baseline: baseline})
	})
}

func TestANewNameBranchFails(t *testing.T) {
	fixture(t, map[string]string{"a.go": "package p\n\nfunc f(verb string) bool { return verb == \"set\" }\n"})
	msg := check(t, "base.txt")
	if !strings.Contains(msg, "new:") || !strings.Contains(msg, "a.go") || !strings.Contains(msg, `"set"`) {
		t.Fatalf("a seeded branch did not fail with its place: %q", msg)
	}
	// the same branch, covered by a baseline row, passes
	os.WriteFile("base.txt", []byte("a.go\tf\tcompare\t\"set\"\t1\n"), 0o644)
	if msg := check(t, "base.txt"); msg != "" {
		t.Fatalf("a baselined branch failed: %s", msg)
	}
	// a second one in the same function exceeds the row
	os.WriteFile("a.go", []byte("package p\n\nfunc f(verb string) bool { return verb == \"set\" || verb == \"set\" }\n"), 0o644)
	if msg := check(t, "base.txt"); !strings.Contains(msg, "new:") {
		t.Fatalf("a second branch passed: %q", msg)
	}
}

func TestADeclarativeUsePassesWithAnEmptyBaseline(t *testing.T) {
	fixture(t, map[string]string{"a.go": `package p

type Arguments map[string]any

func Do(verb string, a Arguments) {}

func f() { Do("task.set", Arguments{"motion": "left", "name": "x"}) }
`})
	if msg := check(t, "none.txt"); msg != "" {
		t.Fatalf("a declarative use failed: %s", msg)
	}
}

func TestTheBaselineOnlyShrinks(t *testing.T) {
	src := "package p\n\nfunc f(verb string) bool { return verb == \"set\" || verb == \"set\" }\n"
	fixture(t, map[string]string{"a.go": src})
	os.WriteFile("base.txt", []byte("a.go\tf\tcompare\t\"set\"\t2\tbecause\n"), 0o644)
	if msg := check(t, "base.txt"); msg != "" {
		t.Fatalf("a matching baseline failed: %s", msg)
	}
	os.WriteFile("base.txt", []byte("a.go\tf\tcompare\t\"set\"\t2\tbecause\ngone.go\tg\tcompare\t\"set\"\t1\n"), 0o644)

	// a branch removed without its row leaves a stale row, which fails
	os.WriteFile("a.go", []byte("package p\n\nfunc f(verb string) bool { return verb == \"set\" }\n"), 0o644)
	msg := check(t, "base.txt")
	if !strings.Contains(msg, "stale:") || !strings.Contains(msg, "gone.go") {
		t.Fatalf("stale rows passed: %q", msg)
	}

	// NAMECHECK_UPDATE=1 lowers and removes, keeps reasons, and adds nothing
	t.Setenv(UpdateEnv, "1")
	os.WriteFile("a.go", []byte("package p\n\nfunc f(verb string) bool { return verb == \"set\" }\n\nfunc h(verb string) bool { return verb == \"set\" }\n"), 0o644)
	msg = check(t, "base.txt")
	if !strings.Contains(msg, "new:") || strings.Contains(msg, "stale:") {
		t.Fatalf("update did not shrink, or let a new branch through: %q", msg)
	}
	data, _ := os.ReadFile("base.txt")
	got := string(data)
	if !strings.Contains(got, "a.go\tf\tcompare\t\"set\"\t1\tbecause") || strings.Contains(got, "gone.go") || strings.Contains(got, "\th\t") {
		t.Fatalf("update wrote %q", got)
	}
}

func TestSeedWritesOnlyAMissingBaseline(t *testing.T) {
	fixture(t, map[string]string{"a.go": "package p\n\nfunc f(verb string) bool { return verb == \"set\" }\n"})
	t.Setenv(UpdateEnv, "seed")
	if msg := check(t, "base.txt"); msg != "" {
		t.Fatalf("seed failed: %s", msg)
	}
	first, _ := os.ReadFile("base.txt")
	os.WriteFile("a.go", []byte("package p\n\nfunc f(verb string) bool { return verb == \"set\" || verb == \"insert\" }\n"), 0o644)
	if msg := check(t, "base.txt"); !strings.Contains(msg, "new:") {
		t.Fatalf("seed added a row to an existing baseline: %q", msg)
	}
	after, _ := os.ReadFile("base.txt")
	if string(first) != string(after) {
		t.Fatalf("seed rewrote an existing baseline")
	}
}

func TestBaselineKeysHoldNoLineNumbers(t *testing.T) {
	a := "package p\n\nfunc f(verb string) bool { return verb == \"set\" }\n"
	fixture(t, map[string]string{"a.go": a})
	t.Setenv(UpdateEnv, "seed")
	check(t, "base.txt")
	t.Setenv(UpdateEnv, "")
	// the code moves down the file; the baseline still matches
	os.WriteFile("a.go", []byte("package p\n\n// a comment\n\nvar x = 1\n\nfunc f(verb string) bool { return verb == \"set\" }\n"), 0o644)
	if msg := check(t, "base.txt"); msg != "" {
		t.Fatalf("moving code inside a file broke the baseline: %s", msg)
	}
}

func busy(name string, branches int) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "func %s(n int) int {\n", name)
	for i := 0; i < branches; i++ {
		fmt.Fprintf(&sb, "\tif n == %d {\n\t\treturn %d\n\t}\n", i, i)
	}
	sb.WriteString("\treturn 0\n}\n")
	return sb.String()
}

func TestComplexityCeilingAndRatchet(t *testing.T) {
	fixture(t, map[string]string{"a.go": "package p\n\n" + busy("ok", 24) + "\n" + busy("big", 25)})
	rep := scan(t, 0)
	if got := rep.Cyclo[Key{"a.go", "ok", KindCyclo, "-"}]; got != 25 {
		t.Fatalf("complexity of a 24-branch function = %d, want 25", got)
	}
	msg := check(t, "none.txt")
	if !strings.Contains(msg, "big") || strings.Contains(msg, " ok ") || !strings.Contains(msg, "26") {
		t.Fatalf("only the function over 25 should fail: %q", msg)
	}

	// a baseline row holds it where it is; growth fails
	os.WriteFile("base.txt", []byte("a.go\tbig\tcyclo\t\"-\"\t26\n"), 0o644)
	if msg := check(t, "base.txt"); msg != "" {
		t.Fatalf("a baselined function failed: %s", msg)
	}
	os.WriteFile("a.go", []byte("package p\n\n"+busy("ok", 24)+"\n"+busy("big", 26)), 0o644)
	if msg := check(t, "base.txt"); !strings.Contains(msg, "grew") {
		t.Fatalf("a grown function passed: %q", msg)
	}

	// shrinking leaves a stale row; at 25 or below the row is not needed
	os.WriteFile("a.go", []byte("package p\n\n"+busy("ok", 24)+"\n"+busy("big", 20)), 0o644)
	if msg := check(t, "base.txt"); !strings.Contains(msg, "stale:") {
		t.Fatalf("a shrunk function kept its row: %q", msg)
	}
}

func TestComplexityCountsCasesAndBooleanOperators(t *testing.T) {
	fixture(t, map[string]string{"a.go": `package p

func f(a, b bool, n int) int {
	switch n {
	case 1, 2:
		return 1
	case 3:
		return 2
	default:
		return 3
	}
	for range 3 {
		if a && b || a {
			return 4
		}
	}
	return 0
}
`})
	// 1 + two cases + for + if + && + ||
	if got := scan(t, 0).Cyclo[Key{"a.go", "f", KindCyclo, "-"}]; got != 7 {
		t.Fatalf("complexity = %d, want 7", got)
	}
}

func TestBaselineRoundTrip(t *testing.T) {
	in := "a.go\tT.M\tcompare\t\"a\\tb\"\t2\tlegit\n"
	b, err := ParseBaseline(in)
	if err != nil {
		t.Fatal(err)
	}
	out := b.Format()
	if !strings.HasSuffix(out, in) {
		t.Fatalf("round trip lost a row: %q", out)
	}
	for _, bad := range []string{"a.go\tf\tcompare\t\"x\"\n", "a.go\tf\tcompare\tx\t1\n", "a.go\tf\tcompare\t\"x\"\t0\n", "a\tf\tcase\t\"x\"\t1\na\tf\tcase\t\"x\"\t1\n"} {
		if _, err := ParseBaseline(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}
