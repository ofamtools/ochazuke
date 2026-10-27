package main

import (
	"testing"

	"github.com/unixsuperhero/ochazuke"
	"github.com/unixsuperhero/ochazuke/explore"
)

// TestExplore surveys the core example's tree behind a Model (it has no
// page: the Model draws its placeholder).
func TestExplore(t *testing.T) {
	explore.Survey(t, explore.Subject{Name: "core", New: func() (*ochazuke.Model, error) {
		return ochazuke.NewModel(newRuntime()), nil
	}})
}
