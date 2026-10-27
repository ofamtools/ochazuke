//go:build explore

package main

import (
	"strings"
	"testing"

	"github.com/unixsuperhero/ochazuke"
	"github.com/unixsuperhero/ochazuke/explore"
)

// TestExplore surveys the embedded configuration (its console terminal is
// the in-memory echo stream).
func TestExplore(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("PATH", "") // the picker probes git; without it the index is a plain walk
	explore.Survey(t, subject())
}

func subject() explore.Subject {
	return explore.Subject{Expected: expected, Serial: true, Name: "full-project", New: func() (*ochazuke.Model, error) {
		cfg, err := LoadConfig("")
		if err != nil {
			return nil, err
		}
		return Build(cfg)
	}}
}

// TestExploreGaps are the full project's known gaps: each workspace.open
// key is hinted inside the modal it opened and fails there.
func TestExploreGaps(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("PATH", "")
	for _, key := range []string{"?", "ctrl+h", "ctrl+n", "ctrl+o"} {
		t.Run(key, func(t *testing.T) {
			explore.Gap(t, subject(), "honest-hints", key+" workspace.open fails when pressed: modal is already open", []string{key, key})
		})
	}
}

// expected is the framework's refusals and the picker's note that it
// indexed without git (PATH is empty here).
func expected(op, err string) bool {
	return explore.FrameworkRefusal(op, err) || strings.Contains(err, "locate Git ignore rules")
}
