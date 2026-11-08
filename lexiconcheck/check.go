package lexiconcheck

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
)

// UpdateEnv selects what Check does with a stale baseline: "1" rewrites it
// with the stale rows removed (it never adds one); "seed" writes a baseline
// that does not exist yet from the registry as it is.
const UpdateEnv = "LEXICONCHECK_UPDATE"

const baselineHeader = `# lexiconcheck baseline: rule, subject (tab separated). One row per known violation.
# A row may only be removed: LEXICONCHECK_UPDATE=1 drops the rows the registry no longer breaks.
# Adding a row is a reviewed edit; LEXICONCHECK_UPDATE=seed writes a baseline that does not exist yet.
`

// Baseline is the accepted violations by key (Violation.Key).
type Baseline map[string]bool

// ParseBaseline reads a baseline file's text.
func ParseBaseline(text string) (Baseline, error) {
	b := Baseline{}
	sc := bufio.NewScanner(strings.NewReader(text))
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 2 || f[0] == "" || f[1] == "" {
			return nil, fmt.Errorf("baseline line %d: want rule, subject", n)
		}
		if b[line] {
			return nil, fmt.Errorf("baseline line %d: duplicate row", n)
		}
		b[line] = true
	}
	return b, sc.Err()
}

// Format writes the baseline sorted.
func (b Baseline) Format() string {
	rows := make([]string, 0, len(b))
	for k := range b {
		rows = append(rows, k)
	}
	sort.Strings(rows)
	return baselineHeader + strings.Join(rows, "\n") + "\n"
}

// LoadBaseline reads a baseline file; a missing file is an empty baseline.
func LoadBaseline(path string) (Baseline, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Baseline{}, nil
	}
	if err != nil {
		return nil, err
	}
	return ParseBaseline(string(b))
}

// Result is the comparison of the violations with a baseline.
type Result struct {
	New    []Violation // broken and not in the baseline
	Stale  []string    // baseline rows no longer broken
	Shrunk Baseline    // the baseline less the stale rows
	Known  int         // violations the baseline covers
}

// Compare matches the violations against the baseline.
func Compare(found []Violation, base Baseline) Result {
	res := Result{Shrunk: Baseline{}}
	seen := map[string]bool{}
	for _, v := range found {
		seen[v.Key()] = true
		if base[v.Key()] {
			res.Known++
			res.Shrunk[v.Key()] = true
		} else {
			res.New = append(res.New, v)
		}
	}
	for k := range base {
		if !seen[k] {
			res.Stale = append(res.Stale, k)
		}
	}
	sort.Strings(res.Stale)
	return res
}

// Check scans cfg.Operations and fails t on a violation cfg.Baseline does
// not hold and on a baseline row the registry no longer breaks. It logs the
// totals by rule. A tool or the mtm host calls it from its own test over its
// mounted registry.
func Check(t testing.TB, cfg Config) {
	t.Helper()
	if cfg.Operations == nil {
		t.Fatalf("lexiconcheck: Config.Operations is nil")
	}
	if cfg.Baseline == "" {
		t.Fatalf("lexiconcheck: Config.Baseline is empty")
	}
	var words Words
	if cfg.Words != "" {
		w, err := LoadWords(cfg.Words)
		if err != nil {
			t.Fatalf("lexiconcheck: %s: %v", cfg.Words, err)
		}
		words = w
	}
	found := Scan(cfg.Operations, words, cfg.MainNouns)
	base, err := LoadBaseline(cfg.Baseline)
	if err != nil {
		t.Fatalf("lexiconcheck: %s: %v", cfg.Baseline, err)
	}
	mode := os.Getenv(UpdateEnv)
	if _, statErr := os.Stat(cfg.Baseline); mode == "seed" && os.IsNotExist(statErr) {
		base = Baseline{}
		for _, v := range found {
			base[v.Key()] = true
		}
		write(t, cfg.Baseline, base)
	}
	res := Compare(found, base)
	if mode == "1" && len(res.Stale) > 0 {
		write(t, cfg.Baseline, res.Shrunk)
		t.Logf("lexiconcheck: %s shrunk by %d rows", cfg.Baseline, len(res.Stale))
		res = Compare(found, res.Shrunk)
	}
	t.Log(Summary(found, res))
	if len(res.New)+len(res.Stale) == 0 {
		return
	}
	var sb strings.Builder
	for _, v := range res.New {
		sb.WriteString("new:   " + v.Rule + " " + v.Subject + ": " + v.Message + "\n")
	}
	for _, k := range res.Stale {
		sb.WriteString("stale: " + strings.Replace(k, "\t", " ", 1) + " (no longer broken: remove the row)\n")
	}
	t.Fatalf("lexiconcheck: %d new, %d stale (baseline %s)\n%s", len(res.New), len(res.Stale), cfg.Baseline, sb.String())
}

// Summary is the count of violations by rule and how many the baseline holds.
func Summary(found []Violation, res Result) string {
	by := map[string]int{}
	for _, v := range found {
		by[v.Rule]++
	}
	rules := sortedKeys(by)
	parts := make([]string, 0, len(rules))
	for _, r := range rules {
		parts = append(parts, fmt.Sprintf("%s %d", r, by[r]))
	}
	return fmt.Sprintf("lexiconcheck: %d violations (%s); baseline covers %d, %d new, %d stale",
		len(found), strings.Join(parts, ", "), res.Known, len(res.New), len(res.Stale))
}

func write(t testing.TB, path string, b Baseline) {
	t.Helper()
	if err := os.WriteFile(path, []byte(b.Format()), 0o644); err != nil {
		t.Fatalf("lexiconcheck: write %s: %v", path, err)
	}
}
