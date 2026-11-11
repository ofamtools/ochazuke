package demo

import (
	"encoding/json"
	"fmt"
	"maps"
	"strconv"

	"ofam.dev/monogatari"
	"ofam.dev/ochazuke"
)

// Keep the opening fixture available to recordings and player rewinds. Only
// geometry follows the active checkpoint; the model owns its live state.
func prepareCheckpoints(env monogatari.Env) error {
	s := env.Story
	if _, ok := s.Fields["checkpoints"]; !ok {
		return nil
	}
	origin := s.Fields["checkpoint-start"]
	if origin == nil {
		initial := *s
		initial.Script, initial.Expect, initial.Notes = nil, nil, nil
		initial.Fields = maps.Clone(s.Fields)
		delete(initial.Fields, "checkpoints")
		var err error
		origin, err = json.Marshal(initial)
		if err != nil {
			return err
		}
		s.Fields = maps.Clone(s.Fields)
		s.Fields["checkpoint-start"] = origin
	}
	opening, err := checkpointPosition(origin)
	if err != nil {
		return err
	}
	position := -1
	if len(env.Seed) > 0 {
		if err := json.Unmarshal(env.Seed, &position); err != nil {
			return err
		}
	}
	if position == opening {
		// A saved partial recording may carry the active frame's geometry.
		return loadCheckpoint(s, origin, opening)
	}
	return nil
}

func checkpointPosition(data json.RawMessage) (int, error) {
	header := struct {
		Seed int `json:"seed"`
	}{Seed: -1}
	err := json.Unmarshal(data, &header)
	return header.Seed, err
}

// A composite has one script and step-numbered assertions. Its checkpoints
// contain only incompatible starting fixtures, never nested scripts or tests.
func (ss *session) checkpoints() error {
	var checkpoints []json.RawMessage
	if ok, err := ss.env.Field("checkpoints", &checkpoints); err != nil || !ok {
		return err
	}
	if len(checkpoints) == 0 {
		return fmt.Errorf("a composite needs checkpoints")
	}
	position := -1
	if len(ss.env.Seed) > 0 {
		if err := json.Unmarshal(ss.env.Seed, &position); err != nil {
			return fmt.Errorf("checkpoint position: %w", err)
		}
	}
	if position < -1 || position >= len(checkpoints) {
		return fmt.Errorf("checkpoint position %d is out of range", position)
	}
	origin := ss.env.Story.Fields["checkpoint-start"]
	opening, err := checkpointPosition(origin)
	if err != nil {
		return err
	}
	ss.restoreCheckpoint = func(keepGeometry bool) error {
		s := ss.env.Story
		w, h, chrome := s.Width, s.Height, s.Chrome
		if err := loadCheckpoint(s, origin, opening); err != nil {
			return err
		}
		if keepGeometry {
			s.Width, s.Height, s.Chrome = w, h, chrome
		}
		return nil
	}
	ss.R.Define("walkthrough").Verb("next", func(ochazuke.Ctx) ochazuke.Result {
		next := position + 1
		if next >= len(checkpoints) {
			next = 0
		}
		fixture := checkpoints[next]
		if position+1 == len(checkpoints) {
			next = -1
		}
		if err := loadCheckpoint(ss.env.Story, fixture, next); err != nil {
			return ochazuke.Failed(err)
		}
		ss.checkpointAdvance = true
		ss.env.Reset()
		return ochazuke.Done(nil)
	}, "Advance the composite walkthrough to its next isolated fixture").Quiet()
	// Harness controls apply only to delivered keys, not hypothetical events
	// used to inspect the component's own help and binding tables.
	ss.R.DefineGuard("walkthrough-key", func(_ *ochazuke.Runtime, ev ochazuke.KeyEvent) bool {
		return ev.Key.Key != ""
	})
	// The app instance stays on the walk when a verb creates a new page.
	for _, mode := range append([]string{""}, ss.R.PendingModeNames()...) {
		row := ochazuke.Do("walkthrough.next").Hide().Captured().Guard("walkthrough-key")
		row.Keys, row.Mode, row.Instance = "ctrl+alt+n", mode, "app"
		if err := ss.R.Bind(row); err != nil {
			return err
		}
	}
	// Passthrough skips capture, so its noun needs an exact row before any.
	for _, mode := range ss.R.BaseModeNames() {
		traits, _ := ss.R.ModeTraits(mode)
		if !traits.Passthrough {
			continue
		}
		row := ochazuke.Do("walkthrough.next").Hide().Guard("walkthrough-key")
		row.Keys, row.Mode, row.Noun = "ctrl+alt+n", mode, "ochazuke.terminal"
		if err := ss.R.Bind(row); err != nil {
			return err
		}
	}
	return nil
}

func loadCheckpoint(s *monogatari.Story, data json.RawMessage, position int) error {
	name, area, script, expect, notes := s.Name, s.Area, s.Script, s.Expect, s.Notes
	fixtures, origin := s.Fields["checkpoints"], s.Fields["checkpoint-start"]
	// Story.UnmarshalJSON keeps source filesystem metadata. Preserve the shared
	// script and assertion offsets, replacing only the starting fixture.
	if err := json.Unmarshal(data, s); err != nil {
		return err
	}
	s.Name, s.Area, s.Script, s.Expect, s.Notes = name, area, script, expect, notes
	s.Fields["checkpoints"], s.Fields["checkpoint-start"] = fixtures, origin
	s.Seed = json.RawMessage(strconv.Itoa(position))
	return nil
}
