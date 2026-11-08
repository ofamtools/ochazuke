package ochazuke

import (
	"encoding/json"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// DefaultNamespace is the namespace of the program's own nouns: the
// program's name without its o- prefix (o-lane's nouns are lane.<noun>,
// mtm's are mtm.<noun>). A key written with two parts (pane.kill) is in
// it, so Register stores mtm.pane.kill and bindings, typed lines and the
// program's own code keep the two-part spelling (Key.Short). The
// framework's nouns are in FrameworkNamespace and a mounted extension's in
// its own. It comes from the binary's name, "main" in a test binary or
// when that name is the framework's or the app noun's; a program whose
// binary is named otherwise sets it before its first NewRuntime, since
// the stored keys of a runtime are filled with the value it had then.
var DefaultNamespace = ProgramNamespace(os.Args)

// DeclaredNamespace is the namespace the program declared
// (DeclareNamespace), "" when it declared none and DefaultNamespace comes
// from the binary's name. A catalog carries it (cli.CatalogDoc.Namespace)
// so a host mounts the extension under it rather than under its bin name.
var DeclaredNamespace string

// DeclareNamespace declares the program's namespace, singular (question
// for o-questions, cmd for o-cmds): DefaultNamespace and DeclaredNamespace
// become name. Call it before the first NewRuntime. A name that is not one
// lower-case word (a letter, then letters, digits and dashes), or is the
// framework's or the app noun's, is refused.
func DeclareNamespace(name string) error {
	if !namespaceWord(name) || slices.Contains(ReservedNamespaces, name) {
		return fmt.Errorf("namespace %q is not a lower-case noun word of its own", name)
	}
	DefaultNamespace, DeclaredNamespace = name, name
	return nil
}

// namespaceWord reports whether name is a letter, then letters, digits
// and dashes, all lower case.
func namespaceWord(name string) bool {
	for i, c := range name {
		switch {
		case c >= 'a' && c <= 'z':
		case i > 0 && (c >= '0' && c <= '9' || c == '-'):
		default:
			return false
		}
	}
	return name != ""
}

// ReservedNamespaces are names no program's namespace may take: the
// framework's, and the app noun's (app.<verb> would read as a namespace).
var ReservedNamespaces = []string{FrameworkNamespace, AppNoun}

// FallbackNamespace is DefaultNamespace when the binary's name cannot be
// one: a test binary, a name that is the framework's or the app noun's,
// or one holding a dot.
const FallbackNamespace = "main"

// ProgramNamespace is the namespace a program run as args[0] owns: its
// base name less a .exe suffix and an o- prefix (o-lane is lane).
func ProgramNamespace(args []string) string {
	if len(args) == 0 {
		return FallbackNamespace
	}
	name := strings.TrimSuffix(filepath.Base(args[0]), ".exe")
	if strings.HasSuffix(name, ".test") {
		return FallbackNamespace
	}
	name = strings.TrimPrefix(name, "o-")
	switch {
	case name == "", slices.Contains(ReservedNamespaces, name), strings.ContainsAny(name, ". "):
		return FallbackNamespace
	}
	return name
}

// Key is an operation's name: namespace.noun.name, three parts once
// registered (Operations.Push fills the namespace). A key of two parts
// (noun.name) is in DefaultNamespace. No part holds a dot.
type Key string

// KeyOf is the key of name on a registered noun (pane, ochazuke.input,
// lane.lane): the noun's name, a dot and name. A name holding a dot
// panics: names have at most three parts.
func KeyOf(noun, name string) Key {
	if strings.Contains(name, ".") {
		panic(fmt.Sprintf("operation name %q holds a dot: a key is namespace.noun.name", name))
	}
	return Key(noun + "." + name)
}

// Parts is the key's namespace, noun and name. A two-part key is in
// DefaultNamespace; a one-part key is all noun. A key of more than three
// parts (which Push refuses) reads its first part as the namespace and its
// last as the name.
func (k Key) Parts() (namespace, noun, name string) {
	s := string(k)
	first := strings.IndexByte(s, '.')
	if first < 0 {
		return DefaultNamespace, s, ""
	}
	last := strings.LastIndexByte(s, '.')
	if first == last {
		return DefaultNamespace, s[:first], s[first+1:]
	}
	return s[:first], s[first+1 : last], s[last+1:]
}

// Namespace is the key's first part: who defined the operation.
func (k Key) Namespace() string { ns, _, _ := k.Parts(); return ns }

// Noun is the key's second part, the noun's own word (input, pane, lane).
func (k Key) Noun() string { _, noun, _ := k.Parts(); return noun }

// Name is the key's third part, the operation's name (what was the verb).
func (k Key) Name() string { _, _, name := k.Parts(); return name }

// Full is the key with all three parts.
func (k Key) Full() Key {
	ns, noun, name := k.Parts()
	return Key(ns + "." + noun + "." + name)
}

// NounPath is the registered noun's name: namespace.noun, the namespace
// left out in DefaultNamespace (ochazuke.input, lane.lane, pane). It is
// the name Runtime.Nouns holds the noun under.
func (k Key) NounPath() string {
	ns, noun, _ := k.Parts()
	if ns == DefaultNamespace {
		return noun
	}
	return ns + "." + noun
}

// Short is the spelling people, bindings and stored history use: NounPath
// and the name (pane.kill, ochazuke.input.undo).
func (k Key) Short() string { return k.NounPath() + "." + k.Name() }

// String is the full key.
func (k Key) String() string { return string(k.Full()) }

// Check refuses a key that is not three nonempty parts once filled.
func (k Key) Check() error {
	parts := strings.Split(string(k.Full()), ".")
	if len(parts) != 3 || slices.Contains(parts, "") {
		return fmt.Errorf("operation key %q is not namespace.noun.name", string(k))
	}
	return nil
}

// App is the program whose process runs an operation's handler: the
// running program (DefaultNamespace) for its own and the framework's
// operations, the extension's binary for a proxied one.
type App string

// HandlerID addresses an operation's handler on its App: in process the
// operation's full key, for a proxied extension operation the key the
// extension registered it under.
type HandlerID string

// Namespace is one namespace of the registry: who defined a set of
// nouns. Plain data; its nouns and actions come from Operations.
type Namespace struct {
	Name string `json:"name"`
}

// Action is the cross-noun object of an operation's name (what was the
// verb): one per name, whatever noun has it. Plain data; its nouns,
// namespaces and operations come from Operations.
type Action struct {
	Name string `json:"name"`
}

// PartOfSpeech is what a word stands for in the registry.
type PartOfSpeech string

// The parts of speech a word can be.
const (
	NamespacePart PartOfSpeech = "namespace"
	NounPart      PartOfSpeech = "noun"
	ActionPart    PartOfSpeech = "action"
	PropPart      PartOfSpeech = "prop"
)

// Match is one thing a typed word matched: OriginalValue is the word as
// typed, PartOfSpeech says which of Namespace, Noun or Action is set, and
// Exact that the word is the whole name (or a noun's own word) rather
// than its prefix.
type Match struct {
	OriginalValue string       `json:"originalValue"`
	PartOfSpeech  PartOfSpeech `json:"partOfSpeech"`
	Namespace     *Namespace   `json:"namespace,omitempty"`
	Noun          *Noun        `json:"-"`
	Action        *Action      `json:"action,omitempty"`
	Exact         bool         `json:"exact,omitempty"`
	// Via is how the word matched (exact, alias, tail, prefix, or omitted:
	// no word, which matches every name); Alias the noun or action alias
	// it was typed as, "" for the thing's own name.
	Via   Via    `json:"via,omitempty"`
	Alias string `json:"alias,omitempty"`
}

// Candidate is one operation a line of words may name, with the matches
// that picked its noun and its action (the reader, reader.go). Prop is
// the prop a NOUN PROP reading names (Operation is then the noun's get or
// set); Order which word named the noun; Words how many leading words the
// pair read and Rest the words after them; Rank the reader's keys for it.
type Candidate struct {
	Operation *Operation
	Noun      Match
	Action    Match
	Prop      string
	Order     Order
	Words     int
	Rest      []string
	Rank      []int
}

// Operations is the operation registry (Runtime.Operations) and every
// lookup over it. All holds each operation by its full key; Order is
// registration order; NounTable is the runtime's noun table (the same map as
// Runtime.Nouns). Only Push, Register, Delete and Invalidate write All,
// Order and Index (TestOperationsWriters): Push adds or replaces one,
// Invalidate drops the memoized Index, and the next lookup builds it again
// in one pass. A mount pushes many and invalidates once.
type Operations struct {
	All       map[Key]*Operation
	Order     []Key
	Index     *Index
	NounTable map[string]*Noun
	// Holding counts the open Holds: while one is open Register and
	// Delete push without invalidating, and the last Hold to end
	// invalidates once (a mount of many operations).
	Holding int
	// Invalidations and Builds count Invalidate calls and Index builds,
	// for inspection.
	Invalidations int
	Builds        int
}

// NewOperations is an empty registry over a noun table.
func NewOperations(nouns map[string]*Noun) Operations {
	return Operations{All: map[Key]*Operation{}, NounTable: nouns}
}

// Push adds the operation key names (or replaces the one there) and
// returns it to be filled. The key gets DefaultNamespace when it has two
// parts; app defaults to DefaultNamespace and handler to the full key. A
// key that is not three parts panics. Push leaves the Index as it was:
// call Invalidate after the last Push, or use Register.
func (ops *Operations) Push(key Key, app App, handler HandlerID) *Operation {
	if err := key.Check(); err != nil {
		panic(err.Error())
	}
	full := key.Full()
	if app == "" {
		app = App(DefaultNamespace)
	}
	if handler == "" {
		handler = HandlerID(full)
	}
	if ops.All == nil {
		ops.All = map[Key]*Operation{}
	}
	if _, ok := ops.All[full]; !ok {
		ops.Order = append(ops.Order, full)
	}
	op := &Operation{Key: full, App: app, Handler: handler}
	ops.All[full] = op
	return op
}

// Invalidate drops the memoized Index; the next lookup builds it again.
func (ops *Operations) Invalidate() {
	ops.Index = nil
	ops.Invalidations++
}

// Register is Push then Invalidate (deferred to the end of an open Hold).
func (ops *Operations) Register(key Key, app App, handler HandlerID) *Operation {
	op := ops.Push(key, app, handler)
	ops.changed()
	return op
}

// Hold defers Register's and Delete's invalidation until the returned
// end runs, which invalidates once: defer ops.Hold()().
func (ops *Operations) Hold() (end func()) {
	ops.Holding++
	return func() {
		ops.Holding--
		ops.changed()
	}
}

// changed invalidates unless a Hold is open.
func (ops *Operations) changed() {
	if ops.Holding == 0 {
		ops.Invalidate()
	}
}

// Set registers a whole operation (Register, then every field of o but
// the key, app and handler Register filled) and returns the stored one.
func (ops *Operations) Set(o Operation) *Operation {
	op := ops.Register(o.Key, o.App, o.Handler)
	key, app, handler := op.Key, op.App, op.Handler
	*op = o
	op.Key, op.App, op.Handler = key, app, handler
	return op
}

// Delete removes the operation key names, if any, and invalidates.
func (ops *Operations) Delete(key Key) {
	full := key.Full()
	if _, ok := ops.All[full]; !ok {
		return
	}
	delete(ops.All, full)
	ops.Order = slices.DeleteFunc(ops.Order, func(k Key) bool { return k == full })
	ops.changed()
}

// Get is the stored operation key names (two parts read in
// DefaultNamespace).
func (ops *Operations) Get(key Key) (*Operation, bool) {
	op, ok := ops.All[key.Full()]
	return op, ok
}

// At is a copy of the operation key names, the zero Operation when none.
func (ops *Operations) At(key Key) Operation {
	o, _ := ops.Find(key)
	return o
}

// Find is a copy of the operation key names and whether it exists.
func (ops *Operations) Find(key Key) (Operation, bool) {
	op, ok := ops.Get(key)
	if !ok {
		return Operation{}, false
	}
	return *op, true
}

// Len is the number of operations.
func (ops *Operations) Len() int { return len(ops.All) }

// Entries yields every operation as its Short key and a copy, in no fixed
// order (a map's).
func (ops *Operations) Entries() iter.Seq2[string, Operation] {
	return func(yield func(string, Operation) bool) {
		for _, op := range ops.All {
			if !yield(op.Key.Short(), *op) {
				return
			}
		}
	}
}

// List is every operation in registration order.
func (ops *Operations) List() []*Operation {
	out := make([]*Operation, 0, len(ops.Order))
	for _, k := range ops.Order {
		out = append(out, ops.All[k])
	}
	return out
}

// Index holds every relation of the registry, built in one pass
// (Operations.Lookups) on the first lookup after Invalidate. The maps are
// keyed by name: a namespace's, a noun's registered name, an action's.
// The lists are sorted by name; the relations keep registration order.
type Index struct {
	Namespaces           map[string]*Namespace
	Nouns                map[string]*Noun
	Actions              map[string]*Action
	NamespaceList        []*Namespace
	NounList             []*Noun
	ActionList           []*Action
	NounsWithAction      map[string][]*Noun
	NamespacesWithAction map[string][]*Namespace
	ActionsOnNoun        map[string][]*Action
	NounsInNamespace     map[string][]*Noun
	ActionsInNamespace   map[string][]*Action
	OperationsOfAction   map[string][]*Operation
	ByPair               map[string]map[string]*Operation
}

// nounNamespace is the namespace of a registered noun name (pane is in
// DefaultNamespace, ochazuke.input in ochazuke).
func nounNamespace(noun string) string { return KeyOf(noun, "x").Namespace() }

// nounWord is a registered noun name's own word (input for ochazuke.input).
func nounWord(noun string) string { return KeyOf(noun, "x").Noun() }

// Lookups is the memoized Index, built when there is none.
func (ops *Operations) Lookups() *Index {
	if ops.Index != nil {
		return ops.Index
	}
	ops.Builds++
	ix := &Index{
		Namespaces: map[string]*Namespace{}, Nouns: map[string]*Noun{}, Actions: map[string]*Action{},
		NounsWithAction: map[string][]*Noun{}, NamespacesWithAction: map[string][]*Namespace{},
		ActionsOnNoun: map[string][]*Action{}, NounsInNamespace: map[string][]*Noun{},
		ActionsInNamespace: map[string][]*Action{}, OperationsOfAction: map[string][]*Operation{},
		ByPair: map[string]map[string]*Operation{},
	}
	namespace := func(name string) *Namespace {
		ns := ix.Namespaces[name]
		if ns == nil {
			ns = &Namespace{Name: name}
			ix.Namespaces[name] = ns
		}
		return ns
	}
	noun := func(name string) *Noun {
		n := ix.Nouns[name]
		if n == nil {
			n = ops.NounTable[name]
			if n == nil {
				n = &Noun{Name: name}
			}
			ix.Nouns[name] = n
			ns := namespace(nounNamespace(name))
			ix.NounsInNamespace[ns.Name] = append(ix.NounsInNamespace[ns.Name], n)
		}
		return n
	}
	for _, name := range sortedNames(ops.NounTable) {
		noun(name)
	}
	seen := map[[2]string]bool{}
	for _, k := range ops.Order {
		op := ops.All[k]
		n, ns := noun(op.Noun()), namespace(op.Namespace())
		a := ix.Actions[op.Name()]
		if a == nil {
			a = &Action{Name: op.Name()}
			ix.Actions[a.Name] = a
		}
		ix.OperationsOfAction[a.Name] = append(ix.OperationsOfAction[a.Name], op)
		if ix.ByPair[n.Name] == nil {
			ix.ByPair[n.Name] = map[string]*Operation{}
		}
		ix.ByPair[n.Name][a.Name] = op
		ix.ActionsOnNoun[n.Name] = append(ix.ActionsOnNoun[n.Name], a)
		ix.NounsWithAction[a.Name] = append(ix.NounsWithAction[a.Name], n)
		if !seen[[2]string{ns.Name, a.Name}] {
			seen[[2]string{ns.Name, a.Name}] = true
			ix.NamespacesWithAction[a.Name] = append(ix.NamespacesWithAction[a.Name], ns)
			ix.ActionsInNamespace[ns.Name] = append(ix.ActionsInNamespace[ns.Name], a)
		}
	}
	for _, name := range sortedNames(ix.Namespaces) {
		ix.NamespaceList = append(ix.NamespaceList, ix.Namespaces[name])
	}
	for _, name := range sortedNames(ix.Nouns) {
		ix.NounList = append(ix.NounList, ix.Nouns[name])
	}
	for _, name := range sortedNames(ix.Actions) {
		ix.ActionList = append(ix.ActionList, ix.Actions[name])
	}
	ops.Index = ix
	return ix
}

func sortedNames[V any](m map[string]V) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Namespaces is every namespace, by name.
func (ops *Operations) Namespaces() []*Namespace { return ops.Lookups().NamespaceList }

// Nouns is every noun (the noun table's and every operation's), by name.
func (ops *Operations) Nouns() []*Noun { return ops.Lookups().NounList }

// Actions is every action, by name.
func (ops *Operations) Actions() []*Action { return ops.Lookups().ActionList }

// Namespace is the namespace of that name, nil when none.
func (ops *Operations) Namespace(name string) *Namespace { return ops.Lookups().Namespaces[name] }

// Noun is the noun of that registered name, or of that name written in
// DefaultNamespace (mtm.pane for pane); nil when none.
func (ops *Operations) Noun(name string) *Noun {
	ix := ops.Lookups()
	if n := ix.Nouns[name]; n != nil {
		return n
	}
	return ix.Nouns[strings.TrimPrefix(name, DefaultNamespace+".")]
}

// Action is the action of that name, nil when none.
func (ops *Operations) Action(name string) *Action { return ops.Lookups().Actions[name] }

// OperationFor is the operation of action a on noun n, nil when none.
func (ops *Operations) OperationFor(n *Noun, a *Action) *Operation {
	if n == nil || a == nil {
		return nil
	}
	return ops.Lookups().ByPair[n.Name][a.Name]
}

// NounsWithAction is every noun that has action a, in registration order.
func (ops *Operations) NounsWithAction(a *Action) []*Noun {
	if a == nil {
		return nil
	}
	return ops.Lookups().NounsWithAction[a.Name]
}

// NamespacesWithAction is every namespace with an operation named a.
func (ops *Operations) NamespacesWithAction(a *Action) []*Namespace {
	if a == nil {
		return nil
	}
	return ops.Lookups().NamespacesWithAction[a.Name]
}

// OperationsOfAction is every operation named a, in registration order.
func (ops *Operations) OperationsOfAction(a *Action) []*Operation {
	if a == nil {
		return nil
	}
	return ops.Lookups().OperationsOfAction[a.Name]
}

// ActionsOnNoun is every action noun n has, in registration order.
func (ops *Operations) ActionsOnNoun(n *Noun) []*Action {
	if n == nil {
		return nil
	}
	return ops.Lookups().ActionsOnNoun[n.Name]
}

// NounsInNamespace is every noun of namespace ns, by name.
func (ops *Operations) NounsInNamespace(ns *Namespace) []*Noun {
	if ns == nil {
		return nil
	}
	return ops.Lookups().NounsInNamespace[ns.Name]
}

// ActionsInNamespace is every action an operation of namespace ns has.
func (ops *Operations) ActionsInNamespace(ns *Namespace) []*Action {
	if ns == nil {
		return nil
	}
	return ops.Lookups().ActionsInNamespace[ns.Name]
}

// NamespacesMatching is every namespace whose name starts with prefix.
func (ops *Operations) NamespacesMatching(prefix string) []Match {
	var out []Match
	for _, ns := range ops.Namespaces() {
		if strings.HasPrefix(ns.Name, prefix) {
			out = append(out, Match{OriginalValue: prefix, PartOfSpeech: NamespacePart, Namespace: ns, Exact: ns.Name == prefix})
		}
	}
	return out
}

// NounsMatching is every noun whose registered name or own word starts
// with prefix.
func (ops *Operations) NounsMatching(prefix string) []Match {
	var out []Match
	for _, n := range ops.Nouns() {
		word := nounWord(n.Name)
		if strings.HasPrefix(n.Name, prefix) || strings.HasPrefix(word, prefix) {
			out = append(out, Match{OriginalValue: prefix, PartOfSpeech: NounPart, Noun: n, Exact: n.Name == prefix || word == prefix})
		}
	}
	return out
}

// ActionsMatching is every action whose name starts with prefix.
func (ops *Operations) ActionsMatching(prefix string) []Match {
	var out []Match
	for _, a := range ops.Actions() {
		if strings.HasPrefix(a.Name, prefix) {
			out = append(out, Match{OriginalValue: prefix, PartOfSpeech: ActionPart, Action: a, Exact: a.Name == prefix})
		}
	}
	return out
}

// Namespace is the operation's namespace (Key.Namespace).
func (o Operation) Namespace() string { return o.Key.Namespace() }

// Noun is the operation's registered noun name (Key.NounPath): the name
// Runtime.Nouns holds it under.
func (o Operation) Noun() string { return o.Key.NounPath() }

// Name is the operation's name, the key's third part.
func (o Operation) Name() string { return o.Key.Name() }

// Action is the cross-noun object of the operation's name.
func (o Operation) Action() *Action { return &Action{Name: o.Name()} }

// operationJSON is Operation without its methods, for MarshalJSON.
type operationJSON Operation

// operationWire is Operation's JSON: its fields, and its key as the noun
// and verb it always wrote plus the key, name and namespace.
type operationWire struct {
	Noun      string `json:"noun"`
	Verb      string `json:"verb"`
	Key       string `json:"key"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	*operationJSON
}

// MarshalJSON writes the operation with noun and verb as before, and its
// full key, name and namespace.
func (o Operation) MarshalJSON() ([]byte, error) {
	j := operationJSON(o)
	return json.Marshal(operationWire{Noun: o.Noun(), Verb: o.Name(), Key: o.Key.String(), Name: o.Name(), Namespace: o.Namespace(), operationJSON: &j})
}

// UnmarshalJSON reads what MarshalJSON writes: the key, else noun and verb.
func (o *Operation) UnmarshalJSON(data []byte) error {
	w := operationWire{operationJSON: (*operationJSON)(o)}
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	switch {
	case w.Key != "":
		o.Key = Key(w.Key)
	case w.Noun != "" || w.Verb != "":
		o.Key = Key(w.Noun + "." + w.Verb)
	}
	return nil
}

// ActionNames is the names of ActionsOnNoun(n), in registration order.
func (ops *Operations) ActionNames(n *Noun) []string {
	acts := ops.ActionsOnNoun(n)
	out := make([]string, 0, len(acts))
	for _, a := range acts {
		out = append(out, a.Name)
	}
	return out
}
