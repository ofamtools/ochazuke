package ochazuke

import (
	"fmt"
	"strings"
)

// Placeholder is what a list-like component draws in place of rows, as
// data: Empty while it has no rows, Loading while the job that fills it
// runs, so "loading" never reads as "empty". Every text follows one format
// (UX-RULES R5.19): lower case, no parentheses, the rows' noun named ("no
// panes", "loading panes…", "no panes match 'x'").
type Placeholder struct {
	// Plural names the rows ("panes"); "" is "rows". It fills the default
	// texts and the no-match line.
	Plural string `json:"plural,omitempty"`
	// Empty is the line for no rows at all; "" is EmptyText(Plural).
	Empty string `json:"empty,omitempty"`
	// Loading is the line while LoadingJob runs and there are no rows yet;
	// "" is LoadingText(Plural).
	Loading string `json:"loading,omitempty"`
	// LoadingJob names the job that fills the component: while a job runs
	// in that slot the component draws Loading instead of Empty. The zero
	// value names none.
	LoadingJob JobSlot `json:"loadingJob,omitzero"`
}

// JobSlot names a replacing job's slot: Scope and Slot as
// Result.ReplacingAt(scope, slot) gives them (Replacing(slot) scopes the
// slot to the job's owner).
type JobSlot struct {
	Scope string `json:"scope,omitempty"`
	Slot  string `json:"slot,omitempty"`
}

// Running reports whether a job runs in the slot; false for the zero slot.
func (s JobSlot) Running(r *Runtime) bool {
	return r != nil && s.Slot != "" && r.HasRunningJob(s.Scope, s.Slot)
}

// EmptyText is the one format's empty line: "no PLURAL" ("no rows" for "").
func EmptyText(plural string) string { return "no " + pluralOr(plural) }

// LoadingText is the one format's loading line: "loading PLURAL…"
// ("loading…" for "").
func LoadingText(plural string) string {
	if plural == "" {
		return "loading…"
	}
	return "loading " + plural + "…"
}

// NoMatchText is the one format's line for a filter that lets no row
// through: "no PLURAL match 'QUERY'"; with no text query, "no PLURAL match
// the filter".
func NoMatchText(plural, query string) string {
	if strings.TrimSpace(query) == "" {
		return "no " + pluralOr(plural) + " match the filter"
	}
	return fmt.Sprintf("no %s match '%s'", pluralOr(plural), query)
}

func pluralOr(plural string) string {
	if plural == "" {
		return "rows"
	}
	return plural
}

// Text is the line for a component with no rows: Loading while its job
// runs, else Empty, in the one format.
func (p Placeholder) Text(r *Runtime) string {
	if p.LoadingJob.Running(r) {
		if p.Loading != "" {
			return p.Loading
		}
		return LoadingText(p.Plural)
	}
	if p.Empty != "" {
		return p.Empty
	}
	return EmptyText(p.Plural)
}
