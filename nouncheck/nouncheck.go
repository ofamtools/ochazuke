// Package nouncheck fails an application's tests when one of its nouns
// breaks the thing and command noun rules (Runtime.ValidateNouns): a
// thing noun (a noun with a Store) that lacks a core verb or a key prop,
// or a command noun (no Store) with a verb named like a core verb.
//
// Breaks an application cannot fix yet live in a baseline file that only
// shrinks, one tab-separated row per problem (kind, noun, verb), like
// namecheck's: a new problem fails, and so does a row that no longer
// matches, which NOUNCHECK_UPDATE=1 removes (it never adds one; adding a
// row is a reviewed edit, NOUNCHECK_UPDATE=seed writes a baseline that
// does not exist yet). An application adopts it in its bindings test,
// once every noun and verb is registered:
//
//	func TestNouns(t *testing.T) {
//		nouncheck.Check(t, rt, nouncheck.Config{Baseline: "testdata/nouncheck.txt"})
//	}
package nouncheck

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

// Config is where the baseline lives; "" means none (every problem
// fails).
type Config struct {
	Baseline string
}

const header = `# nouncheck baseline: kind, noun, verb (tab separated), one row per accepted break of the thing and command noun rules.
# It only shrinks: NOUNCHECK_UPDATE=1 removes rows that no longer match, never adds them. Adding a row is a reviewed edit.
`

// Rows are the problems ValidateNouns reports on r, as baseline rows.
func Rows(r *ochazuke.Runtime) []string {
	var out []string
	for _, err := range r.ValidateNouns() {
		var p *ochazuke.NounProblem
		if errors.As(err, &p) {
			out = append(out, p.Row())
		}
	}
	return out
}

// Compare splits rows against a baseline: the rows the baseline lacks
// (new) and the baseline rows the problems no longer have (stale).
func Compare(rows, baseline []string) (fresh, stale []string) {
	for _, row := range rows {
		if !slices.Contains(baseline, row) {
			fresh = append(fresh, row)
		}
	}
	for _, row := range baseline {
		if !slices.Contains(rows, row) {
			stale = append(stale, row)
		}
	}
	return fresh, stale
}

// Parse reads a baseline file's rows, comments and blank lines left out.
func Parse(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

// Format writes rows as a baseline file, sorted, under the header.
func Format(rows []string) string {
	rows = slices.Sorted(slices.Values(rows))
	return header + strings.Join(rows, "\n") + "\n"
}

// Check fails t for every problem ValidateNouns reports on r that the
// baseline lacks, and for every baseline row that no longer matches.
func Check(t testing.TB, r *ochazuke.Runtime, c Config) {
	t.Helper()
	CheckRows(t, Rows(r), c)
}

// CheckRows is Check over rows already gathered (Rows of several
// runtimes, merged): one baseline for all of them.
func CheckRows(t testing.TB, rows []string, c Config) {
	t.Helper()
	var baseline []string
	if c.Baseline != "" {
		data, err := os.ReadFile(c.Baseline)
		switch {
		case os.IsNotExist(err) && os.Getenv("NOUNCHECK_UPDATE") == "seed":
			if err := os.WriteFile(c.Baseline, []byte(Format(rows)), 0o644); err != nil {
				t.Fatalf("nouncheck: %v", err)
			}
			return
		case err != nil && !os.IsNotExist(err):
			t.Fatalf("nouncheck: %v", err)
		}
		baseline = Parse(string(data))
	}
	fresh, stale := Compare(rows, baseline)
	if len(stale) > 0 && os.Getenv("NOUNCHECK_UPDATE") != "" && c.Baseline != "" {
		kept := slices.DeleteFunc(slices.Clone(baseline), func(row string) bool { return slices.Contains(stale, row) })
		if err := os.WriteFile(c.Baseline, []byte(Format(kept)), 0o644); err != nil {
			t.Fatalf("nouncheck: %v", err)
		}
		stale = nil
	}
	for _, row := range fresh {
		t.Errorf("nouncheck: new: %s", describe(row))
	}
	for _, row := range stale {
		t.Errorf("nouncheck: stale baseline row %q: remove it (NOUNCHECK_UPDATE=1)", row)
	}
}

// describe is a baseline row in words.
func describe(row string) string {
	f := strings.SplitN(row, "\t", 3)
	for len(f) < 3 {
		f = append(f, "")
	}
	return (&ochazuke.NounProblem{Kind: f[0], Noun: f[1], Verb: f[2]}).Error()
}
