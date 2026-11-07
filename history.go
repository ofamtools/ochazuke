package ochazuke

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// HistoryLimit bounds the action history ring.
const HistoryLimit = 500

// HistoryFetchLimit bounds the fetches the ring keeps apart from the user's
// actions and changes, so a host polling a state verb never pushes those out.
const HistoryFetchLimit = 100

// The kinds of history entry (HistoryEntry.Kind, Operation.HistoryKind).
// An action is the user's interaction (a motion, a focus, page or modal
// move, a mode, the palette's line), a change is an edit or a mutating verb
// (what dot-repeat reads), a fetch asks for data (a state, list or show
// verb, a reload, a host calling an extension). HistorySystem is reserved
// for what the runtime does on its own (a resize, a job that finished); no
// entry has it yet.
const (
	HistoryAction = "action"
	HistoryChange = "change"
	HistoryFetch  = "fetch"
	HistorySystem = "system"
)

// historyUserKinds are the kinds history.list and /history show by default.
var historyUserKinds = []string{HistoryAction, HistoryChange}

// HistoryEntry is one user action: what ran, where, from which source, how
// it ended and, when it moved the interaction, from where to where (Moved).
// It holds ids and the request, never the data the operation returned, so
// an entry costs the same over a list of ten rows or ten thousand. Kind is
// the operation's HistoryKind. Job is the job id an application verb
// launched.
type HistoryEntry struct {
	Seq     uint64         `json:"seq"`
	At      time.Time      `json:"at"`
	Kind    string         `json:"kind,omitempty"`
	Request Request        `json:"request"`
	Context HistoryContext `json:"context"`
	Result  HistoryResult  `json:"result"`
	Moved   *HistoryMove   `json:"moved,omitempty"`
	Job     uint64         `json:"job,omitempty"`
	// Depth is how many requests enclose this one: a request an operation
	// runs while it runs (a choices source, a macro's keys) is one deeper.
	Depth int `json:"depth,omitempty"`
	// Restored marks an entry read from the history file at start
	// (EnsureHistory): an earlier session ran it. Dot-repeat skips it.
	Restored bool `json:"restored,omitempty"`
	// Changes are what a standard write did to each thing, before and
	// after (standard_undo.go): what history.undo reverts.
	Changes []ThingChange `json:"changes,omitempty"`
	// Group joins entries that undo as one step (history.undo reverts
	// every entry of the newest one's group): the writes one edit form
	// makes, over every subject and every save. "" for none.
	Group string `json:"group,omitempty"`
}

// newHistoryGroup is a new group name for entries that undo together;
// what names its source (an edit form's thing) for reading.
func (r *Runtime) newHistoryGroup(what string) string {
	r.history.groups++
	return fmt.Sprintf("%s #%d", what, r.history.groups)
}

// inHistoryGroup makes the entries recorded until restore join group,
// unless an enclosing call already set one.
func (r *Runtime) inHistoryGroup(group string) (restore func()) {
	if r.history.group != "" {
		return func() {}
	}
	r.history.group = group
	return func() { r.history.group = "" }
}

// HistoryContext is the interaction state a request ran in. Source is
// "key:<keys>", "macro:<register>", "palette", "cli", "mcp" or "api".
type HistoryContext struct {
	Actor  Actor  `json:"actor,omitzero"`
	Focus  string `json:"focus"`
	Page   string `json:"page,omitempty"`
	Mode   string `json:"mode"`
	Source string `json:"source"`
}

// HistoryResult is the outcome: ok, or the error. The value the operation
// returned is the caller's (the CLI prints it), not the history's; a
// reference to it (a hash into a content-addressed store) would go here,
// beside ok.
type HistoryResult struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// HistoryPlace is where the interaction stands: the focused instance (a
// list's focus is its selected row), the page and the mode.
type HistoryPlace struct {
	Focus string `json:"focus"`
	Page  string `json:"page,omitempty"`
	Mode  string `json:"mode"`
}

// HistoryMove is how a request moved the interaction: j on a list moves
// the focus from row A to row B, page.next from one page to the next, a
// modal's open into the modal and its close back out.
type HistoryMove struct {
	From HistoryPlace `json:"from"`
	To   HistoryPlace `json:"to"`
}

// place is where the interaction stands now.
func (r *Runtime) place() HistoryPlace {
	return HistoryPlace{Focus: r.Focus, Page: r.Page, Mode: r.Mode.Name}
}

// actionHistory is the ring ExecuteAt appends to and the source the current
// caller set (empty: "api"). entries holds the user's actions and changes
// and the lines typed into the palette whatever they ran; fetches holds the
// other fetches, under their own bound (HistoryFetchLimit). Seq orders both.
type actionHistory struct {
	entries []HistoryEntry
	fetches []HistoryEntry
	seq     uint64
	source  string
	actor   Actor
	depth   int                   // requests running now
	changes map[int][]ThingChange // the changes noted for the entry at each depth (noteChange)
	limit   int                   // the ring's bound while persisted (history-limit); 0 is HistoryLimit
	group   string                // the group the entries recorded now join (inHistoryGroup)
	groups  uint64                // groups made (newHistoryGroup)
}

// ringLimit is how many entries the ring keeps.
func (h *actionHistory) ringLimit() int {
	if h.limit > 0 {
		return h.limit
	}
	return HistoryLimit
}

// fetchRing reports whether an entry goes to the fetch ring: a fetch the
// user did not type into the palette.
func fetchRing(e HistoryEntry) bool {
	return e.Kind == HistoryFetch && e.Context.Source != "palette"
}

// appendBounded appends e and drops the oldest entries past n.
func appendBounded(entries []HistoryEntry, e HistoryEntry, n int) []HistoryEntry {
	if len(entries) >= n {
		entries = append(entries[:0], entries[len(entries)-n+1:]...)
	}
	return append(entries, e)
}

// Sourced sets the source the requests executed until restore is called
// are recorded with, unless an outer caller already set one (a row run by a
// key inside a macro replay keeps "macro:q"). Callers: the resolver
// ("key:<keys>"), the transport ("cli"), the palette ("palette").
func (r *Runtime) Sourced(source string) (restore func()) {
	if r.history.source != "" {
		return func() {}
	}
	r.history.source = source
	return func() { r.history.source = "" }
}

// personSource reports whether a history source names a person at the UI:
// a key ("key:<keys>", leader and fall-through keys included, and the
// form, confirm and deferred steps that keep their key's source), the
// palette, the help's run, or a macro replaying keys. Everything else (""/"api", "cli",
// "mcp", "hook:<event>") is a program, whose requests act on what they
// name and never on a selection.
func personSource(source string) bool {
	return source == "palette" || source == helpSource || strings.HasPrefix(source, "key:") || strings.HasPrefix(source, "macro:")
}

// forceSource overrides the source until restore: a macro replay records
// the keys it dispatches as "macro:<register>".
func (r *Runtime) forceSource(source string) (restore func()) {
	before := r.history.source
	r.history.source = source
	return func() { r.history.source = before }
}

// record appends one entry, dropping the oldest past the ring's limit, and
// sends it to the history file when the history is persisted. from is the
// place before the request ran; the entry's Moved is set when it changed.
// The value is read only for the job a Ticket names: encoding it would cost
// what the value weighs (a list's snapshot) on every key.
func (r *Runtime) record(at time.Time, q Request, ctx HistoryContext, depth int, from HistoryPlace, value any, err error) {
	// Attribution belongs to the entry, not its replayable request. A repeat
	// runs as the caller, never as the actor who originally made the change.
	q.Actor, q.source = nil, ""

	if len(q.Args) > 0 {
		args := make(Arguments, len(q.Args))
		for k, v := range q.Args {
			args[k] = v
		}
		q.Args = args
	}
	r.history.seq++
	entry := HistoryEntry{Seq: r.history.seq, At: at, Kind: r.historyKind(q), Request: q, Context: ctx, Depth: depth, Result: HistoryResult{OK: err == nil}, Group: r.history.group}
	if err != nil {
		entry.Result.Error = err.Error()
	}
	if to := r.place(); to != from {
		entry.Moved = &HistoryMove{From: from, To: to}
	}
	if ticket, ok := value.(Ticket); ok {
		entry.Job = ticket.Job
	}
	if changes := r.takeChanges(depth); err == nil {
		entry.Changes = changes
	}
	h := &r.history
	if fetchRing(entry) {
		h.fetches = appendBounded(h.fetches, entry, HistoryFetchLimit)
	} else {
		h.entries = appendBounded(h.entries, entry, h.ringLimit())
	}
	r.persist(entry)
}

// historyKind is the kind of entry q records: its operation's HistoryKind,
// an action for a request no operation answers.
func (r *Runtime) historyKind(q Request) string {
	if q.IsProp() {
		// A prop write is a change, a prop read a fetch: get's and set's.
		if q.Writes() {
			return HistoryChange
		}
		return HistoryFetch
	}
	if op, ok := r.Operations[q.Key()]; ok {
		return op.HistoryKindOf()
	}
	return HistoryAction
}

// History returns the last n entries of every kind, oldest first (all of
// them for n <= 0): the user's and the fetches, merged by Seq.
func (r *Runtime) History(n int) []HistoryEntry { return r.HistoryOf(n) }

// HistoryOf returns the last n entries of the given kinds (every kind when
// none is given), oldest first; n <= 0 returns all of them.
func (r *Runtime) HistoryOf(n int, kinds ...string) []HistoryEntry {
	h := &r.history
	keep := func(e HistoryEntry) bool { return len(kinds) == 0 || slices.Contains(kinds, e.Kind) }
	var out []HistoryEntry
	i, j := len(h.entries)-1, len(h.fetches)-1
	for (n <= 0 || len(out) < n) && (i >= 0 || j >= 0) {
		var e HistoryEntry
		if j < 0 || i >= 0 && h.entries[i].Seq > h.fetches[j].Seq {
			e, i = h.entries[i], i-1
		} else {
			e, j = h.fetches[j], j-1
		}
		if keep(e) {
			out = append(out, e)
		}
	}
	slices.Reverse(out)
	if out == nil {
		out = []HistoryEntry{}
	}
	return out
}

// historyKinds reads history.list's kind argument: "all", or kinds
// separated by commas; empty is the user's (actions and changes).
func historyKinds(arg string) ([]string, error) {
	switch arg {
	case "":
		return historyUserKinds, nil
	case "all":
		return nil, nil
	}
	var kinds []string
	for _, k := range strings.Split(arg, ",") {
		k = strings.TrimSpace(k)
		switch k {
		case HistoryAction, HistoryChange, HistoryFetch, HistorySystem:
			kinds = append(kinds, k)
		default:
			return nil, fmt.Errorf("unknown history kind %q (action, change, fetch, system or all)", k)
		}
	}
	return kinds, nil
}

// repeatableOperations are the framework's repeatable changes: the
// editor's text edits. Motions, focus, pages, modals, lists and the history
// itself are not.
var repeatableOperations = []string{"ochazuke.input.insert", "ochazuke.input.erase", "ochazuke.input.kill", "ochazuke.input.paste", "ochazuke.input.operator", "ochazuke.input.replace"}

// markRepeatable flags registered framework operations as repeatable.
func markRepeatable(r *Runtime, keys ...string) {
	for _, key := range keys {
		if op, ok := r.Operations[key]; ok {
			op.Repeatable = true
			r.Operations[key] = op
		}
	}
}

// errNothingToRepeat is dot-repeat's answer when no recorded change can
// run at the focus: told per the repeat.empty setting, never an error.
var errNothingToRepeat = errors.New("nothing to repeat")

// repeatRequest is the request dot-repeat runs: the last successful
// repeatable request that can run at focus (repeatApplies), with its
// arguments (and Confirmed) kept. Its target is dropped so it re-resolves
// to the current focus, unless the request came from the CLI and names an
// instance that still exists. steps > 0 replaces the recorded count on an
// operation that takes steps.
func (r *Runtime) repeatRequest(focus string, steps int) (Request, error) {
	entries := r.history.entries
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.Kind != HistoryChange {
			continue
		}
		if !e.Result.OK || e.Restored {
			continue
		}
		// A standard write (a prop row, set) repeats only when its values
		// are simple (simpleWrite, owner decision 12); a write that is not
		// leaves the last repeatable change in place, so . after a text
		// edit repeats the simple write before it.
		op := r.Operations[e.Request.Key()]
		if !op.Repeatable && !r.repeatableStandard(e.Request) {
			continue
		}
		q := e.Request
		args := Arguments{}
		for k, v := range q.Args {
			args[k] = v
		}
		if _, takes := op.Parameters["steps"]; takes && steps > 0 {
			args["steps"] = steps
		}
		q.Args = args
		if q.Target != "" && (e.Context.Source != "cli" || r.Instances[q.Target] == nil) {
			q.Target, q.Subjects = "", nil
		}
		at := focus
		if q.Target != "" {
			at = q.Target
		}
		if !r.repeatApplies(q, at) {
			continue
		}
		return q, nil
	}
	return Request{}, errNothingToRepeat
}

// repeatApplies reports whether a recorded change can run at focus: its
// action is available there (Available), and what it acts on is there
// (Operation.Requires: typing, a delete, a paste into an input need an
// input focused). So typing into a line that has since closed is never
// replayed outside an input: the change before it is.
func (r *Runtime) repeatApplies(q Request, focus string) bool {
	focus = r.rowInstance(level{}, q, focus)
	if !r.Available(q, focus, r.Mode) {
		return false
	}
	if op, ok := r.Operation(q.Key()); ok && op.Requires != "" {
		guard := r.guard(op.Requires)
		return guard == nil || guard(r, KeyEvent{Mode: r.Mode, Focus: focus, Level: focus, Args: q.Args})
	}
	return true
}

// tellNothingToRepeat answers a dot-repeat with nothing to repeat as the
// repeat.empty setting says: a plain line (info, the default) or nothing.
func (r *Runtime) tellNothingToRepeat() {
	if v, _ := r.settingValue(RepeatEmptySetting); v == RepeatEmptySilent {
		return
	}
	r.UI().Info(errNothingToRepeat.Error())
}

func registerHistory(r *Runtime) {
	markRepeatable(r, repeatableOperations...)
	r.Register(Operation{Verb: "repeat", Noun: "ochazuke.history", Description: "Dot-repeat: run the last repeatable change again at the current focus, its arguments kept; steps replaces its count", Parameters: map[string]Parameter{"steps": {Type: "integer", Description: "Count replacing the recorded one; 0 keeps it"}}, Result: "JSON value", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		q, err := r.repeatRequest(focusOf(c), IntArg(a, "steps", 0))
		if errors.Is(err, errNothingToRepeat) {
			r.tellNothingToRepeat()
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return r.ExecuteAt(c, q)
	}})
	// The history's entries are things (a system fact): list is the
	// standard list, with last, actor and kind as its own parameters.
	r.defineFacts(factsSpec{Noun: "ochazuke.history", Sample: HistoryEntry{}, Key: "seq",
		Docs: map[string]string{"seq": "The entry's number, in the order requests ran", "at": "When it ran", "kind": "action, change, fetch or system",
			"request": "The request: noun, verb, arguments, target", "context": "Where it ran: actor, focus, page, mode, source",
			"result": "ok, or the error; the value when it is small", "moved": "Where a motion moved the focus from and to",
			"job": "The job it started, when it ran off the loop", "depth": "How many requests enclose it (a choices source, a macro's keys)",
			"restored": "Read from the history file at start: an earlier session ran it", "changes": "What a standard write did to each thing, before and after"},
		ListParams: map[string]Parameter{
			"last":  {Type: "integer", Description: "How many of the newest entries the list reads (default 50, 0 for all), before page and per_page"},
			"actor": {Type: "string", Description: "Actor kind (owner, agent, unknown), self, or exact actor JSON; empty means all"},
			"kind":  {Type: "string", Description: "Kinds separated by commas (action, change, fetch, system) or all; default action,change"},
		},
		List: func(c Ctx, q ListQuery) ([]Thing, error) {
			kinds, err := historyKinds(StringArg(q.Args, "kind", ""))
			if err != nil {
				return nil, err
			}
			entries, err := c.Runtime.historyByActor(IntArg(q.Args, "last", 50), kinds, StringArg(q.Args, "actor", ""))
			if err != nil {
				return nil, err
			}
			return factThings(entries, nil)
		},
		Get: func(c Ctx, key string) (Thing, error) {
			entries, err := c.Runtime.historyByActor(0, nil, "")
			if err != nil {
				return nil, err
			}
			for _, e := range entries {
				if strconv.FormatUint(e.Seq, 10) == key {
					return ThingOf(e)
				}
			}
			return nil, fmt.Errorf("no history entry %q", key)
		}})
	r.Register(Operation{Verb: "path", Noun: "ochazuke.history", Description: "Print the history file: $XDG_STATE_HOME/<app>/history.jsonl, else ~/.local/state/<app>/history.jsonl", Result: "path", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		path := r.Config().HistoryPath()
		if path == "" {
			return nil, errors.New("this application does not keep its history")
		}
		return path, nil
	}})
	registerUndo(r)
}
