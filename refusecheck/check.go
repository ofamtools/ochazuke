package refusecheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// UpdateEnv selects what Check does with a stale baseline: "1" rewrites it
// with the stale rows removed or lowered (it never adds or raises one);
// "seed" writes a baseline that does not exist yet from the code as it is.
const UpdateEnv = "REFUSECHECK_UPDATE"

// Check scans dir and everything below it (hidden directories, testdata,
// vendor and nested modules are skipped) and fails t on a refusal the
// baseline does not cover and on a baseline row the code no longer
// matches. File names in the baseline are relative to dir. It logs the
// totals by class. An application's one test is:
//
//	refusecheck.Check(t, ".", "testdata/refusecheck.txt")
func Check(t testing.TB, dir, baselinePath string) {
	t.Helper()
	CheckConfig(t, Config{Root: dir, Dirs: []string{"./..."}}, baselinePath)
}

// CheckConfig is Check over chosen directories (Config.Dirs, relative to
// Config.Root), for an application that scans only some packages.
func CheckConfig(t testing.TB, cfg Config, baselinePath string) {
	t.Helper()
	sites, err := Scan(cfg)
	if err != nil {
		t.Fatalf("refusecheck: %v", err)
	}
	base, err := LoadBaseline(baselinePath)
	if err != nil {
		t.Fatalf("refusecheck: %s: %v", baselinePath, err)
	}
	mode := os.Getenv(UpdateEnv)
	if _, statErr := os.Stat(baselinePath); mode == "seed" && os.IsNotExist(statErr) {
		base = Seed(sites)
		write(t, baselinePath, base)
	}
	res := Compare(sites, base)
	if mode == "1" && len(res.Stale) > 0 {
		write(t, baselinePath, res.Shrunk)
		t.Logf("refusecheck: %s shrunk by %d rows", baselinePath, len(res.Stale))
		res = Compare(sites, res.Shrunk)
	}
	t.Log(res.Summary)
	if len(res.New)+len(res.Stale) == 0 {
		return
	}
	var sb strings.Builder
	for _, p := range res.New {
		sb.WriteString("new:   " + p.Message + "\n")
	}
	for _, p := range res.Stale {
		sb.WriteString("stale: " + p.Message + "\n")
	}
	t.Fatalf("refusecheck: %d new, %d stale (baseline %s)\n%s", len(res.New), len(res.Stale), baselinePath, sb.String())
}

// seedReasons are the classes whose sites are exceptions by the plan:
// agent rights (E1), declaration errors in code (E4), real failures (E3).
// Every other class seeds as debt.
var seedReasons = map[string]string{"K12": "rights", "K13": "declaration", "K14": "failure"}

// Seed is every site in the code as a baseline row: debt, except the
// classes that stay by rule.
func Seed(sites []Site) Baseline {
	b := Baseline{}
	for _, s := range sites {
		r := b[s.Key()]
		r.Key = s.Key()
		r.Count++
		r.Reason = seedReasons[s.Class]
		b[s.Key()] = r
	}
	return b
}

func write(t testing.TB, file string, b Baseline) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatalf("refusecheck: %v", err)
	}
	if err := os.WriteFile(file, []byte(b.Format()), 0o644); err != nil {
		t.Fatalf("refusecheck: %v", err)
	}
}
