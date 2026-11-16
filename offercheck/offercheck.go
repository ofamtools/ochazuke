// Package offercheck fails a test when something the UI offers would be
// refused (no-refusals standing check 3: "if it is offered, it works").
//
// Deprecated: the walk is the rule check outcomes (rulecheck.Outcomes),
// which every application's TestRules runs: it dry-runs every verb
// offered at every place, not only at one focus, and classifies each
// answer as the MCP gateway does. This package is that walk over one
// runtime, kept for a caller that has not moved to TestRules; it is
// removed after one release.
package offercheck

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/rulecheck"
)

// DefaultCap is the most requests one walk makes.
const DefaultCap = 20000

// Config shapes a walk: Cap bounds its requests (0: DefaultCap); Skip
// names operations (noun.verb, as Key.Short prints it) that are reviewed
// exceptions, each with its reason.
type Config struct {
	Cap  int
	Skip map[string]string
}

// Refusal is one offer the dry run refused.
type Refusal struct {
	Key  string
	Args ochazuke.Arguments
	Err  error
}

// Check runs the walk with the default Config and fails t on each refusal.
func Check(t testing.TB, r *ochazuke.Runtime) { CheckConfig(t, r, Config{}) }

// CheckConfig is Check with a Config.
func CheckConfig(t testing.TB, r *ochazuke.Runtime, c Config) {
	t.Helper()
	refusals, n, err := Walk(r, c)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range refusals {
		t.Errorf("offercheck: %s %v is offered but refused: %v", f.Key, f.Args, f.Err)
	}
	if len(refusals) > 0 {
		t.Logf("offercheck: %d of %d offered requests refused", len(refusals), n)
	}
}

// Walk is rulecheck.Outcomes over r alone: its failed answers as
// refusals, and the number of dry runs made; err when the walk would pass
// the cap.
func Walk(r *ochazuke.Runtime, c Config) ([]Refusal, int, error) {
	limit := c.Cap
	if limit <= 0 {
		limit = DefaultCap
	}
	units := 0
	found, cov := rulecheck.Outcomes([]*ochazuke.Runtime{r}, nil, func(n int) bool { units += n; return units <= limit })
	if units > limit {
		return nil, cov.Runs, errors.New("offercheck: more than " + strconv.Itoa(limit) + " offered requests; raise Config.Cap or skip reviewed operations")
	}
	var out []Refusal
	for _, f := range found {
		if f.Fields[0] != rulecheck.OutcomeFailedKind {
			continue
		}
		key, args, _ := strings.Cut(f.Fields[1], " ")
		if c.Skip[key] != "" {
			continue
		}
		var a ochazuke.Arguments
		if args != "" {
			_ = json.Unmarshal([]byte(args), &a)
		}
		out = append(out, Refusal{Key: key, Args: a, Err: errors.New(f.Message)})
	}
	return out, cov.Runs, nil
}
