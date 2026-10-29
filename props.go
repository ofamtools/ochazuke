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
// this phase get and list.
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
// get's and list's records unless asked for by name. Multiline asks a form
// for a text area.
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
// that read its things, each optional. A standard verb exists on the noun
// exactly when its handler is set (standardVerbs' Needs). Get answers the
// thing with that key ("" for a singleton); List answers the things.
// ListParams are list's parameters beside where, props and limit.
type Store struct {
	Kind       string
	Get        func(Ctx, string) (Thing, error)
	List       func(Ctx, ListQuery) ([]Thing, error)
	ListParams map[string]Parameter
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
