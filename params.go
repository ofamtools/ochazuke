package ochazuke

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Choice is one value a parameter accepts: ID is the argument value, Label
// what a pick list shows.
type Choice struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Word is what the palette offers and takes for the choice when it is
	// not the id ("" the id): a page's title for page open. Typed whole,
	// the id still names it (palette.choice-words).
	Word string `json:"word,omitempty"`
}

// ParseChoices reads a source operation's result: []string, []Choice,
// []PaletteInstance, []Operation (the id is noun.verb), []map whose id is
// "id", else "key", else "name" and whose label is "label", else "title"
// (and word "word", Choice.Word),
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
			word, _ := firstString(item, "word")
			out = append(out, Choice{ID: id, Label: label, Word: word})
		case map[string]string:
			id, ok := firstString(item, "id", "key", "name")
			if !ok {
				return errors.New("choice map needs a string id, key or name")
			}
			label, _ := firstString(item, "label", "title")
			word, _ := firstString(item, "word")
			out = append(out, Choice{ID: id, Label: label, Word: word})
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
func (r *Runtime) Choices(c Context, p Parameter, siblings ...Arguments) ([]Choice, error) {
	return r.ChoicesFor(c, p, "", siblings...)
}

// ChoicesQueryParameter is the parameter a choices source declares to see
// the typed text: a source verb with a parameter of this name is called
// with what the user has typed so far (the word in the palette, the query
// in a form's suggestions chooser, the value being checked), so it can
// complete as you type; one without it receives only mapped sibling arguments.
const ChoicesQueryParameter = "query"

// ChoicesTakeQuery reports whether a parameter's choices source sees the
// typed text (it declares ChoicesQueryParameter).
func (r *Runtime) ChoicesTakeQuery(p Parameter) bool {
	op, ok := r.Operations[r.ChoicesSource(p)]
	_, takes := op.Parameters[ChoicesQueryParameter]
	return ok && takes
}

// choicesQueryArgs are the arguments a choices source is called with.
func choicesQueryArgs(op Operation, query string) Arguments {
	if _, ok := op.Parameters[ChoicesQueryParameter]; ok {
		return Arguments{ChoicesQueryParameter: query}
	}
	return nil
}

// ChoicesFor is Choices for the text typed so far: a source that declares
// ChoicesQueryParameter receives query. The optional siblings map supplies
// the surrounding request or form values named by SourceArgs. Pending sources
// are awaited; UI callers use the asynchronous loaders instead.
func (r *Runtime) ChoicesFor(c Context, p Parameter, query string, siblings ...Arguments) ([]Choice, error) {
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
	result, err := r.executeAt(c, Request{Verb: op.Verb, Noun: op.Noun, Args: choicesSourceArgs(op, p, query, siblings...)}, r.awaitChoices)
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
func (r *Runtime) loadChoices(c Context, p Parameter, owner string, done func([]Choice, error), siblings ...Arguments) bool {
	return r.loadChoicesFor(c, p, "", owner, done, siblings...)
}

// loadChoicesFor is loadChoices for the text typed so far (ChoicesFor).
func (r *Runtime) loadChoicesFor(c Context, p Parameter, query, owner string, done func([]Choice, error), siblings ...Arguments) bool {
	enum := enumChoices(p)
	source := r.ChoicesSource(p)
	op, ok := r.Operations[source]
	if source == "" || !ok || op.App == nil || op.Confirm != "" {
		choices, err := r.ChoicesFor(c, p, query, siblings...)
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
	ctx, err := r.choicesContext(c, op, p, query, siblings...)
	if err != nil {
		done(enum, err)
		return false
	}
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
		choices, err := r.ChoicesFor(r.Context(), p, s, args)
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

// choicesLoad is a parameter's choices for query as a Result whose value
// is a []Choice: Done when they are at hand, Pending when the source verb
// returns Pending (its work then runs off the loop wherever the caller
// launches it).
func (r *Runtime) choicesLoad(c Context, p Parameter, query string, siblings ...Arguments) Result {
	enum := enumChoices(p)
	source := r.ChoicesSource(p)
	op, ok := r.Operations[source]
	if source == "" || !ok || op.App == nil || op.Confirm != "" {
		choices, err := r.ChoicesFor(c, p, query, siblings...)
		if err != nil && len(choices) == 0 {
			return Failed(err)
		}
		return Done(choices)
	}
	parse := func(value any) ([]Choice, error) {
		more, err := ParseChoices(value)
		if err != nil {
			return nil, fmt.Errorf("choices source %q: %w", source, err)
		}
		return append(slices.Clone(enum), more...), nil
	}
	ctx, err := r.choicesContext(c, op, p, query, siblings...)
	if err != nil {
		return Failed(err)
	}
	res := op.App(ctx)
	if res.err != nil {
		return Failed(fmt.Errorf("choices source %q: %w", source, res.err))
	}
	if res.work == nil {
		choices, err := parse(res.value)
		if err != nil {
			return Failed(err)
		}
		return Done(choices)
	}
	work := res.work
	return Pending(func(ctx context.Context) (any, error) {
		value, err := work(ctx)
		if err != nil {
			return nil, fmt.Errorf("choices source %q: %w", source, err)
		}
		return parse(value)
	}).Label(source)
}

// CheckSourceArgs validates choice dependencies against a complete sibling schema.
// Sources must already be registered. Catalog hosts call it after mounting all verbs.
func (r *Runtime) CheckSourceArgs(params map[string]Parameter) error {
	for _, name := range slices.Sorted(maps.Keys(params)) {
		p := params[name]
		if len(p.SourceArgs) == 0 {
			continue
		}
		source := r.ChoicesSource(p)
		op, ok := r.Operations[source]
		if !ok {
			return fmt.Errorf("%s: unknown choices source %q", name, source)
		}
		for _, arg := range slices.Sorted(maps.Keys(p.SourceArgs)) {
			sibling := p.SourceArgs[arg]
			if _, ok := op.Parameters[arg]; !ok {
				return fmt.Errorf("%s: choices source %s has no parameter %q", name, source, arg)
			}
			if _, ok := params[sibling]; !ok {
				return fmt.Errorf("%s: sourceArgs names no sibling %q", name, sibling)
			}
			if sibling == name {
				return fmt.Errorf("%s: sourceArgs must name another field", name)
			}
		}
	}
	return nil
}

func choicesSourceArgs(op Operation, p Parameter, query string, siblings ...Arguments) Arguments {
	args := choicesQueryArgs(op, query)
	for arg, name := range p.SourceArgs {
		if args == nil {
			args = Arguments{}
		}
		// An explicitly mapped query takes precedence, even when unset.
		delete(args, arg)
		if len(siblings) == 0 {
			continue
		}
		if value, ok := siblings[0][name]; ok && value != nil {
			if text, ok := value.(string); !ok || text != "" {
				args[arg] = value
			}
		}
	}
	return args
}

func (r *Runtime) choicesContext(c Context, op Operation, p Parameter, query string, siblings ...Arguments) (Ctx, error) {
	args := choicesSourceArgs(op, p, query, siblings...)
	if err := r.checkArguments(op, args); err != nil {
		return Ctx{}, err
	}
	q := Request{Noun: op.Noun, Verb: op.Verb, Args: args}
	focus := focusOf(c)
	if !r.Available(q, focus, r.Mode) {
		return Ctx{}, fmt.Errorf("choices source %q is not applicable now", op.Key())
	}
	if owner := r.nounInstance(op.Noun, focus); owner != "" {
		c, focus = r.contextOf(owner), owner
	}
	verdict := r.runChecks(c, q, op)
	if verdict.Refuse != "" {
		return Ctx{}, fmt.Errorf("choices source %q: %s", op.Key(), verdict.Refuse)
	}
	if verdict.Ask != "" {
		return Ctx{}, fmt.Errorf("choices source %q asks for confirmation", op.Key())
	}
	return Ctx{actor: r.Actor(), Runtime: r, Context: c, Args: args, Instance: r.Instances[focus]}, nil
}

// ParameterChoices lists a request parameter's choices, including ValueOf
// and Pairs prop parameters, with sibling arguments passed to its source.
func (r *Runtime) ParameterChoices(c Context, op Operation, name, query string, args Arguments) ([]Choice, error) {
	p, _ := r.choiceParameter(op, name, args)
	if p.Pairs && (strings.Contains(query, "=") || len(p.Enum) == 0) {
		return r.pairChoices(c, op, query, args)
	}
	if p.Type == "boolean" {
		return []Choice{{ID: "true", Label: "true"}, {ID: "false", Label: "false"}}, nil
	}
	return r.ChoicesFor(c, p, query, args)
}

// awaitChoices preserves ExecuteAt's admission, overrides and history while
// waiting for an application's read result instead of returning its job ticket.
func (r *Runtime) awaitChoices(op Operation, c Context, args Arguments) (any, error) {
	if op.App == nil {
		return op.Run(r, c, args)
	}
	inst := r.Instances[focusOf(c)]
	ctx := Ctx{actor: r.Actor(), source: r.history.source, Runtime: r, Context: c, Args: args, Instance: inst, Subjects: r.takeSubjects(inst), Invoked: r.Invoked()}
	res := op.App(ctx)
	if res.err != nil {
		return nil, res.err
	}
	if res.fg != nil {
		return nil, errors.New("a choices source cannot run a foreground program")
	}
	if res.work == nil {
		return res.value, nil
	}
	value, err := res.work(context.Background())
	if res.then != nil {
		res.then(ctx, value, err)
	}
	return value, err
}
