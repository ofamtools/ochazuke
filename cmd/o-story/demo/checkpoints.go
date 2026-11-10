package demo

import (
	"encoding/json"
	"fmt"
	"strconv"

	"ofam.dev/ochazuke"
)

// A composite has one script and step-numbered assertions. Checkpoints supply
// incompatible starting fixtures (empty/populated stores, failed writes, config)
// through the host's existing reset and command-generation fencing. They contain
// no scripts or assertions and do not create nested story tests.
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
	ss.restoreCheckpoint = func() {
		if err := ss.loadCheckpoint(checkpoints[0], -1); err != nil {
			ss.R.UI().Error(err.Error())
		}
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
		if err := ss.loadCheckpoint(fixture, next); err != nil {
			return ochazuke.Failed(err)
		}
		ss.env.Reset()
		return ochazuke.Done(nil)
	}, "Advance the composite walkthrough to its next isolated fixture").Quiet()
	// Harness controls apply only to delivered keys, not the hypothetical
	// events used to inspect the component's own help and binding tables.
	ss.R.DefineGuard("walkthrough-key", func(_ *ochazuke.Runtime, ev ochazuke.KeyEvent) bool {
		return ev.Key.Key != ""
	})
	// Page-instance rows do not pollute noun-only help tables or custom base
	// modes' inheritance. A modal boundary reads the modal noun instead.
	for _, mode := range append([]string{""}, ss.R.PendingModeNames()...) {
		row := ochazuke.Do("walkthrough.next").Hide().Captured().Guard("walkthrough-key")
		row.Keys, row.Mode, row.Noun = "ctrl+alt+n", mode, "ochazuke.modal"
		if err := ss.R.Bind(row); err != nil {
			return err
		}
		row.Noun = ""
		for _, page := range ss.all {
			row.Instance = page.ID
			if err := ss.R.Bind(row); err != nil {
				return err
			}
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

func (ss *session) loadCheckpoint(data json.RawMessage, position int) error {
	s := ss.env.Story
	name, area, script, expect, notes := s.Name, s.Area, s.Script, s.Expect, s.Notes
	fixtures := s.Fields["checkpoints"]
	// Story.UnmarshalJSON keeps source filesystem metadata. Keep the shared
	// script and assertion offsets, replacing only the starting fixture.
	if err := json.Unmarshal(data, s); err != nil {
		return err
	}
	s.Name, s.Area, s.Script, s.Expect, s.Notes = name, area, script, expect, notes
	s.Fields["checkpoints"] = fixtures
	s.Seed = json.RawMessage(strconv.Itoa(position))
	return nil
}
