package cli

import (
	"encoding/json"
	"slices"
	"sort"

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
// line, no outer array, each tagged with the list noun whose row source (the
// verb declaring rows, else list) a host would otherwise run:
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
	Schema  int    `json:"schema"`
	Name    string `json:"name"`
	Version string `json:"version"`
	State   string `json:"state,omitempty"`
	// StateDelta is the state delta protocol version (StateDeltaVersion), 0 for none.
	StateDelta int                `json:"stateDelta,omitempty"`
	Nouns      []CatalogNoun      `json:"nouns"`
	Bindings   []ochazuke.Binding `json:"bindings"`
	Pages      []CatalogPage      `json:"pages"`
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
type CatalogNoun struct {
	Name    string            `json:"name"`
	Pin     string            `json:"pin,omitempty"`
	Columns []ochazuke.Column `json:"columns,omitempty"`
	Verbs   []CatalogVerb     `json:"verbs"`
}

// CatalogVerb is one ochazuke.Operation. Params are the operation's
// ochazuke.Parameter values as they are: type, required, description, the
// static enum, the source (an operation, NOUN.VERB, whose result lists more
// choices) and free. Free (omitted when false) says the choices are
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
// History is the kind of history entry the verb records
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
	Name        string                        `json:"name"`
	Description string                        `json:"description,omitempty"`
	Kind        string                        `json:"kind,omitempty"`
	Params      map[string]ochazuke.Parameter `json:"params,omitempty"`
	Result      string                        `json:"result,omitempty"`
	Confirm     string                        `json:"confirm,omitempty"`
	Repeatable  bool                          `json:"repeatable,omitempty"`
	Rows        string                        `json:"rows,omitempty"`
	Quiet       bool                          `json:"quiet,omitempty"`
	Positional  []string                      `json:"positional,omitempty"`
	History     string                        `json:"history,omitempty"`
	Builtin     bool                          `json:"builtin"`
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

// RowSourceVerb is the verb whose answer is a noun's own rows when none of
// the noun's verbs declares Rows.
const RowSourceVerb = "list"

// RowSource is the verb whose JSON answer lists a noun's rows and the noun
// those rows answer to: the first verb declaring Rows (CatalogVerb.Rows),
// else a verb named list (RowSourceVerb), whose rows are the noun's own.
// Both are "" when the noun has neither or is not in the catalog.
func (d CatalogDoc) RowSource(noun string) (verb, rows string) {
	n, ok := d.noun(noun)
	if !ok {
		return "", ""
	}
	for _, v := range n.Verbs {
		if v.Rows != "" {
			return v.Name, v.Rows
		}
	}
	for _, v := range n.Verbs {
		if v.Name == RowSourceVerb {
			return v.Name, n.Name
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

func (d CatalogDoc) noun(name string) (CatalogNoun, bool) {
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
func Catalog(r *ochazuke.Runtime, name, version string) ([]byte, error) {
	return catalog(r, name, version, true)
}

// catalog is Catalog; delta declares the state delta protocol (a state
// verb run through a Shell whose StateCache is on).
func catalog(r *ochazuke.Runtime, name, version string, delta bool) ([]byte, error) {
	doc := CatalogDoc{Schema: Schema, Name: name, Version: version, Nouns: []CatalogNoun{}, Bindings: []ochazuke.Binding{}, Pages: []CatalogPage{}}
	for _, op := range r.Catalog() {
		if n := len(doc.Nouns); n == 0 || doc.Nouns[n-1].Name != op.Noun {
			noun := CatalogNoun{Name: op.Noun}
			if n := r.Nouns[op.Noun]; n != nil {
				noun.Pin, noun.Columns = n.Pin, slices.Clone(n.Columns)
			}
			doc.Nouns = append(doc.Nouns, noun)
		}
		if op.State && doc.State == "" {
			doc.State = op.Key()
		}
		noun := &doc.Nouns[len(doc.Nouns)-1]
		verb := CatalogVerb{Name: op.Verb, Description: op.Description, Kind: op.Kind, Result: op.Result, Confirm: op.Confirm, Repeatable: op.Repeatable, Rows: op.Rows, Quiet: op.Quiet, Positional: op.Positional, History: op.HistoryKindOf(), Builtin: op.Builtin}
		if len(op.Parameters) > 0 {
			verb.Params = op.Parameters
		}
		noun.Verbs = append(noun.Verbs, verb)
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
	return json.MarshalIndent(doc, "", "  ")
}

// requestKey orders requests by their JSON, whose object keys
// encoding/json writes sorted.
func requestKey(req ochazuke.Request) string {
	data, _ := json.Marshal(req)
	return string(data)
}
