package ochazuke_deps_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

type hygieneFinding struct {
	Rule     string `json:"rule"`
	Category string `json:"category"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Func     string `json:"func"`
	Detail   string `json:"detail"`
	Snippet  string `json:"snippet"`
	Reusable string `json:"reusable"`
	Severity string `json:"severity"` // "ratcheted" or "blocked"
	Value    int    `json:"value,omitempty"`
	Cap      int    `json:"cap,omitempty"`
}

type hygieneReport struct {
	Repo      string           `json:"repo"`
	Commit    string           `json:"commit"`
	Generated string           `json:"generated"`
	Summary   map[string]int   `json:"summary"`
	Findings  []hygieneFinding `json:"findings"`
}

func currentGitCommit() string {
	out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

// TestHygieneReport writes a deterministic JSON baseline report when HYGIENE_REPORT
// is set in the environment, and is a silent no-op during normal `go test` runs.
func TestHygieneReport(t *testing.T) {
	outPath := os.Getenv("HYGIENE_REPORT")
	if outPath == "" {
		return
	}

	coreFindings, _ := scanCoreStructSurfaceBudget(t)
	modalFindings := scanCustomModalChrome(t)
	widgetIDFindings := scanHardcodedWidgetIDs(t)
	cyclomaticFindings := scanCyclomaticComplexity(t)

	all := make([]hygieneFinding, 0, len(coreFindings)+len(modalFindings)+len(widgetIDFindings)+len(cyclomaticFindings))
	all = append(all, coreFindings...)
	all = append(all, modalFindings...)
	all = append(all, widgetIDFindings...)
	all = append(all, cyclomaticFindings...)

	sort.Slice(all, func(i, j int) bool {
		a, b := all[i], all[j]
		if a.Rule != b.Rule {
			return a.Rule < b.Rule
		}
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Func != b.Func {
			return a.Func < b.Func
		}
		return a.Detail < b.Detail
	})

	summary := map[string]int{
		ruleCoreStructBudget:  0,
		ruleCustomModalChrome: 0,
		ruleHardcodedWidgetID: 0,
		ruleCyclomatic:        0,
	}
	for _, f := range all {
		summary[f.Rule]++
	}

	report := hygieneReport{
		Repo:      "ochazuke",
		Commit:    currentGitCommit(),
		Generated: time.Now().UTC().Format(time.RFC3339),
		Summary:   summary,
		Findings:  all,
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatalf("marshal hygiene report: %v", err)
	}
	data = append(data, '\n')

	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(outPath), err)
	}
	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", outPath, err)
	}
}
