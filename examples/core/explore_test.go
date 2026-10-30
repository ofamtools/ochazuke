//go:build explore

package main

import (
	"strings"
	"testing"

	"github.com/unixsuperhero/monogatari/explore"
	"github.com/unixsuperhero/ochazuke"
)

// subject is the core example's tree behind a Model (it has no page: the
// Model draws its placeholder).
func subject() explore.Subject {
	return explore.Subject{Name: "core", Expected: explore.FrameworkRefusal, New: func() (*ochazuke.Model, error) {
		return ochazuke.NewModel(newRuntime()), nil
	}}
}

// TestExplore surveys the core example.
func TestExplore(t *testing.T) {
	explore.Survey(t, subject())
}

// TestExploreGaps are the core example's known gaps (explore findings no
// story application shows), skipped while they still reproduce. With no
// page, shift+tab lands on the closed palette's line: its rows and the
// input's are hinted there and every one fails.
func TestExploreGaps(t *testing.T) {
	for _, g := range []struct{ check, summary, keys string }{
		{"honest-hints", "' page.pick fails when pressed: no focused input", "shift+tab '"},
		{"honest-hints", ": palette.open fails when pressed: no focused input", "shift+tab :"},
		{"honest-hints", "<printable> input.insert fails when pressed: no focused input", "shift+tab x"},
		{"honest-hints", "? bindings.help fails when pressed: no focused input", "shift+tab ?"},
		{"honest-hints", "down palette.recall fails when pressed: palette is not open", "shift+tab down"},
		{"honest-hints", "up palette.recall fails when pressed: palette is not open", "shift+tab up"},
		{"honest-hints", "enter palette.run fails when pressed: palette is not open", "shift+tab enter"},
		{"honest-hints", "esc palette.cancel fails when pressed: palette is not open", "shift+tab esc"},
		{"honest-hints", "tab palette.complete fails when pressed: palette is not open", "shift+tab tab"},
		{"operation-error", "palette.move: palette is not open", "shift+tab ctrl+n"},
		{"operation-error", "input.insert: no focused input", `shift+tab "`},
	} {
		t.Run(g.keys, func(t *testing.T) { explore.Gap(t, subject(), g.check, g.summary, strings.Fields(g.keys)) })
	}
}
