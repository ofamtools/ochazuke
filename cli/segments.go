package cli

import (
	"time"

	"ofam.dev/ochazuke"
)

// CatalogSegment is a status segment an extension answers from one of
// its verbs: the row's data less the sources only its own process may run
// (a Go provider, a command). Verb is "NOUN.VERB" in the extension's
// names; Every and Timeout are durations ("2s").
//
//	{id: "usage", label: "ai", verb: "usage.brief", every: "30s", tone: "state.done"}
//
// A command never travels in a catalog: as with actions, only the
// application's Go code and the user's own segments.json run commands.
type CatalogSegment struct {
	ID          string             `json:"id"`
	Label       string             `json:"label,omitempty"`
	Format      string             `json:"format,omitempty"`
	Verb        string             `json:"verb"`
	Args        ochazuke.Arguments `json:"args,omitempty"`
	Every       string             `json:"every,omitempty"`
	Timeout     string             `json:"timeout,omitempty"`
	Tone        string             `json:"tone,omitempty"`
	Order       int                `json:"order,omitempty"`
	Enabled     *bool              `json:"enabled,omitempty"`
	Description string             `json:"description,omitempty"`
}

func catalogSegment(r *ochazuke.Runtime, s ochazuke.Segment) CatalogSegment {
	out := CatalogSegment{ID: s.ID, Label: s.Label, Format: s.Format, Verb: r.CanonicalKey(s.Verb), Args: s.Args, Tone: s.Tone, Order: s.Order, Enabled: s.Enabled, Description: s.Description}
	if s.Every > 0 {
		out.Every = s.Every.String()
	}
	if s.Timeout > 0 {
		out.Timeout = s.Timeout.String()
	}
	return out
}

// CatalogSegments are doc's segments as rows a host adds with
// Runtime.AddSegment: each id prefixed (prefix + "-" + id, so two tools'
// "usage" stay apart; "" keeps it), each verb's noun mapped to the host's
// (noun, as PageConfig.Noun maps it; nil keeps it). A row whose duration
// does not parse keeps the default.
func CatalogSegments(doc CatalogDoc, prefix string, noun func(string) string) []ochazuke.Segment {
	var out []ochazuke.Segment
	for _, c := range doc.Segments {
		n, v := ochazuke.SplitKey(c.Verb)
		if noun != nil {
			n = noun(n)
		}
		id := c.ID
		if prefix != "" {
			id = prefix + "-" + id
		}
		s := ochazuke.Segment{ID: id, Label: c.Label, Format: c.Format, Verb: n + "." + v, Args: c.Args, Tone: c.Tone, Order: c.Order, Enabled: c.Enabled, Description: c.Description}
		s.Every, _ = time.ParseDuration(c.Every)
		s.Timeout, _ = time.ParseDuration(c.Timeout)
		out = append(out, s)
	}
	return out
}
