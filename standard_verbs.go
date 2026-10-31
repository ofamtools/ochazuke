package ochazuke

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// The standard verbs: one table (standardVerbs), one row per verb, like
// the setting verbs (setting_verbs.go). A noun that declares props and a
// store (props.go) gets NOUN.VERB for every row that fits its store's kind
// and whose handler the store has (Needs); the result is marked
// Operation.Standard with the row's name. Which nouns support which verb is
// a query over this table, and the catalog publishes it (standard).
//
// The application wins: a verb it registers under a standard verb's name
// replaces the generated one, before or after (Register; the generated
// verb is then never added), keeping the aliases that name it.
//
// The subject of a verb that takes one is the parameter named after the
// noun's key prop (Parameter.Focus): given, it is the key; omitted, it is
// the instance the request acts for once ExecuteAt has applied the target
// and the focus path (Ctx.Instance, or the nearest instance of the noun
// above it), read as the key prop in its Data, else its id. So a key
// argument wins over a target, and a target over the focus.

// standardVerb is one row of the table: the verb, the store kinds it fits,
// what it needs of the noun (its store's handler, a writable prop),
// whether it takes a subject, whether its answer is rows of the noun,
// whether a visual selection runs it once over every row (Many), its
// history kind, whether . repeats it (only ever a simple-value write,
// simpleWrite), the confirm prompt it asks by default, its own parameters
// and its handler. Writes names the props a request writes, for a verb
// that writes values (create, set): the simple-value test reads them.
type standardVerb struct {
	Verb        string
	Description string
	Kinds       []string
	Needs       func(n *Noun) bool
	Subject     bool
	// NoFocus makes the subject optional and never filled from the focus
	// (watch: omitted, it watches every thing).
	NoFocus bool
	Rows    bool
	// Record marks a verb whose answer is one thing's props: the palette
	// shows it as prop and value rows (palette.standard-verbs record).
	Record      bool
	Many        bool
	Repeatable  bool
	HistoryKind string
	Destructive bool
	Confirm     string
	// FocusSetting names the boolean setting that turns off the row's
	// focus fills of values (a parameter with Focus that is not the
	// subject: create's FromFocus props); "" for none.
	FocusSetting string
	Positional   func(n *Noun) []string
	Params       func(n *Noun) map[string]Parameter
	Writes       func(n *Noun, a Arguments) ([]string, error)
	Run          func(r *Runtime, n *Noun, c Ctx) (any, error)
	// ConfirmFor is the row's Operation.ConfirmFor on noun n, set when the
	// operation asks at all (Confirm, or the store's prompt, not "").
	ConfirmFor func(r *Runtime, n *Noun, c Context, q Request, prompt string) string
}

// DefaultDeleteConfirm is the prompt a standard delete asks before it runs
// unless the store names its own (Store.Confirm["delete"]; "" asks
// nothing): a delete is never silent.
const DefaultDeleteConfirm = "Delete {{noun}} {{label}}?"

// DefaultArchiveConfirm is what a standard delete asks instead when it
// will only archive the thing (delete.archive): the confirm says what
// happens. delete.archive-confirm off archives without asking.
const DefaultArchiveConfirm = "Archive {{noun}} {{label}}? (delete it again to delete it for good)"

// standardVerbs are the standard verbs this phase implements, in the order
// help lists them. patch is not a row: it is an alias of set (owner
// decision 1), registered with it.
var standardVerbs = []standardVerb{
	{Verb: "get", Description: "Read a thing's props: every prop as an object, one prop's value (prop), or some props (props)",
		Kinds: []string{Collection, Singleton, Instances}, Needs: func(n *Noun) bool { return n.Store.Get != nil },
		Subject: true, Record: true, HistoryKind: HistoryFetch, Positional: func(*Noun) []string { return []string{"prop"} }, Params: getParams, Run: runGet},
	{Verb: "list", Description: "List the things, each its props with id and label; where keeps the things whose props equal the values given; filter applies a named saved prop filter",
		Kinds: []string{Collection, Instances}, Needs: func(n *Noun) bool { return n.Store.List != nil },
		Rows: true, HistoryKind: HistoryFetch, Params: listParams, Positional: listPositional, Run: runList},
	{Verb: "watch", Description: "Stream what happens to the things (one, or those where matches): a JSON line per thing added, modified or deleted, from since when given; the CLI streams until interrupted",
		Kinds: []string{Collection, Singleton, Instances}, Needs: func(n *Noun) bool { return watchFits(*n.Store) },
		Subject: true, NoFocus: true, HistoryKind: HistoryFetch, Params: watchParams, Run: runWatch},
	{Verb: "create", Description: "Create a thing from the props given (each omitted one its default); answers the new thing",
		Kinds: []string{Collection}, Needs: func(n *Noun) bool { return n.Store.Create != nil && len(createProps(n)) > 0 },
		HistoryKind: HistoryChange, FocusSetting: CreateFromFocusSetting, Positional: createPositional, Params: createParams, Writes: createWrites, Run: runCreate},
	{Verb: "set", Description: "Write a thing's props: one prop (prop and value, or toggle on a boolean), or several (changes, or prop=value words); answers the thing after the write",
		Kinds: []string{Collection, Singleton, Instances}, Needs: func(n *Noun) bool { return n.Store.Patch != nil && len(writableProps(n)) > 0 },
		Subject: true, Many: true, Repeatable: true, HistoryKind: HistoryChange, Positional: func(*Noun) []string { return []string{"prop", "value", "pairs"} },
		Params: setParams, Writes: setWrites, Run: runSet},
	{Verb: "delete", Description: "Delete a thing (asks first)",
		Kinds: []string{Collection}, Needs: func(n *Noun) bool { return n.Store.Delete != nil },
		Subject: true, Many: true, Destructive: true, HistoryKind: HistoryChange, Confirm: DefaultDeleteConfirm, ConfirmFor: deleteConfirm, Run: runDelete},
}

// reservedStandardVerbs are the names of every standard verb, those later
// phases add included: no prop takes one.
var reservedStandardVerbs = []string{"get", "list", "create", "set", "patch", "delete", "watch", "rename", "move", "duplicate", "archive", "restore"}

// standardParamNames are the parameters the standard verbs take beside the
// subject: no key prop and no store list parameter takes one.
var standardParamNames = []string{"prop", "props", "where", "filter", "limit", "since", "value", "toggle", "changes", "pairs", "to"}

// StandardVerbs lists the verbs of the standard verb table, in order.
func StandardVerbs() []string {
	out := make([]string, len(standardVerbs))
	for i, v := range standardVerbs {
		out[i] = v.Verb
	}
	return out
}

// standardRow is the table's row of that verb, nil when none is.
func standardRow(verb string) *standardVerb {
	for i := range standardVerbs {
		if standardVerbs[i].Verb == verb {
			return &standardVerbs[i]
		}
	}
	return nil
}

// fits reports whether a row applies to a noun with a store.
func (v standardVerb) fits(n *Noun) bool {
	return slices.Contains(v.Kinds, n.Store.Kind) && v.Needs(n)
}

// ensureStandardVerbs registers, refreshes or drops the generated verbs of
// a noun with a store: a row that fits is registered (or re-registered with
// the noun's current props); one that no longer fits is dropped; a verb the
// application registered under the row's name is left alone. set brings
// its alias patch, unless the noun has a verb of that name.
func (r *Runtime) ensureStandardVerbs(n *Noun) {
	for _, v := range standardVerbs {
		key := n.Name + "." + v.Verb
		existing, ok := r.Operations[key]
		if ok && existing.Standard == "" {
			continue // the application's verb wins
		}
		if ok {
			delete(r.Operations, key)
		}
		if !v.fits(n) {
			n.Verbs = slices.DeleteFunc(n.Verbs, func(verb string) bool { return verb == v.Verb })
			if v.Verb == "set" {
				r.dropStandardAlias(n.Name + ".patch")
			}
			continue
		}
		r.Register(v.operation(r, n))
		if g := n.Store.When[v.Verb]; g != "" {
			r.SetAvailability(key, g)
		}
		if !slices.Contains(n.Verbs, v.Verb) {
			n.Verbs = append(n.Verbs, v.Verb)
		}
		if v.Verb == "set" {
			if _, taken := r.Operations[n.Name+".patch"]; !taken {
				if err := r.AddActionAlias(ActionAlias{Alias: n.Name + ".patch", Action: key, Source: AliasStandard}); err != nil {
					panic(err.Error())
				}
			}
		}
	}
}

// dropStandardAlias removes a generated alias of that name, if there is one.
func (r *Runtime) dropStandardAlias(name string) {
	if a, ok := r.actionNames.aliases[name]; ok && a.Source == AliasStandard {
		r.removeAlias(name)
	}
}

// operation is the row's operation on noun n.
func (v standardVerb) operation(r *Runtime, n *Noun) Operation {
	name := n.Name
	params := map[string]Parameter{}
	if v.Params != nil {
		params = v.Params(n)
	}
	op := Operation{Verb: v.Verb, Noun: name, Description: v.Description, Parameters: params, Result: "JSON value", HistoryKind: v.HistoryKind, Standard: v.Verb, Many: v.Many, Destructive: v.Destructive}
	if key := n.keyProp(); v.Subject && key != nil {
		p := subjectParameter(r, n, *key)
		if v.NoFocus {
			p.Focus = false
			p.Description = fmt.Sprintf("The %s's %s: this one alone (default: every %s)", r.DisplayNoun(n.Name), key.Name, r.DisplayNoun(n.Name))
		}
		params[key.Name] = p
		op.Positional = []string{key.Name}
	}
	if v.Positional != nil {
		for _, p := range v.Positional(n) {
			if _, ok := params[p]; ok {
				op.Positional = append(op.Positional, p)
			}
		}
	}
	op.Confirm = v.Confirm
	if prompt, ok := n.Store.Confirm[v.Verb]; ok {
		op.Confirm = prompt
	}
	if confirmFor, verb := v.ConfirmFor, v.Verb; op.Confirm != "" && (confirmFor != nil || n.Store.ConfirmFor != nil) {
		op.ConfirmFor = func(r *Runtime, c Context, q Request, prompt string) string {
			noun := r.Nouns[name]
			if noun == nil || noun.Store == nil {
				return prompt
			}
			if confirmFor != nil {
				if prompt = confirmFor(r, noun, c, q, prompt); prompt == "" {
					return ""
				}
			}
			if hook := noun.Store.ConfirmFor; hook != nil {
				ctx, keys := r.confirmKeys(noun, c, q)
				return hook(ctx, verb, keys, prompt)
			}
			return prompt
		}
	}
	if v.Rows {
		op.Rows = name
		op.Result = "rows"
	}
	run, many, verb := v.Run, v.Many, v.Verb
	one := func(c Ctx) Result {
		noun := c.Runtime.Nouns[name]
		if noun == nil || noun.Store == nil {
			return Failed(fmt.Errorf("%s has no store", name))
		}
		value, err := c.Runtime.runRecording(verb, noun, c, run) // standard_undo.go
		if err != nil {
			return Failed(err)
		}
		return Done(value)
	}
	op.App = func(c Ctx) Result {
		if many && len(c.Subjects) > 1 {
			// A visual selection: one write per row, one undo group
			// (Ctx.Each); each row's subject is that row, never a key
			// argument meant for one thing.
			if key := c.Runtime.Nouns[name].keyProp(); key != nil {
				c.Args = cloneArgs(c.Args)
				delete(c.Args, key.Name)
			}
			return c.Each(one)
		}
		return one(c)
	}
	return op
}

// confirmKeys are the context a store's ConfirmFor reads and the keys of
// the things the request acts on: each of a visual selection's subjects,
// else the one the key argument or the focus names (none when no key
// resolves).
func (r *Runtime) confirmKeys(n *Noun, c Context, q Request) (Ctx, []string) {
	inst := r.Instances[focusOf(c)]
	ctx := Ctx{actor: r.Actor(), Runtime: r, Context: c, Args: q.Args, Instance: inst}
	insts := []*Instance{inst}
	if len(q.Subjects) > 1 {
		insts = insts[:0]
		for _, id := range q.Subjects {
			insts = append(insts, r.Instances[id])
		}
	}
	var keys []string
	for _, in := range insts {
		one := ctx
		one.Instance = in
		if len(q.Subjects) > 1 {
			if kp := n.keyProp(); kp != nil {
				one.Args = cloneArgs(q.Args)
				delete(one.Args, kp.Name)
			}
		}
		if key, err := r.subjectKey(n, one); err == nil && key != "" {
			keys = append(keys, key)
		}
	}
	return ctx, keys
}

// subjectParameter is the key parameter of a verb that takes a subject:
// named after the key prop, its choices the noun's things, filled from the
// focus when omitted (Focus).
func subjectParameter(r *Runtime, n *Noun, key PropSpec) Parameter {
	typ := "string"
	if key.Type == SettingInteger {
		typ = "integer"
	}
	return Parameter{Type: typ, Noun: n.Name, Focus: true, Free: true,
		Description: fmt.Sprintf("The %s's %s (default: the %s the request acts for)", r.DisplayNoun(n.Name), key.Name, r.DisplayNoun(n.Name))}
}

// propNames are the noun's prop names in order.
func propNames(n *Noun) []string {
	out := make([]string, len(n.Props))
	for i, p := range n.Props {
		out[i] = p.Name
	}
	return out
}

func getParams(n *Noun) map[string]Parameter {
	return map[string]Parameter{
		"prop":  {Type: "string", Enum: propNames(n), Description: "One prop: answer its value alone"},
		"props": {Type: "string", Description: "The props to answer, comma-separated (default: every prop but secrets)"},
	}
}

func listParams(n *Noun) map[string]Parameter {
	params := map[string]Parameter{
		"filter": {Type: "string", Description: "Apply a named saved prop filter for this noun, ANDed with where"},
		"where":  {Type: "string", Free: true, Pairs: true, Description: "prop=value pairs, comma- or space-separated: keep the things whose props equal every value"},
		"props":  {Type: "string", Description: "The props each row holds besides id and label, comma-separated (default: every prop but secrets)"},
		"limit":  {Type: "integer", Description: "At most this many rows (0: every row)"},
	}
	for name, p := range n.Store.ListParams {
		params[name] = p
	}
	return params
}

// Keep existing bare list words in their previous order when adding the
// optional filter flag. In particular, :setting list 0 value owner=app
// continues to mean limit, props, where. Store parameters keep their order too.
func listPositional(n *Noun) []string {
	params := listParams(n)
	delete(params, "filter")
	return (Operation{Parameters: params}).ParameterOrder()
}

// subjectKey is the key a request's subject has: the key argument when
// given, else the instance the request acts for (Ctx.Instance, or the
// nearest instance of the noun above it; for an instances noun the
// nearest live thing, liveSubject): an instances noun's instance id; else
// the key prop in its Data, the Data itself when it is a plain string
// (a row whose Data is its thing's key, as the Settings page's rows are),
// else its id. A singleton's is "".
func (r *Runtime) subjectKey(n *Noun, c Ctx) (string, error) {
	key := n.keyProp()
	if key == nil {
		return "", nil
	}
	if v, ok := c.Args[key.Name]; ok && !emptyProp(v) {
		return fmt.Sprint(v), nil
	}
	inst := c.Instance
	if inst != nil && n.Store.Kind == Instances {
		inst = r.Instances[r.liveSubject(n, inst.ID)] // its things are live instances
		if inst != nil {
			return inst.ID, nil
		}
	} else if inst != nil && inst.Noun != n.Name {
		inst = r.Instances[r.nounInstance(n.Name, inst.ID)]
	}
	if inst == nil {
		return "", &ArgumentError{fmt.Errorf("%s is required: no %s is focused", key.Name, r.DisplayNoun(n.Name))}
	}
	if v := dataField(inst.Data, key.Name); !emptyProp(v) {
		return fmt.Sprint(v), nil
	}
	if s, ok := inst.Data.(string); ok && s != "" {
		return s, nil
	}
	return inst.ID, nil
}

// listWords splits a list argument at commas and spaces.
func listWords(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' })
}

// pickedProps reads a props argument: the names it gives, each a prop of
// n; nil when it is empty.
func pickedProps(n *Noun, a Arguments) ([]string, error) {
	names := listWords(StringArg(a, "props", ""))
	for _, name := range names {
		if n.prop(name) == nil {
			return nil, &ArgumentError{fmt.Errorf("%s has no prop %q", n.Name, name)}
		}
	}
	return names, nil
}

// thingRecord is a thing's props as get and list answer them: the props named
// (every prop but the secret ones when none is), in declaration order,
// each in its file form.
func thingRecord(n *Noun, t Thing, names []string) map[string]any {
	out := map[string]any{}
	for _, p := range n.Props {
		if len(names) > 0 && !slices.Contains(names, p.Name) || len(names) == 0 && p.Secret {
			continue
		}
		if v, ok := t[p.Name]; ok {
			out[p.Name] = fileForm(v)
		}
	}
	return out
}

func runGet(r *Runtime, n *Noun, c Ctx) (any, error) {
	prop := StringArg(c.Args, "prop", "")
	names, err := pickedProps(n, c.Args)
	switch {
	case err != nil:
		return nil, err
	case prop != "" && len(names) > 0:
		return nil, &ArgumentError{errors.New("get takes prop or props, not both")}
	case prop != "" && n.prop(prop) == nil:
		return nil, &ArgumentError{fmt.Errorf("%s has no prop %q", n.Name, prop)}
	}
	// Only the props offered where the request acts (PropSpec.When).
	if hidden := r.unshownProp(n, c, append([]string{prop}, names...)); hidden != "" {
		return nil, fmt.Errorf("%s.%s is not available here", r.DisplayNoun(n.Name), hidden)
	}
	if len(names) == 0 && prop == "" {
		focus := focusOf(c.Context)
		for _, p := range n.Props {
			if !p.Secret && r.propShown(p, focus, r.Mode, c.Args) {
				names = append(names, p.Name)
			}
		}
		if len(names) == 0 {
			names = []string{""} // no prop is offered here: an empty record
		}
	}
	id, err := r.subjectKey(n, c)
	if err != nil {
		return nil, err
	}
	t, err := n.Store.Get(c, id)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, fmt.Errorf("no %s %q", r.DisplayNoun(n.Name), id)
	}
	if prop != "" {
		return fileForm(t[prop]), nil
	}
	return thingRecord(n, t, names), nil
}

// parseWhere reads a where argument: prop=value pairs, each value typed by
// its prop.
func parseWhere(n *Noun, text string) (map[string]any, error) {
	out := map[string]any{}
	for _, pair := range listWords(text) {
		name, value, ok := strings.Cut(pair, "=")
		if !ok {
			return nil, &ArgumentError{fmt.Errorf("where takes prop=value pairs, not %q", pair)}
		}
		p := n.prop(name)
		if p == nil {
			return nil, &ArgumentError{fmt.Errorf("%s has no prop %q", n.Name, name)}
		}
		v, err := propValue(*p, value)
		if err != nil {
			return nil, &ArgumentError{fmt.Errorf("where %s %w", name, err)}
		}
		out[name] = v
	}
	return out, nil
}

func runList(r *Runtime, n *Noun, c Ctx) (any, error) {
	names, err := pickedProps(n, c.Args)
	if err != nil {
		return nil, err
	}
	where, err := parseWhere(n, StringArg(c.Args, "where", ""))
	if err != nil {
		return nil, err
	}
	filters, err := r.namedPropFilter(n, StringArg(c.Args, "filter", ""))
	if err != nil {
		return nil, err
	}
	// A store must not limit before the framework applies saved conditions.
	limit := max(0, IntArg(c.Args, "limit", 0))
	q := ListQuery{Where: where, Limit: limit}
	if len(filters) > 0 {
		q.Limit = 0
	}
	for name := range n.Store.ListParams {
		if v, ok := c.Args[name]; ok {
			if q.Args == nil {
				q.Args = Arguments{}
			}
			q.Args[name] = v
		}
	}
	things, err := n.Store.List(c, q)
	if err != nil {
		return nil, err
	}
	key := n.keyProp()
	rows := []map[string]any{}
	for _, t := range things {
		if !thingMatches(n, t, where) || !propsMatch(n, t, filters) {
			continue
		}
		row := thingRecord(n, t, names)
		if key != nil {
			row["id"] = fmt.Sprint(t[key.Name])
		}
		if label := n.displayLabel(func(name string) any { return t[name] }); label != "" {
			row["label"] = label
		}
		rows = append(rows, row)
		if limit > 0 && len(rows) == limit {
			break
		}
	}
	return rows, nil
}

// thingMatches reports whether a thing's props equal every where value.
func thingMatches(n *Noun, t Thing, where map[string]any) bool {
	for name, want := range where {
		p := n.prop(name)
		if p == nil || !propMatches(*p, t[name], PropFilter{Prop: name, Op: "=", Value: want}) {
			return false
		}
	}
	return true
}

// labelFirst is create's first word: the label prop (task create "Buy
// milk"); the other props are named.
func labelFirst(n *Noun) []string {
	if p := n.roleProp(RoleLabel); p != nil {
		return []string{p.Name}
	}
	return nil
}

// createProps are the props create writes: every one set may write, and
// the create-only ones; never a read-only prop or the key (the store
// gives the key).
func createProps(n *Noun) []PropSpec {
	var out []PropSpec
	for _, p := range n.Props {
		if !p.ReadOnly && !p.Key {
			out = append(out, p)
		}
	}
	return out
}

// writableProps are the props set may write.
func writableProps(n *Noun) []PropSpec {
	var out []PropSpec
	for _, p := range n.Props {
		if p.writable() {
			out = append(out, p)
		}
	}
	return out
}

// propParameter is a prop as a verb parameter: its own Parameter, typed so
// a word from the CLI or the palette reaches the handler (propValue types
// it): strings, integers and booleans as they are, every other type any.
func propParameter(p PropSpec) Parameter {
	out := p.Parameter
	out.Enum = slices.Clone(p.Enum)
	switch p.Type {
	case SettingString, "", SettingInteger, SettingBoolean:
		if out.Type == "" {
			out.Type = SettingString
		}
	default:
		out.Type = AnyParameterType
	}
	return out
}

func createParams(n *Noun) map[string]Parameter {
	params := map[string]Parameter{}
	for _, p := range createProps(n) {
		param := propParameter(p)
		param.Focus = p.FromFocus // create fills it from the focus (createFill)
		params[p.Name] = param
	}
	params["pairs"] = Parameter{Type: "string", Rest: true, Pairs: true, Description: "prop=value words for the props (plan 2.6: NOUN create [p=v ...]), after the label"}
	return params
}

// createArgs reads a create request's props as create writes them: the
// props given by name, a prop=value word given in a prop's own slot (a
// bare word on the CLI or the palette: the label's, a focus fill's)
// whose name is a prop create writes, and the pairs words. A word whose
// left side names no such prop stays the value of its slot (a label
// "a=b"); a prop given twice is refused. pairs is consumed.
func createArgs(n *Noun, a Arguments) (Arguments, error) {
	pairs := StringArg(a, "pairs", "")
	_, hasPairs := a["pairs"]
	out := Arguments{}
	moved := map[string]bool{}
	for k, v := range a {
		if k != "pairs" {
			out[k] = v
		}
	}
	writes := func(name string) bool {
		return slices.ContainsFunc(createProps(n), func(p PropSpec) bool { return p.Name == name })
	}
	put := func(name string, v any) error {
		if _, given := out[name]; given || moved[name] {
			return &ArgumentError{fmt.Errorf("%s is written twice", name)}
		}
		out[name], moved[name] = v, true
		return nil
	}
	for _, p := range createProps(n) {
		word, ok := a[p.Name].(string)
		if !ok {
			continue
		}
		name, v, ok := strings.Cut(word, "=")
		if !ok || !writes(name) {
			continue
		}
		delete(out, p.Name)
		if err := put(name, unquoteWord(v)); err != nil {
			return nil, err
		}
	}
	for _, w := range splitPairs(pairs) {
		name, v, ok := strings.Cut(w, "=")
		switch {
		case !ok:
			return nil, &ArgumentError{fmt.Errorf("%q is not prop=value", w)}
		case !writes(name):
			return nil, &ArgumentError{fmt.Errorf("%s has no prop %q that create writes", n.Name, name)}
		}
		if err := put(name, unquoteWord(v)); err != nil {
			return nil, err
		}
	}
	if !hasPairs && len(moved) == 0 {
		return a, nil
	}
	return out, nil
}

func setParams(n *Noun) map[string]Parameter {
	var names []string
	for _, p := range writableProps(n) {
		names = append(names, p.Name)
	}
	return map[string]Parameter{
		"prop":    {Type: "string", Enum: names, Free: true, Pairs: true, Description: "The prop to write (with value or toggle); a prop=value word starts the several-prop form"},
		"value":   {Type: AnyParameterType, ValueOf: "prop", Description: "The prop's new value, typed by the prop (a word such as 3, true or 400ms, a JSON array or object)"},
		"toggle":  {Type: "boolean", Excludes: []string{"value"}, Description: "Flip the boolean prop instead of giving a value"},
		"changes": {Type: "object", Description: "Several props at once: an object of prop to value"},
		"pairs":   {Type: "string", Rest: true, Description: "More prop=value words after prop and value (the several-prop form)"},
	}
}

// createWrites are the props a create request gives.
func createWrites(n *Noun, a Arguments) ([]string, error) {
	a, err := createArgs(n, a)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, p := range createProps(n) {
		if _, ok := a[p.Name]; ok {
			out = append(out, p.Name)
		}
	}
	return out, nil
}

// setWrites are the props a set request writes, in its order.
func setWrites(n *Noun, a Arguments) ([]string, error) {
	_, order, err := setChanges(n, a)
	return order, err
}

// setChanges reads a set request's changes: prop and value (or toggle,
// answered later against the current value: nil here), the changes
// object, or prop=value words (prop, value and pairs: the several-prop
// form, quoted values unquoted). The one-prop and several-prop forms do
// not mix. Each prop must be one set may write; values are typed by it.
func setChanges(n *Noun, a Arguments) (map[string]any, []string, error) {
	prop, _ := a["prop"].(string)
	value, hasValue := a["value"]
	pairs := StringArg(a, "pairs", "")
	toggle, _ := a["toggle"].(bool)
	changes := map[string]any{}
	var order []string
	add := func(name string, v any) error {
		p := n.prop(name)
		switch {
		case p == nil:
			return &ArgumentError{fmt.Errorf("%s has no prop %q; only declared props are writable", n.Name, name)}
		case !p.writable():
			return &ArgumentError{fmt.Errorf("%s.%s is read-only", n.Name, name)}
		}
		if _, dup := changes[name]; dup {
			return &ArgumentError{fmt.Errorf("%s is written twice", name)}
		}
		if v != nil {
			typed, err := propValue(*p, v)
			if err != nil {
				return &ArgumentError{fmt.Errorf("%s %w", name, err)}
			}
			v = typed
		}
		changes[name] = v
		order = append(order, name)
		return nil
	}
	obj, hasChanges := a["changes"]
	switch {
	case hasChanges:
		if prop != "" || hasValue || toggle || pairs != "" {
			return nil, nil, &ArgumentError{errors.New("set takes changes or prop with a value, not both")}
		}
		m, err := propValue(PropSpec{Name: "changes", Parameter: Parameter{Type: PropObject}}, obj)
		if err != nil {
			return nil, nil, &ArgumentError{fmt.Errorf("changes %w", err)}
		}
		names := slices.Sorted(maps.Keys(m.(map[string]any)))
		// Declaration order, so a write reads the same every time.
		slices.SortStableFunc(names, func(x, y string) int { return propIndex(n, x) - propIndex(n, y) })
		for _, name := range names {
			if err := add(name, m.(map[string]any)[name]); err != nil {
				return nil, nil, err
			}
		}
	case strings.Contains(prop, "="):
		words := []string{prop}
		if hasValue {
			words = append(words, fmt.Sprint(value))
		}
		words = append(words, splitPairs(pairs)...)
		if toggle {
			return nil, nil, &ArgumentError{errors.New("toggle goes with one prop, not prop=value words")}
		}
		for _, w := range words {
			name, v, ok := strings.Cut(w, "=")
			if !ok {
				return nil, nil, &ArgumentError{fmt.Errorf("%q is not prop=value: the one-prop and several-prop forms do not mix", w)}
			}
			if err := add(name, unquoteWord(v)); err != nil {
				return nil, nil, err
			}
		}
	case prop != "":
		switch {
		case pairs != "":
			return nil, nil, &ArgumentError{fmt.Errorf("set %s takes one value, not %q too", prop, pairs)}
		case toggle && hasValue:
			return nil, nil, &ArgumentError{errors.New("set takes a value or toggle, not both")}
		case toggle:
			if p := n.prop(prop); p != nil && p.Type != SettingBoolean {
				return nil, nil, &ArgumentError{fmt.Errorf("%s is %s: only a boolean prop toggles", prop, p.Type)}
			}
			return changes, order, add(prop, nil)
		case !hasValue:
			return nil, nil, &ArgumentError{fmt.Errorf("value is required: set %s VALUE (or toggle a boolean)", prop)}
		}
		if s, ok := value.(string); ok {
			value = unquoteWord(s)
		}
		if err := add(prop, value); err != nil {
			return nil, nil, err
		}
	default:
		return nil, nil, &ArgumentError{errors.New("set needs a prop and a value, prop=value words or changes")}
	}
	return changes, order, nil
}

// propIndex is a prop's place in its noun's declaration order.
func propIndex(n *Noun, name string) int {
	return slices.IndexFunc(n.Props, func(p PropSpec) bool { return p.Name == name })
}

// splitPairs splits prop=value words at spaces outside double quotes.
func splitPairs(text string) []string {
	var out []string
	var b strings.Builder
	quoted, escaped := false, false
	for _, c := range text {
		switch {
		case escaped:
			escaped = false
		case c == '\\' && quoted:
			escaped = true
		case c == '"':
			quoted = !quoted
		case (c == ' ' || c == '\t') && !quoted:
			if b.Len() > 0 {
				out = append(out, b.String())
				b.Reset()
			}
			continue
		}
		b.WriteRune(c)
	}
	if b.Len() > 0 {
		out = append(out, b.String())
	}
	return out
}

// unquoteWord is a value word without its double quotes, when it is one
// quoted string ("buy milk"); any other word as it is.
func unquoteWord(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		if u, err := strconv.Unquote(s); err == nil {
			return u
		}
	}
	return s
}

// readBack is the thing a write answers: the store's answer, else the
// thing read again (Get), else the props written.
func readBack(c Ctx, n *Noun, key string, t Thing, written map[string]any) (map[string]any, error) {
	if t == nil && n.Store.Get != nil {
		var err error
		if t, err = n.Store.Get(c, key); err != nil {
			return nil, err
		}
	}
	if t == nil {
		t = Thing(written)
	}
	return thingRecord(n, t, nil), nil
}

// unshownProp is the first of names whose When guard is false where the
// request acts, "" when every one is offered there.
func (r *Runtime) unshownProp(n *Noun, c Ctx, names []string) string {
	focus := focusOf(c.Context)
	for _, name := range names {
		if p := n.prop(name); p != nil && !r.propShown(*p, focus, r.Mode, c.Args) {
			return name
		}
	}
	return ""
}

func runCreate(r *Runtime, n *Noun, c Ctx) (any, error) {
	args, err := createArgs(n, c.Args)
	if err != nil {
		return nil, err
	}
	c.Args = args
	values := Thing{}
	for _, p := range createProps(n) {
		v, ok := c.Args[p.Name]
		if !ok {
			if p.Default == nil {
				continue
			}
			v = p.Default
		}
		typed, err := propValue(p, v)
		if err != nil {
			return nil, &ArgumentError{fmt.Errorf("%s %w", p.Name, err)}
		}
		values[p.Name] = typed
	}
	given, _ := createWrites(n, c.Args)
	if name := r.unshownProp(n, c, given); name != "" {
		return nil, fmt.Errorf("%s.%s is not available here", r.DisplayNoun(n.Name), name)
	}
	filled := r.createFill(n, c, values)
	t, err := n.Store.Create(c, values)
	if err != nil {
		return nil, err
	}
	filled.notify(c)
	if t == nil {
		t = values
	}
	return thingRecord(n, t, nil), nil
}

func runSet(r *Runtime, n *Noun, c Ctx) (any, error) {
	changes, order, err := setChanges(n, c.Args)
	if err != nil {
		return nil, err
	}
	if name := r.unshownProp(n, c, order); name != "" {
		return nil, fmt.Errorf("%s.%s is not available here", r.DisplayNoun(n.Name), name)
	}
	key, err := r.subjectKey(n, c)
	if err != nil {
		return nil, err
	}
	for name, v := range changes {
		if v != nil {
			continue
		}
		// toggle: the current value, flipped
		if n.Store.Get == nil {
			return nil, fmt.Errorf("%s cannot toggle %s: its store does not read things", r.DisplayNoun(n.Name), name)
		}
		t, err := n.Store.Get(c, key)
		if err != nil {
			return nil, err
		}
		current, _ := propValue(*n.prop(name), t[name])
		b, _ := current.(bool)
		changes[name] = !b
	}
	t, err := n.Store.Patch(c, key, Thing(changes))
	if err != nil {
		return nil, err
	}
	return readBack(c, n, key, t, changes)
}

func runDelete(r *Runtime, n *Noun, c Ctx) (any, error) {
	key, err := r.subjectKey(n, c)
	if err != nil {
		return nil, err
	}
	if archived, ok, err := r.archiveOnDelete(n, c, key); ok || err != nil {
		return archived, err
	}
	if err := n.Store.Delete(c, key); err != nil {
		return nil, err
	}
	return map[string]any{"deleted": key}, nil
}

// simpleWrite reports whether a request writes only simple values (owner
// decision 12, PropSpec.simple): a prop-shaped write of a simple prop, or a
// standard verb that writes values (create, set) whose every written prop
// is simple. It is the one test . (repeat) and the history file read; no
// verb or noun name is consulted.
func (r *Runtime) simpleWrite(q Request) bool {
	if q.IsProp() {
		p, ok := r.PropOf(q.Noun, q.Prop)
		return q.Writes() && ok && p.simple()
	}
	op, ok := r.Operations[q.Key()]
	row := standardRow(op.Standard)
	n := r.Nouns[op.Noun]
	if !ok || row == nil || row.Writes == nil || n == nil {
		return false
	}
	names, err := row.Writes(n, q.Args)
	if err != nil || len(names) == 0 {
		return false
	}
	for _, name := range names {
		if p := n.prop(name); p == nil || !p.simple() {
			return false
		}
	}
	return true
}

// writesValues reports whether a request writes prop values (a prop-shaped
// write, a standard create or set): its history line keeps them only when
// they are simple (simpleWrite), else it writes none.
func (r *Runtime) writesValues(q Request) bool {
	if q.IsProp() {
		return q.Writes()
	}
	row := standardRow(r.Operations[q.Key()].Standard)
	return row != nil && row.Writes != nil
}

// repeatableStandard reports whether . repeats a recorded request as a
// standard write: a prop-shaped write, or a standard verb whose row is
// repeatable, when it writes only simple values.
func (r *Runtime) repeatableStandard(q Request) bool {
	if !q.IsProp() {
		row := standardRow(r.Operations[q.Key()].Standard)
		if row == nil || !row.Repeatable {
			return false
		}
	}
	return r.simpleWrite(q)
}

// propsAvailable is the per-prop layer of a standard verb's availability
// (PropSpec.When): a request naming a prop the guard hides at focus is
// unavailable, and so is set naming none when no prop it could write is
// offered there.
func (r *Runtime) propsAvailable(q Request, focus string, mode ModeState) bool {
	op, ok := r.Operations[q.Key()]
	if !ok || op.Standard == "" {
		return true
	}
	n := r.Nouns[op.Noun]
	if n == nil || n.Store == nil {
		return true
	}
	var names []string
	switch row := standardRow(op.Standard); {
	case row == nil:
		return true
	case row.Writes != nil:
		names, _ = row.Writes(n, q.Args)
	default:
		if p := StringArg(q.Args, "prop", ""); p != "" {
			names = []string{p}
		}
	}
	shown := func(p PropSpec) bool { return r.propShown(p, focus, mode, q.Args) }
	for _, name := range names {
		if p := n.prop(name); p != nil && !shown(*p) {
			return false
		}
	}
	if op.Standard == "set" && len(names) == 0 {
		return slices.ContainsFunc(writableProps(n), shown)
	}
	return true
}
