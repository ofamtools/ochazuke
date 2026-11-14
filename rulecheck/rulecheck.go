// Package rulecheck runs the family's rule checks as one test: static
// scans of the source (names, refusals) and walks over a built runtime
// (dead keys, the lexicon, the noun rules). Every check runs in-process in
// seconds, with no PTY, daemon, story or subprocess. An application adopts
// it with one test over its runtime:
//
//	func TestRules(t *testing.T) {
//		rulecheck.Check(t, rulecheck.Config{Runtime: rt, Root: "../.."})
//	}
//
// Each check is a subtest (TestRules/deadkeys, TestRules/names, ...), in
// the order of Checks. Each logs its time and Check logs the total.
//
// A check's accepted findings are a baseline file: a new check's is
// testdata/rules/<id>.txt (tab separated: the check's key fields, a
// count, an optional reason); the checks that had their own package keep
// their own files and update variables. A row with a reason is a
// reviewed exception that covers its finding; a row without one is debt
// that may only shrink. RULECHECK_UPDATE=1 removes stale rows and lowers
// counts, never adds or raises one; RULECHECK_UPDATE=seed writes a
// baseline that does not exist yet. Either value is passed on to the
// older checks' own variables (NAMECHECK_UPDATE, ...) when those are
// unset.
//
// Speed is held by counting work, not by timing: each registry walk
// counts its units (one row-level key lookup) and fails over
// Config.MaxWork, so a walk that stops de-duplicating or a fixture that
// grows fails deterministically.
package rulecheck

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/lexiconcheck"
	"ofam.dev/ochazuke/namecheck"
)

// UpdateEnv selects what a check does with its baseline: "1" removes
// stale rows and lowers counts; "seed" writes a missing baseline.
const UpdateEnv = "RULECHECK_UPDATE"

// DefaultMaxWork is the work cap of one registry walk when Config.MaxWork
// is 0: about a second of lookups.
const DefaultMaxWork = 50000

// Target is the time all the checks together should stay under; the
// total line names it, nothing asserts it.
const Target = 5 * time.Second

// Config is what the checks read.
type Config struct {
	// Runtime is the application's runtime, built once with its fixture
	// data. The registry checks fail when it is nil, unless Off names
	// them.
	Runtime *ochazuke.Runtime
	// Runtimes are more runtimes the registry walks and the names
	// vocabulary read beside Runtime (ochazuke: the demo app). The
	// lexicon reads Runtime only.
	Runtimes []*ochazuke.Runtime
	// Root is the directory the static scans start at and their file
	// names are relative to; default ".".
	Root string
	// Dirs are the package directories the static scans read, relative
	// to Root; "dir/..." walks dir and below. Default "./...".
	Dirs []string
	// Baselines is the directory of the baseline files, relative to the
	// test's directory; default "testdata".
	Baselines string
	// Off turns checks off by id, each with the reason it is off (logged
	// as the subtest's skip). An empty reason or an unknown id fails.
	Off map[string]string
	// MaxWork is the work cap of one registry walk; default
	// DefaultMaxWork.
	MaxWork int
	// Names is passed through to namecheck: Vocab defaults to the
	// runtimes' NameVocab, Baseline to Baselines/namecheck.txt, Dirs to
	// Root and Dirs; Ceiling, DeclCalls and DeclTypes as given.
	Names namecheck.Config
	// Lexicon is passed through to lexiconcheck: Operations defaults to
	// Runtime's, Baseline to Baselines/lexiconcheck.txt, Words to
	// Baselines/lexicon-words.tsv when that file exists; MainNouns as
	// given.
	Lexicon lexiconcheck.Config
}

// Kind says what a check reads.
type Kind string

const (
	// Static checks scan the source.
	Static Kind = "static"
	// Registry checks walk the built runtime.
	Registry Kind = "registry"
)

// Rule is one check: its id (the subtest name and baseline file name),
// its kind, what it enforces, and its run.
type Rule struct {
	ID   string
	Kind Kind
	Doc  string
	Run  func(t *testing.T, c *Context)
}

// Checks are every check, in the order Check runs them.
func Checks() []Rule {
	return []Rule{
		{ID: "deadkeys", Kind: Registry, Run: runDeadKeys,
			Doc: "a key one list row answers at its own level, a sibling row of another noun answers too (its own row or its key redirect)"},
		{ID: "names", Kind: Static, Run: runNames,
			Doc: "no comparison, case or table keyed by a noun, verb, mode, motion or parameter name (namecheck)"},
		{ID: "refusals", Kind: Static, Run: runRefusals,
			Doc: "no new refusal site (refusecheck)"},
		{ID: "lexicon", Kind: Registry, Run: runLexicon,
			Doc: "a word is one part of speech; nouns singular; keys of three parts (lexiconcheck)"},
		{ID: "nouns", Kind: Registry, Run: runNouns,
			Doc: "thing nouns have the core verbs and a key prop; command nouns no core verb names (nouncheck)"},
	}
}

// Context is what a running check reads: the config with its defaults
// filled, and the walk's work counter.
type Context struct {
	Config
	work, max int
}

// AllRuntimes are Runtime and Config.Runtimes, nil ones left out.
func (c *Context) AllRuntimes() []*ochazuke.Runtime {
	var out []*ochazuke.Runtime
	for _, r := range append([]*ochazuke.Runtime{c.Runtime}, c.Runtimes...) {
		if r != nil {
			out = append(out, r)
		}
	}
	return out
}

// Work counts n units of a registry walk; false once the walk is over
// the cap (the check then stops and fails, see Over).
func (c *Context) Work(n int) bool {
	c.work += n
	return c.work <= c.max
}

// Units is the work counted so far.
func (c *Context) Units() int { return c.work }

// Over fails t when the walk passed the cap.
func (c *Context) Over(t testing.TB) {
	t.Helper()
	if c.work > c.max {
		t.Fatalf("walk too large: %d units (cap %d); the fixture grew or the walk stopped de-duplicating (raise Config.MaxWork with a comment only for a larger app)", c.work, c.max)
	}
}

// BaselinePath is the baseline file of a new check: Baselines/rules/<id>.txt.
func (c *Context) BaselinePath(id string) string {
	return filepath.Join(c.Baselines, "rules", id+".txt")
}

// Check runs every check as a subtest of t and logs each one's time and
// the total.
func Check(t *testing.T, cfg Config) {
	t.Helper()
	if cfg.Root == "" {
		cfg.Root = "."
	}
	if len(cfg.Dirs) == 0 {
		cfg.Dirs = []string{"./..."}
	}
	if cfg.Baselines == "" {
		cfg.Baselines = "testdata"
	}
	if cfg.MaxWork <= 0 {
		cfg.MaxWork = DefaultMaxWork
	}
	rules := Checks()
	for id, why := range cfg.Off {
		if !slices.ContainsFunc(rules, func(r Rule) bool { return r.ID == id }) {
			t.Fatalf("rulecheck: Off names %q, no check (checks: %s)", id, ids(rules))
		}
		if strings.TrimSpace(why) == "" {
			t.Fatalf("rulecheck: Off %q has no reason; a check is turned off only with one", id)
		}
	}
	start := time.Now()
	var times []string
	for _, rule := range rules {
		began := time.Now()
		c := &Context{Config: cfg, max: cfg.MaxWork}
		t.Run(rule.ID, func(t *testing.T) {
			if why, off := cfg.Off[rule.ID]; off {
				t.Skipf("off: %s", why)
			}
			if rule.Kind == Registry && len(c.AllRuntimes()) == 0 {
				t.Fatalf("rulecheck: %s walks the runtime and Config.Runtime is nil: set it, or Off %q with a reason", rule.ID, rule.ID)
			}
			rule.Run(t, c)
			if c.work > 0 {
				t.Logf("%s: %d work units (cap %d)", rule.ID, c.work, c.max)
			}
		})
		took := time.Since(began)
		times = append(times, fmt.Sprintf("%s %s", rule.ID, took.Round(time.Millisecond)))
	}
	t.Logf("rules: %s (target %s): %s", time.Since(start).Round(100*time.Millisecond), Target, strings.Join(times, ", "))
}

func ids(rules []Rule) string {
	out := make([]string, len(rules))
	for i, r := range rules {
		out[i] = r.ID
	}
	return strings.Join(out, ", ")
}

// passUpdate sets an older check's own update variable from
// RULECHECK_UPDATE when it is unset, for the subtest.
func passUpdate(t *testing.T, env string) {
	if v := os.Getenv(UpdateEnv); v != "" && os.Getenv(env) == "" {
		t.Setenv(env, v)
	}
}

// scanDirs are cfg.Dirs joined to Root, keeping a "/..." suffix.
func scanDirs(root string, dirs []string) []string {
	out := make([]string, len(dirs))
	for i, d := range dirs {
		if base, ok := strings.CutSuffix(d, "..."); ok {
			out[i] = filepath.Join(root, strings.TrimSuffix(base, "/")) + "/..."
			continue
		}
		out[i] = filepath.Join(root, d)
	}
	return out
}
