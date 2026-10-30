package ochazuke

import (
	"errors"
	"fmt"
	"slices"
)

// ModeDeclared is the mode name mode.set reads as "the declared mode of the
// focus" (DeclaredMode): an esc row written {mode: declared} lands on the
// mode the focused pane declares (an instance's Pin, its noun's Pin), or
// normal where nothing declares one, so no row names a pane's mode. It is
// reserved: DefineMode refuses it.
const ModeDeclared = "declared"

// declaredPin is the mode the focus at id declares and whether id itself
// declares it (own): a pinned input's mode (an input whose noun pins, such
// as a prompt), else id's own Pin, else its noun's Pin, else the Pin of
// the nearest ancestor that has one ("" when none does). A pin on id
// itself applies on every focus move to it; an ancestor's applies when
// the focus enters a stop beneath it (focusMoved). The walk stops at an
// open modal's instance, so a modal's input never takes the pin of the
// pane it opened over.
func (r *Runtime) declaredPin(id string) (string, bool) {
	if in := r.Input(id); in != nil && in.pin != "" {
		return in.pin, true
	}
	inst := r.Instances[id]
	if inst == nil {
		return "", false
	}
	if inst.Pin != "" {
		return inst.Pin, true
	}
	if noun := r.Nouns[inst.Noun]; noun != nil && noun.Pin != "" {
		return noun.Pin, true
	}
	for i, at := range r.Ancestry(id) {
		a := r.Instances[at]
		if i > 0 && a != nil && a.Pin != "" {
			return a.Pin, false
		}
		if slices.Contains(r.modals, at) {
			break // a modal is a boundary: the pane beneath declares nothing for it
		}
	}
	return "", false
}

// DeclaredMode is the base mode the focus at id declares: the pin of the
// instance, its noun or the nearest pinning ancestor (Instance.Pin,
// Define(noun).Pin), normal when none declares one. mode.set {mode:
// declared} sets the focus's.
func (r *Runtime) DeclaredMode(id string) string {
	if pin, _ := r.declaredPin(id); pin != "" {
		return pin
	}
	return "normal"
}

// PinInstance makes id declare mode as its base mode (Instance.Pin), "" to
// drop the pin. The focus landing on id, or entering a stop beneath it
// with no nearer pin, puts the runtime in mode; mode.set {mode: declared}
// returns to it. A re-pin applies at once: when the focus beneath any
// modal declares through id and the runtime stands in the mode it
// declared before (not in a mode the user lifted into, such as insert),
// the base mode follows in the same update, so the next frame's badge
// shows it; under an open modal the mode the outermost modal restores on
// close follows instead. A pin names a registered base mode.
func (r *Runtime) PinInstance(id, mode string) error {
	inst := r.Instances[id]
	if inst == nil {
		return fmt.Errorf("unknown instance %q", id)
	}
	if mode != "" && !r.Modes[mode] {
		if r.pendingModes[mode] {
			return fmt.Errorf("mode %q is a pending mode; pin a base mode", mode)
		}
		return fmt.Errorf("unknown mode %q", mode)
	}
	focus := r.BaseFocus()
	before := r.DeclaredMode(focus)
	inst.Pin = mode
	after := r.DeclaredMode(focus)
	if before == after {
		return nil
	}
	if len(r.modals) > 0 {
		if outer := r.Instances[r.modals[0]]; outer != nil && outer.returnMode.Name == before {
			outer.returnMode = ModeState{Name: after}
		}
		return nil
	}
	if r.BaseMode() != before {
		return nil
	}
	if r.Mode.Name != before {
		// A pending mode over the declared one: it returns to the new pin.
		m := r.Mode
		m.Payload.Return = after
		r.setMode(m)
		return nil
	}
	if in := r.Input(r.Focus); in != nil {
		in.transition(after, false)
		return nil
	}
	r.setMode(ModeState{Name: after})
	return nil
}

// resolveDeclared reads mode.set's ModeDeclared as the declared mode of the
// focus; any other name is itself.
func (r *Runtime) resolveDeclared(mode string) string {
	if mode == ModeDeclared {
		return r.DeclaredMode(r.Focus)
	}
	return mode
}

func registerModePin(r *Runtime) {
	r.Register(Operation{Verb: "pin", Noun: "ochazuke.mode", Description: "Make an instance declare its base mode (Instance.Pin): focus on it, or entering a stop beneath it, puts the runtime in that mode, and mode.set {mode: declared} returns to it; a re-pin of the focused pane applies at once; an empty mode drops the pin",
		Parameters: map[string]Parameter{
			"mode":     {Type: "string", Description: "The base mode to declare; empty drops the pin", Noun: "ochazuke.modes"},
			"instance": {Type: "string", Description: "The instance (default the focused stop)"},
		}, Result: "instance and mode", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
			id := StringArg(a, "instance", "")
			if id == "" {
				id = r.stopOf(r.BaseFocus())
			}
			if id == "" {
				return nil, errors.New("no instance to pin")
			}
			mode := StringArg(a, "mode", "")
			if err := r.PinInstance(id, mode); err != nil {
				return nil, err
			}
			return map[string]any{"instance": id, "mode": mode}, nil
		}})
}
