package ochazuke

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Choice is one value a parameter accepts: ID is the argument value, Label
// what a pick list shows.
type Choice struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// ParseChoices reads a source operation's result: []string, []Choice,
// []PaletteInstance, []map with "id" and optional "label", or []any of
// strings or such maps (the JSON-decoded form).
func ParseChoices(result any) ([]Choice, error) {
	var out []Choice
	add := func(v any) error {
		switch item := v.(type) {
		case string:
			out = append(out, Choice{ID: item, Label: item})
		case Choice:
			out = append(out, item)
		case PaletteInstance:
			out = append(out, Choice(item))
		case map[string]any:
			id, ok := item["id"].(string)
			if !ok {
				return errors.New("choice map needs a string id")
			}
			label, _ := item["label"].(string)
			out = append(out, Choice{ID: id, Label: label})
		case map[string]string:
			if _, ok := item["id"]; !ok {
				return errors.New("choice map needs a string id")
			}
			out = append(out, Choice{ID: item["id"], Label: item["label"]})
		default:
			return fmt.Errorf("unsupported choice %T", v)
		}
		return nil
	}
	var err error
	switch list := result.(type) {
	case []string:
		for _, v := range list {
			err = errors.Join(err, add(v))
		}
	case []Choice:
		out = append(out, list...)
	case []PaletteInstance:
		for _, v := range list {
			err = errors.Join(err, add(v))
		}
	case []map[string]any:
		for _, v := range list {
			err = errors.Join(err, add(v))
		}
	case []map[string]string:
		for _, v := range list {
			err = errors.Join(err, add(v))
		}
	case []any:
		for _, v := range list {
			err = errors.Join(err, add(v))
		}
	default:
		return nil, fmt.Errorf("unsupported choices result %T", result)
	}
	if err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Label == "" {
			out[i].Label = out[i].ID
		}
	}
	return out, nil
}

// Choices lists a parameter's accepted values in context c: its Enum, then
// the result of its Source operation. A source that asks for confirmation
// is refused; it must be a read.
func (r *Runtime) Choices(c Context, p Parameter) ([]Choice, error) {
	var out []Choice
	for _, id := range p.Enum {
		out = append(out, Choice{ID: id, Label: id})
	}
	if p.Source == "" {
		return out, nil
	}
	op, ok := r.Operations[p.Source]
	if !ok {
		return out, fmt.Errorf("unknown choices source %q", p.Source)
	}
	if op.Confirm != "" {
		return out, fmt.Errorf("choices source %q asks for confirmation", p.Source)
	}
	result, err := r.ExecuteAt(c, Request{Verb: op.Verb, Noun: op.Noun})
	if err != nil {
		return out, fmt.Errorf("choices source %q: %w", p.Source, err)
	}
	more, err := ParseChoices(result)
	if err != nil {
		return out, fmt.Errorf("choices source %q: %w", p.Source, err)
	}
	return append(out, more...), nil
}

// loadChoices lists a parameter's choices like Choices without blocking
// the loop on an application Source: its Enum, then the Source's result,
// where a Source verb that returns Pending runs off the loop as a job of
// owner (closing owner cancels it) and done runs when it lands. done runs
// at once when nothing is pending, and never for a cancelled load; it
// reports whether a load is in flight.
func (r *Runtime) loadChoices(c Context, p Parameter, owner string, done func([]Choice, error)) bool {
	var enum []Choice
	for _, id := range p.Enum {
		enum = append(enum, Choice{ID: id, Label: id})
	}
	op, ok := r.Operations[p.Source]
	if p.Source == "" || !ok || op.App == nil || op.Confirm != "" {
		choices, err := r.Choices(c, p)
		done(choices, err)
		return false
	}
	parse := func(value any, err error) ([]Choice, error) {
		if err != nil {
			return enum, fmt.Errorf("choices source %q: %w", p.Source, err)
		}
		more, err := ParseChoices(value)
		if err != nil {
			return enum, fmt.Errorf("choices source %q: %w", p.Source, err)
		}
		return append(slices.Clone(enum), more...), nil
	}
	ctx := Ctx{Runtime: r, Context: c, Instance: r.Instances[focusOf(c)]}
	res := op.App(ctx)
	if res.err != nil || res.work == nil {
		done(parse(res.value, res.err))
		return false
	}
	inner := res.then
	res = res.Then(func(c Ctx, value any, err error) {
		if inner != nil {
			inner(c, value, err)
		}
		done(parse(value, err))
	})
	if _, err := r.Launch(owner, op.Key(), res, ctx); err != nil {
		done(enum, fmt.Errorf("choices source %q: %w", p.Source, err))
		return false
	}
	return true
}

// ArgumentError is a request refused for its arguments (a missing required
// one, a wrong type, a value outside the enum or the source's choices)
// before any instance was chosen, so a caller never blames the target for
// it (the CLI adds no "pass --target" hint).
type ArgumentError struct{ Err error }

func (e *ArgumentError) Error() string { return e.Err.Error() }
func (e *ArgumentError) Unwrap() error { return e.Err }

// checkArguments is validateArguments plus the lazy Source check: a string
// argument of a parameter with a Source must be one of the source's choices
// at execution time. A source that fails skips the check rather than
// blocking the operation; Free skips it always.
func (r *Runtime) checkArguments(op Operation, args Arguments) error {
	if err := validateArguments(op, args); err != nil {
		return err
	}
	for name, v := range args {
		p := op.Parameters[name]
		s, ok := v.(string)
		if !ok || p.Source == "" || p.Free {
			continue
		}
		choices, err := r.Choices(r.Context(), p)
		if err != nil {
			continue
		}
		if !hasChoice(choices, s) {
			return choiceError(name, choices)
		}
	}
	return nil
}

func hasChoice(choices []Choice, id string) bool {
	return slices.ContainsFunc(choices, func(c Choice) bool { return c.ID == id })
}

func choiceError(name string, choices []Choice) error {
	ids := make([]string, len(choices))
	for i, c := range choices {
		ids[i] = c.ID
	}
	return fmt.Errorf("%s must be one of %s", name, strings.Join(ids, ", "))
}
