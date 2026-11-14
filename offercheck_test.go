package ochazuke_test

import (
	"testing"
	"time"

	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/grid"
	"ofam.dev/ochazuke/markdown"
	"ofam.dev/ochazuke/offercheck"
)

// Every verb ochazuke offers, with each listed value, passes a dry run:
// it would run, or it asks (a missing value, a ChoiceAnswer), or agent
// rights hold it (no-refusals standing check 3). One shared runtime with
// the markdown and grid components; a dry run, so it takes well under a
// second.
func TestEveryOfferIsAccepted(t *testing.T) {
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	markdown.Register(r)
	grid.Register(r)
	start := time.Now()
	offercheck.Check(t, r)
	_, n, _ := offercheck.Walk(r, offercheck.Config{})
	if n < 1000 {
		t.Fatalf("offercheck walked only %d offers", n)
	}
	t.Logf("offercheck: %d offers in %s", n, time.Since(start))
}
