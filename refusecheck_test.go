package ochazuke_test

import (
	"testing"

	"ofam.dev/ochazuke/refusecheck"
)

// ochazuke holds itself to refusecheck: its non-test Go (the root, the cli,
// markdown and grid packages) adds no refusal site (an error template that
// turns a request away, an Unavailable or Lacks declaration, a When with no
// Fallback, a Confirm without Loses, an Enum without Accept). The accepted
// sites are testdata/refusecheck.txt: a row with an exception reason stays,
// a row without one is debt that only shrinks (REFUSECHECK_UPDATE=1).
func TestNoNewRefusals(t *testing.T) {
	refusecheck.CheckConfig(t, refusecheck.Config{Dirs: []string{".", "cli", "markdown", "grid"}}, "testdata/refusecheck.txt")
}
