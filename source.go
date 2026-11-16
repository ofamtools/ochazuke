package ochazuke

import (
	"context"
	"strings"
)

// RequestSource says who asked for a request: the history source it runs under
// (HistoryEntry.Context.Source). A key, the palette and the help's run are
// a person at the UI; the CLI, MCP (an agent), the API and a hook are
// programs; RequestFromBackground is the runtime's own work that nobody is
// waiting on: a page's refresh, a list reading its store, the Nouns page's
// counts, a detail's related sections. An application reads it to put the
// calls a person or an agent waits for ahead of background reads
// (Ctx.Source, RequestSourceOf inside a job's work).
type RequestSource string

// The sources. RequestFromKey, RequestFromMacro and RequestFromHook are prefixes: the
// source carries the keys ("key:g g"), the register ("macro:q") or the
// event ("hook:app.quitting") after them; RequestSource.Class answers the prefix.
const (
	RequestFromAPI        RequestSource = "api"
	RequestFromCLI        RequestSource = "cli"
	RequestFromMCP        RequestSource = "mcp"
	RequestFromPalette    RequestSource = "palette"
	RequestFromHelp       RequestSource = helpSource
	RequestFromKey        RequestSource = "key:"
	RequestFromMacro      RequestSource = "macro:"
	RequestFromHook       RequestSource = "hook:"
	RequestFromBackground RequestSource = "background"
)

// Class is the source without what follows its prefix: RequestFromKey for
// "key:j", RequestFromMacro for "macro:q", RequestFromHook for "hook:EVENT"; any
// other source is its own class, and "" is RequestFromAPI.
func (s RequestSource) Class() RequestSource {
	if s == "" {
		return RequestFromAPI
	}
	for _, prefix := range []RequestSource{RequestFromKey, RequestFromMacro, RequestFromHook} {
		if strings.HasPrefix(string(s), string(prefix)) {
			return prefix
		}
	}
	return s
}

// Person reports whether a person at the UI asked: a key, the palette,
// the help's run or a macro replaying keys.
func (s RequestSource) Person() bool { return personSource(string(s)) }

// Background reports whether nobody waits on the request: the runtime's
// own refreshes and reads (RequestFromBackground).
func (s RequestSource) Background() bool { return s == RequestFromBackground }

// Source is who asked for the request the handler serves, or for the job
// or refresh a store call is made in: a store call made for a page's own
// refresh or a background job answers RequestFromBackground. With none set it
// is the source of the request running now (the runtime's), else
// RequestFromAPI.
func (c Ctx) Source() RequestSource {
	if c.source != "" {
		return RequestSource(c.source)
	}
	if c.Runtime != nil && c.Runtime.history.source != "" {
		return RequestSource(c.Runtime.history.source)
	}
	return RequestFromAPI
}

// backgroundCtx is the Ctx of the runtime's own reads (a page's refresh, a
// list's store read, a count): RequestFromBackground, so an application can tell
// them from what a person or agent asked for.
func (r *Runtime) backgroundCtx() Ctx {
	return Ctx{actor: r.Actor(), source: string(RequestFromBackground), Runtime: r, Context: r.Context()}
}

// requestSourceKey keys a job context's RequestSource.
type requestSourceKey struct{}

// RequestSourceOf answers the RequestSource a job's context carries (the work of a
// Pending result, a remote store's call inside it): the source of the Ctx
// the job was launched with. A context of no job answers RequestFromAPI.
func RequestSourceOf(ctx context.Context) RequestSource {
	if ctx != nil {
		if s, ok := ctx.Value(requestSourceKey{}).(RequestSource); ok && s != "" {
			return s
		}
	}
	return RequestFromAPI
}
