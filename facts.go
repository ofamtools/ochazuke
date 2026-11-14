package ochazuke

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
)

// System facts: nouns whose things are read from the registry (the modes,
// the operations, the themes ...). Each is a thing noun with a facts store
// (Store.Facts): list and get are the standard verbs over the registry's
// own data, and create, set, delete and archive are on the noun but
// unavailable, "a system fact". The props of a fact noun are the JSON
// fields of the struct it already answered with, so what a host read from
// the old list is still there, one row per thing.

// factsSpec declares one facts noun.
type factsSpec struct {
	Noun string
	// Sample is a value of the struct the things are read from: its json
	// tags are the props (embedded structs flattened, "-" skipped).
	Sample any
	// Key is the key prop's name, a field of Sample or one of Extra; it is
	// also the human key. Label is the prop that labels a row ("" none).
	Key, Label string
	// Extra are props the sample has no field for (a key made up for a
	// row that has none); they come first.
	Extra []PropSpec
	// Rename gives a field another prop name where its own is a verb of the
	// noun (a prop and a verb never share a word): json name to prop name.
	Rename map[string]string
	// Docs describes the props by name.
	Docs map[string]string
	// ListParams are list's parameters beside where, props and limit.
	ListParams map[string]Parameter
	// List answers the things, in order.
	List func(Ctx, ListQuery) ([]Thing, error)
	// Get answers the thing with that key; nil looks it up in List's
	// answer to no arguments.
	Get func(Ctx, string) (Thing, error)
	// KeyPrefix lets the default Get also find a thing by its key without
	// this prefix ("branch/" for branch/repo:topic); "" none.
	KeyPrefix string
	// Own declares the noun with DefineOwn (an application's own noun)
	// instead of Define; Remote marks the store Remote (List reads through
	// Ctx.Await).
	Own, Remote bool
	// Create makes a thing from the props create was given: a fact the
	// registry can be told (a mode name). When set, every prop is
	// create-only (create writes it, set does not), the key included: create
	// names the thing.
	Create func(Ctx, Thing) (Thing, error)
	// Patch writes the props named in Writable (each of them, and no
	// other, is writable): a fact the registry can be told to change. The
	// store's Patch runs the verb the fact belongs to (the mode noun's
	// current runs mode.switch), so set on the fact acts instead of
	// refusing (needPropWriter).
	Patch    func(Ctx, string, Thing) (Thing, error)
	Writable []string
}

// defineFacts declares the noun, its props and its facts store. The
// standard verbs name the noun as the user sees it, which reads the
// settings, so it runs after registerSettings (registerFactNouns declares
// the ones the earlier registrations own).
func (r *Runtime) defineFacts(f factsSpec) {
	var props []PropSpec
	props = append(props, f.Extra...)
	if f.Sample != nil {
		props = append(props, jsonProps(reflect.TypeOf(f.Sample), f.Docs)...)
	}
	for i := range props {
		if to, ok := f.Rename[props[i].Name]; ok {
			props[i].Name = to
		}
	}
	if len(f.Rename) > 0 {
		list := f.List
		f.List = func(c Ctx, q ListQuery) ([]Thing, error) {
			things, err := list(c, q)
			for _, t := range things {
				for from, to := range f.Rename {
					if v, ok := t[from]; ok {
						t[to] = v
						delete(t, from)
					}
				}
			}
			return things, err
		}
	}
	for i := range props {
		switch props[i].Name {
		case f.Key:
			props[i].Key, props[i].Human = true, true
		case f.Label:
			props[i].Role = RoleLabel
		}
		if props[i].Description == "" {
			props[i].Description = f.Docs[props[i].Name]
		}
		props[i].ReadOnly = true
		if f.Create != nil {
			props[i].ReadOnly, props[i].CreateOnly = false, true
		}
		if f.Patch != nil && slices.Contains(f.Writable, props[i].Name) {
			props[i].ReadOnly, props[i].CreateOnly = false, false
		}
	}
	// The key first, then the rest in declaration order.
	for i, p := range props {
		if p.Key {
			props = append([]PropSpec{p}, append(props[:i:i], props[i+1:]...)...)
			break
		}
	}
	keyName := f.Key
	get := f.Get
	if get == nil {
		get = func(c Ctx, key string) (Thing, error) {
			things, err := f.List(c, ListQuery{Args: Arguments{}})
			if err != nil {
				return nil, err
			}
			for _, t := range things {
				if id := fmt.Sprint(t[keyName]); id == key || f.KeyPrefix != "" && strings.TrimPrefix(id, f.KeyPrefix) == key {
					return t, nil
				}
			}
			return nil, c.Runtime.NotFound(f.Noun, key, factKeys(things, keyName))
		}
	}
	define := r.Define
	if f.Own {
		define = r.DefineOwn
	}
	define(f.Noun).Props(props...).Store(Store{
		Kind: KindCollection, Facts: true, Remote: f.Remote, ListParams: f.ListParams, List: f.List, Get: get,
		Create: f.Create, Patch: f.Patch, AcceptsKey: f.Create != nil,
	})
}

// factThings reads rows as things: each row's JSON fields (ThingOf), then
// set(i, thing) for the props the row has no field for.
func factThings[T any](rows []T, set func(i int, t Thing)) ([]Thing, error) {
	out := make([]Thing, 0, len(rows))
	for i, row := range rows {
		t, err := ThingOf(row)
		if err != nil {
			return nil, err
		}
		if set != nil {
			set(i, t)
		}
		out = append(out, t)
	}
	return out, nil
}

// jsonProps are the props a struct's json tags name, in field order:
// strings, booleans, integers and numbers as themselves, lists of strings
// as lists, everything else (a map, a nested struct, a list of them) as an
// object.
func jsonProps(t reflect.Type, docs map[string]string) []PropSpec {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	var out []PropSpec
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		if f.Anonymous && tag == "" && f.Type.Kind() == reflect.Struct {
			out = append(out, jsonProps(f.Type, docs)...)
			continue
		}
		if !f.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if name == "" {
			name = f.Name
		}
		p := PropSpec{Name: name}
		p.Type = propTypeOf(f.Type)
		p.Description = docs[name]
		out = append(out, p)
	}
	return out
}

// propTypeOf is the prop type that holds a Go type's JSON form.
func propTypeOf(t reflect.Type) string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == reflect.TypeOf(time.Duration(0)) {
		return SettingDuration
	}
	switch t.Kind() {
	case reflect.String:
		return SettingString
	case reflect.Bool:
		return SettingBoolean
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return SettingInteger
	case reflect.Float32, reflect.Float64:
		return SettingNumber
	case reflect.Slice:
		if t.Elem().Kind() == reflect.String {
			return SettingList
		}
	}
	return PropObject
}

// registerCoreFacts declares the operations and bindings nouns.
func registerCoreFacts(r *Runtime) {
	r.defineFacts(factsSpec{Noun: "ochazuke.operation", Sample: Operation{}, Key: "key", Label: "description",
		Extra: []PropSpec{{Name: "key"}, {Name: "noun"}, {Name: "verb"}, {Name: "name"}, {Name: "namespace"}},
		Docs: map[string]string{"key": "The operation's noun.verb", "noun": "The noun", "verb": "The verb", "name": "The operation's name (its verb)", "namespace": "The namespace that defined it", "description": "What the operation does",
			"parameters": "The arguments it takes, by name", "confirm": "The prompt it asks first, when it asks", "result": "What it answers",
			"unavailable": "Why it is on its noun but cannot run, when it cannot"},
		List: func(c Ctx, _ ListQuery) ([]Thing, error) {
			ops := c.Runtime.Catalog()
			return factThings(ops, func(i int, t Thing) { t["key"] = ops[i].Key.Short() })
		}})
	r.defineFacts(factsSpec{Noun: "ochazuke.binding", Sample: Binding{}, Key: "id", Extra: []PropSpec{{Name: "id"}}, Rename: map[string]string{"help": "helptext"},
		Docs:       map[string]string{"id": "The row's place in the list", "helptext": "Its help text, in place of the operation's description", "keys": "The keys that run it", "request": "The request it runs", "noun": "The noun whose table holds it", "mode": "The mode it answers in"},
		ListParams: map[string]Parameter{"all": {Type: "boolean", Description: "Return every stored binding row, including inactive modes and contexts, in configuration JSON format"}},
		List: func(c Ctx, q ListQuery) ([]Thing, error) {
			rows := c.Runtime.ListBindings(focusOf(c.Context))
			if all, _ := q.Args["all"].(bool); all {
				rows = c.Runtime.allBindings()
			}
			return factThings(rows, func(i int, t Thing) { t["id"] = strconv.Itoa(i + 1) })
		}})
}

// registerFactNouns declares the facts nouns whose registrations run before
// the settings exist.
func registerFactNouns(r *Runtime) {
	registerCoreFacts(r)
	registerModeFacts(r)
	registerAssociations(r)
	registerReferrers(r)
	registerRelationKinds(r)
	registerActionFacts(r)
	registerStateFacts(r)
	registerJobFacts(r)
	registerRegisterThings(r)
}

// stateProps are the state noun's props: the snapshot's fields
// (Runtime.Snapshot), each read-only.
var stateProps = []PropSpec{
	{Name: "page", Parameter: Parameter{Description: "The current page's id"}},
	{Name: "pages", Parameter: Parameter{Type: PropObject, Description: "The registered pages by id"}},
	{Name: "focus", Parameter: Parameter{Description: "The focused instance's id"}},
	{Name: "context", Parameter: Parameter{Type: PropObject, Description: "The focus path, page and mode"}},
	{Name: "mode", Parameter: Parameter{Description: "The interaction mode's name; set it to switch modes (any name: one nobody registered becomes a mode)", Noun: "ochazuke.mode", Free: true}},
	{Name: "modeState", Parameter: Parameter{Type: PropObject, Description: "The mode with its payload (count, operator, prefix)"}},
	{Name: "modes", Parameter: Parameter{Type: SettingList, Description: "Every mode's name"}},
	{Name: "modals", Parameter: Parameter{Type: SettingList, Description: "The open modals, bottom first"}},
	{Name: "pending", Parameter: Parameter{Type: PropObject, Description: "The pending key sequence"}},
	{Name: "quit", Parameter: Parameter{Type: SettingBoolean, Description: "Whether the application is quitting"}},
	{Name: "bindings", Parameter: Parameter{Type: PropObject, Description: "The rows the focus answers to"}},
	{Name: "inputs", Parameter: Parameter{Type: PropObject, Description: "The inputs by id: text, cursor, mode"}},
	{Name: "lists", Parameter: Parameter{Type: PropObject, Description: "The lists by id: rows, selection, filter"}},
	{Name: "registers", Parameter: Parameter{Type: PropObject, Description: "The named registers"}},
	{Name: "yankHistory", Parameter: Parameter{Type: PropObject, Description: "The yank history, newest first"}},
	{Name: "activeRegister", Parameter: Parameter{Description: "The register the next yank, delete or paste uses"}},
	{Name: "macro", Parameter: Parameter{Type: PropObject, Description: "The macro being recorded or played"}},
	{Name: "nouns", Parameter: Parameter{Type: PropObject, Description: "The nouns by name: verbs, bindings, actions, pin"}},
	{Name: "instances", Parameter: Parameter{Type: PropObject, Description: "The live instances by id: noun, parent, payload"}},
	{Name: "components", Parameter: Parameter{Type: PropObject, Description: "The components by instance id"}},
	{Name: "jobs", Parameter: Parameter{Type: PropObject, Description: "The running jobs (job.list lists the finished ones too)"}},
	{Name: "history", Parameter: Parameter{Type: PropObject, Description: "The last 50 actions and changes (history-entry.list reads more)"}},
}

// registerStateFacts declares the state noun: a read-only singleton (a
// system fact) whose props are the public state, so get {prop} reads one
// field and a dotted path reads inside it (state get
// instances.editor.noun), as the JSON Pointer of the old snapshot.get did.
// stateWritable are the snapshot props set writes: mode, through
// mode.switch's request (snapshot set mode=nested, snapshot mode nested),
// any name.
var stateWritable = []string{"mode"}

func registerStateFacts(r *Runtime) {
	props := slices.Clone(stateProps)
	for i := range props {
		props[i].ReadOnly = !slices.Contains(stateWritable, props[i].Name)
	}
	// Declared facts: a system fact whose one writable prop is mode, so
	// its writing verb being available does not make it a plain singleton.
	n := r.Define("ochazuke.snapshot").Kind(NounKindFacts).Props(props...)
	n.Store(Store{
		Kind: Singleton, Facts: true,
		Patch: func(c Ctx, _ string, t Thing) (Thing, error) {
			if mode, ok := t["mode"].(string); ok {
				if _, err := c.Runtime.switchMode(mode, c.Runtime.Input(c.Context.InputID)); err != nil {
					return nil, err
				}
			}
			return Thing{"mode": c.Runtime.Mode.Name}, nil
		},
		// The snapshot in its JSON form, as the old JSON Pointer read it:
		// what a CLI or an agent sees, never a live Go value.
		Get: func(c Ctx, _ string) (Thing, error) {
			data, err := json.Marshal(c.Runtime.Snapshot())
			if err != nil {
				return nil, err
			}
			var t Thing
			return t, json.Unmarshal(data, &t)
		},
	})
	// get {lists}: the rows of every list of the program's own nouns in one
	// call, as JSON Lines tagged by noun. It is the framework's state source
	// (ResultLists): what a tool's own "every list's rows" verb answered
	// for the nouns that have a store. Without lists, get is the standard one.
	params := getParams(r.Nouns["ochazuke.snapshot"])
	params["lists"] = Parameter{Type: "boolean", Description: "Answer the rows of every list of the program's nouns that have a store, as JSON Lines {noun, id, label, data} (the state source), instead of the snapshot"}
	r.Register(Operation{Key: "ochazuke.snapshot.get", Description: "Read the runtime snapshot or one field of it; with lists, the rows of every list in one call", Parameters: params,
		Result: "JSON value", HistoryKind: HistoryFetch, ShadowsStandard: true, Agent: AgentAllow,
		AppHandler: func(c Ctx) Result {
			noun := c.Runtime.Nouns["ochazuke.snapshot"]
			if on, _ := c.Args["lists"].(bool); on {
				rows, err := c.Runtime.listRows(c)
				if err != nil {
					return Failed(err)
				}
				return Done(rows)
			}
			v, err := runGet(c.Runtime, noun, c)
			if err != nil {
				return Failed(err)
			}
			return Done(v)
		}})
}

// listRows are the rows of every list of the program's own nouns: each
// noun with a local store that lists (not the framework's, not a system fact, not one whose
// things live in another program), its things as the standard list shows
// them, one JSON object per row {noun, id, label, data}, in noun order.
func (r *Runtime) listRows(c Ctx) (Lines, error) {
	out := Lines{}
	for _, name := range slices.Sorted(maps.Keys(r.Nouns)) {
		n := r.Nouns[name]
		if n.Store == nil || n.Store.List == nil || n.Store.Facts || n.Store.Remote || n.Store.Kind != KindCollection {
			continue
		}
		if list, ok := r.Operations.Find(KeyOf(name, "list")); !ok || list.Builtin || list.Unavailable != "" {
			continue // the framework's own noun, or one that cannot list
		}
		q, err := r.listQueryOf(n, Arguments{})
		if err != nil {
			return nil, err
		}
		page, err := r.queryThings(c, n, q)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", r.DisplayNoun(name), err)
		}
		key := n.keyProp()
		for _, t := range page.Items {
			row := map[string]any{"noun": name, "data": thingRecord(n, t, nil)}
			if key != nil {
				row["id"] = fmt.Sprint(t[key.Name])
			}
			if label := n.displayLabel(func(p string) any { return t[p] }); label != "" {
				row["label"] = label
			}
			out = append(out, row)
		}
	}
	return out, nil
}

// registerJobFacts declares the job noun: the off-loop jobs, running and
// finished (done, failed or cancelled, with the error), kept after they
// end up to jobHistory finished ones; job.cancel stops a running one.
func registerJobFacts(r *Runtime) {
	r.defineFacts(factsSpec{Noun: "ochazuke.job", Sample: JobInfo{}, Key: "job", Label: "label",
		Docs: map[string]string{"job": "The job's number", "owner": "The instance that started it", "verb": "The verb it runs", "label": "What it says it does",
			"slot": "Its replacing slot, when it has one", "scope": "The scope of its slot", "started": "When it started",
			"status": "running, done, failed or cancelled", "error": "Why it failed, when it did"},
		List: func(c Ctx, _ ListQuery) ([]Thing, error) {
			return factThings(c.Runtime.Jobs(), nil)
		}})
}

// factKeys are the keys of things, for a not-found error's did-you-mean.
func factKeys(things []Thing, keyName string) []string {
	out := make([]string, 0, len(things))
	for _, t := range things {
		out = append(out, fmt.Sprint(t[keyName]))
	}
	return out
}
