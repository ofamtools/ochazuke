package ochazuke

import (
	"encoding/json"
	"errors"
	"time"
)

// HistoryLimit bounds the action history ring.
const HistoryLimit = 500

// HistoryEntry is one executed request: what ran, where, from which source
// and how it ended. Value is the result as JSON at the time it ran; Job is
// the job id an application verb launched.
type HistoryEntry struct {
	Seq     uint64         `json:"seq"`
	At      time.Time      `json:"at"`
	Request Request        `json:"request"`
	Context HistoryContext `json:"context"`
	Result  HistoryResult  `json:"result"`
	Job     uint64         `json:"job,omitempty"`
	// Depth is how many requests enclose this one: a request an operation
	// runs while it runs (a choices source, a macro's keys) is one deeper.
	Depth int `json:"depth,omitempty"`
	// Restored marks an entry read from the history file at start
	// (EnsureHistory): an earlier session ran it. Dot-repeat skips it.
	Restored bool `json:"restored,omitempty"`
}

// HistoryContext is the interaction state a request ran in. Source is
// "key:<keys>", "macro:<register>", "palette", "cli" or "api".
type HistoryContext struct {
	Focus  string `json:"focus"`
	Page   string `json:"page,omitempty"`
	Mode   string `json:"mode"`
	Source string `json:"source"`
}

// HistoryResult is the outcome: ok with the value, or the error.
type HistoryResult struct {
	OK    bool            `json:"ok"`
	Value json.RawMessage `json:"value,omitempty"`
	Error string          `json:"error,omitempty"`
}

// actionHistory is the ring ExecuteAt appends to and the source the current
// caller set (empty: "api").
type actionHistory struct {
	entries []HistoryEntry
	seq     uint64
	source  string
	depth   int // requests running now
	limit   int // the ring's bound while persisted (history-limit); 0 is HistoryLimit
}

// ringLimit is how many entries the ring keeps.
func (h *actionHistory) ringLimit() int {
	if h.limit > 0 {
		return h.limit
	}
	return HistoryLimit
}

// unrecorded operations only read the history or the state.
var unrecorded = map[string]bool{"history.list": true, "state.get": true}

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

// forceSource overrides the source until restore: a macro replay records
// the keys it dispatches as "macro:<register>".
func (r *Runtime) forceSource(source string) (restore func()) {
	before := r.history.source
	r.history.source = source
	return func() { r.history.source = before }
}

// record appends one entry, dropping the oldest past the ring's limit, and
// sends it to the history file when the history is persisted.
func (r *Runtime) record(at time.Time, q Request, ctx HistoryContext, depth int, value any, err error) {
	if len(q.Args) > 0 {
		args := make(Arguments, len(q.Args))
		for k, v := range q.Args {
			args[k] = v
		}
		q.Args = args
	}
	r.history.seq++
	entry := HistoryEntry{Seq: r.history.seq, At: at, Request: q, Context: ctx, Depth: depth, Result: HistoryResult{OK: err == nil}}
	if err != nil {
		entry.Result.Error = err.Error()
	} else if value != nil {
		if data, jerr := json.Marshal(value); jerr == nil {
			entry.Result.Value = data
		}
	}
	if ticket, ok := value.(Ticket); ok {
		entry.Job = ticket.Job
	}
	h := &r.history
	if n := h.ringLimit(); len(h.entries) >= n {
		h.entries = append(h.entries[:0], h.entries[len(h.entries)-n+1:]...)
	}
	h.entries = append(h.entries, entry)
	r.persist(entry)
}

// History returns the last n entries, oldest first (all of them for n <= 0).
func (r *Runtime) History(n int) []HistoryEntry {
	entries := r.history.entries
	if n > 0 && n < len(entries) {
		entries = entries[len(entries)-n:]
	}
	return append([]HistoryEntry{}, entries...)
}

// repeatableOperations are the framework's changes: the editor's text
// edits. Motions, focus, pages, modals, lists and the history itself are not.
var repeatableOperations = []string{"input.insert", "input.delete", "input.kill", "input.paste", "input.operator", "input.replace"}

// markRepeatable flags registered framework operations as repeatable.
func markRepeatable(r *Runtime, keys ...string) {
	for _, key := range keys {
		if op, ok := r.Operations[key]; ok {
			op.Repeatable = true
			r.Operations[key] = op
		}
	}
}

// repeatRequest is the request dot-repeat runs: the last successful
// repeatable request with its arguments (and Confirmed) kept. Its target is
// dropped so it re-resolves to the current focus, unless the request came
// from the CLI and names an instance that still exists. steps > 0 replaces
// the recorded count on an operation that takes steps.
func (r *Runtime) repeatRequest(steps int) (Request, error) {
	entries := r.history.entries
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		op, ok := r.Operations[e.Request.Key()]
		if !ok || !op.Repeatable || !e.Result.OK || e.Restored {
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
			q.Target = ""
		}
		return q, nil
	}
	return Request{}, errors.New("nothing to repeat")
}

func registerHistory(r *Runtime) {
	markRepeatable(r, repeatableOperations...)
	r.Register(Operation{Verb: "repeat", Noun: "history", Description: "Dot-repeat: run the last repeatable change again at the current focus, its arguments kept; steps replaces its count", Parameters: map[string]Parameter{"steps": {Type: "integer", Description: "Count replacing the recorded one; 0 keeps it"}}, Result: "JSON value", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		q, err := r.repeatRequest(IntArg(a, "steps", 0))
		if err != nil {
			return nil, err
		}
		return r.ExecuteAt(c, q)
	}})
	r.Define("app").Mode("normal").Bind(".", Do("history.repeat").FromPayload("steps", SourceLine).Hide())
	r.Register(Operation{Verb: "list", Noun: "history", Description: "List the last executed requests, oldest first, with their context, source and result", Parameters: map[string]Parameter{"last": {Type: "integer", Description: "How many entries (default 50, 0 for all)"}}, Result: "history[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		return r.History(IntArg(a, "last", 50)), nil
	}})
	r.Register(Operation{Verb: "path", Noun: "history", Description: "Print the history file: $XDG_STATE_HOME/<app>/history.jsonl, else ~/.local/state/<app>/history.jsonl", Result: "path", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		path := r.Config().HistoryPath()
		if path == "" {
			return nil, errors.New("this application does not keep its history")
		}
		return path, nil
	}})
}
