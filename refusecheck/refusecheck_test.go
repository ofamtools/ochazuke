package refusecheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fixture = `package x

import (
	"errors"
	"fmt"
)

type Operation struct {
	Unavailable, Confirm, Loses string
}

func choose(v string) error { return fmt.Errorf("status %v is not one of %v", v, "a") }
func missing() error        { return errors.New("name is required") }
func disk(err error) error  { return fmt.Errorf("open store: %w", err) }

var ops = []Operation{
	{Unavailable: "a system fact"},
	{Confirm: "Kill {{label}}?", Loses: "the pane"},
	{Confirm: "Clear {{label}}?"},
}
`

func put(t *testing.T, dir, name, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func scan(t *testing.T, dir string) []Site {
	t.Helper()
	sites, err := Scan(Config{Root: dir, Dirs: []string{"./..."}})
	if err != nil {
		t.Fatal(err)
	}
	return sites
}

// One fixture covers the classes, the three outcomes of a comparison with a
// baseline (new fails, removed is reported, an exception stays) and the
// declarations that tolerate a field the framework lacks.
func TestRefusecheck(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, "x.go", fixture)
	sites := scan(t, dir)
	got := map[string]string{}
	for _, s := range sites {
		got[s.Template] = s.Class
	}
	want := map[string]string{
		"status %v is not one of %v":              "K2",
		"name is required":                        "K3",
		"open store: %w":                          "K14",
		"Unavailable: a system fact":              "K1",
		"Confirm: Clear {{label}}? without Loses": "K11",
	}
	if len(got) != len(want) {
		t.Fatalf("sites = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%q is %s, want %s", k, got[k], v)
		}
	}

	base := Seed(sites)
	if r := base[Key{"x.go", "disk", "K14", "open store: %w"}]; r.Reason != "failure" {
		t.Errorf("a wrap seeds as the failure exception, got %q", r.Reason)
	}
	if res := Compare(sites, base); len(res.New)+len(res.Stale) != 0 {
		t.Fatalf("seeded baseline: %v %v", res.New, res.Stale)
	}

	// a new refusal fails and names what to use instead
	put(t, dir, "y.go", "package x\n\nimport \"fmt\"\n\nfunc g() error { return fmt.Errorf(\"nothing is focused\") }\n")
	res := Compare(scan(t, dir), base)
	if len(res.New) != 1 || !strings.Contains(res.New[0].Message, "Ctx.Need") {
		t.Fatalf("new = %v", res.New)
	}
	os.Remove(filepath.Join(dir, "y.go"))

	// a removed site is stale and shrinks the baseline; an exception row
	// whose site is still there stays
	put(t, dir, "x.go", strings.Replace(fixture, `func missing() error        { return errors.New("name is required") }`, "func missing() error { return errors.New(\"\") }", 1))
	res = Compare(scan(t, dir), base)
	if len(res.Stale) != 1 || !strings.Contains(res.Stale[0].Message, "name is required") {
		t.Fatalf("stale = %v", res.Stale)
	}
	if _, ok := res.Shrunk[Key{"x.go", "missing", "K3", "name is required"}]; ok {
		t.Error("shrunk baseline keeps the removed row")
	}
	if _, ok := res.Shrunk[Key{"x.go", "disk", "K14", "open store: %w"}]; !ok {
		t.Error("shrunk baseline lost the exception row")
	}

	// the baseline round-trips and rejects an unknown reason
	back, err := ParseBaseline(base.Format())
	if err != nil || len(back) != len(base) {
		t.Fatalf("round trip: %v (%d rows, want %d)", err, len(back), len(base))
	}
	if _, err := ParseBaseline("a.go\tf\tK2\t\"x\"\t1\tbecause\n"); err == nil {
		t.Error("an unknown reason must fail")
	}
}
