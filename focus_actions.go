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
	r.Register(Operation{Key: "ochazuke.focus.act", Description: "List every verb from the focused thing's key walk, grouped by origin, with full names, keys and unmet needs; enter runs at the original focus, asking for missing arguments. Default g . (setting actions.focused-picker)", Result: "picker state", HistoryKind: HistoryAction, Agent: AgentAllow, Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		_, err := r.OpenFocusActions()
		return nil, err
	}})
}

// FocusedActions lists every operation of every noun on the key walk, plus
// operations its binding rows name in other namespaces. Nothing is hidden
// by availability, motion or typing. Choices are grouped innermost first
// (Noun is the originating walk noun), sorted by full operation name within
// each group. Cells hold that name, its key and its description or unmet needs.
func (r *Runtime) FocusedActions(focus string) (noun string, choices []Choice) {
	if r.Instances[focus] == nil {
		return "", nil
	}
	levels := r.stack(focus)
	keys, bound := r.focusedActionBindings(levels, focus)
	seen := map[Key]bool{}
	for _, lv := range levels {
		origin := lv.name()
		var group []Choice
		add := func(op *Operation) {
			if seen[op.Key] {
				return
			}
			seen[op.Key] = true
			key := op.Key.Short()
			detail := firstLine(op.Description)
			label := key + " " + origin + " " + op.Description
			if needs := r.focusedActionNeeds(*op, focus); needs != "" {
				detail = "needs: " + needs
				label += " " + detail
			}
			group = append(group, Choice{ID: key, Noun: origin,
				Label: label,
				Cells: []string{key, keys[key], detail}, RowFlags: RowFlags{Bare: true}})
		}
		for _, op := range r.Operations.Lookups().ByPair[origin] {
			add(op)
		}
		for _, op := range bound[origin] {
			add(op)
		}
		slices.SortFunc(group, func(a, b Choice) int { return strings.Compare(a.ID, b.ID) })
		if noun == "" && len(group) > 0 {
			noun = origin
		}
		choices = append(choices, group...)
	}
	return noun, choices
}

func (r *Runtime) focusedActionBindings(levels []level, focus string) (map[string]string, map[string][]*Operation) {
	keys := map[string]string{}
	bound := map[string][]*Operation{}
	take := func(row levelRow, origin string) {
		q := r.canonicalRequest(row.Request)
		if q.IsProp() {
			var err error
			q, err = r.propVerbRequest(q)
			if err != nil {
				return
			}
		}
		op, ok := r.Operations.Get(Key(q.Key()))
		if !ok {
			return
		}
		key := op.Key.Short()
		if keys[key] == "" {
			keys[key], _ = helpKey(rowKey(row.Binding))
		}
		if origin != "" {
			bound[origin] = append(bound[origin], op)
		}
	}
	// Prefer an effective key, then a declared one for a currently unmet
	// need. Read each level separately so shadowing and an any row cannot
	// hide the other operations that level offers.
	for _, row := range r.levelRows(focus) {
		take(row, "")
	}
	for _, lv := range levels {
		for _, row := range r.scanRows(r.walkOf([]level{lv}, false), r.Mode, "") {
			take(row, lv.name())
		}
	}
	return keys, bound
}

func (r *Runtime) focusedActionNeeds(op Operation, focus string) string {
	q := Request{Noun: op.Noun(), Verb: op.Name()}
	args, err := r.sourcesAt(op, q, focus)
	var needs []string
	if err != nil {
		needs = append(needs, err.Error())
	}
	for _, name := range append(missingRequired(op, args), r.missingTargets(op, args, focus)...) {
		if !op.Parameters[name].Plumbing && !slices.Contains(needs, name) {
			needs = append(needs, name)
		}
	}
	if reason := r.unavailableReason(op); reason != "" {
		needs = append(needs, reason)
	}
	q.Args = args
	at := r.rowInstance(level{}, q, focus)
	if !r.Available(q, at, r.Mode) {
		err := r.refusal(op, q, at, r.Mode)
		var held *NotApplicableError
		switch {
		case errors.As(err, &held):
			for _, name := range held.Held {
				needs = append(needs, "guard "+name)
			}
		case needed(err) != nil:
			if name := needed(err).Param; !slices.Contains(needs, name) {
				needs = append(needs, name)
			}
		default:
			needs = append(needs, err.Error())
		}
	}
	return strings.Join(needs, "; ")
}

// OpenFocusActions opens the focused-actions picker over the current focus,
// including a viewer or another modal, without crossing its key-walk boundary.
func (r *Runtime) OpenFocusActions() (*Chooser, error) {
	if !r.Settings().Bool(FocusedPickerSetting) {
		return nil, errors.New("the focused-actions picker is off (setting actions.focused-picker)")
	}
	focus := r.Focus
	noun, choices := r.FocusedActions(focus)
	if noun == "" {
		return nil, errors.New("nothing at the focus has verbs to list")
	}
	var rows []PickerRow
	origin := ""
	for _, choice := range choices {
		if choice.Noun != origin {
			origin = choice.Noun
			rows = append(rows, PickerRow{ID: "origin:" + origin, Label: origin, Header: true, Section: origin})
		}
		row := choice.Row()
		row.Section = origin
		rows = append(rows, row)
	}
	return r.OpenPicker(PickerSpec{
		Kind: FocusActionsKind, Title: "Actions · " + r.DisplayNoun(noun), Plural: "actions", Noun: noun,
		NoPropFilters: true, NoSort: true, Match: "substring", Rows: rows, Keep: []int{0, 2},
		Choose: func(row ChooserRow) Result {
			op, ok := r.Operation(row.ID)
			if !ok {
				return Failed(fmt.Errorf("unknown operation %q", row.ID))
			}
			if err := r.RunUnbound(focus, Request{Noun: op.Noun(), Verb: op.Name()}); err != nil {
				return Failed(fmt.Errorf("%s: %w", row.ID, err))
			}
			return Done(nil)
		},
	})
}
