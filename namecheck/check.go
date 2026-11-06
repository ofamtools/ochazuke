package namecheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// UpdateEnv selects what Check does with a stale baseline: "1" rewrites
// it with the stale rows removed or lowered (it never adds or raises one);
// "seed" writes a baseline that does not exist yet from the code as it is.
const UpdateEnv = "NAMECHECK_UPDATE"

// Check scans cfg.Dirs and fails t on a name branch or table the baseline
// does not cover, on a function over the complexity ceiling or grown past
// its baseline number, and on a baseline row that no longer matches. It
// logs the totals by kind and category.
func Check(t testing.TB, cfg Config) {
	t.Helper()
	if cfg.Baseline == "" {
		t.Fatalf("namecheck: Config.Baseline is empty")
	}
	if len(cfg.Vocab) == 0 {
		t.Fatalf("namecheck: Config.Vocab is empty: take it from the runtime (Runtime.NameVocab)")
	}
	rep, err := Scan(cfg)
	if err != nil {
		t.Fatalf("namecheck: %v", err)
	}
	base, err := LoadBaseline(cfg.Baseline)
	if err != nil {
		t.Fatalf("namecheck: %s: %v", cfg.Baseline, err)
	}
	mode := os.Getenv(UpdateEnv)
	_, statErr := os.Stat(cfg.Baseline)
	if mode == "seed" && os.IsNotExist(statErr) {
		base = seed(rep, cfg.Ceiling)
		write(t, cfg.Baseline, base)
	}
	res := Compare(rep, base, cfg.Ceiling)
	if mode == "1" && len(res.Stale) > 0 {
		write(t, cfg.Baseline, res.Shrunk)
		t.Logf("namecheck: %s shrunk by %d rows", cfg.Baseline, len(res.Stale))
		res = Compare(rep, res.Shrunk, cfg.Ceiling)
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
	t.Fatalf("namecheck: %d new, %d stale (baseline %s)\n%s", len(res.New), len(res.Stale), cfg.Baseline, sb.String())
}

// seed is every use in the code, as debt (no reasons yet).
func seed(rep *Report, ceiling int) Baseline {
	if ceiling <= 0 {
		ceiling = DefaultCeiling
	}
	b := Baseline{}
	for k, n := range rep.counts(ceiling, nil) {
		b[k] = Row{Key: k, Count: n}
	}
	return b
}

func write(t testing.TB, file string, b Baseline) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatalf("namecheck: %v", err)
	}
	if err := os.WriteFile(file, []byte(b.Format()), 0o644); err != nil {
		t.Fatalf("namecheck: %v", err)
	}
}
