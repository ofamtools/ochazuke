package ochazuke

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// FocusedPickerSetting turns the focused-actions picker (ochazuke.focus.act)
// on (the default) or off.
const FocusedPickerSetting = "actions.focused-picker"

// FocusActionsKind is the picker's modal kind.
const FocusActionsKind = "focus-actions"

func registerFocusActions(r *Runtime) {
	r.Register(Operation{Key: "ochazuke.focus.act", Description: "Open a picker of the verbs the focused thing's noun has, each with its key; enter runs the verb on the focused thing (a verb with missing arguments opens its form). Default g . (setting actions.focused-picker)", Result: "picker state", HistoryKind: HistoryAction, Agent: AgentAllow, Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		_, err := r.OpenFocusActions()
		return nil, err
	}})
}

// FocusedActions are the verbs the thing at focus can run, as picker
// choices: the operations of the nearest instance's noun that are
// available there (VerbShown), motions and typing left out, by name. The
// id of a choice is the verb's key (noun.verb), its cells the verb, the
// key bound to it where the focus is, and its description.
func (r *Runtime) FocusedActions(focus string) (noun string, choices []Choice) {
	for _, n := range r.focusChain(focus) {
		if n != "" && len(r.Operations.Lookups().ByPair[n]) > 0 {
			noun = n
			break
		}
	}
	if noun == "" {
		return "", nil
	}
	keys := map[string]string{}
	for _, row := range r.levelRows(focus) {
		if row.Request.Empty() || row.Request.IsProp() || row.Keys == "" {
			continue
		}
		if k := r.canonicalRequest(row.Request).Key(); keys[k] == "" {
			keys[k] = row.Keys
		}
	}
	for _, o := range r.Operations.Lookups().ByPair[noun] {
		op, key := *o, o.Key.Short()
		if op.Motion || op.Typing || op.Unavailable != "" || !r.VerbShown(key, focus, r.Mode) {
			continue
		}
		choices = append(choices, Choice{ID: key, Label: op.Name(), Cells: []string{op.Name(), keys[key], firstLine(op.Description)}})
	}
	slices.SortFunc(choices, func(a, b Choice) int { return strings.Compare(a.Label, b.Label) })
	return noun, choices
}

// OpenFocusActions opens the focused-actions picker over the base focus.
func (r *Runtime) OpenFocusActions() (*Chooser, error) {
	if !r.Settings().Bool(FocusedPickerSetting) {
		return nil, errors.New("the focused-actions picker is off (setting actions.focused-picker)")
	}
	focus := r.BaseFocus()
	noun, choices := r.FocusedActions(focus)
	if noun == "" {
		return nil, errors.New("nothing at the focus has verbs to list")
	}
	return r.OpenPicker(PickerSpec{
		Kind: FocusActionsKind, Title: "Actions · " + r.DisplayNoun(noun), Plural: "actions", Noun: noun,
		NoPropFilters: true, NoSort: true, Rows: ChoiceRows(choices),
		Choose: func(row ChooserRow) Result {
			i := strings.LastIndex(row.ID, ".")
			n, verb := row.ID[:i], row.ID[i+1:]
			if err := r.RunUnbound(focus, Request{Noun: n, Verb: verb}); err != nil {
				return Failed(fmt.Errorf("%s: %w", row.ID, err))
			}
			return Done(nil)
		},
	})
}
