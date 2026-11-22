package core

import (
	"maps"
	"slices"
	"strconv"
)

// MacroRecorder records key events into named registers 'a'..'z' and
// replays them. Its zero value is ready to use. Writes are copy-on-write,
// so a copied MacroRecorder is an independent snapshot.
//
// A host typically binds "q {reg}" to Start, "q" (while recording) to Stop
// and "@ {reg}" / "@ @" to Replay / ReplayLast, calls Record for every key
// it feeds to Engine.Step while recording, and feeds replayed keys through
// Engine.ReplayKeys. Call Stop before Record for the key that ends the
// recording, so the stop key is not part of the macro.
type MacroRecorder struct {
	macros    map[rune][]KeyEvent
	recording rune
	buf       []KeyEvent
	last      rune
}

// Recording returns the register being recorded.
func (m MacroRecorder) Recording() (rune, bool) { return m.recording, m.recording != 0 }

// Start begins recording into reg ('a'..'z'; 'A'..'Z' appends to the
// lowercase macro). It reports false for another name or while recording.
func (m *MacroRecorder) Start(reg rune) bool {
	if !IsNamed(reg) || m.recording != 0 {
		return false
	}
	m.recording, m.buf = reg, nil
	return true
}

// Record appends ev to the macro being recorded; it is a no-op otherwise.
func (m *MacroRecorder) Record(ev KeyEvent) {
	if m.recording != 0 {
		m.buf = append(slices.Clip(m.buf), ev)
	}
}

// Stop ends recording, stores the macro and returns its keys (nil when not
// recording).
func (m *MacroRecorder) Stop() []KeyEvent {
	reg := m.recording
	if reg == 0 {
		return nil
	}
	keys := slices.Clone(m.buf)
	m.recording, m.buf = 0, nil
	stored := keys
	if reg >= 'A' && reg <= 'Z' {
		reg = lower(reg)
		stored = append(slices.Clone(m.macros[reg]), keys...)
	}
	m.Set(reg, stored)
	return keys
}

// Set stores keys as the macro in reg ('a'..'z'; uppercase is lowered).
func (m *MacroRecorder) Set(reg rune, keys []KeyEvent) bool {
	if !IsNamed(reg) {
		return false
	}
	macros := maps.Clone(m.macros)
	if macros == nil {
		macros = map[rune][]KeyEvent{}
	}
	macros[lower(reg)] = slices.Clone(keys)
	m.macros = macros
	return true
}

// Macro returns a copy of the macro stored in reg.
func (m MacroRecorder) Macro(reg rune) ([]KeyEvent, bool) {
	keys, ok := m.macros[lower(reg)]
	return slices.Clone(keys), ok && len(keys) > 0
}

// Names returns the registers holding macros, sorted.
func (m MacroRecorder) Names() []rune {
	out := slices.Collect(maps.Keys(m.macros))
	slices.Sort(out)
	return out
}

// Replay returns the keys of reg repeated times (at least once) and makes
// reg the last played macro. '@' replays the last played macro.
func (m *MacroRecorder) Replay(reg rune, times int) ([]KeyEvent, bool) {
	if reg == '@' {
		return m.ReplayLast(times)
	}
	keys, ok := m.Macro(reg)
	if !ok {
		return nil, false
	}
	m.last = lower(reg)
	out := make([]KeyEvent, 0, len(keys)*max(times, 1))
	for range max(times, 1) {
		out = append(out, keys...)
	}
	return out, true
}

// LastPlayed returns the register last replayed (0 when none).
func (m MacroRecorder) LastPlayed() rune { return m.last }

// ReplayLast replays the last played macro.
func (m *MacroRecorder) ReplayLast(times int) ([]KeyEvent, bool) {
	if m.last == 0 {
		return nil, false
	}
	return m.Replay(m.last, times)
}

// ReplayKeys feeds keys through Step one at a time, recomputing the
// context chain from the current state before each key (chainFn may be
// nil for an empty chain), and returns the combined result: the final
// state, every effect in order, and the merged trace. Prefix timeouts made
// stale by later keys are dropped, so replay is deterministic; a timeout
// for a prefix still pending at the end is kept.
func (e *Engine) ReplayKeys(s State, chainFn func(State) []ContextFrame, keys []KeyEvent) StepResult {
	out := StepResult{State: s, Trace: StepTrace{Event: "replay:" + strconv.Itoa(len(keys))}}
	for _, k := range keys {
		var chain []ContextFrame
		if chainFn != nil {
			chain = chainFn(out.State)
		}
		r := e.Step(out.State, chain, k)
		out.State = r.State
		out.Effects = append(out.Effects, r.Effects...)
		out.Trace.Keys = append(out.Trace.Keys, r.Trace.Keys...)
		out.Trace.Commands = append(out.Trace.Commands, r.Trace.Commands...)
		out.Trace.Notes = append(out.Trace.Notes, r.Trace.Notes...)
	}
	in := out.State.Input
	out.Effects = slices.DeleteFunc(out.Effects, func(ef Effect) bool {
		t, ok := ef.(TimeoutEffect)
		return ok && (t.Token != in.Seq || len(in.Prefix) == 0)
	})
	return out
}
