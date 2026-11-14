package ochazuke

import (
	"cmp"
	"encoding/json"
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
	r.ensureVerbsOf(n)
	if declaresAbout(n) {
		// An annotating noun gives the nouns it can be about annotate.
		for _, name := range slices.Sorted(maps.Keys(r.Nouns)) {
			if m := r.Nouns[name]; m != n && m.Store != nil {
				r.ensureVerbsOf(m)
			}
		}
	}
}

// ensureVerbsOf is ensureStandardVerbs for one noun, then its generated
// default keys (noun_keys.go).
func (r *Runtime) ensureVerbsOf(n *Noun) {
	defer r.ensureNounKeys(n)
	for _, v := range standardTable() {
		key := n.Name + "." + v.Verb
		existing, ok := r.Operations.Find(Key(key))
		if ok && existing.Standard == "" {
			r.adoptFieldParam(key) // the application's verb wins, the row's field still reaches its form
			continue
		}
		if ok && existing.CatalogProxy {
			// A mounted extension's own verb (cli.MountNouns marks it
			// Standard as the extension's catalog does): the extension
			// serves it, so a later refresh of the noun's verbs (another
			// noun declaring about, a setting) neither replaces nor drops
			// it, whatever the host store meets.
			continue
		}
		if !ok && r.nameTaken(key) {
			continue // the application's alias or noun wins
		}
		if ok {
			r.Operations.Delete(Key(key))
		}
		wasBuiltin := ok && existing.Builtin // a regenerated verb keeps its mark
		reason := v.unmet(r, n)
		if v.YieldsToProp && n.prop(v.Verb) != nil {
			reason = "a prop takes the name" // the word is the prop's
		}
		if reason != "" && v.optional {
			for _, alias := range v.Aliases {
				r.dropStandardAlias(n.Name + "." + alias)
			}
			continue
		}
		op := v.operation(r, n, reason)
		op.Builtin = wasBuiltin
		r.Register(op)
		if g := n.Store.When[v.Verb]; g != "" {
			r.SetAvailability(key, g)
		}
		for _, alias := range v.Aliases {
			if _, taken := r.Operations.Find(Key(n.Name + "." + alias)); !taken {
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
	op, ok := r.Operations.Find(Key(r.CanonicalNoun(noun) + "." + row.Verb))
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
	op, ok := r.Operations.Find(Key(key))
	if !ok || op.Standard != "" {
		return
	}
	row, n := standardRow(op.Name()), r.Nouns[op.Noun()]
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
	// The field and the row's other parameters that go with it (edit's
	// chooser), each one the app's verb does not declare itself.
	for _, name := range append([]string{field}, row.ParamNames...) {
		if _, has := params[name]; !has {
			if p, ok := std.Parameters[name]; ok {
				params[name] = p
			}
		}
	}
	if kp := n.keyProp(); kp != nil {
		if _, has := params[kp.Name]; !has {
			params[kp.Name] = std.Parameters[kp.Name]
		}
	}
	own, run, form := op.AppHandler, op.Run, row.Handler(r, n)
	op.Parameters = params
	op.AppHandler = func(c Ctx) Result {
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
			return r.runApplication(r.Operations.At(Key(key)), c, a)
		}
	}
	r.Operations.Set(op)
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
	op := Operation{Key: KeyOf(name, v.Verb), Description: v.Description, Parameters: params, Result: "JSON value", HistoryKind: v.HistoryKind, Standard: v.Verb, Subject: v.subjectScope(), Many: v.Many, Creates: v.Creates, Destructive: v.Destructive, Agent: v.Agent, Unavailable: reason}
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
		op.Result = ResultRows
	}
	if v.RowsOf != "" {
		op.Rows = v.RowsOf
		op.Result = ResultRows
	}
	op.AppHandler = v.handler(r, n, v.hardReason(r, n))
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
		h := v.Handler(r, n)
		if h == nil {
			return nil
		}
		return r.resolvingHandler(v, n.Name, h)
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
		noun := c.Runtime.Nouns[name]
		if noun == nil || noun.Store == nil {
			return Failed(fmt.Errorf("%s has no store", name))
		}
		if noun.Store.Remote {
			return c.Runtime.runRemote(row, noun, c)
		}
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
	return Parameter{Type: typ, Noun: n.Name, Focus: true, Accept: AcceptKeep,
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
		"prop":  {Type: "string", Enum: slices.Concat(propNames(n), n.roleNames()), Accept: AcceptKeep, Description: "One prop (or @role: the prop with that role): answer its value alone; a dotted path (prop.key.0.key) reads inside an object or list prop"},
		"props": {Type: "string", Description: "The props to answer, comma-separated (default: every prop but secrets)"},
	}
}

func listParams(n *Noun) map[string]Parameter {
	params := map[string]Parameter{
		"filter":     {Type: "string", Accept: AcceptKeep, Description: "Keep the things that meet every condition: words prop=value, prop!=value, prop~value (contains), prop!~value, with a|b for alternatives; or the name of a saved prop filter for this noun. ANDed with where"},
		"where":      {Type: "string", Accept: AcceptKeep, Pairs: true, Description: "prop=value pairs, comma- or space-separated: keep the things whose props equal every value"},
		"props":      {Type: "string", Description: "The props each row holds besides id and label, comma-separated (default: every prop but secrets)"},
		"limit":      {Type: "integer", Description: "At most this many rows: the first page of that size (0: every row)"},
		"sort":       {Type: "string", Accept: AcceptKeep, Description: "Order the rows by props, comma-separated, each prop[:desc]; ties keep the store's order"},
		"group-by":   {Type: "string", Accept: AcceptKeep, Description: "Gather the rows that share these props' values into groups (comma-separated props); each row names its group"},
		"search":     {Type: "string", Accept: AcceptKeep, Named: true, Description: "Only the things holding every word of this text in their key, label or text props (case-insensitive); a store that pages searches the whole store"},
		"group-sort": {Type: "string", Accept: AcceptKeep, Description: "Order the groups by group-by props, each prop[:desc] (default: the order the groups first appear in)"},
		"page":       {Type: "integer", Description: "The page to answer, from 1 (with per-page)"},
		"per-page":   {Type: "integer", Description: "Rows per page, counting rows, never groups (0: one page of every row)"},
		"archived":   {Type: "boolean", Description: "Include the archived things collection.hide-archived hides by default"},
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
		if len(names) > 0 && !slices.Contains(names, p.Name) || len(names) == 0 && (p.Secret || p.computed()) {
			continue
		}
		if v, ok := t[p.Name]; ok {
			out[p.Name] = fileForm(v)
		}
	}
	return out
}

func runGet(r *Runtime, n *Noun, c Ctx) (any, error) {
	prop, path := propPath(n, StringArg(c.Args, "prop", ""))
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
	if p := n.prop(prop); p != nil && p.ofCollection() {
		return readCollectionProp(c, n, p, path)
	}
	if err := refuseCollectionProps(r, n, names); err != nil {
		return nil, err
	}
	if len(names) == 0 && prop == "" {
		names = r.recordProps(n, c)
	}
	id, err := r.subjectKey(n, c)
	if err != nil {
		return nil, err
	}
	t, err := r.getThing(c, n, id, id)
	if err != nil {
		return nil, err
	}
	t = maps.Clone(t) // computed props are added to the copy, never to the store's thing
	if err := readComputed(c, n, t, append([]string{prop}, names...)); err != nil {
		return nil, err
	}
	if prop != "" {
		return propAnswer(r, n, c, t, prop, path)
	}
	return thingRecord(n, t, names), nil
}

// recordProps are the props a get without prop or props answers: every
// one offered where the request acts, but secret and computed ones (those
// are asked for by name); a single empty name when none is.
func (r *Runtime) recordProps(n *Noun, c Ctx) []string {
	var names []string
	focus := focusOf(c.Context)
	for _, p := range n.Props {
		if !p.Secret && !p.computed() && r.propShown(p, focus, c.mode(), c.Args) {
			names = append(names, p.Name)
		}
	}
	if len(names) == 0 {
		names = []string{""} // no prop is offered here: an empty record
	}
	return names
}

// propAnswer is one prop of a thing as get answers it: its value in file
// form, or the place inside it a dotted path names.
func propAnswer(r *Runtime, n *Noun, c Ctx, t Thing, prop string, path []string) (any, error) {
	if len(path) == 0 {
		return fileForm(t[prop]), nil
	}
	v, err := nestedValue(t[prop], path)
	if err != nil {
		return nil, &ArgumentError{fmt.Errorf("%s.%s: %w", r.DisplayNoun(n.Name), StringArg(c.Args, "prop", ""), err)}
	}
	return v, nil
}

// refuseCollectionProps refuses a props list that names a collection prop:
// one describes all the noun's things, so it is read alone, with prop.
func refuseCollectionProps(r *Runtime, n *Noun, names []string) error {
	for _, name := range names {
		if p := n.prop(name); p != nil && p.ofCollection() {
			return &ArgumentError{fmt.Errorf("%s.%s describes all the %s things: read it with prop, not props", r.DisplayNoun(n.Name), p.Name, r.DisplayNoun(n.Name))}
		}
	}
	return nil
}

// readComputed fills t with the computed props (PropSpec.Read) among names,
// each read from the thing as the store gave it. Only the names asked for
// are read: a computed prop costs nothing until it is wanted.
func readComputed(c Ctx, n *Noun, t Thing, names []string) error {
	done := map[string]bool{}
	for _, name := range names {
		p := n.prop(name)
		if p == nil || p.Read == nil || p.ofCollection() || done[p.Name] {
			continue
		}
		done[p.Name] = true
		c.Noun = n.Name
		v, err := p.Read(c, t)
		if err != nil {
			return fmt.Errorf("%s.%s: %w", c.Runtime.DisplayNoun(n.Name), p.Name, err)
		}
		t[p.Name] = v
	}
	return nil
}

// readCollectionProp answers a collection-scope prop (PropSpec.Subject):
// its Read, with no thing, optionally read inside by a dotted path.
func readCollectionProp(c Ctx, n *Noun, p *PropSpec, path []string) (any, error) {
	c.Noun = n.Name
	v, err := p.Read(c, nil)
	if err != nil {
		return nil, fmt.Errorf("%s.%s: %w", c.Runtime.DisplayNoun(n.Name), p.Name, err)
	}
	if len(path) > 0 {
		inner, err := nestedValue(v, path)
		if err != nil {
			return nil, &ArgumentError{fmt.Errorf("%s.%s: %w", c.Runtime.DisplayNoun(n.Name), p.Name, err)}
		}
		return inner, nil
	}
	return fileForm(v), nil
}

// propPath reads get's prop argument: a prop (or @role), or a dotted path
// whose first segments name a prop and the rest a place inside its value
// (state get modeState.payload.count). A prop whose name holds a dot is
// read whole first; otherwise the longest prefix that is a prop names it.
// A prop the noun lacks comes back whole, so the caller says so.
func propPath(n *Noun, arg string) (string, []string) {
	if arg == "" || n.prop(arg) != nil || !strings.Contains(arg, ".") {
		return arg, nil
	}
	segs := strings.Split(arg, ".")
	for i := len(segs) - 1; i >= 1; i-- {
		if n.prop(strings.Join(segs[:i], ".")) != nil {
			return strings.Join(segs[:i], "."), segs[i:]
		}
	}
	return arg, nil
}

// nestedValue reads a place inside a prop's value by path segments, over
// its JSON form: an object's key (the longest run of segments that is a
// key, so a key holding dots is reached: nouns.ochazuke.list.verbs), a
// list's 0-based index.
func nestedValue(v any, path []string) (any, error) {
	data, err := json.Marshal(fileForm(v))
	if err != nil {
		return nil, err
	}
	var node any
	if err := json.Unmarshal(data, &node); err != nil {
		return nil, err
	}
	for len(path) > 0 {
		switch at := node.(type) {
		case map[string]any:
			found := false
			for i := len(path); i >= 1; i-- {
				if next, ok := at[strings.Join(path[:i], ".")]; ok {
					node, path, found = next, path[i:], true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("no key %q", path[0])
			}
		case []any:
			i, err := strconv.Atoi(path[0])
			if err != nil || i < 0 || i >= len(at) {
				return nil, fmt.Errorf("no index %q (a list of %d)", path[0], len(at))
			}
			node, path = at[i], path[1:]
		default:
			return nil, fmt.Errorf("cannot read %q inside a %T", path[0], node)
		}
	}
	return node, nil
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
const HideArchivedSetting = "collection.hide-archived"

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
	for _, name := range names {
		if p := n.prop(name); p != nil && p.computed() {
			return nil, &ArgumentError{fmt.Errorf("%s.%s is computed when asked for: read it with get, list rows do not hold it", r.DisplayNoun(n.Name), p.Name)}
		}
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
// gives the key), unless the key is create-only too: then the caller names
// the thing at create and it never changes (a mode's name).
func createProps(n *Noun) []PropSpec {
	var out []PropSpec
	for _, p := range n.Props {
		if !p.ReadOnly && !p.Key || p.Key && p.CreateOnly {
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
	if out.IsPath() {
		return out // a path stays a path: its form field opens the path chooser
	}
	switch p.Type {
	case PropURL:
		out.Type = SettingString
	case SettingString, "", SettingInteger, SettingBoolean, PropTime:
		if out.Type == "" {
			out.Type = SettingString
		}
	default:
		out.Type = AnyParameterType
	}
	return out
}

const (
	createObjectArg = "props"
	createPairsArg  = "pairs"
	createSourceArg = "from"
)

// createParamName gives a control parameter a spelling no prop or prop
// alias owns. The noun's words win; the catalog publishes the escaped name.
func createParamName(n *Noun, name string) string {
	for n.prop(name) != nil {
		name = "create-" + name
	}
	return name
}

func createParams(n *Noun) map[string]Parameter {
	params := map[string]Parameter{}
	for _, p := range createProps(n) {
		param := propParameter(p)
		param.Pairs = true        // a prop=value word may fill any create slot
		param.Focus = p.FromFocus // create fills it from the focus (createFill)
		params[p.Name] = param
	}
	params[createParamName(n, createObjectArg)] = Parameter{Type: PropObject, Named: true, Description: "Props to create: an object of prop (or alias or @role) to value; prop=value words and named props fill the same values"}
	params[createParamName(n, createPairsArg)] = Parameter{Type: "string", Rest: true, Pairs: true, Description: "prop=value words for the props, after the label"}
	if n.keyProp() != nil && n.Store.Get != nil {
		params[createParamName(n, createSourceArg)] = Parameter{Type: AnyParameterType, Noun: n.Name, Accept: AcceptKeep, Covers: true, Named: true, Description: "Copy this thing (an id or a human key, or a row holding its key): every prop create writes, read from it, unless given; a colliding prop or alias owns the bare name"}
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
	t, err := r.getThing(c, n, id, text)
	if err != nil {
		return nil, err
	}
	return t, nil
}

// createArgs merges the props object, named props and prop=value words,
// resolving aliases and roles once before required/type checks. The result
// uses the prop parameters so sources, forms and validation share one shape.
func createArgs(n *Noun, a Arguments) (Arguments, error) {
	objectArg, pairsArg := createParamName(n, createObjectArg), createParamName(n, createPairsArg)
	if v, given := a[pairsArg]; given && !kindOf(SettingString).accepts(v) {
		return nil, &ArgumentError{fmt.Errorf("%s must be string", pairsArg)}
	}
	obj, err := propObject(a[objectArg], objectArg)
	if err != nil {
		return nil, err
	}
	out := Arguments{}
	writes := func(name string) bool {
		p := n.prop(name)
		return p != nil && (!p.ReadOnly && !p.Key || p.Key && p.CreateOnly)
	}
	put := func(name string, v any) error {
		p := n.prop(name)
		if !writes(name) {
			return &ArgumentError{fmt.Errorf("%s has no prop %q that create writes", n.Name, name)}
		}
		name = p.Name
		if _, given := out[name]; given {
			return &ArgumentError{fmt.Errorf("%s is written twice", name)}
		}
		out[name] = v
		return nil
	}
	for _, k := range sortedKeys(a) {
		if k == objectArg || k == pairsArg {
			continue
		}
		if n.prop(k) == nil {
			out[k] = a[k] // control parameters; unknown ones fail validation
			continue
		}
		word, text := a[k].(string)
		name, v, pair := strings.Cut(word, "=")
		if text && pair && writes(name) {
			if err := put(name, unquoteWord(v)); err != nil {
				return nil, err
			}
		} else if err := put(k, a[k]); err != nil {
			return nil, err
		}
	}
	for _, name := range sortedKeys(obj) {
		if err := put(name, obj[name]); err != nil {
			return nil, err
		}
	}
	if err := addPairWords(splitPairs(StringArg(a, pairsArg, "")), put); err != nil {
		return nil, err
	}
	for _, p := range createProps(n) {
		if v, given := out[p.Name]; given {
			typed, err := propValue(p, v)
			if err != nil {
				return nil, &ArgumentError{fmt.Errorf("%s %w", p.Name, err)}
			}
			out[p.Name] = typed
		}
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
		"prop":    {Type: "string", Enum: names, Accept: AcceptKeep, Pairs: true, Description: "The prop toggle flips; a prop=value word fills props (with value, the one-prop form props replaces)"},
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
		return nil, &ArgumentError{&ReadOnlyError{Noun: n.Name, Prop: p.Name, Reason: p.readOnlyReason()}}
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
	add := newChangeAdder(n, changes, &order)
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
		// A thing named with nothing to write is made current where its
		// noun says what current is (RoleCurrent): mode set nested.
		if p := writableRole(n, RoleCurrent); p != nil {
			return changes, order, add(p.Name, true)
		}
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
	m, err := propObject(obj, name)
	return m, name, err
}

// propObject reads a props mapping in its API or JSON word form.
func propObject(obj any, name string) (map[string]any, error) {
	if obj == nil {
		return nil, nil
	}
	m, err := propValue(PropSpec{Name: name, Parameter: Parameter{Type: PropObject}}, obj)
	if err != nil {
		return nil, &ArgumentError{fmt.Errorf("%s %w", name, err)}
	}
	return m.(map[string]any), nil
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
		if p := n.prop(name); p != nil && !r.propShown(*p, focus, c.mode(), c.Args) {
			return name
		}
	}
	return ""
}

func runCreate(r *Runtime, n *Noun, c Ctx) (any, error) {
	var err error
	var source Thing
	if from, ok := c.Args[createParamName(n, createSourceArg)]; ok && !emptyProp(from) {
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
		values[p.Name] = r.acceptProp(n.Name, p, typed)
	}
	if name := r.unshownProp(n, c, sortedKeys(c.Args)); name != "" {
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
	for name, v := range changes {
		if p := n.prop(name); p != nil {
			changes[name] = r.acceptProp(n.Name, *p, v)
		}
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
	for name, v := range changes {
		edit, isEdit := v.(listEdit)
		if !isEdit {
			continue
		}
		if n.Store.Get == nil {
			return nil, fmt.Errorf("%s cannot change a list by item: its store does not read things", r.DisplayNoun(n.Name))
		}
		t, err := n.Store.Get(c, key)
		if err != nil {
			return nil, err
		}
		current, _ := propValue(*n.prop(name), t[name])
		list, _ := current.([]string)
		changes[name] = edit.apply(list)
	}
	if routed, err := r.routeWrites(n, c, changes); err != nil || routed && len(changes) == 0 {
		if err != nil {
			return nil, err
		}
		return readBack(c, n, key, nil, changes)
	}
	n.stamp(Thing(changes), RoleUpdated)
	t, err := n.Store.Patch(c, key, Thing(changes))
	if err != nil {
		return nil, err
	}
	return readBack(c, n, key, t, changes)
}

// routeWrites runs, for each change of a prop with WrittenBy, the verb that
// changes it (the value as its WrittenArg) in place of the store's Patch,
// and takes the change out of changes; it reports whether any ran.
func (r *Runtime) routeWrites(n *Noun, c Ctx, changes map[string]any) (bool, error) {
	routed := false
	for _, name := range slices.Sorted(maps.Keys(changes)) {
		p := n.prop(name)
		if p == nil || p.WrittenBy == "" {
			continue
		}
		noun, verb, _ := CutKey(p.WrittenBy)
		arg := cmp.Or(p.WrittenArg, p.Name)
		if _, err := r.ExecuteAt(c.Context, Request{Noun: noun, Verb: verb, Args: Arguments{arg: changes[name]}}); err != nil {
			return routed, err
		}
		r.notice("%s.%s: ran %s", r.DisplayNoun(n.Name), name, p.WrittenBy)
		delete(changes, name)
		routed = true
	}
	return routed, nil
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
	// The thing's relations keep their rows, the end moving to its former
	// end (relation_ops.go): nothing is lost, nothing points at a ghost.
	if moved, err := r.DetachRelations(AnyRef(n.Name, key)); err != nil {
		c.Notify(fmt.Sprintf("deleted %s, but its relations could not be detached: %v", key, err))
	} else if moved > 0 {
		c.Notify(fmt.Sprintf("%d relations of %s kept as unassociated", moved, key))
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
	op, ok := r.Operations.Find(Key(q.Key()))
	row := standardRow(op.Standard)
	n := r.Nouns[op.Noun()]
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
	row := standardRow(r.Operations.At(Key(q.Key())).Standard)
	return row != nil && row.Writes != nil
}

// repeatableStandard reports whether . repeats a recorded request as a
// standard write: a prop-shaped write, or a standard verb whose row is
// repeatable, when it writes only simple values.
func (r *Runtime) repeatableStandard(q Request) bool {
	if !q.IsProp() {
		row := standardRow(r.Operations.At(Key(q.Key())).Standard)
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
	op, ok := r.Operations.Find(Key(q.Key()))
	if !ok {
		return nil
	}
	n := r.Nouns[op.Noun()]
	row := standardRow(op.Standard)
	if row == nil {
		row = standardRow(op.Name()) // an application's verb that adopted the row's field
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
// prop it could write is offered there. The prop guards judge within's
// interaction for a query, the live one for nil.
func (r *Runtime) propsAvailable(q Request, focus string, mode ModeState, within *resolutionContext) bool {
	if r.missingRole(q) != nil {
		return false // a role the noun cannot write: as the verb that wrote it was
	}
	op, ok := r.Operations.Find(Key(q.Key()))
	if !ok || op.Standard == "" {
		return true
	}
	n := r.Nouns[op.Noun()]
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
	shown := func(p PropSpec) bool { return r.propShownWithin(p, focus, mode, q.Args, within) }
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

// listEdit is a write of one list prop by item, not as a whole: `PROP+=x`
// adds the items missing from the list, `PROP-=x` removes the ones it has
// (any list prop, a relation-backed one included). Resolved against the
// current list in runSet, so the store sees the whole new list.
type listEdit struct {
	remove bool
	items  []string
}

// apply is list with the items added (those it lacks, at the end) or removed.
func (e listEdit) apply(list []string) []string {
	out := slices.Clone(list)
	for _, it := range e.items {
		switch {
		case e.remove:
			out = slices.DeleteFunc(out, func(o string) bool { return o == it })
		case !slices.Contains(out, it):
			out = append(out, it)
		}
	}
	if out == nil {
		out = []string{}
	}
	return out
}

// listEditOf reads a write's name as an item edit: name ends in + or - and
// the rest is a list prop of the noun (and the whole name is not a prop).
// It answers '+', '-' or 0, and the list prop's name.
func listEditOf(n *Noun, name string) (rune, string) {
	if name == "" || n.prop(name) != nil {
		return 0, name
	}
	last := rune(name[len(name)-1])
	if last != '+' && last != '-' {
		return 0, name
	}
	if p := n.prop(name[:len(name)-1]); p != nil && p.listed() {
		return last, p.Name
	}
	return 0, name
}

// listEditItems reads the items of a list edit's value: a list, a JSON
// array word, or one item.
func listEditItems(v any) ([]string, error) {
	switch x := v.(type) {
	case []string:
		return x, nil
	case string:
		if t := strings.TrimSpace(x); strings.HasPrefix(t, "[") {
			if items, err := coerceList(t); err == nil {
				return items.([]string), nil
			}
		}
		return []string{x}, nil
	}
	items, err := coerceList(v)
	if err != nil {
		return nil, err
	}
	return items.([]string), nil
}

// newChangeAdder is set's adder of one write: the prop (or a list edit
// PROP+ / PROP-) checked, typed and recorded in changes and order.
func newChangeAdder(n *Noun, changes map[string]any, order *[]string) func(string, any) error {
	return func(name string, v any) error {
		edit, base := listEditOf(n, name)
		if edit != 0 {
			name = base
		}
		p, err := writeTarget(n, name)
		if err != nil {
			return err
		}
		name = p.Name
		if _, dup := changes[name]; dup {
			return &ArgumentError{fmt.Errorf("%s is written twice", name)}
		}
		if edit != 0 {
			items, err := listEditItems(v)
			if err != nil {
				return &ArgumentError{fmt.Errorf("%s%c= %w", name, edit, err)}
			}
			changes[name] = listEdit{remove: edit == '-', items: items}
			*order = append(*order, name)
			return nil
		}
		if _, toggles := v.(propToggle); !toggles {
			typed, err := propValue(*p, v)
			if err != nil {
				return &ArgumentError{fmt.Errorf("%s %w", name, err)}
			}
			v = typed
		}
		changes[name] = v
		*order = append(*order, name)
		return nil
	}
}
