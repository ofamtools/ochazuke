package ochazuke

import (
	"encoding/json"
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

// record appends one entry, dropping the oldest past HistoryLimit.
func (r *Runtime) record(at time.Time, q Request, ctx HistoryContext, value any, err error) {
	if len(q.Args) > 0 {
		args := make(Arguments, len(q.Args))
		for k, v := range q.Args {
			args[k] = v
		}
		q.Args = args
	}
	r.history.seq++
	entry := HistoryEntry{Seq: r.history.seq, At: at, Request: q, Context: ctx, Result: HistoryResult{OK: err == nil}}
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
	if len(h.entries) >= HistoryLimit {
		h.entries = append(h.entries[:0], h.entries[len(h.entries)-HistoryLimit+1:]...)
	}
	h.entries = append(h.entries, entry)
}

// History returns the last n entries, oldest first (all of them for n <= 0).
func (r *Runtime) History(n int) []HistoryEntry {
	entries := r.history.entries
	if n > 0 && n < len(entries) {
		entries = entries[len(entries)-n:]
	}
	return append([]HistoryEntry{}, entries...)
}

func registerHistory(r *Runtime) {
	r.Register(Operation{Verb: "list", Noun: "history", Description: "List the last executed requests, oldest first, with their context, source and result", Parameters: map[string]Parameter{"last": {Type: "integer", Description: "How many entries (default 50, 0 for all)"}}, Result: "history[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		return r.History(IntArg(a, "last", 50)), nil
	}})
}
