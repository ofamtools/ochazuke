package rulecheck

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"ofam.dev/ochazuke/lexiconcheck"
	"ofam.dev/ochazuke/namecheck"
	"ofam.dev/ochazuke/nouncheck"
	"ofam.dev/ochazuke/refusecheck"
)

// The checks that had their own package run here as subtests over the
// same config, with their own baseline files and update variables.

func runNames(t *testing.T, c *Context) {
	passUpdate(t, namecheck.UpdateEnv)
	cfg := c.Names
	if len(cfg.Vocab) == 0 {
		cfg.Vocab = namecheck.Vocab{}
		for _, r := range c.AllRuntimes() {
			for name, cats := range r.NameVocab() {
				for _, cat := range cats {
					if !slices.Contains(cfg.Vocab[name], cat) {
						cfg.Vocab[name] = append(cfg.Vocab[name], cat)
					}
				}
			}
		}
	}
	if cfg.Baseline == "" {
		cfg.Baseline = filepath.Join(c.Baselines, "namecheck.txt")
	}
	if len(cfg.Dirs) == 0 {
		cfg.Dirs = scanDirs(c.Root, c.Dirs)
	}
	namecheck.Check(t, cfg)
}

func runRefusals(t *testing.T, c *Context) {
	passUpdate(t, refusecheck.UpdateEnv)
	refusecheck.CheckConfig(t, refusecheck.Config{Root: c.Root, Dirs: c.Dirs}, filepath.Join(c.Baselines, "refusecheck.txt"))
}

func runLexicon(t *testing.T, c *Context) {
	passUpdate(t, lexiconcheck.UpdateEnv)
	cfg := c.Lexicon
	if cfg.Operations == nil {
		if c.Runtime == nil {
			t.Fatalf("lexicon reads Config.Runtime, which is nil: set it, or Off %q with a reason", "lexicon")
		}
		cfg.Operations = &c.Runtime.Operations
	}
	if cfg.Baseline == "" {
		cfg.Baseline = filepath.Join(c.Baselines, "lexiconcheck.txt")
	}
	if cfg.Words == "" {
		if words := filepath.Join(c.Baselines, "lexicon-words.tsv"); fileExists(words) {
			cfg.Words = words
		}
	}
	lexiconcheck.Check(t, cfg)
}

func runNouns(t *testing.T, c *Context) {
	passUpdate(t, "NOUNCHECK_UPDATE")
	var rows []string
	for _, r := range c.AllRuntimes() {
		for _, row := range nouncheck.Rows(r) {
			if !slices.Contains(rows, row) {
				rows = append(rows, row)
			}
		}
	}
	nouncheck.CheckRows(t, rows, nouncheck.Config{Baseline: filepath.Join(c.Baselines, "nouncheck.txt")})
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
