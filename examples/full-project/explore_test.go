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
	explore.Survey(t, explore.Subject{Expected: noGit, Serial: true, Name: "full-project", New: func() (*ochazuke.Model, error) {
		cfg, err := LoadConfig("")
		if err != nil {
			return nil, err
		}
		return Build(cfg)
	}})
}

// noGit expects the picker's note that it indexed without git (PATH is
// empty here).
func noGit(_, err string) bool { return strings.Contains(err, "locate Git ignore rules") }
