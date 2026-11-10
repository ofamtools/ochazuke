package ochazuke

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
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
	// Cells, when set, are the choice's columns in a picker (a list row's
	// cells: status, topic, age); Label stays what the filter reads.
	Cells []string `json:"cells,omitempty"`
	// Tones colour runs of the row (PickerRow.Tones): Cell indexes Cells,
	// or is 0 for the Label.
	Tones []ToneSpan `json:"tones,omitempty"`
	// Data is the opaque handle the picked row carries (PickerRow.Data); a
	// source whose pick needs more than the id sets it. Noun is the row's
	// noun when it differs from the picker's (PickerRow.Noun) and Props its
	// typed values (PickerRow.Props). None is part of a source's wire form.
	Data  any    `json:"-"`
	Noun  string `json:"-"`
	Props Thing  `json:"-"`
	RowFlags
}

// RowFlags are the picker row flags a choice may carry (PickerRow's of the
// same names). They are an embedded struct so a source that builds rows
// (a path source's folders, a fixed "none" row) names them as one group and
// Choice.Row copies them in one assignment.
type RowFlags struct {
	Opens    bool   `json:"opens,omitempty"`
	NoPick   bool   `json:"noPick,omitempty"`
	Refuse   string `json:"refuse,omitempty"`
	Trailing bool   `json:"trailing,omitempty"`
	Fixed    bool   `json:"fixed,omitempty"`
	// Bare marks an id that is only a handle (a column's index, "" for
	// the default): the row shows its label alone, no dim id.
	Bare bool `json:"bare,omitempty"`
}

// Label orders for a choice shown in the palette, completion and chips
// (setting palette.choice-label).
const (
	ChoiceLabelFirst = "label-first"
	ChoiceIDFirst    = "id-first"
)

// shortID is the part of a choice's id after its last "/" (a thing's id
// without its noun prefix).
func (c Choice) shortID() string {
	short := c.ID
	if i := strings.LastIndex(short, "/"); i >= 0 && i < len(short)-1 {
		short = short[i+1:]
	}
	return short
}

// labelled reports whether the choice has a label of its own.
func (c Choice) labelled() bool { return c.Label != "" && c.Label != c.ID }

// ChoiceEntry is a choice as the palette, completion and chips show it:
// Text, with Tones dimming the id.
type ChoiceEntry struct {
	Text  string
	Tones []ToneSpan
}

// Entry is the choice as a palette completion, completion and chip, the one
// label rule pickers use (Row): the human label first and the short id dim
// at the end (ChoiceLabelFirst, the default), or the id, two spaces, then
// the label (ChoiceIDFirst, palette.choice-label id-first). A choice with
// no label of its own shows its id alone.
func (c Choice) Entry(order string) ChoiceEntry {
	if c.Bare && c.Label != "" {
		return ChoiceEntry{Text: c.Label}
	}
	if !c.labelled() {
		return ChoiceEntry{Text: c.ID}
	}
	if order == ChoiceIDFirst {
		return ChoiceEntry{Text: c.ID + "  " + c.Label}
	}
	return ChoiceEntry{
		Text:  c.Label + "  " + c.shortID(),
		Tones: []ToneSpan{SpanTone(0, len([]rune(c.Label))+2, 0, ToneChoiceID)},
	}
}

// Row is the choice as a picker row, the only place choices become rows
// (a noun reference's field, a registered noun picker, a source's rows): the
// label first and the short id dim at the end (ToneChoiceID), both in the
// text the filter reads, so typing either finds the thing; its Cells, Tones
// and flags carry across. A choice with no label of its own shows its id
// alone. The id is not toned on a row with Cells: it is not drawn there.
func (c Choice) Row() PickerRow {
	row := PickerRow{ID: c.ID, Label: c.ID, Cells: c.Cells, Tones: slices.Clone(c.Tones),
		Opens: c.Opens, NoPick: c.NoPick, Refuse: c.Refuse, Trailing: c.Trailing, Fixed: c.Fixed,
		Data: c.Data, Noun: c.Noun, Props: c.Props}
	if c.Bare {
		if c.Label != "" {
			row.Label = c.Label
		}
		return row
	}
	if !c.labelled() {
		return row
	}
	entry := c.Entry(ChoiceLabelFirst)
	row.Label = entry.Text
	if len(c.Cells) == 0 {
		row.Tones = append(row.Tones, entry.Tones...)
	}
	return row
}

// ParseChoices reads a source operation's result: a slice of strings,
// Choices, PaletteInstances, Operations (the id is noun.verb), ListItems (a
// list verb's rows, rowChoice), maps whose id is "id", else
// "key", else "name" and whose label is "label", else "title" (and word
// "word", Choice.Word), or of any value that encodes as such a map (a list
// verb's own row struct, a Thing); []any of them is the JSON-decoded form.
// A list answer (ListAnswer, a page object) reads as its items.
func ParseChoices(result any) ([]Choice, error) {
	result = AnswerRows(result)
	v := reflect.ValueOf(result)
	if !v.IsValid() || v.Kind() != reflect.Slice || v.Type().Elem().Kind() == reflect.Uint8 {
		return nil, fmt.Errorf("unsupported choices result %T", result)
	}
	out := make([]Choice, 0, v.Len())
	var err error
	for i := range v.Len() {
		choice, e := parseChoice(v.Index(i).Interface())
		err = errors.Join(err, e)
		if choice.Label == "" {
			choice.Label = choice.ID
		}
		out = append(out, choice)
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

// parseChoice reads one item of a choices result (ParseChoices).
func parseChoice(v any) (Choice, error) {
	switch item := v.(type) {
	case string:
		return Choice{ID: item, Label: item}, nil
	case Choice:
		return item, nil
	case PaletteInstance:
		return Choice{ID: item.ID, Label: item.Label, Word: item.Word}, nil
	case Operation:
		return Choice{ID: item.Key.Short()}, nil
	case ListItem:
		return rowChoice(item), nil
	case *ListItem:
		if item != nil {
			return rowChoice(*item), nil
		}
	case map[string]any:
		return mapChoice(item)
	case map[string]string:
		return mapChoice(item)
	case Thing:
		return mapChoice(item)
	}
	if k := reflect.Indirect(reflect.ValueOf(v)).Kind(); k == reflect.Struct || k == reflect.Map {
		// A row of another type (*ListItem, an application's row struct):
		// read through its JSON form.
		var m map[string]any
		if raw, err := json.Marshal(v); err == nil && json.Unmarshal(raw, &m) == nil {
			return mapChoice(m)
		}
	}
	return Choice{}, fmt.Errorf("unsupported choice %T", v)
}

// rowChoice is a list row as a choice: the thing it names is its Data when
// that is a plain string (the row's key itself, as rowKeyReaders read it:
// a Commands row is command:greet holding greet), else its id; its label
// the row's.
func rowChoice(item ListItem) Choice {
	if key, ok := item.Data.(string); ok && key != "" {
		return Choice{ID: key, Label: item.Label, Cells: item.Cells, Tones: item.Tones}
	}
	return Choice{ID: item.ID, Label: item.Label, Cells: item.Cells, Tones: item.Tones}
}

// mapChoice reads a choice map: its id ("id", else "key", else "name"),
// label ("label", else "title") and word.
func mapChoice[V any](m map[string]V) (Choice, error) {
	id, ok := firstString(m, "id", "key", "name")
	if !ok {
		return Choice{}, errors.New("choice map needs a string id, key or name")
	}
	label, _ := firstString(m, "label", "title")
	word, _ := firstString(m, "word")
	c := Choice{ID: id, Label: label, Word: word}
	c.Cells = stringList(m["cells"])
	if raw, ok := any(m["tones"]).([]ToneSpan); ok {
		c.Tones = raw
	} else if v, ok := m["tones"]; ok {
		if b, err := json.Marshal(v); err == nil {
			_ = json.Unmarshal(b, &c.Tones)
		}
	}
	c.Opens = flagValue(m["opens"])
	c.NoPick = flagValue(m["noPick"])
	c.Trailing = flagValue(m["trailing"])
	c.Fixed = flagValue(m["fixed"])
	c.Refuse, _ = any(m["refuse"]).(string)
	return c, nil
}

// stringList reads a JSON array of strings (a row's cells).
func stringList(v any) []string {
	switch list := v.(type) {
	case []string:
		return slices.Clone(list)
	case []any:
		out := make([]string, len(list))
		for i, item := range list {
			out[i], _ = item.(string)
		}
		return out
	}
	return nil
}

// flagValue reads a boolean row flag.
func flagValue(v any) bool { b, _ := v.(bool); return b }

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
	op, ok := r.standardOperation(p.Noun, "list")
	key := op.Key.Short()
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
		list, ok := inst.Component.(*Collection)
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
		label := p.EnumLabels[id]
		if label == "" {
			label = id
		}
		out = append(out, Choice{ID: id, Label: label})
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
	if p.Noun == AnyNoun && p.Source == "" {
		return true
	}
	op, ok := r.Operations.Find(Key(r.ChoicesSource(p)))
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
	if source == "" && p.Noun == AnyNoun {
		more, err := r.anyRefChoices(c, query)
		return append(out, more...), err
	}
	if source == "" {
		if p.Noun != "" {
			out = append(out, r.nounInstanceChoices(p.Noun)...)
		}
		return out, nil
	}
	op, ok := r.Operations.Find(Key(source))
	if !ok {
		return out, fmt.Errorf("unknown choices source %q", source)
	}
	if op.Confirm != "" {
		return out, fmt.Errorf("choices source %q asks for confirmation", source)
	}
	result, err := r.executeAt(c, Request{Verb: op.Name(), Noun: op.Noun(), Args: choicesSourceArgs(op, p, query, siblings...)}, r.awaitChoices)
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
	op, ok := r.Operations.Find(Key(source))
	if source == "" || !ok || op.AppHandler == nil || op.Confirm != "" {
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
	res := op.AppHandler(ctx)
	if res.err != nil || res.work == nil {
		done(parse(res.value, res.err))
		return false
	}
	res = res.also(func(c Ctx, value any, err error) {
		done(parse(value, err))
	})
	if _, err := r.Launch(owner, op.Key.Short(), res, ctx); err != nil {
		done(enum, fmt.Errorf("choices source %q: %w", source, err))
		return false
	}
	return true
}

// startChoices is ChoicesFor that never waits: the source runs through
// ExecuteAt (admission, overrides, history) as ChoicesFor's does, and when
// its verb returns Pending the work launches as a job of owner in slot
// (Result.Replacing: one in flight, a newer one replacing it) instead of
// being awaited. It then answers the parameter's Enum and true, and done
// gets the choices (or the error) when the job lands; never for a
// cancelled one. Otherwise it answers what ChoicesFor would, and false.
func (r *Runtime) startChoices(c Context, p Parameter, query, owner, slot string, done func([]Choice, error), siblings ...Arguments) ([]Choice, bool, error) {
	source := r.ChoicesSource(p)
	op, ok := r.Operations.Find(Key(source))
	if source == "" || !ok || op.Confirm != "" {
		choices, err := r.ChoicesFor(c, p, query, siblings...)
		return choices, false, err
	}
	enum := enumChoices(p)
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
	pending := false
	launch := func(op Operation, c Context, args Arguments) (any, error) {
		if op.AppHandler == nil {
			return op.Run(r, c, args)
		}
		res, ctx, err := r.runChoicesApp(op, c, args)
		if err != nil || res.work == nil {
			return res.value, err
		}
		res = res.Replacing(slot).also(func(c Ctx, value any, err error) {
			done(parse(value, err))
		})
		if _, err := r.Launch(owner, op.Key.Short(), res, ctx); err != nil {
			return nil, err
		}
		pending = true
		return nil, nil
	}
	value, err := r.executeAt(c, Request{Verb: op.Name(), Noun: op.Noun(), Args: choicesSourceArgs(op, p, query, siblings...)}, launch)
	if pending && err == nil {
		return enum, true, nil
	}
	choices, err := parse(value, err)
	return choices, false, err
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
	op, ok := r.Operations.Find(Key(source))
	if source == "" || !ok || op.AppHandler == nil || op.Confirm != "" {
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
	res := op.AppHandler(ctx)
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
	return res.bare().Map(func(value any, err error) (any, error) {
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
		if len(p.SourceArgs) == 0 && len(p.SourceFixed) == 0 {
			continue
		}
		source := r.ChoicesSource(p)
		if source == "" && len(p.SourceArgs) == 0 {
			continue // fixed arguments of a parameter with no source (a noun with no list) are moot
		}
		op, ok := r.Operations.Find(Key(source))
		if !ok {
			return fmt.Errorf("%s: unknown choices source %q", name, source)
		}
		for _, arg := range slices.Sorted(maps.Keys(p.SourceFixed)) {
			if _, ok := op.Parameters[arg]; !ok {
				return fmt.Errorf("%s: choices source %s has no parameter %q", name, source, arg)
			}
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
	for arg, value := range p.SourceFixed {
		if args == nil {
			args = Arguments{}
		}
		args[arg] = value
	}
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
	q := Request{Noun: op.Noun(), Verb: op.Name(), Args: args}
	focus := focusOf(c)
	if !r.Available(q, focus, r.Mode) {
		return Ctx{}, fmt.Errorf("choices source %q is not applicable now", op.Key.Short())
	}
	if owner := r.nounInstance(op.Noun(), focus); owner != "" {
		c, focus = r.contextOf(owner), owner
	}
	verdict := r.runChecks(c, q, op)
	if verdict.Refuse != "" {
		return Ctx{}, fmt.Errorf("choices source %q: %s", op.Key.Short(), verdict.Refuse)
	}
	if verdict.Ask != "" {
		return Ctx{}, fmt.Errorf("choices source %q asks for confirmation", op.Key.Short())
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

// runChoicesApp runs an application choices source's verb in c, as
// ExecuteAt's runner does: its result, and the Ctx it ran in for a job.
func (r *Runtime) runChoicesApp(op Operation, c Context, args Arguments) (Result, Ctx, error) {
	inst := r.Instances[focusOf(c)]
	ctx := Ctx{actor: r.Actor(), source: r.history.source, Runtime: r, Context: c, Args: args, Instance: inst, Subjects: r.takeSubjects(c, inst), Invoked: r.Invoked()}
	res := op.AppHandler(ctx)
	if res.err != nil {
		return Result{}, ctx, res.err
	}
	if res.fg != nil {
		return Result{}, ctx, errors.New("a choices source cannot run a foreground program")
	}
	return res, ctx, nil
}

// awaitChoices preserves ExecuteAt's admission, overrides and history while
// waiting for an application's read result instead of returning its job ticket.
func (r *Runtime) awaitChoices(op Operation, c Context, args Arguments) (any, error) {
	if op.AppHandler == nil {
		return op.Run(r, c, args)
	}
	res, ctx, err := r.runChoicesApp(op, c, args)
	if err != nil || res.work == nil {
		return res.value, err
	}
	value, err := res.Await(context.Background())
	if res.then != nil {
		res.then(ctx, value, err)
	}
	return value, err
}
