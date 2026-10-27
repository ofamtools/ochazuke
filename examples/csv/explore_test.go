//go:build explore

package main

import (
	"strings"
	"testing"

	"github.com/unixsuperhero/ochazuke"
	"github.com/unixsuperhero/ochazuke/explore"
)

// TestExplore surveys an untitled sheet in an empty working directory (a
// save or the picker touches only it).
func TestExplore(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("PATH", "") // the picker probes git; without it the index is a plain walk
	explore.Survey(t, explore.Subject{Expected: expected, Name: "csv", New: func() (*ochazuke.Model, error) {
		a, err := New("")
		if err != nil {
			return nil, err
		}
		return a.Model(), nil
	}})
}

// expected is the framework's refusals, the sheet's own (nothing to undo
// or redo, no column to sort or filter by: an untitled sheet has none) and
// the picker's note that it indexed without git (PATH is empty here). The
// sheet's errors are plain text, so they are matched by message.
func expected(op, err string) bool {
	switch {
	case explore.FrameworkRefusal(op, err), strings.Contains(err, "locate Git ignore rules"):
		return true
	}
	switch err {
	case "nothing to undo", "nothing to redo", "no column to sort by", "no column to filter by":
		return true
	}
	return false
}
