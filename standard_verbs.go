package ochazuke

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// The standard verbs: the core verb table and the legacy one
// (core_verbs.go), one row per verb, like the setting verbs
// (setting_verbs.go). A noun that declares props and a store (props.go)
// gets NOUN.VERB for every core row, unavailable with a reason where its
// store lacks what the row needs, and for every legacy row its store
// meets; each is marked Operation.Standard with the row's name. Which
// nouns support which verb is a query over the tables, and the catalog
// publishes it (standard, unavailable).
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

// DefaultDeleteConfirm is the prompt a standard delete asks before it runs
// unless the store names its own (Store.Confirm["delete"]; "" asks
// nothing): a delete is never silent.
const DefaultDeleteConfirm = "Delete {{noun}} {{label}}?"

// DefaultArchiveConfirm is what a standard delete asks instead when it
// will only archive the thing (delete.archive): the confirm says what
// happens. delete.archive-confirm off archives without asking.
const DefaultArchiveConfirm = "Archive {{noun}} {{label}}? (delete it again to delete it for good)"

// ensureStandardVerbs registers or refreshes the generated verbs of a
// noun with a store: every core row (core_verbs.go), available when the
// store meets its needs and otherwise registered unavailable with the
// reason (Operation.Unavailable); a legacy row only where it is met, and
// dropped where it no longer is. A verb the application registered under
// the row's name is left alone, and so is a name the application gave an
// alias or a nested noun. A row's Aliases come with it, unless the noun
// has a verb of that name.
func (r *Runtime) ensureStandardVerbs(n *Noun) {
	for _, v := range standardTable() {
		key := n.Name + "." + v.Verb
		existing, ok := r.Operations[key]
		if ok && existing.Standard == "" {
			r.adoptFieldParam(key) // the application's verb wins, the row's field still reaches its form
			continue
		}
		if !ok && r.nameTaken(key) {
			continue // the application's alias or noun wins
		}
		if ok {
			delete(r.Operations, key)
		}
		reason := v.unmet(r, n)
		if reason != "" && v.optional {
			n.Verbs = slices.DeleteFunc(n.Verbs, func(verb string) bool { return verb == v.Verb })
			for _, alias := range v.Aliases {
				r.dropStandardAlias(n.Name + "." + alias)
			}
			continue
		}
		r.Register(v.operation(r, n, reason))
		if g := n.Store.When[v.Verb]; g != "" {
			r.SetAvailability(key, g)
		}
		if !slices.Contains(n.Verbs, v.Verb) {
			n.Verbs = append(n.Verbs, v.Verb)
		}
		for _, alias := range v.Aliases {
			if _, taken := r.Operations[n.Name+"."+alias]; !taken {
				if err := r.AddActionAlias(ActionAlias{Alias: n.Name + "." + alias, Action: key, Source: AliasStandard}); err != nil {
					panic(err.Error())
				}
			}
		}
	}
}

// standardOperation is the noun's operation for a standard verb of the
// table, whether the framework generated it or the application registered
// its own under the row's name; false when the verb is not in the table or
// the noun has no such operation.
func (r *Runtime) standardOperation(noun, verb string) (Operation, bool) {
	row := standardRow(verb)
	if row == nil {
		return Operation{}, false
	}
	op, ok := r.Operations[r.CanonicalNoun(noun)+"."+row.Verb]
	return op, ok
}

// adoptFieldParam gives the application's own verb registered under a
// core row's name the row's FieldParam (edit's field): a request giving
// it runs the row's own handler (the one-field form, which every thing
// noun's r and m keys reach), any other the application's verb. Read off
// the row (FieldParam, Handler), never a verb's name; a no-op for a noun
// without a store, a row without a FieldParam, or a verb that declares
// the parameter itself.
func (r *Runtime) adoptFieldParam(key string) {
	op, ok := r.Operations[key]
	if !ok || op.Standard != "" {
		return
	}
	row, n := standardRow(op.Verb), r.Nouns[op.Noun]
	if row == nil || row.optional || row.FieldParam == "" || row.Handler == nil || n == nil || n.Store == nil {
		return
	}
	field := row.FieldParam
	if _, declared := op.Parameters[field]; declared {
		return
	}
	std := row.operation(r, n, "")
	params := maps.Clone(op.Parameters)
	if params == nil {
		params = map[string]Parameter{}
	}
	params[field] = std.Parameters[field]
	if kp := n.keyProp(); kp != nil {
		if _, has := params[kp.Name]; !has {
			params[kp.Name] = std.Parameters[kp.Name]
		}
	}
	own, run, form := op.App, op.Run, row.Handler(r, n)
	op.Parameters = params
	op.App = func(c Ctx) Result {
		if v, given := c.Args[field]; given && !emptyProp(v) {
			return form(c)
		}
		if own != nil {
			return own(c)
		}
		v, err := run(c.Runtime, c.Context, c.Args)
		if err != nil {
			return Failed(err)
		}
		return Done(v)
	}
	if own == nil {
		op.Kind = KindApplication
		op.Run = func(r *Runtime, c Context, a Arguments) (any, error) {
			return r.runApplication(r.Operations[key], c, a)
		}
	}
	r.Operations[key] = op
}

// nameTaken reports whether key names something the application declared
// that is not an operation: an alias it (or the user) registered, or a
// noun nested under the noun.
func (r *Runtime) nameTaken(key string) bool {
	if a, ok := r.actionNames.aliases[key]; ok && a.Source != AliasStandard {
		return true
	}
	return r.Nouns[key] != nil
}

// dropStandardAlias removes a generated alias of that name, if there is one.
func (r *Runtime) dropStandardAlias(name string) {
	if a, ok := r.actionNames.aliases[name]; ok && a.Source == AliasStandard {
		r.removeAlias(name)
	}
}

// operation is the row's operation on noun n; reason, when not "", is why
// it cannot run there (Operation.Unavailable): the operation is listed,
// never available, and fails with the reason if run.
func (v standardVerb) operation(r *Runtime, n *Noun, reason string) Operation {
	name := n.Name
	params := map[string]Parameter{}
	if v.Params != nil {
		params = v.Params(n)
	}
	op := Operation{Verb: v.Verb, Noun: name, Description: v.Description, Parameters: params, Result: "JSON value", HistoryKind: v.HistoryKind, Standard: v.Verb, Many: v.Many, Destructive: v.Destructive, Unavailable: reason}
	if key := n.keyProp(); v.TakesSubject && key != nil {
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
	op.App = v.handler(r, n, v.hardReason(r, n))
	return op
}

// handler is the row's AppHandler on noun n: one that fails with reason
// when the verb is unavailable there, the row's own Handler, else Run
// through runRecording, once per selected row for a Many verb.
func (v standardVerb) handler(r *Runtime, n *Noun, reason string) AppHandler {
	if reason != "" {
		return func(Ctx) Result { return Failed(errors.New(reason)) }
	}
	if v.Handler != nil {
		return v.Handler(r, n)
	}
	name := n.Name
	row, many, key := v, v.Many, name+"."+v.Verb
	one := func(c Ctx) Result {
		noun := c.Runtime.Nouns[name]
		if noun == nil || noun.Store == nil {
			return Failed(fmt.Errorf("%s has no store", name))
		}
		c.standard = key
		value, err := c.Runtime.runRecording(row, noun, c) // standard_undo.go
		if err != nil {
			return Failed(err)
		}
		return Done(value)
	}
	return func(c Ctx) Result {
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
		"prop":  {Type: "string", Enum: slices.Concat(propNames(n), n.roleNames()), Free: slices.ContainsFunc(n.Props, func(p PropSpec) bool { return len(p.Aliases) > 0 }), Description: "One prop (or @role: the prop with that role): answer its value alone"},
		"props": {Type: "string", Description: "The props to answer, comma-separated (default: every prop but secrets)"},
	}
}

func listParams(n *Noun) map[string]Parameter {
	params := map[string]Parameter{
		"filter":     {Type: "string", Free: true, Description: "Keep the things that meet every condition: words prop=value, prop!=value, prop~value (contains), prop!~value, with a|b for alternatives; or the name of a saved prop filter for this noun. ANDed with where"},
		"where":      {Type: "string", Free: true, Pairs: true, Description: "prop=value pairs, comma- or space-separated: keep the things whose props equal every value"},
		"props":      {Type: "string", Description: "The props each row holds besides id and label, comma-separated (default: every prop but secrets)"},
		"limit":      {Type: "integer", Description: "At most this many rows: the first page of that size (0: every row)"},
		"sort":       {Type: "string", Free: true, Description: "Order the rows by props, comma-separated, each prop[:desc]; ties keep the store's order"},
		"group-by":   {Type: "string", Free: true, Description: "Gather the rows that share these props' values into groups (comma-separated props); each row names its group"},
		"group-sort": {Type: "string", Free: true, Description: "Order the groups by group-by props, each prop[:desc] (default: the order the groups first appear in)"},
		"page":       {Type: "integer", Description: "The page to answer, from 1 (with per-page)"},
		"per-page":   {Type: "integer", Description: "Rows per page, counting rows, never groups (0: one page of every row)"},
		"archived":   {Type: "boolean", Description: "Include the archived things list.hide-archived hides by default"},
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
	for _, name := range []string{"filter", "sort", "group-by", "group-sort", "page", "per-page", "archived"} {
		delete(params, name)
	}
	return (Operation{Parameters: params}).ParameterOrder()
}

// listWords splits a list argument at commas and spaces.
func listWords(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' })
}

// pickedProps reads a props argument: the names it gives, each a prop of
// n; nil when it is empty.
func pickedProps(n *Noun, a Arguments) ([]string, error) {
	names := listWords(StringArg(a, "props", ""))
	for i, name := range names {
		p := n.prop(name)
		if p == nil {
			return nil, &ArgumentError{fmt.Errorf("%s has no prop %q", n.Name, name)}
		}
		names[i] = p.Name
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
	if p := n.prop(prop); p != nil {
		prop = p.Name
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
		out[p.Name] = v
	}
	return out, nil
}

// HideArchivedSetting hides things whose archive-role prop is true from
// standard list results and from row-fed lists and trees (default true).
const HideArchivedSetting = "list.hide-archived"

// archiveName is the name of the noun's archive prop, "" when it has none.
func archiveName(n *Noun) string {
	if p := n.roleProp(RoleArchive); p != nil {
		return p.Name
	}
	return ""
}

// archiveHiding is the archive prop a default view of n's things hides on,
// or nil when it hides nothing: the noun has no archive prop, the setting
// is off, the query includes archived things (Archived) or one of its
// conditions names the prop (a where, a filter). queryAll and every
// row-fed list or tree (List.archiveHidden) decide through it, so the
// rule is one.
func (r *Runtime) archiveHiding(n *Noun, q ListQuery) *PropSpec {
	if n == nil {
		return nil
	}
	p := n.roleProp(RoleArchive)
	if p == nil || q.Archived || !r.Settings().Bool(HideArchivedSetting) || slices.ContainsFunc(q.Filters, func(f PropFilter) bool { return f.Prop == p.Name }) {
		return nil
	}
	return p
}

// archivedThing reports whether the thing's archive prop is true.
func archivedThing(p *PropSpec, t Thing) bool {
	v, _ := propValue(*p, t[p.Name])
	return v == true
}

func runList(r *Runtime, n *Noun, c Ctx) (any, error) {
	names, err := pickedProps(n, c.Args)
	if err != nil {
		return nil, err
	}
	q, err := r.listQueryOf(n, c.Args)
	if err != nil {
		return nil, err
	}
	page, err := r.queryThings(c, n, q)
	if err != nil {
		return nil, err
	}
	return answerOf(n, page, q, names), nil
}

// thingMatches reports whether a thing's props equal every where value.
func thingMatches(n *Noun, t Thing, where map[string]any) bool {
	for name, want := range where {
		p := n.prop(name)
		if p == nil || !propMatches(*p, t[p.Name], PropFilter{Prop: name, Op: "=", Value: want}) {
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
	if n.keyProp() != nil && n.Store.Get != nil {
		params["from"] = Parameter{Type: AnyParameterType, Noun: n.Name, Free: true, Covers: true, Description: "Copy this thing (an id or a human key, or a row holding its key): every prop create writes, read from it, unless given"}
	}
	return params
}

// createSource is the thing create {from} copies: from names it by an id
// or a human key (resolveTarget), or is a row's Data holding its key.
func (r *Runtime) createSource(n *Noun, c Ctx, from any) (Thing, error) {
	key := n.keyProp()
	if key == nil || n.Store.Get == nil {
		return nil, &ArgumentError{fmt.Errorf("%s create takes no from: its things are not read by key", r.DisplayNoun(n.Name))}
	}
	var text string
	switch v := from.(type) {
	case string, int, int64, float64:
		text = fmt.Sprint(v)
	default:
		row, err := ThingOf(v)
		if err != nil || emptyProp(row[key.Name]) {
			return nil, &ArgumentError{fmt.Errorf("from names no %s: give an id, a human key or a row holding its %s", r.DisplayNoun(n.Name), key.Name)}
		}
		text = fmt.Sprint(row[key.Name])
	}
	id, err := r.resolveTarget(n, c, text)
	if err != nil {
		return nil, err
	}
	t, err := n.Store.Get(c, id)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, fmt.Errorf("no %s %q", r.DisplayNoun(n.Name), text)
	}
	return t, nil
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
	for _, k := range sortedKeys(a) {
		if k == "pairs" {
			continue
		}
		name := k
		if p := n.prop(k); p != nil {
			name = p.Name
		}
		if _, exists := out[name]; exists {
			return nil, &ArgumentError{fmt.Errorf("%s is written twice", name)}
		}
		out[name] = a[k]
		if name != k {
			moved[name] = true
		}
	}
	writes := func(name string) bool {
		p := n.prop(name)
		return p != nil && !p.ReadOnly && !p.Key
	}
	put := func(name string, v any) error {
		if p := n.prop(name); p != nil {
			name = p.Name
		}
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
		"props":   {Type: "object", Description: "The props to write: an object of prop (or @role: @label, @owner, @archive) to value, a subset or all of them; the others keep their values"},
		"prop":    {Type: "string", Enum: names, Free: true, Pairs: true, Description: "The prop toggle flips; a prop=value word fills props (with value, the one-prop form props replaces)"},
		"value":   {Type: AnyParameterType, ValueOf: "prop", Description: "The prop's new value, typed by the prop (a word such as 3, true or 400ms, a JSON array or object); props replaces it"},
		"toggle":  {Type: "boolean", Excludes: []string{"value"}, Description: "Flip the boolean prop instead of giving a value"},
		"changes": {Type: "object", Description: "The old name of props (accepted for one release)"},
		"pairs":   {Type: "string", Rest: true, Description: "More prop=value words after the first, filling props"},
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

// setWrites are the props a set request writes, in its order; none (and
// no error) for a request that names no prop yet (set's args form).
func setWrites(n *Noun, a Arguments) ([]string, error) {
	if !setNamesProps(a) {
		return nil, nil
	}
	_, order, err := setChanges(n, a)
	return order, err
}

// setNamesProps reports whether a set request names any prop to write.
func setNamesProps(a Arguments) bool {
	for _, name := range []string{"props", "changes", "prop", "pairs"} {
		if v, ok := a[name]; ok && !emptyProp(v) {
			return true
		}
	}
	return false
}

// setWritten is the value a set request's arguments write to the prop
// named name: from props (or changes), or the one-prop form's prop and
// value; false when it writes none.
func setWritten(a Arguments, name string) (any, bool) {
	for _, obj := range []string{"props", "changes"} {
		if m, ok := a[obj].(map[string]any); ok {
			v, ok := m[name]
			return v, ok
		}
	}
	if StringArg(a, "prop", "") != name {
		return nil, false
	}
	v, ok := a["value"]
	return v, ok
}

// propToggle is distinct from a literal null, which clears an optional prop.
type propToggle struct{}

// propMissingError is a write addressing a role (@label, @owner,
// @archive) the noun has no writable prop for (inside an ArgumentError):
// availability reads it (propsAvailable), so a request bound to a role is
// unavailable on a noun without it, as the verb that wrote the role was. A
// prop named by its name that is missing or read-only is an error.
type propMissingError struct{ error }

func (e propMissingError) Unwrap() error { return e.error }

// writeTarget is the writable prop a write names (a name, an alias or
// @role).
func writeTarget(n *Noun, name string) (*PropSpec, error) {
	p := n.prop(name)
	if role, ok := strings.CutPrefix(name, "@"); ok && (p == nil || !p.writable()) {
		return nil, &ArgumentError{propMissingError{fmt.Errorf("%s has no writable prop with the role %s", n.Name, role)}}
	}
	switch {
	case p == nil:
		return nil, &ArgumentError{fmt.Errorf("%s has no prop %q; only declared props are writable", n.Name, name)}
	case !p.writable():
		return nil, &ArgumentError{fmt.Errorf("%s.%s is read-only", n.Name, p.Name)}
	}
	return p, nil
}

// setChanges reads a set request's changes. The one shape is props, an
// object of prop to value; prop=value words (prop, value and pairs, quoted
// values unquoted) fill it the same way. toggle flips the one boolean
// prop named by prop, answered later against the current value. The old
// one-prop form (prop and value) and changes (props' old name) are
// accepted for one release (removed_verbs.json lists them as deprecated
// parameters). The forms do not mix. Each prop must be one set may
// write; values are typed by it.
func setChanges(n *Noun, a Arguments) (map[string]any, []string, error) {
	prop, _ := a["prop"].(string)
	value, hasValue := a["value"]
	pairs := StringArg(a, "pairs", "")
	toggle, _ := a["toggle"].(bool)
	changes := map[string]any{}
	var order []string
	add := func(name string, v any) error {
		p, err := writeTarget(n, name)
		if err != nil {
			return err
		}
		name = p.Name
		if _, dup := changes[name]; dup {
			return &ArgumentError{fmt.Errorf("%s is written twice", name)}
		}
		if _, toggles := v.(propToggle); !toggles {
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
	obj, objName, err := setObject(a)
	if err != nil {
		return nil, nil, err
	}
	switch {
	case objName != "":
		if prop != "" || hasValue || toggle || pairs != "" {
			return nil, nil, &ArgumentError{fmt.Errorf("set takes %s or prop words, not both", objName)}
		}
		names := slices.Sorted(maps.Keys(obj))
		// Declaration order, so a write reads the same every time.
		slices.SortStableFunc(names, func(x, y string) int { return propIndex(n, x) - propIndex(n, y) })
		for _, name := range names {
			if err := add(name, obj[name]); err != nil {
				return nil, nil, err
			}
		}
	case strings.Contains(prop, "=") || prop == "" && pairs != "":
		if toggle {
			return nil, nil, &ArgumentError{errors.New("toggle goes with one prop, not prop=value words")}
		}
		if err := addPairWords(setPairWords(prop, value, hasValue, pairs), add); err != nil {
			return nil, nil, err
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
			return changes, order, add(prop, propToggle{})
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
		return nil, nil, &ArgumentError{errors.New("set needs props (an object of prop to value), prop=value words, or a prop to toggle")}
	}
	return changes, order, nil
}

// setObject is a set request's props object and the parameter that gave
// it: props, or changes (its old name); "" for neither.
func setObject(a Arguments) (map[string]any, string, error) {
	obj, name := a["props"], "props"
	if old, ok := a["changes"]; ok {
		if obj != nil {
			return nil, "", &ArgumentError{errors.New("set takes props, not changes too (changes is props' old name)")}
		}
		obj, name = old, "changes"
	}
	if obj == nil {
		return nil, "", nil
	}
	m, err := propValue(PropSpec{Name: name, Parameter: Parameter{Type: PropObject}}, obj)
	if err != nil {
		return nil, "", &ArgumentError{fmt.Errorf("%s %w", name, err)}
	}
	return m.(map[string]any), name, nil
}

// setPairWords are a set request's prop=value words: prop, value (the line's
// second word) and the pairs rest.
func setPairWords(prop string, value any, hasValue bool, pairs string) []string {
	var words []string
	if prop != "" {
		words = append(words, prop)
	}
	if hasValue {
		words = append(words, fmt.Sprint(value))
	}
	return append(words, splitPairs(pairs)...)
}

// addPairWords adds each prop=value word (quoted values unquoted).
func addPairWords(words []string, add func(string, any) error) error {
	for _, w := range words {
		name, v, ok := strings.Cut(w, "=")
		if !ok {
			return &ArgumentError{fmt.Errorf("%q is not prop=value: the one-prop and several-prop forms do not mix", w)}
		}
		if err := add(name, unquoteWord(v)); err != nil {
			return err
		}
	}
	return nil
}

// propIndex is a prop's place in its noun's declaration order.
func propIndex(n *Noun, name string) int {
	if p := n.prop(name); p != nil {
		name = p.Name
	}
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
	var source Thing
	if from, ok := c.Args["from"]; ok && !emptyProp(from) {
		if source, err = r.createSource(n, c, from); err != nil {
			return nil, err
		}
	}
	values := Thing{}
	for _, p := range createProps(n) {
		v, ok := c.Args[p.Name]
		if !ok && source != nil && source[p.Name] != nil {
			// A copy: the source's value, in the store's own form when
			// the prop does not read it (create judges it).
			if typed, err := propValue(p, source[p.Name]); err == nil {
				values[p.Name] = typed
			} else {
				values[p.Name] = source[p.Name]
			}
			continue
		}
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
	var filled focusFills
	if source == nil { // a copy keeps its source's references
		filled = r.createFill(n, c, values)
	}
	n.stamp(values, RoleCreated, RoleUpdated)
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
		if _, toggle := v.(propToggle); !toggle {
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
	n.stamp(Thing(changes), RoleUpdated)
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

// missingRole is the error of a request addressing a role its noun has no
// writable prop for (propMissingError): through the props it writes (a
// row's Writes) or the one prop it opens (a row's FieldParam, also on an
// application's own verb that adopted it). nil otherwise. Execute says it
// instead of "not applicable"; availability reads it.
func (r *Runtime) missingRole(q Request) error {
	op, ok := r.Operations[q.Key()]
	if !ok {
		return nil
	}
	n := r.Nouns[op.Noun]
	row := standardRow(op.Standard)
	if row == nil {
		row = standardRow(op.Verb) // an application's verb that adopted the row's field
		if row != nil && row.Writes != nil {
			row = nil
		}
	}
	if row == nil || n == nil || n.Store == nil {
		return nil
	}
	if row.Writes != nil {
		if _, err := row.Writes(n, q.Args); errors.As(err, new(propMissingError)) {
			return err
		}
	}
	if field := StringArg(q.Args, row.FieldParam, ""); row.FieldParam != "" && field != "" {
		if _, err := writeTarget(n, field); errors.As(err, new(propMissingError)) {
			return err
		}
	}
	return nil
}

// propsAvailable is the per-prop layer of a standard verb's availability
// (PropSpec.When): a request naming a prop the guard hides at focus is
// unavailable, and so is a prop write (WritesProps) naming none when no
// prop it could write is offered there.
func (r *Runtime) propsAvailable(q Request, focus string, mode ModeState) bool {
	if r.missingRole(q) != nil {
		return false // a role the noun cannot write: as the verb that wrote it was
	}
	op, ok := r.Operations[q.Key()]
	if !ok || op.Standard == "" {
		return true
	}
	n := r.Nouns[op.Noun]
	if n == nil || n.Store == nil {
		return true
	}
	row := standardRow(op.Standard)
	if row == nil {
		return true
	}
	var names []string
	if row.Writes != nil {
		names, _ = row.Writes(n, q.Args)
	} else if p := StringArg(q.Args, "prop", ""); p != "" {
		names = []string{p}
	}
	if row.FieldParam != "" {
		if p, err := writeTarget(n, StringArg(q.Args, row.FieldParam, "")); err == nil {
			names = append(names, p.Name)
		}
	}
	shown := func(p PropSpec) bool { return r.propShown(p, focus, mode, q.Args) }
	for _, name := range names {
		if p := n.prop(name); p != nil && !shown(*p) {
			return false
		}
	}
	if row.WritesProps && row.Writes != nil && len(names) == 0 {
		// A write naming no prop yet (set's args form) needs one it could
		// write offered here.
		return slices.ContainsFunc(writableProps(n), shown)
	}
	return true
}
