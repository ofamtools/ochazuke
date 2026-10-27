package demo_test

import (
	"strings"
	"testing"

	"github.com/unixsuperhero/ochazuke"
	"github.com/unixsuperhero/ochazuke/examples/demo"
	"github.com/unixsuperhero/ochazuke/explore"
)

// demoSubject is the demo with its path fields pointed at an empty
// directory, so the picker indexes nothing of the machine.
func demoSubject(dir string) explore.Subject {
	return explore.Subject{Expected: noGit, Name: "demo", New: func() (*ochazuke.Model, error) {
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

// noGit expects the picker's note that it indexed without git (PATH is
// empty here).
func noGit(_, err string) bool { return strings.Contains(err, "locate Git ignore rules") }
