package ochazuke

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
)

// Thing keys. Every thing noun has a primary key (PropSpec.Key) and may
// have a human key (PropSpec.Human: a name, a slug). A request names its
// subject by either: subjectKey reads the key argument through keyResolvers,
// or reads the row the request acts on through rowKeyReaders. Both are
// tables walked in order, so a new way to name a thing is a row.

// humanProp is the noun's human key: the prop marked Human, else the prop
// with the label role; nil when it has neither.
func (n *Noun) humanProp() *PropSpec {
	if n == nil {
		return nil
	}
	for i := range n.Props {
		if n.Props[i].Human {
			return &n.Props[i]
		}
	}
	return n.roleProp(RoleLabel)
}

// roleNames are the "@role" names that name a prop of the noun, in
// declaration order (@label, @owner, @archive ...).
func (n *Noun) roleNames() []string {
	var out []string
	for _, p := range n.Props {
		if p.Role != "" {
			out = append(out, "@"+p.Role)
		}
	}
	return out
}

// keyResolver is one way a target text names a thing: find answers the keys
// of the things the text names this way (none: the text does not).
type keyResolver struct {
	name string
	find func(n *Noun, c Ctx, text string) ([]string, error)
}

// keyResolvers are the ways a target names a thing, tried in order: the
// first that finds any decides. An exact primary key is first, then an
// exact human key. Neither takes a prefix: t1 never names t10.
var keyResolvers = []keyResolver{
	{"key", keyFinder},
	{"human key", humanFinder},
}

// keyFinder: the text is a key when the store has a thing under it.
func keyFinder(n *Noun, c Ctx, text string) ([]string, error) {
	t, err := n.Store.Get(c, text)
	if err != nil || t == nil {
		return nil, nil // not found here: the next resolver, or the store's own error
	}
	return []string{text}, nil
}

// humanFinder: the text is a human key when a thing holds it exactly in
// its human prop. A store that cannot list has no human keys to find.
func humanFinder(n *Noun, c Ctx, text string) ([]string, error) {
	human, key := n.humanProp(), n.keyProp()
	if human == nil || human == key || human.Secret || n.Store.List == nil {
		return nil, nil
	}
	things, err := n.Store.List(c, ListQuery{Filters: whereFilters(map[string]any{human.Name: text})})
	if err != nil {
		return nil, err
	}
	var keys []string
	for _, t := range things {
		if v, ok := t[human.Name]; ok && fmt.Sprint(v) == text {
			keys = append(keys, fmt.Sprint(t[key.Name]))
		}
	}
	return keys, nil
}

// resolvable reports whether a noun's targets go through keyResolvers: its
// store reads things by key and its keys are its own (an instances store's
// are runtime ids).
func (n *Noun) resolvable() bool {
	return n.Store != nil && n.Store.Get != nil && !n.Store.instanceKeyed && n.keyProp() != nil
}

// resolveTarget is the key a target text names (memoized for the request
// that reads it: Runtime.targets). A text no resolver finds is
// returned as it is, for the store to answer (its own not-found error); a
// human key more than one thing holds refuses, naming the candidates.
func (r *Runtime) resolveTarget(n *Noun, c Ctx, text string) (string, error) {
	if !n.resolvable() {
		return text, nil
	}
	memo := n.Name + "\x00" + text
	if key, ok := r.targets[memo]; ok {
		return key, nil
	}
	key, err := r.findTarget(n, c, text)
	if err == nil {
		if r.targets == nil {
			r.targets = map[string]string{}
		}
		r.targets[memo] = key // one probe per request, however often the subject is read
	}
	return key, err
}

// knownTarget records that text is the key itself for the request under
// way (resolveTarget's memo), so the subject is not probed again: a caller
// holding a thing's key (a list row's) names it exactly.
func (r *Runtime) knownTarget(noun, key string) {
	if r.targets == nil {
		r.targets = map[string]string{}
	}
	r.targets[noun+"\x00"+key] = key
}

// findTarget walks keyResolvers for a target text.
func (r *Runtime) findTarget(n *Noun, c Ctx, text string) (string, error) {
	for _, res := range keyResolvers {
		keys, err := res.find(n, c, text)
		switch {
		case errors.Is(err, ErrNotLoaded):
			continue // a remote store's answer is not known: the text stands
		case err != nil:
			return "", err
		case len(keys) == 1:
			return keys[0], nil
		case len(keys) > 1:
			return "", &ArgumentError{fmt.Errorf("%s %q names %d %s: use one id (%s)", res.name, text, len(keys), r.DisplayNoun(n.Name)+"s", candidates(keys))}
		}
	}
	return text, nil
}

// namesThingExactly reports whether a word names one of a noun's things
// exactly, by a keyResolver (its key, or a human key one thing holds),
// whether or not the thing is in the noun's list (an archived one is
// not). The palette reads such a word as typed before it reads it as a
// prefix of other things' keys: an archived t1 beside live t10 and t11 is
// t1, never ambiguous and never t10. A remote store is not probed while a
// line is read (its answer comes later): its word reads as before.
func (r *Runtime) namesThingExactly(noun string, c Context, word string) bool {
	n := r.Noun(noun)
	if word == "" || n == nil || !n.resolvable() || n.Store.Remote {
		return false
	}
	ctx := Ctx{actor: r.Actor(), Runtime: r, Context: c}
	for _, res := range keyResolvers {
		if keys, err := res.find(n, ctx, word); err == nil && len(keys) == 1 {
			return true
		}
	}
	return false
}

// candidates lists keys for a refusal, the first eight.
func candidates(keys []string) string {
	const most = 8
	if len(keys) > most {
		return strings.Join(keys[:most], ", ") + ", ..."
	}
	return strings.Join(keys, ", ")
}

// rowKeyReaders are where a row keeps its thing's key, in order: an
// instances noun's rows are the things (their id); else the key prop in the
// row (Data, then the ListItem.Props of a row whose Data is an opaque
// handle: rowValue); else a Data that is the key itself (a plain string, as
// the Settings page's rows are). A row that has none is an error: a row's
// instance id is never taken for its thing's key.
var rowKeyReaders = []func(r *Runtime, n *Noun, row *Instance, key string) any{
	func(_ *Runtime, n *Noun, row *Instance, _ string) any {
		if n.Store != nil && n.Store.instanceKeyed {
			return row.ID
		}
		return nil
	},
	func(r *Runtime, _ *Noun, row *Instance, key string) any { return r.rowValue(row, key) },
	func(_ *Runtime, _ *Noun, row *Instance, _ string) any {
		s, _ := row.Data.(string)
		return s
	},
}

// rowKey is the key of the thing a row stands for.
func (r *Runtime) rowKey(n *Noun, row *Instance) (string, error) {
	key := n.keyProp()
	for _, read := range rowKeyReaders {
		if v := read(r, n, row, key.Name); !emptyProp(v) {
			return fmt.Sprint(v), nil
		}
	}
	return "", &ArgumentError{fmt.Errorf("the %s row %q holds no %s: put it in the row's Data or Props", r.DisplayNoun(n.Name), row.ID, key.Name)}
}

// subjectRow is the instance a request without a key argument acts for: the
// context's instance, or the nearest instance of the noun above it (for an
// instances noun, the nearest live thing); nil when there is none.
func (r *Runtime) subjectRow(n *Noun, c Ctx) *Instance {
	inst := c.Instance
	switch {
	case inst == nil:
		return nil
	case n.Store != nil && n.Store.instanceKeyed:
		return r.Instances[r.liveSubject(n, inst.ID)] // its things are live instances
	case inst.Noun != n.Name:
		return r.Instances[r.nounInstance(n.Name, inst.ID)]
	}
	return inst
}

// subjectKey is the key a request's subject has: the key argument when
// given, read as an id or a human key (resolveTarget), else the row the
// request acts for (subjectRow, rowKey). A singleton's is "".
func (r *Runtime) subjectKey(n *Noun, c Ctx) (string, error) {
	key := n.keyProp()
	if key == nil {
		return "", nil
	}
	if v, ok := c.Args[key.Name]; ok && !emptyProp(v) {
		return r.resolveTarget(n, c, fmt.Sprint(v))
	}
	row := r.subjectRow(n, c)
	if row == nil {
		return "", &ArgumentError{&MissingParameterError{Param: key.Name, Reason: "no " + r.DisplayNoun(n.Name) + " is focused"}}
	}
	return r.rowKey(n, row)
}

var durationType = reflect.TypeOf(time.Duration(0))

// Into reads the thing into a struct (dst, a pointer to one) by its json
// tags: each prop lands in the field named for it, in the type the field
// has. It is ThingOf's inverse, so a value of any prop type round-trips:
// strings, integers, numbers, booleans, lists, objects, and durations (a
// time.Duration field takes a Duration, its text "1m30s" or a number of
// nanoseconds, as ThingOf's JSON encoding gives it). Props the struct has
// no field for are ignored.
func (t Thing) Into(dst any) error {
	v := reflect.ValueOf(dst)
	if v.Kind() != reflect.Pointer || v.IsNil() || v.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("thing: Into needs a pointer to a struct, not %T", dst)
	}
	fields := jsonFieldTypes(v.Elem().Type())
	form := make(map[string]any, len(t))
	for name, value := range t {
		if fields[strings.ToLower(name)] == durationType {
			d, err := durationValue(value)
			if err != nil {
				return fmt.Errorf("thing: %s: %w", name, err)
			}
			value = int64(d)
		}
		form[name] = fileForm(value)
	}
	raw, err := json.Marshal(form)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, dst)
}

// jsonFieldTypes maps a struct's lower-cased JSON field names to their types.
func jsonFieldTypes(typ reflect.Type) map[string]reflect.Type {
	out := map[string]reflect.Type{}
	for i := range typ.NumField() {
		f := typ.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" || !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name
		}
		out[strings.ToLower(name)] = f.Type
	}
	return out
}

// durationValue reads a duration prop value: a Duration, its text, or a
// number of nanoseconds.
func durationValue(v any) (time.Duration, error) {
	switch d := v.(type) {
	case time.Duration:
		return d, nil
	case string:
		return time.ParseDuration(d)
	case int:
		return time.Duration(d), nil
	case int64:
		return time.Duration(d), nil
	case float64:
		return time.Duration(d), nil
	case json.Number:
		n, err := d.Int64()
		return time.Duration(n), err
	}
	return 0, fmt.Errorf("%T is not a duration", v)
}

// checkHumanKey checks a noun's Human prop: at most one, and never secret
// (a secret prop must not be searched or named in a refusal).
func checkHumanKey(n *Noun) error {
	var found *PropSpec
	for i, p := range n.Props {
		switch {
		case !p.Human:
		case found != nil:
			return fmt.Errorf("%s: props %s and %s are both the Human key; a noun has one", n.Name, found.Name, p.Name)
		case p.Secret:
			return fmt.Errorf("%s: the Human key %s is a secret prop", n.Name, p.Name)
		default:
			found = &n.Props[i]
		}
	}
	return nil
}

// The base props' roles: the time a thing was made and last written.
const (
	RoleCreated = "created"
	RoleUpdated = "updated"
)

// baseProps are the props a thing noun has beside its key, human key and
// archive prop: read-only times, as RFC 3339 text. Every thing noun has
// them but a facts store's (Store.Facts). A noun that already names a prop (or a role) of its own keeps its
// own, and the framework stamps that one.
var baseProps = []PropSpec{
	{Name: RoleCreated, Role: RoleCreated, ReadOnly: true, Parameter: Parameter{Description: "When the thing was made (RFC 3339, UTC)"}},
	{Name: RoleUpdated, Role: RoleUpdated, ReadOnly: true, Parameter: Parameter{Description: "When the thing was last written (RFC 3339, UTC)"}},
}

// addBaseProps declares baseProps on a thing noun with keys of its own: not
// a facts store's, a singleton's or an instances noun's, and not one whose
// store neither writes things (Create, Patch) nor says it keeps its own
// times (Stamps): nothing would ever fill the props.
func (r *Runtime) addBaseProps(n *Noun) {
	s := n.Store
	if s.Facts || n.keyProp() == nil || s.instanceKeyed || s.Create == nil && s.Patch == nil && !s.Stamps {
		return
	}
	for _, p := range baseProps {
		if n.prop(p.Name) != nil || n.roleProp(p.Role) != nil {
			continue
		}
		if err := r.addProp(n, p); err != nil {
			panic(err.Error())
		}
	}
}

// stamp writes the current time into the props that have the roles, before
// the store sees the write; only a store that is not facts and does not declare Stamps
// is stamped for, and a prop that is not text is left alone.
func (n *Noun) stamp(t Thing, roles ...string) {
	if n.Store.Facts || n.Store.Stamps {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, role := range roles {
		if p := n.roleProp(role); p != nil && p.Type == SettingString {
			t[p.Name] = now
		}
	}
}
