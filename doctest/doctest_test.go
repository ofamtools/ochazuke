package doctest_test

import (
	"testing"

	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/doctest"
)

// The reference table keeps the contract.
func TestMemDocTableKeepsTheContract(t *testing.T) {
	doctest.Run(t, func(_ *testing.T, r *ochazuke.Runtime, noun string) ochazuke.DocTable {
		return ochazuke.NewMemDocTable(r, noun)
	})
}
