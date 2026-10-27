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
	explore.Survey(t, explore.Subject{Expected: noGit, Name: "csv", New: func() (*ochazuke.Model, error) {
		a, err := New("")
		if err != nil {
			return nil, err
		}
		return a.Model(), nil
	}})
}

// noGit expects the picker's note that it indexed without git (PATH is
// empty here).
func noGit(_, err string) bool { return strings.Contains(err, "locate Git ignore rules") }
