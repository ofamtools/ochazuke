//go:build explore

package demo_test

import (
	"strings"
	"testing"

	"github.com/unixsuperhero/monogatari/explore"
	"github.com/unixsuperhero/ochazuke"
	"github.com/unixsuperhero/ochazuke/internal/demo"
)

// demoSubject is the demo with its path fields pointed at an empty
// directory, so the picker indexes nothing of the machine.
func demoSubject(dir string) explore.Subject {
	return explore.Subject{Expected: expected, Name: "demo", New: func() (*ochazuke.Model, error) {
		m := demo.New()
		for id, text := range map[string]string{"directory": dir, "paths": dir + "\n" + dir} {
			if in := m.Runtime.Input(id); in != nil {
				if err := in.Replace(0, len(in.Text), text); err != nil {
					return nil, err
				}
			}
		}
		return m, nil
	}}
}

func TestExplore(t *testing.T) {
	explore.Survey(t, demoSubject(t.TempDir()))
}

// TestExploreGaps is the demo's known gap: ? inside its help modal is
// hinted as opening help again and fails.
func TestExploreGaps(t *testing.T) {
	explore.Gap(t, demoSubject(t.TempDir()), "honest-hints", "? help.open fails when pressed: modal is already open", []string{"?", "?"})
}

// expected is the framework's refusals and the picker's note that it
// indexed without git (PATH is empty here).
func expected(op, err string) bool {
	return explore.FrameworkRefusal(op, err) || strings.Contains(err, "locate Git ignore rules")
}
