package rulecheck

import (
	"bufio"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Finding is one thing a check found: its key fields (the baseline row's
// key, never a line number) and a message saying what to change.
type Finding struct {
	Fields  []string
	Message string
}

// Key is the finding's fields joined by tabs, the baseline key.
func (f Finding) Key() string { return strings.Join(f.Fields, "\t") }

// Row is one baseline row: the key fields, how many findings it covers
// and, for a reviewed exception, why.
type Row struct {
	Fields []string
	Count  int
	Reason string
}

// Key is the row's fields joined by tabs.
func (r Row) Key() string { return strings.Join(r.Fields, "\t") }

// Baseline is a check's accepted findings by key.
type Baseline map[string]Row

// ParseBaseline reads a baseline's text whose rows have width key fields,
// then a count, then an optional reason.
func ParseBaseline(text string, width int) (Baseline, error) {
	b := Baseline{}
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(nil, 1<<20)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < width+1 || len(f) > width+2 {
			return nil, fmt.Errorf("line %d: want %d key fields, a count and an optional reason, got %d fields", n, width, len(f))
		}
		count, err := strconv.Atoi(f[width])
		if err != nil || count < 1 {
			return nil, fmt.Errorf("line %d: count %q", n, f[width])
		}
		r := Row{Fields: f[:width], Count: count}
		if len(f) == width+2 {
			r.Reason = f[width+1]
		}
		if _, dup := b[r.Key()]; dup {
			return nil, fmt.Errorf("line %d: duplicate row %q", n, r.Key())
		}
		b[r.Key()] = r
	}
	return b, sc.Err()
}

// LoadBaseline reads a baseline file; a missing file is an empty one.
func LoadBaseline(path string, width int) (Baseline, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Baseline{}, nil
	}
	if err != nil {
		return nil, err
	}
	return ParseBaseline(string(data), width)
}

// Format writes the baseline under header (comment lines), rows sorted by
// key.
func (b Baseline) Format(header string) string {
	var sb strings.Builder
	for _, line := range strings.Split(strings.TrimRight(header, "\n"), "\n") {
		sb.WriteString("# " + strings.TrimPrefix(line, "# ") + "\n")
	}
	for _, k := range slices.Sorted(maps.Keys(b)) {
		r := b[k]
		fmt.Fprintf(&sb, "%s\t%d", k, r.Count)
		if r.Reason != "" {
			sb.WriteString("\t" + r.Reason)
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

// Result compares findings with a baseline.
type Result struct {
	// New are findings the baseline does not cover (a key with no row, or
	// a debt row's count passed); Stale are rows the findings no longer
	// reach (a key gone, or a debt row's count above what is found).
	New, Stale []string
	// Shrunk is the baseline with stale rows removed and debt counts
	// lowered; it never adds or raises a row.
	Shrunk Baseline
	// Legit and Debt count the findings covered by reason rows and by
	// debt rows.
	Legit, Debt int
}

// Compare checks findings against base. A reason row covers its key at
// any count and is stale only when nothing is found for it; a debt row
// covers up to its count and is stale when fewer are found.
func Compare(found []Finding, base Baseline) Result {
	now := map[string]int{}
	msg := map[string]string{}
	for _, f := range found {
		now[f.Key()]++
		if msg[f.Key()] == "" {
			msg[f.Key()] = f.Message
		}
	}
	res := Result{Shrunk: Baseline{}}
	keys := slices.Sorted(maps.Keys(now))
	for k := range base {
		if _, ok := now[k]; !ok {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	for _, k := range keys {
		have := now[k]
		row, listed := base[k]
		switch {
		case !listed:
			res.New = append(res.New, fmt.Sprintf("%s (%d)", msg[k], have))
			continue
		case have == 0:
			res.Stale = append(res.Stale, fmt.Sprintf("%q is not found any more: remove the row (%s=1)", k, UpdateEnv))
			continue
		case row.Reason != "":
			res.Legit += have
		case have > row.Count:
			res.New = append(res.New, fmt.Sprintf("%s (%d, baseline %d)", msg[k], have, row.Count))
			res.Debt += row.Count
		case have < row.Count:
			res.Stale = append(res.Stale, fmt.Sprintf("%q is %d now, the row says %d: lower it (%s=1)", k, have, row.Count, UpdateEnv))
			row.Count = have
			res.Debt += have
		default:
			res.Debt += have
		}
		res.Shrunk[k] = row
	}
	return res
}

// CheckFindings compares found with the check's baseline file and fails
// t on a new finding or a stale row, after seeding a missing file
// (RULECHECK_UPDATE=seed: every finding as debt) or shrinking a stale one
// (RULECHECK_UPDATE=1). width is the number of key fields; header names
// them for the file's comment.
func CheckFindings(t testing.TB, id, path string, width int, header string, found []Finding) {
	t.Helper()
	base, err := LoadBaseline(path, width)
	if err != nil {
		t.Fatalf("%s: %s: %v", id, path, err)
	}
	mode := os.Getenv(UpdateEnv)
	if _, statErr := os.Stat(path); mode == "seed" && os.IsNotExist(statErr) {
		base = Baseline{}
		for _, f := range found {
			r := base[f.Key()]
			r.Fields, r.Count = f.Fields, r.Count+1
			base[f.Key()] = r
		}
		writeBaseline(t, path, base, header)
		t.Logf("%s: seeded %s with %d rows", id, path, len(base))
	}
	res := Compare(found, base)
	if mode == "1" && len(res.Stale) > 0 {
		writeBaseline(t, path, res.Shrunk, header)
		t.Logf("%s: %s shrunk by %d rows", id, path, len(res.Stale))
		res = Compare(found, res.Shrunk)
	}
	t.Logf("%s: %d findings: %d legitimate, %d debt (baseline %s)", id, len(found), res.Legit, res.Debt, path)
	if len(res.New)+len(res.Stale) == 0 {
		return
	}
	var sb strings.Builder
	for _, m := range res.New {
		sb.WriteString("new:   " + m + "\n")
	}
	for _, m := range res.Stale {
		sb.WriteString("stale: " + m + "\n")
	}
	t.Fatalf("%s: %d new, %d stale (baseline %s; a reviewed exception is a row with a reason)\n%s", id, len(res.New), len(res.Stale), path, sb.String())
}

func writeBaseline(t testing.TB, path string, b Baseline, header string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(b.Format(header)), 0o644); err != nil {
		t.Fatal(err)
	}
}
