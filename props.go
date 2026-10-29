package ochazuke

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Props and stores: the record side of a noun. A noun declares its props
// (NounBuilder.Props: typed fields, as data) and one store row
// (NounBuilder.Store: the handlers that read its things), and the runtime
// gives it the standard verbs the store supports (standard_verbs.go): in
// this phase get, list and watch (standard_watch.go).
//
// After a noun a word is a verb or a prop, never both: a prop may not take
// a verb's name on its noun (nor a standard verb's, whether or not its
// noun has it), and a verb registered later may not take a prop's.
//
// A thing is a Thing, its props by name. Its key is the value of the one
// prop marked Key (a collection's or an instances noun's; a singleton has
// none), and the standard verbs' subject parameter is named after that
// prop (schedule's is name), never a generic id. A Key prop is read-only.
//
// An instance stands for a thing when it is an instance of the noun: its
// key is the key prop in its Data record, else the instance id. The rows a
// list answers carry id (the key, as text) and label (the label prop, else
// its Fallback), so hosts, choosers and the palette show them.

// PropObject is the prop type of a JSON object (map[string]any). The other
// prop types are the setting types (SettingString, SettingInteger,
// SettingNumber, SettingBoolean, SettingDuration, SettingList), parsed the
// same way: a word such as "3", "true" or "400ms", a JSON array or object.
const PropObject = "object"

// The kinds of store (Store.Kind): what the subject is and which standard
// verbs make sense.
const (
	// Collection is many things with keys, held by the application
	// (note, task, project).
	Collection = "collection"
	// Singleton is one thing per runtime (app, appearance): get takes no
	// subject.
	Singleton = "singleton"
	// Instances is things that are live runtime instances (pages, panes);
	// the subject is an instance id. In this phase its store reads them
	// like a collection's.
	Instances = "instances"
)

// The prop roles (PropSpec.Role): one prop per role.
const (
	// RoleLabel is the prop a thing is shown by: list rows' label,
	// {{label}} in a confirm or an action template, and choosers.
	RoleLabel = "label"
	// RoleOwner is the reference prop naming the thing that holds this one.
	RoleOwner = "owner"
)

// PropSpec is one field of a noun's things. Parameter carries the type,
// Required, Enum, Source, Noun and Description, as a verb parameter does,
// so the palette, CLI help and forms read it unchanged; Type is a setting
// type or PropObject ("" is string). A prop whose Parameter.Noun is set is
// a reference: its value is the key of one of that noun's things.
//
// ReadOnly props are never written by set (a Key prop always is);
// CreateOnly ones are written by create only. Role gives the prop a part
// (RoleLabel, RoleOwner). Fallback names another prop of the noun shown in
// this prop's place when this one is empty: display only (list rows,
// {{label}}, choosers); get still reads the prop itself. Default is what
// create writes when the value is omitted. A Secret prop is left out of
// get's and list's records unless asked for by name, and its writes are
// never repeated or persisted. Multiline asks a form for a text area.
// When names a guard: where it is false (read where the request acts) the
// prop is not offered, read or written (the catalog's availability); the
// guard is the prop's own, apart from the noun's verb guards.
type PropSpec struct {
	Name string `json:"name"`
	Parameter
	ReadOnly   bool   `json:"readOnly,omitempty"`
	CreateOnly bool   `json:"createOnly,omitempty"`
	Key        bool   `json:"key,omitempty"`
	Role       string `json:"role,omitempty"`
	Fallback   string `json:"fallback,omitempty"`
	Default    any    `json:"default,omitempty"`
	Secret     bool   `json:"secret,omitempty"`
	Multiline  bool   `json:"multiline,omitempty"`
	When       string `json:"availability,omitempty"`
}

// PropField is a prop as a form field (RecordField): its name, its
// parameter and whether it is multiline, so a record editor or an args
// form over a noun's things reads its props.
func PropField(p PropSpec) RecordField {
	return RecordField{Name: p.Name, Parameter: p.Parameter, Multiline: p.Multiline}
}

// writable reports whether set may write the prop: not read-only (a key
// never is), not create-only.
func (p PropSpec) writable() bool { return !p.ReadOnly && !p.Key && !p.CreateOnly }

// simple reports whether a write of the prop holds a simple value (owner
// decision 12): an enum choice, a boolean, a number or a duration, never
// text, a list, an object or a secret. Only simple writes repeat with .
// and reach the history file (simpleWrite).
func (p PropSpec) simple() bool {
	if p.Secret || p.Multiline {
		return false
	}
	switch p.Type {
	case SettingBoolean, SettingInteger, SettingNumber, SettingDuration:
		return true
	case SettingString, "":
		return len(p.Enum) > 0 && p.Noun == ""
	}
	return false
}

// Thing is one thing of a noun: its props by name.
type Thing map[string]any

// ThingOf reads a value as a Thing: a map as it is (a copy), a Record's
// strings, or a struct through its JSON encoding (its json tags name the
// props).
func ThingOf(v any) (Thing, error) {
	switch t := v.(type) {
	case nil:
		return nil, errors.New("no thing")
	case Thing:
		return maps.Clone(t), nil
	case map[string]any:
		return Thing(maps.Clone(t)), nil
	case map[string]string:
		out := make(Thing, len(t))
		for k, v := range t {
			out[k] = v
		}
		return out, nil
	case Record:
		out := make(Thing, len(t))
		for k, v := range t {
			out[k] = v
		}
		return out, nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out Thing
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("thing: %T is not an object", v)
	}
	return out, nil
}

// ListQuery is what list hands its store: the where pairs (prop to typed
// value, equality, all of them), the limit (0 for none) and the store's
// own ListParams the request gave. A store may use them to read less; the
// runtime applies where and limit to the answer either way, so a store
// that ignores them is still right.
type ListQuery struct {
	Where map[string]any
	Limit int
	Args  Arguments
}

// Store is a noun's row in the handler table: its kind and the handlers
// that read and write its things, each optional. A standard verb exists on
// the noun exactly when its handler is set (standardVerbs' Needs). Get
// answers the thing with that key ("" for a singleton); List answers the
// things. ListParams are list's parameters beside where, props and limit.
//
// Create stores a new thing from the props given (typed, each prop's
// Default filling an omitted one) and answers it, its key filled. Patch
// writes the changed props of the thing with that key and answers the
// thing after the write (nil: the runtime reads it back with Get). Delete
// removes the thing with that key.
//
// Confirm is a standard verb's confirm prompt by verb ({{label}} is the
// thing's label prop, else its Fallback); delete asks
// DefaultDeleteConfirm unless the store names its own ("" asks nothing).
// When is a standard verb's availability by verb: a guard name, set on
// the generated verb as SetAvailability does.
//
// Watch, when set, answers watch itself (standard_watch.go): it pushes
// each event to emit, from any goroutine, until stop; without it watch
// lists again (List, a singleton's Get) whenever things may have changed.
type Store struct {
	Kind       string
	Get        func(Ctx, string) (Thing, error)
	List       func(Ctx, ListQuery) ([]Thing, error)
	Create     func(Ctx, Thing) (Thing, error)
	Patch      func(Ctx, string, Thing) (Thing, error)
	Delete     func(Ctx, string) error
	ListParams map[string]Parameter
	Watch      func(c Ctx, q WatchQuery, emit func(WatchEvent)) (stop func(), err error)
	Confirm    map[string]string
	When       map[string]string
	// Undo lets history.undo revert this store's standard changes
	// (standard_undo.go); AcceptsKey says Create keeps a key the values
	// give, so a delete can be undone under its old key.
	Undo       bool
	AcceptsKey bool
}

// propTypes are the types a prop declares.
var propTypes = map[string]bool{
	SettingString: true, SettingInteger: true, SettingNumber: true, SettingBoolean: true,
	SettingDuration: true, SettingList: true, PropObject: true,
}

// prop is the noun's prop of that name, nil when it has none.
func (n *Noun) prop(name string) *PropSpec {
	if n == nil {
		return nil
	}
	for i := range n.Props {
		if n.Props[i].Name == name {
			return &n.Props[i]
		}
	}
	return nil
}

// keyProp is the noun's Key prop, nil when it has none (a singleton).
func (n *Noun) keyProp() *PropSpec {
	for i := range n.Props {
		if n.Props[i].Key {
			return &n.Props[i]
		}
	}
	return nil
}

// roleProp is the noun's prop with that role, nil when none has it.
func (n *Noun) roleProp(role string) *PropSpec {
	if n == nil {
		return nil
	}
	for i := range n.Props {
		if n.Props[i].Role == role {
			return &n.Props[i]
		}
	}
	return nil
}

// StoreKind is the kind of the noun's store (Collection, Singleton,
// Instances), "" when it declares none.
func (n *Noun) StoreKind() string {
	if n == nil || n.Store == nil {
		return ""
	}
	return n.Store.Kind
}

// Props declares props of the noun (see PropSpec), in order. It panics on a
// bad declaration: a name that is not one word, a standard verb's name, a
// name the noun's verbs or a nested noun already take, a second prop of
// that name, an unknown type, or a bad role. Declared after Store, the
// standard verbs follow the new props.
func (b *NounBuilder) Props(props ...PropSpec) *NounBuilder {
	for _, p := range props {
		if err := b.r.addProp(b.n, p); err != nil {
			panic(err.Error())
		}
	}
	if b.n.Store != nil {
		b.r.applyStore(b.n)
	}
	b.last = ""
	return b
}

// Store declares the noun's store row and registers the standard verbs it
// supports (standard_verbs.go). Props come first. It panics when the noun
// has no props, the kind is unknown, the key props do not fit the kind
// (one Key prop for a collection or instances, none for a singleton), or
// a role, fallback or list parameter is wrong.
func (b *NounBuilder) Store(s Store) *NounBuilder {
	if len(b.n.Props) == 0 {
		panic(fmt.Sprintf("%s: declare Props before Store", b.n.Name))
	}
	if s.Kind == "" {
		s.Kind = Collection
	}
	s.ListParams = cloneParams(s.ListParams)
	s.Confirm, s.When = maps.Clone(s.Confirm), maps.Clone(s.When)
	b.n.Store = &s
	b.r.applyStore(b.n)
	b.last = ""
	return b
}

func cloneParams(in map[string]Parameter) map[string]Parameter {
	if in == nil {
		return nil
	}
	out := make(map[string]Parameter, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// applyStore checks the noun's props against its store and registers the
// standard verbs; it panics on a bad schema.
func (r *Runtime) applyStore(n *Noun) {
	if err := checkSchema(n); err != nil {
		panic(err.Error())
	}
	r.ensureStandardVerbs(n)
}

// Props lists a noun's props in declaration order (a copy), nil for an
// unknown noun or one without props.
func (r *Runtime) Props(noun string) []PropSpec {
	if n := r.Noun(noun); n != nil {
		return slices.Clone(n.Props)
	}
	return nil
}

// PropOf is noun's prop of that name.
func (r *Runtime) PropOf(noun, name string) (PropSpec, bool) {
	if p := r.Noun(noun).prop(name); p != nil {
		return *p, true
	}
	return PropSpec{}, false
}

// addProp checks and records one prop of n.
func (r *Runtime) addProp(n *Noun, p PropSpec) error {
	key := n.Name + "." + p.Name
	switch {
	case p.Name == "" || strings.ContainsAny(p.Name, " \t\n=\".,") || strings.HasPrefix(p.Name, "-"):
		return fmt.Errorf("prop %q of %s must be one word without a dot", p.Name, n.Name)
	case slices.Contains(reservedStandardVerbs, p.Name):
		return fmt.Errorf("prop %s of %s takes a standard verb's name", p.Name, n.Name)
	case n.prop(p.Name) != nil:
		return fmt.Errorf("prop %s is already declared", key)
	case r.Operations[key].Noun != "" && r.Operations[key].Standard == "":
		return fmt.Errorf("prop %s clashes with the verb %s: a word after a noun is a verb or a prop, never both", p.Name, key)
	case r.Nouns[key] != nil:
		return fmt.Errorf("prop %s clashes with the noun %s nested under %s", p.Name, key, n.Name)
	}
	if p.Type == "" {
		p.Type = SettingString
	}
	if !propTypes[p.Type] {
		return fmt.Errorf("prop %s: unknown type %q", key, p.Type)
	}
	if p.Noun != "" {
		if p.Type != SettingString {
			return fmt.Errorf("prop %s: a reference (noun %s) holds a key, type string", key, p.Noun)
		}
		p.Noun = r.CanonicalNoun(p.Noun)
	}
	if len(p.Enum) > 0 && p.Type != SettingString {
		return fmt.Errorf("prop %s: only a string prop takes an enum", key)
	}
	switch p.Role {
	case "", RoleLabel:
	case RoleOwner:
		if p.Noun == "" {
			return fmt.Errorf("prop %s: the owner role needs a reference (Noun)", key)
		}
	default:
		return fmt.Errorf("prop %s: unknown role %q", key, p.Role)
	}
	if p.Key {
		if p.Type != SettingString && p.Type != SettingInteger {
			return fmt.Errorf("prop %s: a key is a string or an integer, not %s", key, p.Type)
		}
		if slices.Contains(standardParamNames, p.Name) {
			return fmt.Errorf("prop %s: a key may not take the name %s, a standard verb's parameter", key, p.Name)
		}
		p.ReadOnly = true // a key is the thing's identity: never written by set
	}
	p.Enum = slices.Clone(p.Enum)
	n.Props = append(n.Props, p)
	return nil
}

// checkSchema checks what holds across props and the store: one prop per
// role, fallbacks naming another prop, the key props the kind needs, the
// row fields (a prop named id is the key, one named label the label), and
// list parameters that take no prop's or standard parameter's name.
func checkSchema(n *Noun) error {
	roles := map[string]string{}
	keys := 0
	for _, p := range n.Props {
		if p.Role != "" {
			if other, ok := roles[p.Role]; ok {
				return fmt.Errorf("%s: props %s and %s both take the role %s", n.Name, other, p.Name, p.Role)
			}
			roles[p.Role] = p.Name
		}
		if p.Key {
			keys++
		}
		if p.Fallback != "" && (p.Fallback == p.Name || n.prop(p.Fallback) == nil) {
			return fmt.Errorf("prop %s.%s: fallback %q is not another prop of %s", n.Name, p.Name, p.Fallback, n.Name)
		}
		if p.Name == "id" && !p.Key {
			return fmt.Errorf("prop %s.id: a prop named id is the key (list rows carry the key as id)", n.Name)
		}
		if p.Name == "label" && p.Role != RoleLabel {
			return fmt.Errorf("prop %s.label: a prop named label has the label role (list rows carry the label as label)", n.Name)
		}
	}
	s := n.Store
	if s == nil {
		return nil
	}
	switch s.Kind {
	case Collection, Instances:
		if keys != 1 {
			return fmt.Errorf("%s: a %s store needs exactly one Key prop, not %d", n.Name, s.Kind, keys)
		}
	case Singleton:
		if keys != 0 {
			return fmt.Errorf("%s: a singleton has no Key prop", n.Name)
		}
	default:
		return fmt.Errorf("%s: unknown store kind %q", n.Name, s.Kind)
	}
	for _, m := range []struct {
		field string
		by    map[string]string
	}{{"Confirm", s.Confirm}, {"When", s.When}} {
		for verb := range m.by {
			if !slices.Contains(reservedStandardVerbs, verb) {
				return fmt.Errorf("%s: store %s names %q, not a standard verb", n.Name, m.field, verb)
			}
		}
	}
	key := n.keyProp()
	for name := range s.ListParams {
		switch {
		case slices.Contains(standardParamNames, name):
			return fmt.Errorf("%s: list parameter %s takes a standard parameter's name", n.Name, name)
		case key != nil && name == key.Name:
			return fmt.Errorf("%s: list parameter %s takes the key's name, which is reserved", n.Name, name)
		}
	}
	return nil
}

// propValue types a value for a prop: a value of the type, or a word to
// parse (the CLI's and the palette's), checked against the enum.
func propValue(p PropSpec, value any) (any, error) {
	if p.Type == PropObject {
		switch v := value.(type) {
		case map[string]any:
			return v, nil
		case Thing:
			return map[string]any(v), nil
		case Arguments:
			return map[string]any(v), nil
		case string:
			var out map[string]any
			if err := json.Unmarshal([]byte(strings.TrimSpace(v)), &out); err == nil && out != nil {
				return out, nil
			}
		}
		return nil, errors.New("must be an object, a JSON object such as {\"a\": 1}")
	}
	v, err := coerceType(Setting{Name: p.Name, Type: p.Type, Enum: p.Enum}, value)
	if err != nil {
		return nil, err
	}
	if s, ok := v.(string); ok && len(p.Enum) > 0 && !slices.Contains(p.Enum, s) {
		return nil, fmt.Errorf("must be one of %s", strings.Join(p.Enum, ", "))
	}
	return v, nil
}

// sameProp reports whether two values of a prop are equal: each typed by
// the prop when it can be, then compared in their JSON form (so 2 and
// 2.0, or "2" and 2 for an integer prop, are equal).
func sameProp(p PropSpec, a, b any) bool {
	form := func(v any) string {
		if typed, err := propValue(p, v); err == nil {
			v = typed
		}
		raw, err := json.Marshal(fileForm(v))
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(raw)
	}
	return form(a) == form(b)
}

// emptyProp reports whether a prop value shows as nothing.
func emptyProp(v any) bool { return v == nil || fmt.Sprint(v) == "" }

// displayLabel is how a thing is shown: its label prop, else that prop's
// Fallback; "" when the noun has no label prop or both are empty.
func (n *Noun) displayLabel(read func(name string) any) string {
	p := n.roleProp(RoleLabel)
	if p == nil {
		return ""
	}
	if v := read(p.Name); !emptyProp(v) {
		return fmt.Sprint(v)
	}
	if p.Fallback != "" {
		if v := read(p.Fallback); !emptyProp(v) {
			return fmt.Sprint(v)
		}
	}
	return ""
}

// thingLabel is an instance's label read from the thing it stands for (its
// Data record's label prop, else the Fallback), "" when its noun has no
// label prop or the record holds neither.
func (r *Runtime) thingLabel(inst *Instance) string {
	if inst == nil || inst.Data == nil {
		return ""
	}
	return r.Nouns[inst.Noun].displayLabel(func(name string) any { return dataField(inst.Data, name) })
}

// Prop-shaped requests: {noun, prop} reads a prop of the thing the request
// acts for, {noun, prop, value} writes it, {noun, prop, toggle: true}
// flips a boolean prop. Each runs as the noun's standard get or set
// (propVerbRequest), so availability, confirms, subjects and the store
// are the verbs'; history keeps the request as it was sent.

// propVerbRequest is the verb-shaped request a prop-shaped one runs as:
// get {prop} for a read, set {prop, value} or set {prop, toggle} for a
// write, with its target, subjects, confirmation and subject key kept.
func (r *Runtime) propVerbRequest(q Request) (Request, error) {
	n, p, err := r.requestProp(q)
	if err != nil {
		return q, err
	}
	out := Request{Noun: n.Name, Verb: "get", Target: q.Target, Subjects: q.Subjects, Confirmed: q.Confirmed, Via: q.Via, Args: Arguments{"prop": p.Name}}
	if key := n.keyProp(); key != nil {
		if v, ok := q.Args[key.Name]; ok {
			out.Args[key.Name] = v
		}
	}
	if q.Writes() {
		out.Verb = "set"
		if q.Toggle {
			out.Args["toggle"] = true
		} else {
			out.Args["value"] = q.Value
		}
	}
	if _, ok := r.Operations[out.Key()]; !ok {
		return q, fmt.Errorf("%s has no %s: its store cannot %s it", r.DisplayNoun(n.Name), out.Verb, map[string]string{"get": "read", "set": "write"}[out.Verb])
	}
	return out, nil
}

// requestProp checks a prop-shaped request's shape and finds its noun and
// prop: a verb or a prop, never both; value or toggle, never both; no
// arguments but the subject's key; toggle only on a boolean; a write only
// to a writable prop.
func (r *Runtime) requestProp(q Request) (*Noun, *PropSpec, error) {
	switch {
	case q.Verb != "":
		return nil, nil, &ArgumentError{errors.New("a request names a verb or a prop, not both")}
	case q.Prop == "":
		return nil, nil, &ArgumentError{errors.New("value and toggle go with a prop")}
	case q.Toggle && q.Value != nil:
		return nil, nil, &ArgumentError{errors.New("a prop request takes a value or toggle, not both")}
	}
	n := r.Noun(q.Noun)
	if n == nil {
		return nil, nil, fmt.Errorf("unknown noun %q", q.Noun)
	}
	p := n.prop(q.Prop)
	if p == nil {
		if _, ok := r.Operations[n.Name+"."+q.Prop]; ok {
			return nil, nil, &ArgumentError{fmt.Errorf("%s is a verb, not a prop", r.DisplayKey(n.Name+"."+q.Prop))}
		}
		return nil, nil, &ArgumentError{fmt.Errorf("%s has no prop %q", r.DisplayNoun(n.Name), q.Prop)}
	}
	key := n.keyProp()
	for name := range q.Args {
		if key == nil || name != key.Name {
			return nil, nil, &ArgumentError{fmt.Errorf("%s is a prop: it takes a value or toggle, not the argument %q", r.displayRequest(q), name)}
		}
	}
	switch {
	case q.Writes() && !p.writable():
		return nil, nil, &ArgumentError{fmt.Errorf("%s is read-only", r.displayRequest(q))}
	case q.Toggle && p.Type != SettingBoolean:
		return nil, nil, &ArgumentError{fmt.Errorf("%s is %s: only a boolean prop toggles", r.displayRequest(q), p.Type)}
	}
	return n, p, nil
}

// displayRequest is a request's key as messages print it, the noun by its
// shortest name: noun.verb, or noun.prop.
func (r *Runtime) displayRequest(q Request) string { return r.DisplayKey(q.Key()) }

// requestOperation is the operation key a request runs: its own, or for a
// prop-shaped request its noun's get (a read) or set (a write).
func requestOperation(q Request) string {
	if !q.IsProp() {
		return q.Key()
	}
	if q.Writes() {
		return q.Noun + ".set"
	}
	return q.Noun + ".get"
}

// requestDescription is what help says a row's request does: its
// operation's description, or for a prop row what it writes and the
// prop's description.
func (r *Runtime) requestDescription(q Request) string {
	if !q.IsProp() {
		return r.Operations[q.Key()].Description
	}
	p, _ := r.PropOf(q.Noun, q.Prop)
	text := "Read " + q.Prop
	switch {
	case q.Toggle:
		text = "Toggle " + q.Prop
	case q.Value != nil:
		text = fmt.Sprintf("Set %s to %v", q.Prop, q.Value)
	}
	if p.Description != "" {
		text += ": " + p.Description
	}
	return text
}

// validatePropRow checks a prop-shaped binding row: a known prop of a
// noun whose store reads it (and, for a write, writes it), written with a
// value of its type or toggled when it is a boolean; From may fill only
// the value.
func (r *Runtime) validatePropRow(row Binding) error {
	q := row.Request
	fromValue := false
	for arg, source := range row.From {
		if arg != "value" {
			return fmt.Errorf("a prop row fills only its value, not %q", arg)
		}
		if len(strings.Fields(source)) == 0 {
			return fmt.Errorf("%s has an empty source", arg)
		}
		fromValue = true
	}
	if fromValue && q.Value == nil && !q.Toggle {
		q.Value = "" // filled when the key is pressed
	}
	n, p, err := r.requestProp(q)
	if err != nil {
		return err
	}
	if !fromValue && q.Value != nil {
		if _, err := propValue(*p, q.Value); err != nil {
			return fmt.Errorf("%s %w", r.displayRequest(q), err)
		}
	}
	if _, ok := r.Operations[requestOperation(q)]; !ok {
		return fmt.Errorf("%s: %s has no store verb %s", r.displayRequest(q), r.DisplayNoun(n.Name), requestOperation(q))
	}
	return nil
}

// SetProp builds a row that writes value to noun's prop (the file's
// {"key": KEY, "request": {"noun": NOUN, "prop": PROP, "value": VALUE}}).
func SetProp(noun, prop string, value any) Binding {
	return Binding{Request: Request{Noun: noun, Prop: prop, Value: value}}
}

// ToggleProp builds a row that flips noun's boolean prop
// ({"request": {"noun": NOUN, "prop": PROP, "toggle": true}}).
func ToggleProp(noun, prop string) Binding {
	return Binding{Request: Request{Noun: noun, Prop: prop, Toggle: true}}
}

// propShown reports whether a prop is offered where a request acts: its
// When guard holds at focus, or it has none.
func (r *Runtime) propShown(p PropSpec, focus string, mode ModeState, args Arguments) bool {
	if p.When == "" {
		return true
	}
	guard := r.guard(p.When)
	return guard == nil || guard(r, KeyEvent{Mode: mode, Focus: focus, Level: focus, Args: args})
}

// PropGuard is the guard a noun's prop is available under (PropSpec.When),
// "" for none: the prop guards, apart from the verb guards (VerbGuard).
func (r *Runtime) PropGuard(noun, prop string) string {
	if p := r.Noun(noun).prop(prop); p != nil {
		return p.When
	}
	return ""
}
