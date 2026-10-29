package ochazuke

import (
	"errors"
	"fmt"
	"slices"
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
// the handler it needs, whether it takes a subject, whether its answer is
// rows of the noun, its history kind, its own parameters and its handler.
type standardVerb struct {
	Verb        string
	Description string
	Kinds       []string
	Needs       func(Store) bool
	Subject     bool
	// NoFocus makes the subject optional and never filled from the focus
	// (watch: omitted, it watches every thing).
	NoFocus     bool
	Rows        bool
	HistoryKind string
	Params      func(n *Noun) map[string]Parameter
	Run         func(r *Runtime, n *Noun, c Ctx) (any, error)
}

// standardVerbs are the standard verbs this phase implements, in the order
// help lists them.
var standardVerbs = []standardVerb{
	{Verb: "get", Description: "Read a thing's props: every prop as an object, one prop's value (prop), or some props (props)",
		Kinds: []string{Collection, Singleton, Instances}, Needs: func(s Store) bool { return s.Get != nil },
		Subject: true, HistoryKind: HistoryFetch, Params: getParams, Run: runGet},
	{Verb: "list", Description: "List the things, each its props with id and label; where keeps the things whose props equal the values given",
		Kinds: []string{Collection, Instances}, Needs: func(s Store) bool { return s.List != nil },
		Rows: true, HistoryKind: HistoryFetch, Params: listParams, Run: runList},
	{Verb: "watch", Description: "Stream what happens to the things (one, or those where matches): a JSON line per thing added, modified or deleted, from since when given; the CLI streams until interrupted",
		Kinds: []string{Collection, Singleton, Instances}, Needs: watchFits,
		Subject: true, NoFocus: true, HistoryKind: HistoryFetch, Params: watchParams, Run: runWatch},
}

// reservedStandardVerbs are the names of every standard verb, those later
// phases add included: no prop takes one.
var reservedStandardVerbs = []string{"get", "list", "create", "set", "patch", "delete", "watch", "rename", "move", "duplicate"}

// standardParamNames are the parameters the standard verbs take beside the
// subject: no key prop and no store list parameter takes one.
var standardParamNames = []string{"prop", "props", "where", "limit", "since"}

// StandardVerbs lists the verbs of the standard verb table, in order.
func StandardVerbs() []string {
	out := make([]string, len(standardVerbs))
	for i, v := range standardVerbs {
		out[i] = v.Verb
	}
	return out
}

// fits reports whether a row applies to a noun's store.
func (v standardVerb) fits(s Store) bool {
	return slices.Contains(v.Kinds, s.Kind) && v.Needs(s)
}

// ensureStandardVerbs registers, refreshes or drops the generated verbs of
// a noun with a store: a row that fits is registered (or re-registered with
// the noun's current props); one that no longer fits is dropped; a verb the
// application registered under the row's name is left alone.
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
		if !v.fits(*n.Store) {
			n.Verbs = slices.DeleteFunc(n.Verbs, func(verb string) bool { return verb == v.Verb })
			continue
		}
		r.Register(v.operation(r, n))
		if !slices.Contains(n.Verbs, v.Verb) {
			n.Verbs = append(n.Verbs, v.Verb)
		}
	}
}

// operation is the row's operation on noun n.
func (v standardVerb) operation(r *Runtime, n *Noun) Operation {
	name := n.Name
	params := v.Params(n)
	op := Operation{Verb: v.Verb, Noun: name, Description: v.Description, Parameters: params, Result: "JSON value", HistoryKind: v.HistoryKind, Standard: v.Verb}
	if key := n.keyProp(); v.Subject && key != nil {
		p := subjectParameter(r, n, *key)
		if v.NoFocus {
			p.Focus = false
			p.Description = fmt.Sprintf("The %s's %s: this one alone (default: every %s)", r.DisplayNoun(n.Name), key.Name, r.DisplayNoun(n.Name))
		}
		params[key.Name] = p
		op.Positional = []string{key.Name}
	}
	if v.Rows {
		op.Rows = name
		op.Result = "rows"
	}
	run := v.Run
	op.App = func(c Ctx) Result {
		noun := c.Runtime.Nouns[name]
		if noun == nil || noun.Store == nil {
			return Failed(fmt.Errorf("%s has no store", name))
		}
		value, err := run(c.Runtime, noun, c)
		if err != nil {
			return Failed(err)
		}
		return Done(value)
	}
	return op
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
		"where": {Type: "string", Description: "prop=value pairs, comma- or space-separated: keep the things whose props equal every value"},
		"props": {Type: "string", Description: "The props each row holds besides id and label, comma-separated (default: every prop but secrets)"},
		"limit": {Type: "integer", Description: "At most this many rows (0: every row)"},
	}
	for name, p := range n.Store.ListParams {
		params[name] = p
	}
	return params
}

// subjectKey is the key a request's subject has: the key argument when
// given, else the instance the request acts for (Ctx.Instance, or the
// nearest instance of the noun above it): the key prop in its Data, else
// its id. A singleton's is "".
func (r *Runtime) subjectKey(n *Noun, c Ctx) (string, error) {
	key := n.keyProp()
	if key == nil {
		return "", nil
	}
	if v, ok := c.Args[key.Name]; ok && !emptyProp(v) {
		return fmt.Sprint(v), nil
	}
	inst := c.Instance
	if inst != nil && inst.Noun != n.Name {
		inst = r.Instances[r.nounInstance(n.Name, inst.ID)]
	}
	if inst == nil {
		return "", &ArgumentError{fmt.Errorf("%s is required: no %s is focused", key.Name, r.DisplayNoun(n.Name))}
	}
	if v := dataField(inst.Data, key.Name); !emptyProp(v) {
		return fmt.Sprint(v), nil
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
	q := ListQuery{Where: where, Limit: max(0, IntArg(c.Args, "limit", 0))}
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
		if !thingMatches(n, t, where) {
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
		if q.Limit > 0 && len(rows) == q.Limit {
			break
		}
	}
	return rows, nil
}

// thingMatches reports whether a thing's props equal every where value.
func thingMatches(n *Noun, t Thing, where map[string]any) bool {
	for name, want := range where {
		if !sameProp(*n.prop(name), t[name], want) {
			return false
		}
	}
	return true
}
