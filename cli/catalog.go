package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"

	"ofam.dev/ochazuke"
)

// Schema is the catalog format version Catalog writes.
const Schema = 1

// CatalogDoc is the catalog answer: what a runtime registers, grouped by
// noun, in the runtime's own JSON shapes (ochazuke.Parameter and
// ochazuke.Binding unchanged). Nothing in it is invented: every field reads
// a registry the runtime already has.
//
// State is the state source, "NOUN.VERB", "" for none: the first verb (in
// catalog order) declared with NounBuilder.Lists (Result ochazuke.ResultLists). Its answer is every
// list's rows in one process, as JSON Lines (Lines of Row): one object per
// line, no outer array, each tagged with the list noun whose row source
// (CatalogDoc.RowSource) a host would otherwise run:
//
//	{"noun":"agent","id":"agent/a1","label":"ada"}
//	{"noun":"skill","id":"skill/s1","label":"review [claude]","data":{...}}
//
// data is optional (a host's list needs id and label; show prints the
// record); cells are the row's text per column of its noun's columns
// (CatalogNoun.Columns), optional too. The answer covers every list: a noun with no lines has no rows.
// A host decodes it as it arrives (RowReader), skipping a malformed line
// silently; only a failed exec or a stream with nothing decodable is an
// error. It runs the verb once, keeps the rows as its cached state and
// refreshes them when it chooses; an extension without a state verb keeps
// per-list row sources. The same rows print as tables for a person or an
// agent (BIN NOUN VERB --format table|markdown|toon, see Formats), one per
// noun in its columns. A boolean parameter fresh on the state verb, when
// declared, asks the extension to bypass its own caches (an explicit
// refresh).
//
// StateDelta, when set, is the state delta protocol version the state
// verb speaks (StateDeltaVersion; the shell answers it, StateCache): a host
// may send --since and get, per list, unchanged, the changed rows or the
// list in full, each with its content hash (see StateDeltaVersion). A
// catalog without it (an extension built with an older ochazuke, or
// StateCache.Off) takes no --since: the host asks for full answers.
//
// Every ochazuke tool's catalog lists the framework's own nouns and verbs
// (outline, chooser, settings, history, palette, ...) beside the tool's;
// each framework verb carries builtin (CatalogVerb.Builtin), so a host
// mounting the tool's verbs reads AppNouns and never mounts the
// framework's (an ai-outline group) as the tool's.
//
// Pages are in registration order (tab order). A page the extension draws
// in Go is {id, title}; a data page (DeclarePage) also carries its layout,
// lists, and detail (CatalogPage), and a host builds it with MountPage,
// with no Go code of the extension's: each list's rows come from its
// source (or the state answer, filed by Row.Noun) and draw in the columns
// their noun declares. A data page's widgets (pages[].widgets,
// CatalogWidget: kv, status, table) are drawn above its lists by the
// framework from their sources' JSON answers.
type CatalogDoc struct {
	Schema int    `json:"schema"`
	Name   string `json:"name"`
	// Namespace is the namespace the program declared
	// (ochazuke.DeclareNamespace), "" when it declared none; a host mounts
	// the catalog under it (CatalogNamespace).
	Namespace  string             `json:"namespace,omitempty"`
	Version    string             `json:"version"`
	About      string             `json:"about,omitempty"`
	AgentRules string             `json:"agentRules,omitempty"`
	Extensions []CatalogExtension `json:"extensions,omitempty"`
	State      string             `json:"state,omitempty"`
	// StateDelta is the state delta protocol version (StateDeltaVersion), 0 for none.
	StateDelta int `json:"stateDelta,omitempty"`
	// Commands records the mounted CLI's effective names, aliases and losing
	// claims. Oden commands can own a short name without being runtime nouns.
	Commands []CommandEntry `json:"commands,omitempty"`
	// Aliases are the short names that reach a noun of the catalog
	// (settings for ochazuke.settings: ochazuke.Runtime.Aliases), the ones an
	// application noun or mounted CLI command shadows left out. Every name is
	// canonical; a host resolving a name the extension's user typed reads
	// CanonicalNoun.
	Aliases map[string]string `json:"aliases,omitempty"`
	// RelationKinds are the relation kinds the program declared
	// (ochazuke.Runtime.DeclareRelationKind): their rules and the words
	// each end reads them by. Kinds are data; a kind not listed has no rules.
	RelationKinds []ochazuke.RelationKind `json:"relationKinds,omitempty"`
	Nouns         []CatalogNoun           `json:"nouns"`
	Bindings      []ochazuke.Binding      `json:"bindings"`
	// PeerBindings are host-only declarations, never loaded by the producer.
	PeerBindings []ochazuke.Binding `json:"peerBindings,omitempty"`
	Pages        []CatalogPage      `json:"pages"`
	// Sides are command templates a host may launch beside its own panes.
	Sides []CatalogSide `json:"sides,omitempty"`
	// Segments are the status segments the tool answers from one of its
	// own verbs (CatalogSegment), in drawing order: a host mounts them
	// against the verbs it mounted (CatalogSegments).
	Segments []CatalogSegment `json:"segments,omitempty"`
	// Hooks are the tool's default hook rows (ochazuke.Runtime.DefaultHooks,
	// verbs in the tool's names) and Events the events it declares
	// (DeclareEvent), the framework's left out: a host adds them to its own
	// with CatalogHooks and CatalogEvents. The host's user drops or
	// replaces them in the host's hooks.json; a tool has no hooks file.
	Hooks  []ochazuke.HookRow   `json:"hooks,omitempty"`
	Events []ochazuke.EventSpec `json:"events,omitempty"`
	// Pickers are the pickers the tool declares (ochazuke.PickerDef rows,
	// Runtime.AddPicker), the framework's and Go-only ones left out: a host
	// adds them with MountCatalogPickers, its derived noun pickers coming
	// from the nouns it mounts.
	Pickers []ochazuke.PickerDef `json:"pickers,omitempty"`
}

// CatalogNoun is a noun and its verbs, by verb name. Pin is the noun's
// pinned base mode (ochazuke.Noun.Pin), "" for none: a host can tell a
// modal-only noun from its data without knowing its name. Columns are the
// table columns a list of the noun's rows draws (ochazuke.Noun.Columns,
// declared with NounBuilder.Columns), in order; each row's cells (Row.Cells)
// follow that order. A host mounting a list of a noun's rows passes
// CatalogDoc.ListColumns(noun) to ochazuke.Columns (it follows the noun's
// row source to the noun its rows answer to); a noun without them draws its
// labels.
//
// Kind is the kind of the noun's store (ochazuke.Collection, Singleton,
// Instances; ochazuke.Noun.StoreKind), "" for a noun without one; Props are
// the fields of its things (ochazuke.PropSpec as it is, declared with
// NounBuilder.Props), in declaration order. A noun with a store has the
// standard verbs its store supports (CatalogVerb.Standard).
type CatalogNoun struct {
	About string `json:"about,omitempty"`
	Name  string `json:"name"`
	Pin   string `json:"pin,omitempty"`
	Kind  string `json:"kind,omitempty"`
	// NounKind is what kind of noun it is (ochazuke.Runtime.NounKind:
	// record, singleton, facts, instances, command, part). A catalog
	// written before the field has none: CatalogNoun.KindOf derives it
	// from the rest of the entry the same way.
	NounKind string `json:"nounKind,omitempty"`
	// Facts says the noun's things are system facts (ochazuke.Store.Facts):
	// read-only, no times. Absent (false) in a catalog written before the
	// field; KindOf still reads facts off its writing verbs' reason.
	Facts bool `json:"facts,omitempty"`
	// Store is what the noun's store can do beyond get and list, so a
	// host's generic index picks windowed reads (Query) and skips reads
	// while nothing changed (ChangeToken) for a mounted noun. nil for a
	// noun without a store, and in a catalog written before the field:
	// a host then reads the noun as it always did (whole lists, no token).
	Store *CatalogStore `json:"store,omitempty"`
	// NoEditKey declines only the implicit edit-key fallback, not edit itself.
	NoEditKey bool `json:"noEditKey,omitempty"`
	// NoPicker keeps the noun out of the host's derived pickers
	// (ochazuke.Noun.NoPicker).
	NoPicker bool `json:"noPicker,omitempty"`
	// Unpersisted keeps the noun's history entries out of the history file
	// (ochazuke.Noun.Unpersisted, NounBuilder.Unpersisted).
	Unpersisted bool `json:"unpersisted,omitempty"`
	// Live says the noun's rows are outside the state answer (NounBuilder.Live):
	// a host fills its lists through its row source.
	Live    bool                `json:"live,omitempty"`
	Columns []ochazuke.Column   `json:"columns,omitempty"`
	Props   []ochazuke.PropSpec `json:"props,omitempty"`
	// ReferencedBy are the props of other nouns that name this noun's
	// things, in registration order (ochazuke.Runtime.Referrers): one
	// related section each.
	ReferencedBy []ochazuke.Referrer    `json:"referencedBy,omitempty"`
	Filters      []ochazuke.SavedFilter `json:"filters,omitempty"`
	// PropsOff are fields of the noun's rows its lists' prop filters never
	// offer (ochazuke.NounBuilder.PropsOff); every other field is filterable.
	PropsOff []string `json:"propsOff,omitempty"`
	// Main marks the program's main noun (ochazuke.Noun.Main): a host
	// reads a word that leaves the noun out as this one first, within the
	// extension's namespace (mtm lane state is lane.lane state).
	Main bool `json:"main,omitempty"`
	// DefaultAction is the action the noun's words run with no action
	// word, or only flags (ochazuke.Noun.DefaultAction).
	DefaultAction string        `json:"defaultAction,omitempty"`
	Verbs         []CatalogVerb `json:"verbs"`
}

// CatalogVerb is one ochazuke.Operation. Params are the operation's
// ochazuke.Parameter values as they are: type, required, description, the
// static enum, the source (an operation, NOUN.VERB, whose result lists more
// choices; for a parameter with a noun and no source, the noun's list verb
// when the runtime reads its choices from it: ochazuke.Runtime.ChoicesSource),
// the noun (a noun without such a list verb names its live instances, which
// only the producing runtime lists) and free. Free (omitted when false) says the choices are
// suggestions and any value is accepted (mtm's --model); a parameter with an
// enum or a source and no free accepts only its choices, so a host may
// refuse other values before running the verb.
//
// Kind is "application" for a verb
// whose handler may return pending work, "interaction" otherwise. Rows
// names the noun whose rows the verb's result lists (ochazuke.Operation.Rows,
// declared with NounBuilder.Rows), "" when the result is not rows. Quiet
// marks bookkeeping the jobs ticker hides (NounBuilder.Quiet). Positional
// names the parameters bare words fill first (NounBuilder.Positional).
// Many marks a verb that takes every row of a selection at once
// (ochazuke.Operation.Many, NounBuilder.Many). History is the kind of history entry the verb records
// (ochazuke.Operation.HistoryKindOf: action, change or fetch); a host that
// mounts the verb sets it on its own operation (Operation.HistoryKind), so
// its fetches stay out of the user's history there too.
//
// Builtin marks a verb the framework registers in every ochazuke tool
// (ochazuke.Operation.Builtin): the outline, chooser, settings, palette
// and the other framework nouns' verbs, some of them application verbs
// (outline.add). A host mounting a tool's verbs skips them (AppNouns,
// AppVerbs); a tool's own verb on a framework noun (history.capture-hook)
// is not builtin and mounts. A catalog written before the field reads
// every verb as not builtin.
type CatalogVerb struct {
	Guards      []string           `json:"guards,omitempty"`
	Examples    []ochazuke.Example `json:"examples,omitempty"`
	Destructive bool               `json:"destructive"`
	Agent       string             `json:"agent"`
	// AgentDeclared distinguishes an explicit right from its derived default for hosts.
	AgentDeclared string                        `json:"agentDeclared,omitempty"`
	Name          string                        `json:"name"`
	Description   string                        `json:"description,omitempty"`
	Kind          string                        `json:"kind,omitempty"`
	Params        map[string]ochazuke.Parameter `json:"params,omitempty"`
	Result        string                        `json:"result,omitempty"`
	Confirm       string                        `json:"confirm,omitempty"`
	Repeatable    bool                          `json:"repeatable,omitempty"`
	Foreground    bool                          `json:"foreground,omitempty"`
	OpensPage     string                        `json:"opensPage,omitempty"` // a page the verb opens (ochazuke.Operation.OpensPage)
	Slot          string                        `json:"slot,omitempty"`
	Timeout       string                        `json:"timeout,omitempty"`
	Disabled      bool                          `json:"disabled,omitempty"`
	// Unavailable is why the verb cannot run on its noun (a core verb its
	// store lacks a handler for; Operation.Unavailable); Disabled is then
	// true too.
	Unavailable   string `json:"unavailable,omitempty"`
	Rows          string `json:"rows,omitempty"`
	Quiet         bool   `json:"quiet,omitempty"`
	Many          bool   `json:"many,omitempty"`
	ByAssociation bool   `json:"byAssociation,omitempty"`
	FocusBound    bool   `json:"focusBound,omitempty"`
	// Motion marks movement (footer hints and help sections leave it out),
	// Typing keys passed on as content (the key echo groups them) and
	// Staying a verb that manages the pending mode itself
	// (ochazuke.Operation.Motion, Typing, Staying); a host mounting the
	// verb keeps them.
	Motion     bool     `json:"motion,omitempty"`
	Typing     bool     `json:"typing,omitempty"`
	Staying    bool     `json:"staying,omitempty"`
	Positional []string `json:"positional,omitempty"`
	History    string   `json:"history,omitempty"`
	Builtin    bool     `json:"builtin"`
	// Aliases are the verb's other names (ochazuke.ActionAlias: the alias
	// as noun.verb, its noun and verb, a deprecated one's since and
	// removeAfter). A host mounting the catalog may register them as
	// aliases of the mounted verb; one that does not still runs the
	// canonical name.
	Aliases []ochazuke.ActionAlias `json:"aliases,omitempty"`
	// Standard is the standard verb the runtime generated this verb from
	// (ochazuke.Operation.Standard: get, list), "" for a verb written by
	// hand. A generated verb is not builtin: it is the tool's, on the
	// tool's noun, and mounts like any of its verbs.
	Standard string `json:"standard,omitempty"`
	// Subject is what the verb acts on: "" one thing, "collection" all of
	// the noun's things (ochazuke.Operation.Subject).
	Subject string `json:"subject,omitempty"`
	// Creates marks a verb that makes a new thing of its noun
	// (ochazuke.Operation.Creates): the standard create, or the verb a
	// noun creates through instead (lane.define). Absent in older
	// catalogs: a host then reads only the standard create as creating.
	Creates bool `json:"creates,omitempty"`
}

// CatalogStore is what a noun's store offers a host beyond its verbs
// (ochazuke.Store): Query answers one page of a list request (paging,
// filters and sort run in the store), ChangeToken a cheap token that is
// equal while nothing changed, Watch pushes changes, Undo lets history
// undo its standard changes, Remote says its calls leave the process.
// Every field false is a store that lists whole.
type CatalogStore struct {
	Query       bool `json:"query,omitempty"`
	ChangeToken bool `json:"changeToken,omitempty"`
	Watch       bool `json:"watch,omitempty"`
	Undo        bool `json:"undo,omitempty"`
	Remote      bool `json:"remote,omitempty"`
}

// catalogStore is what the catalog publishes of a noun's store: its facts
// mark and its capabilities; nil for no store.
func catalogStore(s *ochazuke.Store) (facts bool, out *CatalogStore) {
	if s == nil {
		return false, nil
	}
	return s.Facts, &CatalogStore{Query: s.Query != nil, ChangeToken: s.ChangeToken != nil, Watch: s.Watch != nil, Undo: s.Undo, Remote: s.Remote}
}

// KindOf is the noun's kind (ochazuke.NounKindList): NounKind when the
// catalog says it, else derived from the entry as the runtime derives it,
// so a catalog written before the field (v0.117 and older) reads the same:
// a store kind of singleton or instances is that; a store whose writing
// verbs are unavailable as a fact (ochazuke.FactReason) or marked Facts is
// facts; any other store a record; no store, a command noun when one of
// its verbs is an application verb, else a part.
func (n CatalogNoun) KindOf() string {
	if n.NounKind != "" {
		return n.NounKind
	}
	switch {
	case n.Kind == ochazuke.Singleton && !n.factsOf():
		return ochazuke.NounKindSingleton
	case n.Kind == ochazuke.Instances:
		return ochazuke.NounKindInstances
	case n.Kind != "" && n.factsOf():
		return ochazuke.NounKindFacts
	case n.Kind != "":
		return ochazuke.NounKindRecord
	}
	for _, v := range n.Verbs {
		if v.Kind == ochazuke.KindApplication {
			return ochazuke.NounKindCommand
		}
	}
	return ochazuke.NounKindPart
}

// factsOf reports whether the noun's things are facts: the Facts mark, or
// (an older catalog) a standard verb unavailable as a fact.
func (n CatalogNoun) factsOf() bool {
	if n.Facts {
		return true
	}
	for _, v := range n.Verbs {
		if v.Standard != "" && v.Unavailable == ochazuke.FactReason {
			return true
		}
	}
	return false
}

// AppVerbs are the noun's verbs the tool defined: every verb but the
// builtin ones (CatalogVerb.Builtin), in catalog order. It is nil when the
// noun has none.
func (n CatalogNoun) AppVerbs() []CatalogVerb {
	var out []CatalogVerb
	for _, v := range n.Verbs {
		if !v.Builtin {
			out = append(out, v)
		}
	}
	return out
}

// AppNouns are the nouns with verbs the tool defined, each holding only
// those (CatalogNoun.AppVerbs), in catalog order: what a host mounting the
// tool's verbs mounts. A framework noun appears only when the tool put a
// verb of its own on it (history.capture-hook). Kind, pin and the rest are
// the host's to filter.
func (d CatalogDoc) AppNouns() []CatalogNoun {
	var out []CatalogNoun
	for _, n := range d.Nouns {
		if verbs := n.AppVerbs(); len(verbs) > 0 {
			n.Verbs = verbs
			out = append(out, n)
		}
	}
	return out
}

// RowSourceVerb is the verb whose answer is a noun's own rows: preferred
// as the row source when it declares rows of the noun, and taken as the
// noun's own rows when it declares none.
const RowSourceVerb = "list"

// RowSource is the verb whose JSON answer lists a noun's rows and the noun
// those rows answer to, chosen in this order (each step in catalog order,
// verbs sorted):
//
//  1. list (RowSourceVerb) when it declares rows of the noun itself;
//  2. another verb declaring rows of the noun itself;
//  3. list declaring no rows, whose answer is the noun's own rows;
//  4. a verb declaring rows of another noun (a noun whose only listing is
//     of something else, mtm's store tables).
//
// So a verb whose rows are another noun's (slack-channel.history, the
// channel's messages) never beats the noun's own list. Both are "" when
// the noun has none of these or is not in the catalog.
func (d CatalogDoc) RowSource(noun string) (verb, rows string) {
	n, ok := d.noun(noun)
	if !ok {
		return "", ""
	}
	own := func(v CatalogVerb) bool { return d.CanonicalNoun(v.Rows) == n.Name }
	for _, v := range n.Verbs {
		if v.Name == RowSourceVerb && own(v) {
			return v.Name, n.Name
		}
	}
	for _, v := range n.Verbs {
		if v.Rows != "" && own(v) {
			return v.Name, n.Name
		}
	}
	for _, v := range n.Verbs {
		if v.Name == RowSourceVerb && v.Rows == "" {
			return v.Name, n.Name
		}
	}
	for _, v := range n.Verbs {
		if v.Rows != "" {
			return v.Name, v.Rows
		}
	}
	return "", ""
}

// ListColumns is the table a host's list of noun's rows draws: the
// declared columns (CatalogNoun.Columns) of the noun its row source's rows
// answer to (RowSource), in order. A noun without a row source (a host
// lists its verbs instead) or whose rows declare no columns gets none, a
// plain list. Mount a list with ochazuke.Columns(doc.ListColumns(noun)...);
// no columns leaves it plain, so the call needs no check.
func (d CatalogDoc) ListColumns(noun string) []ochazuke.Column {
	verb, rows := d.RowSource(noun)
	if verb == "" {
		return nil
	}
	if n, ok := d.noun(rows); ok {
		return slices.Clone(n.Columns)
	}
	return nil
}

// CanonicalNoun is the catalog's noun a name stands for: the name, else
// the noun its alias names (Aliases), else the name unchanged.
func (d CatalogDoc) CanonicalNoun(name string) string {
	for _, n := range d.Nouns {
		if n.Name == name {
			return name
		}
	}
	if target, ok := d.Aliases[name]; ok {
		return target
	}
	return name
}

func (d CatalogDoc) noun(name string) (CatalogNoun, bool) {
	name = d.CanonicalNoun(name)
	for _, n := range d.Nouns {
		if n.Name == name {
			return n, true
		}
	}
	return CatalogNoun{}, false
}

// Catalog describes r as indented JSON: every operation grouped by noun
// (nouns and verbs sorted; params are a map, which encoding/json writes
// sorted), every noun's binding rows (sorted by mode, noun, key, then
// request, stably; noun and mode filled in, the LoadBindings shape) and the
// pages in registration order (their tab order). A catalog with a state
// verb declares the state delta protocol (StateDelta), which a Shell
// answers. The same runtime gives the same bytes on every run.
func Catalog(r *ochazuke.Runtime, name, version string, opts ...CatalogOption) ([]byte, error) {
	return catalog(r, name, version, true, opts...)
}

// catalog is Catalog; delta declares the state delta protocol (a state
// verb run through a Shell whose StateCache is on).
func catalog(r *ochazuke.Runtime, name, version string, delta bool, opts ...CatalogOption) ([]byte, error) {
	doc := CatalogDoc{Schema: Schema, Name: name, Namespace: ochazuke.DeclaredNamespace, Version: version, Nouns: []CatalogNoun{}, Bindings: []ochazuke.Binding{}, Pages: []CatalogPage{}}
	for _, opt := range opts {
		opt(&doc)
	}
	if err := doc.ValidateAgentText(); err != nil {
		return nil, err
	}
	// One entry per noun, nouns by name and each noun's verbs by name: the
	// operations sorted by key interleave a noun's verbs with the nouns
	// nested under it (app.quit, app.rows.list, app.state), so they are
	// grouped by noun, never by runs.
	ops := r.Catalog()
	kinds := r.AllNounKinds()
	sort.SliceStable(ops, func(i, j int) bool {
		if ops[i].Noun() != ops[j].Noun() {
			return ops[i].Noun() < ops[j].Noun()
		}
		return ops[i].Name() < ops[j].Name()
	})
	for _, op := range ops {
		if n := len(doc.Nouns); n == 0 || doc.Nouns[n-1].Name != op.Noun() {
			noun := CatalogNoun{Name: op.Noun()}
			if n := r.Nouns[op.Noun()]; n != nil {
				if !op.CatalogProxy && InstructionTokens(n.About) > AgentTextLimit {
					return nil, fmt.Errorf("%s about exceeds %d tokens (chars/4)", op.Noun(), AgentTextLimit)
				}
				noun.About = n.About
				noun.Pin, noun.Live, noun.Columns = n.Pin, n.Live, slices.Clone(n.Columns)
				noun.NoEditKey = n.NoEditKey
				noun.NoPicker = n.NoPicker
				noun.Main, noun.DefaultAction = n.Main, n.DefaultAction
				noun.Unpersisted = n.Unpersisted
				noun.Filters = slices.Clone(n.Filters)
				noun.PropsOff = slices.Clone(n.PropsOff)
				noun.Kind, noun.Props = n.StoreKind(), r.Props(op.Noun())
				noun.NounKind = kinds[op.Noun()]
				noun.Facts, noun.Store = catalogStore(n.Store)
				noun.ReferencedBy = referencedBy(r, op.Noun())
				for i := range noun.Props {
					// A reference's choices, as a verb parameter's: the
					// noun's list verb is published as the source.
					noun.Props[i].Source = r.ChoicesSource(noun.Props[i].Parameter)
				}
			}
			doc.Nouns = append(doc.Nouns, noun)
		}
		if op.Result == ochazuke.ResultLists && doc.State == "" {
			doc.State = op.Key.Short()
		}
		noun := &doc.Nouns[len(doc.Nouns)-1]
		verb := CatalogVerb{Guards: append(slices.Clone(r.VerbAvailability(op.Key.Short())), op.CatalogGuards...), Examples: op.ExamplesOf(), Destructive: op.Destructive, Agent: op.AgentRightOf(false), AgentDeclared: op.Agent, Name: op.Name(), Description: op.Description, Kind: op.Kind, Result: op.Result, Confirm: op.Confirm, Repeatable: op.Repeatable, Rows: op.Rows, Quiet: op.Quiet, Many: op.Many, Positional: op.Positional, History: op.HistoryKindOf(), Builtin: op.Builtin, Aliases: r.AliasesOf(op.Key.Short()), Standard: op.Standard}
		verb.Foreground = op.Foreground
		verb.OpensPage = op.OpensPage
		verb.ByAssociation = op.ByAssociation
		verb.Subject = op.Subject
		verb.Creates = op.Creates
		verb.FocusBound = op.FocusBound
		verb.Motion, verb.Typing, verb.Staying = op.Motion, op.Typing, op.Staying
		verb.Slot = op.Slot
		verb.Disabled, verb.Unavailable = verbDisabled(r, op), op.Unavailable
		if op.Timeout > 0 {
			verb.Timeout = op.Timeout.String()
		}
		if len(op.Parameters) > 0 {
			verb.Params = make(map[string]ochazuke.Parameter, len(op.Parameters))
			for name, p := range op.Parameters {
				// A Noun's list verb is published as the source, so a
				// host lists the choices the runtime would.
				p.Source = r.ChoicesSource(p)
				if p.Noun != "" {
					p.Noun = r.CanonicalNoun(p.Noun)
				}
				verb.Params[name] = p
			}
		}
		noun.Verbs = append(noun.Verbs, verb)
	}
	doc.Pickers = r.CatalogPickerDefs()
	doc.RelationKinds = r.RelationKinds()
	for _, s := range r.Segments() {
		if s.Verb == "" {
			continue // a provider or a command runs only in its own process
		}
		doc.Segments = append(doc.Segments, catalogSegment(r, s.Segment))
	}
	commands := map[string]bool{}
	for _, command := range doc.Commands {
		commands[command.Name] = true
	}
	for _, a := range r.Aliases() {
		if !a.Shadowed && !a.Deprecated() {
			if commands[a.Alias] && a.Alias != a.Noun {
				continue
			}
			if doc.Aliases == nil {
				doc.Aliases = map[string]string{}
			}
			doc.Aliases[a.Alias] = a.Noun
		}
	}
	names := make([]string, 0, len(r.Nouns))
	for name := range r.Nouns {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		n := r.Nouns[name]
		modes := make([]string, 0, len(n.Bindings))
		for mode := range n.Bindings {
			modes = append(modes, mode)
		}
		sort.Strings(modes)
		for _, mode := range modes {
			for _, row := range n.Bindings[mode] {
				row.Noun, row.Mode = name, mode
				doc.Bindings = append(doc.Bindings, row)
			}
		}
	}
	// Rows sort by mode, noun, key, then request, stably: equal rows keep
	// table order, and no map order reaches the output.
	sort.SliceStable(doc.Bindings, func(i, j int) bool {
		a, b := doc.Bindings[i], doc.Bindings[j]
		if a.Mode != b.Mode {
			return a.Mode < b.Mode
		}
		if a.Noun != b.Noun {
			return a.Noun < b.Noun
		}
		if a.Keys != b.Keys {
			return a.Keys < b.Keys
		}
		return requestKey(a.Request) < requestKey(b.Request)
	})
	if delta && doc.State != "" {
		doc.StateDelta = StateDeltaVersion
	}
	doc.Pages = declaredPages(r)
	doc.Sides = declaredSides(r)
	doc.Extensions = declaredExtensions(r)
	if err := doc.ValidateSides(); err != nil {
		return nil, err
	}
	doc.Hooks, doc.Events = catalogHooks(r)
	return json.MarshalIndent(doc, "", "  ")
}

// requestKey orders requests by their JSON, whose object keys
// encoding/json writes sorted.
func requestKey(req ochazuke.Request) string {
	data, _ := json.Marshal(req)
	return string(data)
}

// Namespace is the PageConfig.Noun of an extension mounted under ext: the
// catalog's noun skill is the host's ext.skill (ai.skills under ai), so
// two extensions and the host never name the same noun. A host defining
// the extension's verbs itself names its nouns the same way; the
// extension's own binary keeps its short names (Local strips them back).
// Name ext with ExtensionNamespace(bin) so every host mounts an extension
// under the same namespace.
func Namespace(ext string) func(catalogNoun string) string {
	return func(noun string) string { return ochazuke.Namespaced(ext, noun) }
}

// Local is the extension's own noun of a host noun mounted under ext
// (Namespace): ai.skills under ai is skills. ok is false for a noun that is
// not under ext.
func Local(ext, hostNoun string) (string, bool) {
	noun, ok := strings.CutPrefix(hostNoun, ext+".")
	return noun, ok && noun != ""
}

// ExtensionNamespace is the namespace a host mounts the extension binary
// bin under (the ext of Namespace and Local), the same in every host: the
// base name of bin, lower-cased, without a leading "o-" (o-ai is ai, o-qbt
// is qbt, /usr/local/bin/o-git is git; mtm-mermaid stays mtm-mermaid). It
// is an error when what is left is not one noun word (a letter, then
// letters, digits and dashes) or is reserved: ochazuke (the framework's
// namespace), app (the app noun, whose verbs app.<verb> a namespace would
// collide with) or the short name of a framework noun (list, settings,
// command, grid, …), which an extension's group would hide in the CLI. A
// host refuses such an extension or names it itself.
func ExtensionNamespace(bin string) (string, error) {
	name := strings.ToLower(filepath.Base(bin))
	name = strings.TrimPrefix(name, "o-")
	switch {
	case !extensionWord.MatchString(name):
		return "", fmt.Errorf("extension %q: namespace %q is not a noun word", bin, name)
	case name == ochazuke.FrameworkNamespace || name == ochazuke.AppNoun:
		return "", fmt.Errorf("extension %q: namespace %q is reserved", bin, name)
	case frameworkShortNames()[name] || ochazuke.FrameworkNounReserved(ochazuke.Namespaced(ochazuke.FrameworkNamespace, name)):
		return "", fmt.Errorf("extension %q: namespace %q is the short name of the framework noun %s", bin, name, ochazuke.Namespaced(ochazuke.FrameworkNamespace, name))
	}
	return name, nil
}

// CatalogNamespace is the namespace a host mounts an extension's catalog
// under: the one the catalog declares (CatalogDoc.Namespace, singular:
// question for o-questions) when it declares one, else
// ExtensionNamespace(bin). A declared name is checked as a bin name is.
func CatalogNamespace(doc CatalogDoc, bin string) (string, error) {
	if doc.Namespace == "" {
		return ExtensionNamespace(bin)
	}
	ns, err := ExtensionNamespace(doc.Namespace)
	if err != nil {
		return "", fmt.Errorf("extension %q declares namespace %q: %w", bin, doc.Namespace, err)
	}
	return ns, nil
}

// OwnNamespace is the running program's namespace as a host sees it: the
// declared one (ochazuke.DeclaredNamespace), else ExtensionNamespace(bin).
func OwnNamespace(bin string) (string, error) {
	return CatalogNamespace(CatalogDoc{Namespace: ochazuke.DeclaredNamespace}, bin)
}

var extensionWord = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Reserve optional framework packages' established names even in a host that
// does not link them. Linked packages reserve their lazy nouns at init too;
// ExtensionNamespace reads those reservations on every call, outside the
// cached runtime aliases, so a later reservation takes effect immediately.
func init() {
	ochazuke.ReserveFrameworkNouns(CommandNoun, GroupNoun, "ochazuke.grid", "ochazuke.selection", "ochazuke.markdown", "ochazuke.markdown-task", "ochazuke.markdown-line", "ochazuke.markdown-note", "ochazuke.markdown-edit")
}

// frameworkShortNames are the aliases a bare runtime and Model register. Lazy framework constructors reserve their names separately.
var frameworkShortNames = sync.OnceValue(func() map[string]bool {
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	ochazuke.NewModel(r)
	names := map[string]bool{}
	for _, a := range r.Aliases() {
		names[a.Alias] = true
	}
	return names
})

// verbDisabled is a catalog verb's disabled: the operation is disabled,
// unavailable on its noun (Operation.Unavailable) or gated by a boolean
// setting that is off.
func verbDisabled(r *ochazuke.Runtime, op ochazuke.Operation) bool {
	return op.Disabled || op.Unavailable != "" || op.EnabledSetting != "" && !r.Settings().Bool(op.EnabledSetting)
}

// referencedBy are the props that name noun's things; nil when none does,
// so the catalog leaves the field out.
func referencedBy(r *ochazuke.Runtime, noun string) []ochazuke.Referrer {
	rows := r.Referrers(noun)
	if len(rows) == 0 {
		return nil
	}
	return rows
}
