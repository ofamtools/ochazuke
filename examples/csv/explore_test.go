//go:build explore

package main

import (
	"maps"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"
	"ofam.dev/monogatari/explore"
	"ofam.dev/ochazuke"
)

// TestExplore surveys an untitled sheet in an empty working directory (a
// save or the picker touches only it), driving the program main runs
// (App.Program) as an app with a wrapper is driven.
func TestExplore(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("PATH", "") // the picker probes git; without it the index is a plain walk
	var driven atomic.Int64
	explore.Survey(t, subject(&driven))
	if driven.Load() == 0 {
		t.Fatal("the explorer never drove the sheet's program")
	}
}

// subject is the sheet as the explorer sees it: New builds the app,
// Program is the tea.Model main runs over that app's model, and Close
// forgets the app and removes its instances (cancelling its jobs); driven
// counts the programs handed to the explorer.
func subject(driven *atomic.Int64) explore.Subject {
	var apps sync.Map // *ochazuke.Model -> *App
	return explore.Subject{Expected: expected, Name: "csv",
		New: func() (*ochazuke.Model, error) {
			a, err := New("")
			if err != nil {
				return nil, err
			}
			apps.Store(a.Model(), a)
			return a.Model(), nil
		},
		Program: func(m *ochazuke.Model) tea.Model {
			if a, ok := apps.Load(m); ok {
				driven.Add(1)
				return a.(*App).Program()
			}
			return m
		},
		Close: func(m *ochazuke.Model) {
			apps.Delete(m)
			r := m.Runtime
			for _, id := range slices.Sorted(maps.Keys(r.Instances)) {
				if inst := r.Instances[id]; inst != nil && inst.Parent == "app" {
					_ = r.RemoveInstance(id)
				}
			}
		},
	}
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
