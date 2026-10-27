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
// []PaletteInstance, []Operation (the id is noun.verb), []map whose id is
// "id", else "key", else "name" and whose label is "label", else "title",
// or []any of strings or such maps (the JSON-decoded form).
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
		case Operation:
			out = append(out, Choice{ID: item.Key()})
		case map[string]any:
			id, ok := firstString(item, "id", "key", "name")
			if !ok {
				return errors.New("choice map needs a string id, key or name")
			}
			label, _ := firstString(item, "label", "title")
			out = append(out, Choice{ID: id, Label: label})
		case map[string]string:
			id, ok := firstString(item, "id", "key", "name")
			if !ok {
				return errors.New("choice map needs a string id, key or name")
			}
			label, _ := firstString(item, "label", "title")
			out = append(out, Choice{ID: id, Label: label})
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
	case []Operation:
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

// firstString is the first of keys whose value in m is a non-empty string.
func firstString[V any](m map[string]V, keys ...string) (string, bool) {
	for _, k := range keys {
		if s, ok := any(m[k]).(string); ok && s != "" {
			return s, true
		}
	}
	return "", false
}

// ChoicesSource is the operation (noun.verb) a parameter's choices come
// from: its Source, else its Noun's list verb when that is registered,
// asks for no confirmation and needs no argument; empty when there is
// none (a Noun parameter then lists the noun's live instances).
func (r *Runtime) ChoicesSource(p Parameter) string {
	if p.Source != "" {
		return r.CanonicalKey(p.Source)
	}
	if p.Noun == "" {
		return ""
	}
	key := r.CanonicalNoun(p.Noun) + ".list"
	op, ok := r.Operations[key]
	if !ok || op.Confirm != "" {
		return ""
	}
	for _, param := range op.Parameters {
		if param.Required {
			return ""
		}
	}
	return key
}

// nounInstanceChoices lists the live instances of a noun: the instances
// that are not list rows in registration order, each list's rows (every
// row, a filter's hidden ones too) in list order after the list itself.
// The id is the instance id (what --target and ExecuteAt resolve, never a
// foreign id in Data), labelled by the instance's label when it has one.
func (r *Runtime) nounInstanceChoices(noun string) []Choice {
	noun = r.CanonicalNoun(noun)
	var out []Choice
	add := func(id, label string) {
		if label == "" {
			label = id
		}
		out = append(out, Choice{ID: id, Label: label})
	}
	for _, id := range r.holders {
		inst := r.Instances[id]
		if inst == nil {
			continue
		}
		if inst.Noun == noun {
			add(inst.ID, inst.Label)
		}
		list, ok := inst.Component.(*List)
		if !ok {
			continue
		}
		for _, item := range list.all {
			row := r.Instances[item.ID]
			if row == nil || row.Noun != noun {
				continue
			}
			add(row.ID, row.Label)
		}
	}
	return out
}

// enumChoices is a parameter's Enum as choices.
func enumChoices(p Parameter) []Choice {
	var out []Choice
	for _, id := range p.Enum {
		out = append(out, Choice{ID: id, Label: id})
	}
	return out
}

// Choices lists a parameter's accepted values in context c: its Enum, then
// the result of its choices source (ChoicesSource), or, for a Noun with no
// list verb, the noun's live instances. A source that asks for
// confirmation is refused; it must be a read.
func (r *Runtime) Choices(c Context, p Parameter) ([]Choice, error) {
	out := enumChoices(p)
	source := r.ChoicesSource(p)
	if source == "" {
		if p.Noun != "" {
			out = append(out, r.nounInstanceChoices(p.Noun)...)
		}
		return out, nil
	}
	op, ok := r.Operations[source]
	if !ok {
		return out, fmt.Errorf("unknown choices source %q", source)
	}
	if op.Confirm != "" {
		return out, fmt.Errorf("choices source %q asks for confirmation", source)
	}
	result, err := r.ExecuteAt(c, Request{Verb: op.Verb, Noun: op.Noun})
	if err != nil {
		return out, fmt.Errorf("choices source %q: %w", source, err)
	}
	more, err := ParseChoices(result)
	if err != nil {
		return out, fmt.Errorf("choices source %q: %w", source, err)
	}
	return append(out, more...), nil
}

// loadChoices lists a parameter's choices like Choices without blocking
// the loop on an application source: its Enum, then the source's result,
// where a source verb that returns Pending runs off the loop as a job of
// owner (closing owner cancels it) and done runs when it lands. done runs
// at once when nothing is pending, and never for a cancelled load; it
// reports whether a load is in flight.
func (r *Runtime) loadChoices(c Context, p Parameter, owner string, done func([]Choice, error)) bool {
	enum := enumChoices(p)
	source := r.ChoicesSource(p)
	op, ok := r.Operations[source]
	if source == "" || !ok || op.App == nil || op.Confirm != "" {
		choices, err := r.Choices(c, p)
		done(choices, err)
		return false
	}
	parse := func(value any, err error) ([]Choice, error) {
		if err != nil {
			return enum, fmt.Errorf("choices source %q: %w", source, err)
		}
		more, err := ParseChoices(value)
		if err != nil {
			return enum, fmt.Errorf("choices source %q: %w", source, err)
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
		done(enum, fmt.Errorf("choices source %q: %w", source, err))
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
