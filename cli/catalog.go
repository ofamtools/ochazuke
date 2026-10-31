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

	"github.com/unixsuperhero/ochazuke"
)

// Schema is the catalog format version Catalog writes.
const Schema = 1

// CatalogDoc is the catalog answer: what a runtime registers, grouped by
// noun, in the runtime's own JSON shapes (ochazuke.Parameter and
// ochazuke.Binding unchanged). Nothing in it is invented: every field reads
// a registry the runtime already has.
//
// State is the state source, "NOUN.VERB", "" for none: the first verb (in
// catalog order) declared with NounBuilder.State. Its answer is every
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
	Schema     int    `json:"schema"`
	Name       string `json:"name"`
	Version    string `json:"version"`
	About      string `json:"about,omitempty"`
	AgentRules string `json:"agentRules,omitempty"`
	State      string `json:"state,omitempty"`
	// StateDelta is the state delta protocol version (StateDeltaVersion), 0 for none.
	StateDelta int `json:"stateDelta,omitempty"`
	// Aliases are the short names that reach a noun of the catalog
	// (settings for ochazuke.settings: ochazuke.Runtime.Aliases), the ones an
	// application noun shadows left out. Every name in the catalog is
	// canonical; a host resolving a name the extension's user typed reads
	// CanonicalNoun.
	Aliases  map[string]string  `json:"aliases,omitempty"`
	Nouns    []CatalogNoun      `json:"nouns"`
	Bindings []ochazuke.Binding `json:"bindings"`
	Pages    []CatalogPage      `json:"pages"`
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
	About   string                 `json:"about,omitempty"`
	Name    string                 `json:"name"`
	Pin     string                 `json:"pin,omitempty"`
	Kind    string                 `json:"kind,omitempty"`
	Columns []ochazuke.Column      `json:"columns,omitempty"`
	Props   []ochazuke.PropSpec    `json:"props,omitempty"`
	Filters []ochazuke.SavedFilter `json:"filters,omitempty"`
	Verbs   []CatalogVerb          `json:"verbs"`
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
	Rows          string                        `json:"rows,omitempty"`
	Quiet         bool                          `json:"quiet,omitempty"`
	Many          bool                          `json:"many,omitempty"`
	Positional    []string                      `json:"positional,omitempty"`
	History       string                        `json:"history,omitempty"`
	Builtin       bool                          `json:"builtin"`
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
	doc := CatalogDoc{Schema: Schema, Name: name, Version: version, Nouns: []CatalogNoun{}, Bindings: []ochazuke.Binding{}, Pages: []CatalogPage{}}
	for _, opt := range opts {
		opt(&doc)
	}
	// One entry per noun, nouns by name and each noun's verbs by name: the
	// operations sorted by key interleave a noun's verbs with the nouns
	// nested under it (app.quit, app.rows.list, app.state), so they are
	// grouped by noun, never by runs.
	ops := r.Catalog()
	sort.SliceStable(ops, func(i, j int) bool {
		if ops[i].Noun != ops[j].Noun {
			return ops[i].Noun < ops[j].Noun
		}
		return ops[i].Verb < ops[j].Verb
	})
	for _, op := range ops {
		if n := len(doc.Nouns); n == 0 || doc.Nouns[n-1].Name != op.Noun {
			noun := CatalogNoun{Name: op.Noun}
			if n := r.Nouns[op.Noun]; n != nil {
				noun.About = n.About
				noun.Pin, noun.Columns = n.Pin, slices.Clone(n.Columns)
				noun.Filters = slices.Clone(n.Filters)
				noun.Kind, noun.Props = n.StoreKind(), r.Props(op.Noun)
				for i := range noun.Props {
					// A reference's choices, as a verb parameter's: the
					// noun's list verb is published as the source.
					noun.Props[i].Source = r.ChoicesSource(noun.Props[i].Parameter)
				}
			}
			doc.Nouns = append(doc.Nouns, noun)
		}
		if op.State && doc.State == "" {
			doc.State = op.Key()
		}
		noun := &doc.Nouns[len(doc.Nouns)-1]
		verb := CatalogVerb{Guards: append(slices.Clone(r.VerbAvailability(op.Key())), op.CatalogGuards...), Examples: op.ExamplesOf(), Destructive: op.Destructive, Agent: op.AgentRightOf(false), AgentDeclared: op.Agent, Name: op.Verb, Description: op.Description, Kind: op.Kind, Result: op.Result, Confirm: op.Confirm, Repeatable: op.Repeatable, Rows: op.Rows, Quiet: op.Quiet, Many: op.Many, Positional: op.Positional, History: op.HistoryKindOf(), Builtin: op.Builtin, Aliases: r.AliasesOf(op.Key()), Standard: op.Standard}
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
	for _, s := range r.Segments() {
		if s.Verb == "" {
			continue // a provider or a command runs only in its own process
		}
		doc.Segments = append(doc.Segments, catalogSegment(r, s.Segment))
	}
	for _, a := range r.Aliases() {
		if !a.Shadowed && !a.Deprecated() {
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
	case frameworkShortNames()[name]:
		return "", fmt.Errorf("extension %q: namespace %q is the short name of the framework noun %s", bin, name, ochazuke.Namespaced(ochazuke.FrameworkNamespace, name))
	}
	return name, nil
}

var extensionWord = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// frameworkShortNames are the aliases of every framework noun: the ones a
// bare runtime and Model register, and those of the framework packages a
// host may add (cli's commands and page groups, grid, markdown), which
// TestExtensionNamespaceRefusesFrameworkNames keeps in step.
var frameworkShortNames = sync.OnceValue(func() map[string]bool {
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	ochazuke.NewModel(r)
	ochazuke.NewPicker(r)
	names := map[string]bool{}
	for _, a := range r.Aliases() {
		names[a.Alias] = true
	}
	for _, noun := range []string{CommandNoun, GroupNoun, "ochazuke.grid", "ochazuke.selection", "ochazuke.markdown", "ochazuke.markdown-task", "ochazuke.markdown-line", "ochazuke.markdown-note", "ochazuke.markdown-edit"} {
		names[strings.TrimPrefix(noun, ochazuke.FrameworkNamespace+".")] = true
	}
	return names
})
