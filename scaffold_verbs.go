package ochazuke

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// The noun scaffold's standard verbs (plan e): rows of the legacy verb
// table, so every stored noun gets them where its store meets their needs,
// from the one generic mechanism, never by name:
//
//   - show: the thing's props (the CLI prints them; for a person at the UI
//     it also opens the noun's index with the thing selected and the focus
//     in its show form);
//   - restore: the archive role set false (archive's inverse, set's
//     history and undo);
//   - duplicate (alias dup): a copy of the thing, live, through create
//     {from};
//   - yank: the thing's label, key, noun:key reference or record JSON into
//     a register;
//   - history: the history entries that changed or named the thing, newest
//     first (rows of ochazuke.history-entry);
//   - annotate: a new thing of a noun that declares an about prop
//     (RoleAbout) naming this thing (a note about a task).
//
// Each row yields to a prop of its own name (YieldsToProp): a noun with a
// prop called history keeps the word for the prop and has no history verb.

// RoleAbout is the reference prop (usually any-noun) that says what a thing
// is about: a noun declaring one with a store that creates things gives
// every noun it can name the annotate verb.
const RoleAbout = "about"

const (
	yankAsParam      = "as"
	annotateTextArg  = "text"
	annotateByArg    = "by"
	archiveFirstArg  = "archive-first"
	historyEntryNoun = "ochazuke.history-entry"
)

// annotators are the nouns whose things can be about a thing of noun: a
// store that creates, and a writable about prop naming that noun or any.
func (r *Runtime) annotators(noun string) []string {
	var out []string
	for _, name := range slices.Sorted(maps.Keys(r.Nouns)) {
		m := r.Nouns[name]
		if m.Store == nil || m.Store.Create == nil || m.Store.Facts {
			continue
		}
		p := writableRole(m, RoleAbout)
		if p == nil || (p.Noun != AnyNoun && p.Noun != noun) {
			continue
		}
		out = append(out, name)
	}
	return out
}

// declaresAbout reports whether the noun is one that annotates (an about
// prop): other nouns' annotate rows depend on it.
func declaresAbout(n *Noun) bool { return n.roleProp(RoleAbout) != nil }

var needAnnotator = storeNeed{Doc: "a noun with an about prop (RoleAbout) whose store creates", Lacks: "no noun can be about it (no about prop)",
	Has: func(r *Runtime, n *Noun) bool { return len(r.annotators(n.Name)) > 0 }}

func scaffoldVerbs() []standardVerb {
	subject := standardVerb{TakesSubject: true, YieldsToProp: true}
	show := subject
	show.Verb, show.Description = "show", "Show a thing: its props (the CLI prints them); at the UI its noun's index opens with the thing selected and the focus in its form"
	show.Kinds, show.Needs, show.Record, show.HistoryKind, show.Agent = []string{KindCollection, Singleton}, []storeNeed{needGet}, true, HistoryAction, AgentAllow
	show.Run = runShow

	restore := subject
	restore.Verb, restore.Description = "restore", "Restore an archived thing: set its archive prop false (archive's inverse)"
	restore.Kinds, restore.Needs = []string{KindCollection}, []storeNeed{needWriter, needPatch, needRole(RoleArchive)}
	restore.Many, restore.WritesProps, restore.Undo, restore.Repeatable, restore.HistoryKind, restore.Agent = true, true, undoByRestore, true, HistoryChange, AgentAllow
	restore.Writes = roleWrites(RoleArchive)
	restore.Run = func(r *Runtime, n *Noun, c Ctx) (any, error) {
		return r.setVia(n, c, n.roleProp(RoleArchive).Name, false)
	}

	dup := subject
	dup.Verb, dup.Description = "duplicate", "Duplicate a thing: a new thing with its props (those given replace the copied values), never archived; answers the copy"
	dup.Kinds, dup.Needs = []string{KindCollection}, []storeNeed{needFactCreator, needGet, needCreate, needCreateProp}
	dup.Creates, dup.Undo, dup.HistoryKind, dup.Agent, dup.Aliases = true, undoByDelete, HistoryChange, AgentAllow, []string{"dup"}
	dup.Params = func(n *Noun) map[string]Parameter {
		out := map[string]Parameter{}
		for _, p := range createProps(n) {
			if !p.Key {
				pp := propParameter(p)
				pp.Required = false
				out[p.Name] = pp
			}
		}
		return out
	}
	dup.Run = runDuplicate

	yank := subject
	yank.Verb, yank.Description = "yank", "Yank the thing into the active register: its label (default), key, noun:key reference or record JSON (as)"
	yank.Kinds, yank.Needs, yank.HistoryKind, yank.Agent = []string{KindCollection, Singleton}, []storeNeed{needGet}, HistoryChange, AgentAllow
	yank.ParamNames = []string{yankAsParam}
	yank.Params = func(*Noun) map[string]Parameter {
		return map[string]Parameter{yankAsParam: {Type: "string", Enum: []string{"label", "key", "ref", "json"}, Description: "What to yank: label, key, ref (noun:key) or json (the record)"}}
	}
	yank.Positional = func(*Noun) []string { return []string{yankAsParam} }
	yank.Run = runYankThing

	history := subject
	history.Verb, history.Description = "history", "The thing's history: the entries that changed or named it, newest first (rows of history-entry, each undoable by its actor)"
	history.Kinds, history.Needs, history.HistoryKind, history.Agent = []string{KindCollection, Singleton}, []storeNeed{needWriter, needGet}, HistoryFetch, AgentAllow
	history.RowsOf = historyEntryNoun
	history.Run = runThingHistory

	annotate := subject
	annotate.Verb, annotate.Description = "annotate", "Annotate a thing: create a thing about it (text its label) of a noun with an about prop (by, default the first)"
	annotate.Kinds, annotate.Needs, annotate.HistoryKind, annotate.Agent = []string{KindCollection, Singleton}, []storeNeed{needWriter, needAnnotator}, HistoryAction, AgentAllow
	annotate.ParamNames = []string{annotateTextArg, annotateByArg}
	annotate.Params = func(*Noun) map[string]Parameter {
		return map[string]Parameter{
			annotateTextArg: {Type: "string", Required: true, Description: "The annotation's text (its label)"},
			annotateByArg:   {Type: "string", Noun: "ochazuke.noun", Description: "The noun that annotates (default the first with an about prop)"},
		}
	}
	annotate.Positional = func(*Noun) []string { return []string{annotateTextArg} }
	annotate.Run = runAnnotate
	token := standardVerb{YieldsToProp: true}
	token.Verb, token.Description = "token", "The store's change token: text that moves whenever its things change, so a reader (a host's index of a mounted noun, a count) reads again only when it moved"
	token.Kinds, token.Needs, token.HistoryKind, token.Agent = []string{KindCollection, Singleton, Instances}, []storeNeed{needToken}, HistoryFetch, AgentAllow
	token.Run = func(_ *Runtime, n *Noun, c Ctx) (any, error) { return n.Store.ChangeToken(c) }
	return []standardVerb{show, restore, dup, yank, history, annotate, token}
}

var needToken = storeNeed{Doc: "Store.ChangeToken", Lacks: "its store keeps no change token (no Store.ChangeToken)",
	Has: func(_ *Runtime, n *Noun) bool { return n.Store.ChangeToken != nil }}

// thingOf reads the request's subject: its key and the thing.
func (r *Runtime) thingOf(n *Noun, c Ctx) (string, Thing, error) {
	key, err := r.subjectKey(n, c)
	if err != nil {
		return "", nil, err
	}
	t, err := n.Store.Get(c, key)
	if err != nil {
		return "", nil, err
	}
	if t == nil {
		return "", nil, fmt.Errorf("no %s %q", r.DisplayNoun(n.Name), key)
	}
	return key, t, nil
}

func runShow(r *Runtime, n *Noun, c Ctx) (any, error) {
	key, t, err := r.thingOf(n, c)
	if err != nil {
		return nil, err
	}
	if personSource(r.history.source) && StoredKind(r.NounKind(n.Name)) {
		// A person asked: the thing in its noun's index, the focus in its
		// form. A program reads the props and moves nobody's screen.
		if err := r.OpenIndex(n.Name, key); err == nil {
			if form := r.Instances[r.Page+"/form"]; form != nil && r.Page == IndexPageID(n.Name) {
				r.changeFocus(r.focusTargetOf(form.ID), focusMove{cause: focusSettle})
			}
		}
	}
	return thingRecord(n, t, nil), nil
}

func runDuplicate(r *Runtime, n *Noun, c Ctx) (any, error) {
	key, err := r.subjectKey(n, c)
	if err != nil {
		return nil, err
	}
	args := Arguments{createParamName(n, createSourceArg): key}
	for name, v := range c.Args {
		if p := n.prop(name); p != nil && !p.Key {
			args[name] = v
		}
	}
	if p := writableRole(n, RoleArchive); p != nil {
		if _, given := args[p.Name]; !given {
			args[p.Name] = false // a copy is live
		}
	}
	c.Args, err = createArgs(n, args)
	if err != nil {
		return nil, err
	}
	return runCreate(r, n, c)
}

// yankForms are what yank's as can name, each its text of the thing.
var yankForms = func() map[string]func(*Runtime, *Noun, string, Thing) (string, error) {
	label := func(_ *Runtime, n *Noun, key string, t Thing) (string, error) {
		return cmp.Or(n.displayLabel(func(name string) any { return t[name] }), key), nil
	}
	return map[string]func(*Runtime, *Noun, string, Thing) (string, error){
		"label": label,
		"key":   func(_ *Runtime, _ *Noun, key string, _ Thing) (string, error) { return key, nil },
		"ref": func(r *Runtime, n *Noun, key string, _ Thing) (string, error) {
			return AnyRef(r.CanonicalNoun(n.Name), key), nil
		},
		"json": func(_ *Runtime, n *Noun, _ string, t Thing) (string, error) {
			raw, err := json.Marshal(thingRecord(n, t, nil))
			return string(raw), err
		},
	}
}()

func runYankThing(r *Runtime, n *Noun, c Ctx) (any, error) {
	key, t, err := r.thingOf(n, c)
	if err != nil {
		return nil, err
	}
	text, err := yankForms[StringArg(c.Args, yankAsParam, "label")](r, n, key, t)
	if err != nil {
		return nil, err
	}
	return text, r.storeYank(Yank{Text: text, Shape: "char"}, "", false)
}

// ThingHistory are the history entries that changed or named the thing
// (the history verb's rows, the history section's), newest first.
func (r *Runtime) ThingHistory(noun, key string) []HistoryEntry {
	return r.thingHistory(r.CanonicalNoun(noun), key)
}

// thingHistory are the entries that changed the thing (a standard write's
// changes) or named it (a request on its noun whose key argument is it),
// newest first.
func (r *Runtime) thingHistory(noun, key string) []HistoryEntry {
	n := r.Nouns[noun]
	var keyName string
	if n != nil {
		if kp := n.keyProp(); kp != nil {
			keyName = kp.Name
		}
	}
	var out []HistoryEntry
	for _, e := range r.HistoryOf(0, HistoryChange, HistoryAction) {
		if r.entryNames(e, noun, keyName, key) {
			out = append(out, e)
		}
	}
	return out
}

func (r *Runtime) entryNames(e HistoryEntry, noun, keyName, key string) bool {
	for _, ch := range e.Changes {
		if r.CanonicalNoun(ch.Noun) == noun && ch.Key == key {
			return true
		}
	}
	if r.CanonicalNoun(e.Request.Noun) != noun || keyName == "" {
		return false
	}
	return fmt.Sprint(e.Request.Args[keyName]) == key
}

func runThingHistory(r *Runtime, n *Noun, c Ctx) (any, error) {
	key, err := r.subjectKey(n, c)
	if err != nil {
		return nil, err
	}
	rows, err := factThings(r.thingHistory(n.Name, key), nil)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, len(rows))
	for i, t := range rows {
		out[i] = map[string]any(t)
	}
	return out, nil
}

func runAnnotate(r *Runtime, n *Noun, c Ctx) (any, error) {
	key, err := r.subjectKey(n, c)
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(StringArg(c.Args, annotateTextArg, ""))
	if text == "" {
		return nil, c.Need(annotateTextArg, "annotate needs text")
	}
	nouns := r.annotators(n.Name)
	by := r.CanonicalNoun(StringArg(c.Args, annotateByArg, ""))
	if by == "" && len(nouns) > 0 {
		by = nouns[0]
	}
	if !slices.Contains(nouns, by) {
		return nil, &ArgumentError{fmt.Errorf("%s cannot be about a %s (annotators: %s)", by, r.DisplayNoun(n.Name), strings.Join(nouns, ", "))}
	}
	m := r.Nouns[by]
	about := writableRole(m, RoleAbout)
	value := key
	if about.Noun == AnyNoun {
		value = AnyRef(n.Name, key)
	}
	args := Arguments{about.Name: value}
	if label := writableRole(m, RoleLabel); label != nil {
		args[label.Name] = text
	}
	return r.ExecuteAt(c.Context, Request{Noun: by, Verb: "create", Args: args})
}
