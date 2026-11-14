// Package offercheck fails a test when something the UI offers would be
// refused (no-refusals standing check 3: "if it is offered, it works").
//
// It is a dry run on one shared runtime, never an execution: for every
// operation the runtime registers as offered (registered, not
// Operation.Unavailable, not a catalog proxy), and for each value of each
// parameter's Enum, it calls Runtime.CheckRequest (arguments, target,
// subjects, guards and verb checks, without the handler, a modal or
// history) and fails on any answer that is not an accepted one:
//
//   - nil (the request would run);
//   - a MissingParameterError or a ChoiceAnswer (it asks);
//   - a RefusedError (agent rights, exception E1);
//   - an UnknownNameError (the request names a thing that does not exist
//     here; the dry run fills no thing).
//
// The walk counts its work and fails past Config.Cap requests rather than
// timing anything, so it stays a check of seconds (owner 2026-11-13: "we
// can't have a command line TUI that takes an hour").
//
// An application adopts it with one test over its own runtime:
//
//	func TestEveryOfferIsAccepted(t *testing.T) {
//		offercheck.Check(t, app.Runtime)
//	}
package offercheck

import (
	"errors"
	"slices"
	"strconv"
	"testing"

	"ofam.dev/ochazuke"
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

// Walk dry-runs every offer and answers the refusals and the number of
// requests made; err when the walk would pass the cap.
func Walk(r *ochazuke.Runtime, c Config) ([]Refusal, int, error) {
	cap := c.Cap
	if cap <= 0 {
		cap = DefaultCap
	}
	var out []Refusal
	n := 0
	for _, op := range r.Operations.List() {
		key := op.Key.Short()
		if op.Unavailable != "" || op.CatalogProxy || c.Skip[key] != "" {
			continue
		}
		for _, args := range offers(*op) {
			if n++; n > cap {
				return out, n, errors.New("offercheck: more than " + strconv.Itoa(cap) + " offered requests; raise Config.Cap or skip reviewed operations")
			}
			q := ochazuke.Request{Noun: op.Noun(), Verb: op.Name(), Args: args}
			if err := r.CheckRequest(q); err != nil && !accepted(err) {
				out = append(out, Refusal{Key: key, Args: args, Err: err})
			}
		}
	}
	return out, n, nil
}

// offers are the requests an operation is offered with: no arguments,
// then each Enum value of each parameter.
func offers(op ochazuke.Operation) []ochazuke.Arguments {
	out := []ochazuke.Arguments{{}}
	names := make([]string, 0, len(op.Parameters))
	for name := range op.Parameters {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		for _, v := range op.Parameters[name].Enum {
			out = append(out, ochazuke.Arguments{name: v})
		}
	}
	return out
}

// accepted reports an answer that is not a refusal of what was offered.
func accepted(err error) bool {
	var missing *ochazuke.MissingParameterError
	var choice *ochazuke.ChoiceAnswer
	var rights *ochazuke.RefusedError
	var unknown *ochazuke.UnknownNameError
	return errors.As(err, &missing) || errors.As(err, &choice) || errors.As(err, &rights) || errors.As(err, &unknown)
}
