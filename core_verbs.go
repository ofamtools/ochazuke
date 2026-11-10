package ochazuke

import (
	"fmt"
	"slices"
	"strings"
	"sync"
)

// The core verb table (DESIGN-001): every thing noun (a noun with a
// Store) has create, get, set, delete, archive, list and edit, always.
// Each row is data: what the verb is (its capability fields), what it
// needs of the noun's store (Needs), how a change it makes is undone
// (Undo), its parameters and its handler. A store that lacks what a row
// needs still gets the verb, unavailable with the need's reason
// (Operation.Unavailable): availability is data, never a missing verb.
// Code that asks what a standard verb does reads these fields, never its
// name.
//
// legacyVerbs is a second table of the same rows for the verbs offered
// only where their needs are met (watch).
//
// The tables are built on first use (standardTable), not at package
// initialisation, because the handlers they hold reach code that reads
// them (an initialisation cycle otherwise).

// standardVerb is one row of a verb table. Capability fields:
//
//   - TakesSubject: the verb acts on one existing thing, named by the
//     parameter of the noun's key prop (filled from the focus);
//   - Many: a visual selection runs it once over every selected row (one
//     undo group); a verb without both TakesSubject and Many is never
//     fanned out over a selection (create, get, list);
//   - Rows: its answer is rows of the noun;
//   - Record: its answer is one thing's props (the palette shows them as
//     prop and value rows);
//   - Creates: it makes a new thing, which its answer names (create);
//   - WritesProps: it writes props of the thing it acts on (set, and the
//     verbs that run as set); with Writes, a request naming no prop needs
//     a writable prop offered where it acts;
//   - Form: it opens the thing's edit form (the implicit edit key runs it);
//   - FieldParam: the parameter naming the one writable prop the verb
//     acts on (edit's field: a prop name or @role); a request naming a
//     prop the noun cannot write is unavailable;
//   - Undo: how history-entry.undo reverts the change it makes.
type standardVerb struct {
	Verb        string
	Description string
	Kinds       []string
	// Needs are what the row needs of the noun, checked in order; the
	// first one missing makes the verb unavailable with its reason.
	Needs        []storeNeed
	TakesSubject bool
	// NoFocus makes the subject optional and never filled from the focus
	// (watch: omitted, it watches every thing).
	NoFocus     bool
	Rows        bool
	Record      bool
	Many        bool
	Creates     bool
	WritesProps bool
	Form        bool
	FieldParam  string
	Undo        undoShape
	Repeatable  bool
	HistoryKind string
	Destructive bool
	// Agent is the verb's agent right (AgentConfirm for a verb that
	// launches an application); "" the default.
	Agent   string
	Confirm string
	// Aliases are other verb names the row is registered under on every
	// noun that has it, unless the noun has a verb of that name.
	Aliases []string
	// ParamNames are the parameters the row takes beside the subject and
	// the noun's props: no key prop and no store list parameter may take
	// one (standardParams).
	ParamNames []string
	// FocusSetting names the boolean setting that turns off the row's
	// focus fills of values (a parameter with Focus that is not the
	// subject: create's FromFocus props); "" for none.
	FocusSetting string
	Positional   func(n *Noun) []string
	Params       func(n *Noun) map[string]Parameter
	// ReadArgs reads a request's arguments into the ones the handler
	// takes before parameter sources fill them (create's prop aliases and
	// prop=value words); nil keeps them.
	ReadArgs func(n *Noun, a Arguments) (Arguments, error)
	// Writes names the props a request writes, for a verb that writes
	// values (create, set): the simple-value test reads them.
	Writes  func(n *Noun, a Arguments) ([]string, error)
	Run     func(r *Runtime, n *Noun, c Ctx) (any, error)
	Handler func(*Runtime, *Noun) AppHandler
	// ConfirmFor is the row's Operation.ConfirmFor on noun n, set when the
	// operation asks at all (Confirm, or the store's prompt, not "").
	ConfirmFor func(r *Runtime, n *Noun, c Context, q Request, prompt string) string
	// optional marks a legacy row: registered only where its needs are
	// met. Set by standardTable from the table the row is in.
	optional bool
}

// undoShape is how history-entry.undo reverts a verb's change, and so what the
// verb records (runRecording): the thing before and after its write.
type undoShape string

const (
	// undoNone: the verb changes nothing it records (a read, a form that
	// writes through set).
	undoNone undoShape = ""
	// undoByDelete: the verb makes a new thing (its answer names it, no
	// Before); undo deletes it.
	undoByDelete undoShape = "delete"
	// undoByRestore: the verb writes props of its subject; undo writes the
	// previous values back.
	undoByRestore undoShape = "restore"
	// undoByRecreate: the verb removes its subject (no After); undo
	// creates it again under its key. A delete that only archives
	// (delete.archive) records both sides and restores like a set.
	undoByRecreate undoShape = "recreate"
)

// storeNeed is one thing a row needs of a noun: Doc says it as the
// reference prints it, Lacks is the reason the verb is unavailable
// without it.
type storeNeed struct {
	Doc   string
	Lacks string
	Has   func(r *Runtime, n *Noun) bool
	// WaivedBy names a parameter whose value, given, waives the need for
	// that request (edit's field waives forms.edit: the setting controls
	// the generated full form, not the one-field form). The verb is still
	// registered unavailable with Lacks; a request giving it runs.
	WaivedBy string
}

var (
	needGet = storeNeed{Doc: "Store.Get", Lacks: "its store reads no single thing (no Store.Get)",
		Has: func(_ *Runtime, n *Noun) bool { return n.Store.Get != nil }}
	needList = storeNeed{Doc: "Store.List", Lacks: "its store lists no things (no Store.List)",
		Has: func(_ *Runtime, n *Noun) bool { return n.Store.List != nil }}
	needCreate = storeNeed{Doc: "Store.Create", Lacks: "its store creates no things (no Store.Create)",
		Has: func(_ *Runtime, n *Noun) bool { return n.Store.Create != nil }}
	needPatch = storeNeed{Doc: "Store.Patch", Lacks: "its store writes no props (no Store.Patch)",
		Has: func(_ *Runtime, n *Noun) bool { return n.Store.Patch != nil }}
	needDelete = storeNeed{Doc: "Store.Delete", Lacks: "its store deletes no things (no Store.Delete)",
		Has: func(_ *Runtime, n *Noun) bool { return n.Store.Delete != nil }}
	needCreateProp = storeNeed{Doc: "a prop create writes", Lacks: "it has no prop create writes",
		Has: func(_ *Runtime, n *Noun) bool { return len(createProps(n)) > 0 }}
	needWritable = storeNeed{Doc: "a writable prop", Lacks: "it has no writable prop",
		Has: func(_ *Runtime, n *Noun) bool { return len(writableProps(n)) > 0 }}
	needWatch = storeNeed{Doc: "Store.Watch, else Store.List (a singleton: Store.Get); a remote store: Store.Watch", Lacks: "its store has no Watch, List or (for a singleton) Get (a remote store: no Watch)",
		Has: func(_ *Runtime, n *Noun) bool {
			return watchFits(*n.Store) && (!n.Store.Remote || n.Store.Watch != nil)
		}}
	needEditForms = storeNeed{Doc: "forms.edit (unless field is given)", Lacks: "forms.edit is off",
		Has: func(r *Runtime, _ *Noun) bool { return r.Settings().Bool(EditFormsSetting) }, WaivedBy: editFieldParam}
)

// FactReason is why a facts store's writing verbs are unavailable.
const FactReason = "a system fact"

// needWriter is the need of a store whose things can be written: not a
// system fact (Store.Facts), whose things the registry holds. The writing
// verbs list it first, so a facts store has them unavailable with this
// reason.
var needWriter = storeNeed{Doc: "a store that is not a system fact (Store.Facts)", Lacks: FactReason,
	Has: func(_ *Runtime, n *Noun) bool { return !n.Store.Facts }}

// needFactCreator is the need of create: a store that is not a system fact,
// or a facts store that declares Create (the registry can be told a new
// fact: a mode).
var needFactCreator = storeNeed{Doc: "a store that is not a system fact (Store.Facts), or a facts store with Create", Lacks: FactReason,
	Has: func(_ *Runtime, n *Noun) bool { return !n.Store.Facts || n.Store.Create != nil }}

// needRole is the need of a writable prop with that role.
func needRole(role string) storeNeed {
	return storeNeed{
		Doc:   fmt.Sprintf("a writable %s prop (role %s)", role, role),
		Lacks: fmt.Sprintf("it has no writable %s prop (role %s)", role, role),
		Has:   func(_ *Runtime, n *Noun) bool { return writableRole(n, role) != nil },
	}
}

// coreVerbs are the seven verbs every thing noun has, in the order help
// lists them.
func coreVerbs() []standardVerb {
	return []standardVerb{
		{Verb: "get", Description: "Read a thing's props: every prop as an object, one prop's value (prop), or some props (props)",
			Kinds: []string{KindCollection, Singleton, Instances}, Needs: []storeNeed{needGet},
			TakesSubject: true, Record: true, HistoryKind: HistoryFetch, ParamNames: []string{"prop", "props"},
			Positional: func(*Noun) []string { return []string{"prop"} }, Params: getParams, Run: runGet},
		{Verb: "list", Description: "List the things, each its props with id and label, as a page {items, page, per_page, total}; filter and where keep the things that meet conditions, sort orders them, group-by gathers them into groups, per-page and page cut the result into pages (rows, never groups)",
			Kinds: []string{KindCollection, Instances}, Needs: []storeNeed{needList},
			Rows: true, HistoryKind: HistoryFetch, ParamNames: []string{"filter", "where", "props", "limit", "sort", "group-by", "group-sort", "page", "per-page", "archived"},
			Params: listParams, Positional: listPositional, Run: runList},
		{Verb: "create", Description: "Create a thing from the props given (each omitted one its default), or a copy of the thing from names (its props create writes, the props given in place of its values); answers the new thing",
			Kinds: []string{KindCollection}, Needs: []storeNeed{needFactCreator, needCreate, needCreateProp},
			Creates: true, Undo: undoByDelete, HistoryKind: HistoryChange, FocusSetting: CreateFromFocusSetting, ParamNames: []string{"pairs", "from"},
			Positional: createPositional, Params: createParams, ReadArgs: createArgs, Writes: createWrites, Run: runCreate},
		{Verb: "set", Description: "Write a thing's props: props, an object of prop to value (a subset or all of them; @label, @owner and @archive name a prop by its role), or prop=value words; toggle flips one boolean prop; answers the thing after the write",
			Kinds: []string{KindCollection, Singleton, Instances}, Needs: []storeNeed{needWriter, needPatch, needWritable},
			TakesSubject: true, Many: true, WritesProps: true, Undo: undoByRestore, Repeatable: true, HistoryKind: HistoryChange,
			ParamNames: []string{"props", "prop", "value", "toggle", "changes", "pairs"},
			Positional: func(*Noun) []string { return []string{"prop", "value", "pairs"} },
			Params:     setParams, Writes: setWrites, Run: runSet},
		{Verb: "delete", Description: "Delete a thing (asks first)",
			Kinds: []string{KindCollection}, Needs: []storeNeed{needWriter, needDelete},
			TakesSubject: true, Many: true, Undo: undoByRecreate, Destructive: true, HistoryKind: HistoryChange,
			Confirm: DefaultDeleteConfirm, ConfirmFor: deleteConfirm, Run: runDelete},
		{Verb: "archive", Description: "Archive a thing: set its archive prop",
			Kinds: []string{KindCollection}, Needs: []storeNeed{needWriter, needPatch, needRole(RoleArchive)},
			TakesSubject: true, Many: true, WritesProps: true, Undo: undoByRestore, Repeatable: true, HistoryKind: HistoryChange,
			Writes: roleWrites(RoleArchive), Run: runArchive},
		{Verb: "edit", Description: "Edit a thing's writable props in a form, or one of them (field, a prop or @role: with edit.field-only the form holds only it, else it is focused; a field that takes a choice opens its picker instead, edit.field-picker or the chooser argument); supplied props write directly without opening a form",
			Kinds: []string{KindCollection, Singleton, Instances}, Needs: []storeNeed{needGet, needPatch, needWritable, needEditForms},
			TakesSubject: true, Many: true, Form: true, HistoryKind: HistoryAction, ParamNames: []string{editFieldParam, editChooserParam}, FieldParam: editFieldParam, Params: editParams,
			Positional: func(*Noun) []string { return []string{editFieldParam, editChooserParam} },
			Handler:    func(r *Runtime, n *Noun) AppHandler { return r.editHandler(n.Name) }},
	}
}

// legacyVerbs are the rows offered only where their needs are met: watch,
// where the store can be watched, and refresh, where it is a copy of a
// source (copy_refresh.go). (rename, move, duplicate, restore and
// set's alias patch were purged in R1: removed_verbs.json says what
// replaces each.)
func legacyVerbs() []standardVerb {
	return append(append(append(pathVerbs(), refVerbs()...), relationVerbs()...), []standardVerb{
		{Verb: "watch", Description: "Stream what happens to the things (one, or those where matches): a JSON line per thing added, modified or deleted, from since when given; the CLI streams until interrupted",
			Kinds: []string{KindCollection, Singleton, Instances}, Needs: []storeNeed{needWatch},
			TakesSubject: true, NoFocus: true, HistoryKind: HistoryFetch, ParamNames: []string{"where", "since"},
			Params: watchParams, Run: runWatch},
		{Verb: "refresh", Description: "Compare copies with their source: one thing (its key) or every one; a plain diff (- the copy, + the source) opens with a pick per prop, local or source, that writes through set. Differences never block: at most a mark on the row (copies.marks)",
			Kinds: []string{KindCollection}, Needs: []storeNeed{needCopy},
			TakesSubject: true, NoFocus: true, HistoryKind: HistoryFetch,
			Handler: func(r *Runtime, n *Noun) AppHandler { return r.refreshHandler(n.Name) }},
	}...)
}

var (
	verbTableOnce sync.Once
	verbTable     []standardVerb
)

// standardTable is every row: the core verbs, then the legacy ones
// (marked optional), built once.
func standardTable() []standardVerb {
	verbTableOnce.Do(buildVerbTable)
	return verbTable
}

func buildVerbTable() {
	rows := coreVerbs()
	for _, v := range legacyVerbs() {
		v.optional = true
		rows = append(rows, v)
	}
	verbTable = rows
}

// CoreVerbs lists the core verbs every thing noun has, in table order.
func CoreVerbs() []string {
	var out []string
	for _, v := range standardTable() {
		if !v.optional {
			out = append(out, v.Verb)
		}
	}
	return out
}

// StandardVerbs lists the verbs of the standard verb tables (the core
// verbs, then the legacy ones), in order.
func StandardVerbs() []string {
	table := standardTable()
	out := make([]string, len(table))
	for i, v := range table {
		out[i] = v.Verb
	}
	return out
}

// standardRow is the table's row of that verb, nil when none is.
func standardRow(verb string) *standardVerb {
	table := standardTable()
	for i := range table {
		if table[i].Verb == verb {
			return &table[i]
		}
	}
	return nil
}

// reservedVerbNames are the names of every row and its aliases: no prop
// takes one, and a store's Confirm and When name only these.
func reservedVerbNames() []string {
	var out []string
	for _, v := range standardTable() {
		out = append(out, v.Verb)
		out = append(out, v.Aliases...)
	}
	return out
}

// standardParams are the parameters the rows take beside the subject and
// the props (each row's ParamNames): no key prop and no store list
// parameter takes one.
func standardParams() []string {
	var out []string
	for _, v := range standardTable() {
		for _, p := range v.ParamNames {
			if !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
	}
	return out
}

// unmet is why the row's verb cannot run on noun n ("" when it can): the
// store's kind is not one the row is for, or the first need it lacks.
func (v standardVerb) unmet(r *Runtime, n *Noun) string { return v.unmetGiven(r, n, nil) }

// unmetGiven is unmet for a request giving args: a need its WaivedBy
// parameter waives is not asked. waiveAll asks none of the waivable needs
// (the reason no request can get past: what the handler fails with).
func (v standardVerb) unmetGiven(r *Runtime, n *Noun, args Arguments) string {
	if why := n.Store.Missing[v.Verb]; why != "" {
		return why // a source that lacks the call says so (Store.Missing)
	}
	if !slices.Contains(v.Kinds, n.Store.Kind) {
		return fmt.Sprintf("its store is a %s; %s is for a %s", n.Store.Kind, v.Verb, strings.Join(v.Kinds, " or "))
	}
	for _, need := range v.Needs {
		if need.WaivedBy != "" {
			if val, given := args[need.WaivedBy]; given && (val == waiveAll || !emptyProp(val)) {
				continue
			}
		}
		if !need.Has(r, n) {
			return need.Lacks
		}
	}
	return ""
}

// waiveAll is the argument value unmetGiven reads as "waived" for every
// waivable need (hardReason).
var waiveAll any = struct{ all bool }{true}

// hardReason is why the row's verb cannot run on n whatever a request
// gives: unmet with every waivable need waived.
func (v standardVerb) hardReason(r *Runtime, n *Noun) string {
	args := Arguments{}
	for _, need := range v.Needs {
		if need.WaivedBy != "" {
			args[need.WaivedBy] = waiveAll
		}
	}
	return v.unmetGiven(r, n, args)
}

// unavailableFor is why op cannot run for a request giving args ("" when
// it can): its registered reason, unless the request waives every need
// that is missing (storeNeed.WaivedBy).
func (r *Runtime) unavailableFor(op Operation, args Arguments) string {
	if op.Unavailable == "" {
		return ""
	}
	row, n := standardRow(op.Standard), r.Nouns[op.Noun()]
	if row == nil || n == nil || n.Store == nil {
		return op.Unavailable
	}
	return row.unmetGiven(r, n, args)
}

// needsDoc is the row's Needs in words, as the reference prints them.
func (v standardVerb) needsDoc() string {
	docs := make([]string, len(v.Needs))
	for i, need := range v.Needs {
		docs[i] = need.Doc
	}
	return strings.Join(docs, ", ")
}

// fansOut reports whether a visual selection runs the row's verb over
// every selected row: only a verb that takes a subject and many of them.
func (v standardVerb) fansOut() bool { return v.TakesSubject && v.Many }

// offers reports whether the operation key names is registered and can
// run on its noun (not Operation.Unavailable): a thing noun has every core
// verb, so whether a verb exists no longer says whether its store can run
// it.
func (r *Runtime) offers(key string) bool {
	op, ok := r.Operation(key)
	return ok && op.Unavailable == ""
}
